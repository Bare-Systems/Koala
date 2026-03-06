package ingest

import (
	"context"
	"encoding/base64"
	"sync"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/service"
)

type Submitter interface {
	Submit(task service.FrameTask) bool
}

type Snapshotter interface {
	Capture(ctx context.Context, rtspURL string) ([]byte, error)
}

type CameraStats struct {
	CameraID      string        `json:"camera_id"`
	Attempts      int64         `json:"attempts"`
	Successes     int64         `json:"successes"`
	Failures      int64         `json:"failures"`
	Submitted     int64         `json:"submitted"`
	Dropped       int64         `json:"dropped"`
	LastError     string        `json:"last_error,omitempty"`
	LastCaptureAt string        `json:"last_capture_at,omitempty"`
	LastStatus    camera.Status `json:"last_status"`
}

type Status struct {
	StartedAt        string                 `json:"started_at"`
	SampleEveryMs    int64                  `json:"sample_every_ms"`
	CaptureTimeoutMs int64                  `json:"capture_timeout_ms"`
	Cameras          map[string]CameraStats `json:"cameras"`
}

type Manager struct {
	registry       *camera.Registry
	submitter      Submitter
	snapshotter    Snapshotter
	sampleEvery    time.Duration
	captureTimeout time.Duration

	statsMu   sync.Mutex
	stats     map[string]CameraStats
	startedAt time.Time
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
		registry:       registry,
		submitter:      submitter,
		snapshotter:    snapshotter,
		sampleEvery:    sampleEvery,
		captureTimeout: captureTimeout,
		stats:          stats,
		startedAt:      time.Now().UTC(),
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
		Cameras:          out,
	}
}

func (m *Manager) runCamera(ctx context.Context, cam camera.Camera) {
	ticker := time.NewTicker(m.sampleEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.increment(cam.ID, func(s *CameraStats) { s.Attempts++ })
			captureCtx, cancel := context.WithTimeout(ctx, m.captureTimeout)
			frame, err := m.snapshotter.Capture(captureCtx, cam.RTSPURL)
			cancel()
			if err != nil {
				m.registry.SetStatus(cam.ID, camera.StatusUnavailable)
				m.increment(cam.ID, func(s *CameraStats) {
					s.Failures++
					s.LastError = err.Error()
					s.LastStatus = camera.StatusUnavailable
				})
				continue
			}

			m.registry.SetStatus(cam.ID, camera.StatusAvailable)
			accepted := m.submitter.Submit(service.FrameTask{
				CameraID: cam.ID,
				ZoneID:   cam.ZoneID,
				FrameB64: base64.StdEncoding.EncodeToString(frame),
				Captured: time.Now().UTC(),
			})
			m.increment(cam.ID, func(s *CameraStats) {
				s.Successes++
				s.LastCaptureAt = time.Now().UTC().Format(time.RFC3339)
				s.LastStatus = camera.StatusAvailable
				if accepted {
					s.Submitted++
				} else {
					s.Dropped++
				}
			})
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
