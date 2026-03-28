package ingest

import (
	"context"
	"encoding/base64"
	"sync"
	"time"

	"github.com/Bare-Systems/Koala/internal/camera"
	"github.com/Bare-Systems/Koala/internal/service"
)

type Submitter interface {
	Submit(task service.FrameTask) bool
}

// Snapshotter captures a single JPEG frame from an RTSP stream.
type Snapshotter interface {
	Capture(ctx context.Context, rtspURL string) ([]byte, error)
}

// PerCameraSnapshotter is an optional extension of Snapshotter that supports
// per-URL FPS overrides. If the Snapshotter implements this interface, the
// Manager will call CaptureAtFPS instead of Capture when a camera has MaxFPS set.
type PerCameraSnapshotter interface {
	Snapshotter
	CaptureAtFPS(ctx context.Context, rtspURL string, fps int) ([]byte, error)
}

type CameraStats struct {
	CameraID            string        `json:"camera_id"`
	Attempts            int64         `json:"attempts"`
	Successes           int64         `json:"successes"`
	Failures            int64         `json:"failures"`
	Submitted           int64         `json:"submitted"`
	Dropped             int64         `json:"dropped"`
	ConsecutiveFailures int64         `json:"consecutive_failures"`
	LastError           string        `json:"last_error,omitempty"`
	LastCaptureAt       string        `json:"last_capture_at,omitempty"`
	LastStatus          camera.Status `json:"last_status"`
}

type Incident struct {
	CameraID   string `json:"camera_id"`
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	OccurredAt string `json:"occurred_at"`
}

type Status struct {
	StartedAt        string                 `json:"started_at"`
	SampleEveryMs    int64                  `json:"sample_every_ms"`
	CaptureTimeoutMs int64                  `json:"capture_timeout_ms"`
	StallTimeoutMs   int64                  `json:"stall_timeout_ms"`
	Cameras          map[string]CameraStats `json:"cameras"`
	Incidents        []Incident             `json:"incidents"`
}

type Manager struct {
	registry       *camera.Registry
	submitter      Submitter
	snapshotter    Snapshotter
	sampleEvery    time.Duration
	captureTimeout time.Duration

	statsMu   sync.Mutex
	stats     map[string]CameraStats
	incidents []Incident
	stalled   map[string]bool
	startedAt time.Time

	latestFramesMu sync.RWMutex
	latestFrames   map[string][]byte

	stallTimeout    time.Duration
	maxIncidentSize int
}

func NewManager(registry *camera.Registry, submitter Submitter, snapshotter Snapshotter, sampleEvery time.Duration, captureTimeout time.Duration) *Manager {
	if sampleEvery <= 0 {
		sampleEvery = time.Second
	}
	if captureTimeout <= 0 {
		captureTimeout = 5 * time.Second
	}
	stats := map[string]CameraStats{}
	for _, c := range registry.List() {
		stats[c.ID] = CameraStats{CameraID: c.ID, LastStatus: c.Status}
	}
	return &Manager{
		registry:        registry,
		submitter:       submitter,
		snapshotter:     snapshotter,
		sampleEvery:     sampleEvery,
		captureTimeout:  captureTimeout,
		stats:           stats,
		stalled:         map[string]bool{},
		latestFrames:    map[string][]byte{},
		startedAt:       time.Now().UTC(),
		stallTimeout:    maxDuration(10*time.Second, sampleEvery*3),
		maxIncidentSize: 200,
	}
}

func (m *Manager) Start(ctx context.Context) {
	cameras := m.registry.List()
	var wg sync.WaitGroup
	for _, cam := range cameras {
		cameraInfo := cam
		if cameraInfo.RTSPURL == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.runCamera(ctx, cameraInfo)
		}()
	}
	go m.watchdog(ctx)
	go func() {
		<-ctx.Done()
		wg.Wait()
		if closer, ok := m.snapshotter.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()
}

func (m *Manager) Status() Status {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	out := make(map[string]CameraStats, len(m.stats))
	for k, v := range m.stats {
		out[k] = v
	}
	return Status{
		StartedAt:        m.startedAt.Format(time.RFC3339),
		SampleEveryMs:    m.sampleEvery.Milliseconds(),
		CaptureTimeoutMs: m.captureTimeout.Milliseconds(),
		StallTimeoutMs:   m.stallTimeout.Milliseconds(),
		Cameras:          out,
		Incidents:        append([]Incident{}, m.incidents...),
	}
}

func (m *Manager) runCamera(ctx context.Context, cam camera.Camera) {
	// Honour per-camera FPS if set; fall back to manager-wide default.
	sampleEvery := m.sampleEvery
	camFPS := cam.MaxFPS
	if camFPS > 0 {
		sampleEvery = time.Second / time.Duration(camFPS)
	}
	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.increment(cam.ID, func(s *CameraStats) { s.Attempts++ })
			captureCtx, cancel := context.WithTimeout(ctx, m.captureTimeout)
			var frame []byte
			var err error
			if pcs, ok := m.snapshotter.(PerCameraSnapshotter); ok && camFPS > 0 {
				frame, err = pcs.CaptureAtFPS(captureCtx, cam.RTSPURL, camFPS)
			} else {
				frame, err = m.snapshotter.Capture(captureCtx, cam.RTSPURL)
			}
			cancel()
			if err != nil {
				m.markFailure(cam.ID, err)
				continue
			}

			m.latestFramesMu.Lock()
			m.latestFrames[cam.ID] = frame
			m.latestFramesMu.Unlock()

			m.registry.SetStatus(cam.ID, camera.StatusAvailable)
			now := time.Now().UTC()
			accepted := m.submitter.Submit(service.FrameTask{
				CameraID: cam.ID,
				ZoneID:   cam.ZoneID,
				FrameB64: base64.StdEncoding.EncodeToString(frame),
				Captured: now,
			})
			recovered := false
			m.increment(cam.ID, func(s *CameraStats) {
				if s.ConsecutiveFailures > 0 {
					recovered = true
				}
				s.Successes++
				s.ConsecutiveFailures = 0
				s.LastError = ""
				s.LastCaptureAt = now.Format(time.RFC3339)
				s.LastStatus = camera.StatusAvailable
				if accepted {
					s.Submitted++
				} else {
					s.Dropped++
				}
			})
			m.onRecovery(cam.ID, recovered)
		}
	}
}

func (m *Manager) increment(cameraID string, mutator func(*CameraStats)) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	stats, ok := m.stats[cameraID]
	if !ok {
		stats = CameraStats{CameraID: cameraID}
	}
	mutator(&stats)
	m.stats[cameraID] = stats
}

func (m *Manager) markFailure(cameraID string, err error) {
	m.registry.SetStatus(cameraID, camera.StatusUnavailable)
	m.statsMu.Lock()
	stats, ok := m.stats[cameraID]
	if !ok {
		stats = CameraStats{CameraID: cameraID}
	}
	stats.Failures++
	stats.ConsecutiveFailures++
	stats.LastStatus = camera.StatusUnavailable
	if err != nil {
		stats.LastError = err.Error()
	}
	m.stats[cameraID] = stats
	if stats.ConsecutiveFailures == 1 || stats.ConsecutiveFailures%5 == 0 {
		m.recordIncidentLocked(Incident{
			CameraID:   cameraID,
			Type:       "stream_failure",
			Severity:   "high",
			Message:    stats.LastError,
			OccurredAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	m.statsMu.Unlock()
}

func (m *Manager) onRecovery(cameraID string, recoveredFromFailure bool) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	_, ok := m.stats[cameraID]
	if !ok {
		return
	}
	wasStalled := m.stalled[cameraID]
	if recoveredFromFailure || wasStalled {
		m.recordIncidentLocked(Incident{
			CameraID:   cameraID,
			Type:       "stream_recovered",
			Severity:   "info",
			Message:    "camera stream recovered",
			OccurredAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	if wasStalled {
		delete(m.stalled, cameraID)
	}
}

func (m *Manager) watchdog(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkStalls()
		}
	}
}

func (m *Manager) checkStalls() {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	now := time.Now().UTC()
	for cameraID, stats := range m.stats {
		if stats.LastCaptureAt == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, stats.LastCaptureAt)
		if err != nil {
			continue
		}
		if now.Sub(parsed) <= m.stallTimeout {
			continue
		}
		if m.stalled[cameraID] {
			continue
		}
		m.stalled[cameraID] = true
		stats.LastStatus = camera.StatusDegraded
		stats.LastError = "stream stalled"
		m.stats[cameraID] = stats
		m.registry.SetStatus(cameraID, camera.StatusDegraded)
		m.recordIncidentLocked(Incident{
			CameraID:   cameraID,
			Type:       "stream_stalled",
			Severity:   "medium",
			Message:    "no frames received within watchdog threshold",
			OccurredAt: now.Format(time.RFC3339),
		})
	}
}

func (m *Manager) recordIncidentLocked(incident Incident) {
	m.incidents = append(m.incidents, incident)
	if len(m.incidents) > m.maxIncidentSize {
		m.incidents = m.incidents[len(m.incidents)-m.maxIncidentSize:]
	}
}

// LatestFrame returns the most recently captured JPEG frame for the given
// camera ID. Returns false if no frame has been captured yet.
func (m *Manager) LatestFrame(cameraID string) ([]byte, bool) {
	m.latestFramesMu.RLock()
	defer m.latestFramesMu.RUnlock()
	frame, ok := m.latestFrames[cameraID]
	return frame, ok
}

func maxDuration(a time.Duration, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
