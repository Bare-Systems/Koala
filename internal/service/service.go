package service

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/inference"
	"github.com/barelabs/koala/internal/state"
	"github.com/barelabs/koala/internal/telemetry"
	"github.com/barelabs/koala/internal/zone"
)

const defaultMinBBoxOverlap = 0.3

// ZonePolygonConfig carries the polygon filter settings for one zone.
type ZonePolygonConfig struct {
	Polygon    zone.Polygon
	MinOverlap float64 // 0 → defaultMinBBoxOverlap
}

// ZoneFilter holds per-zone polygon configs and filters raw detections.
// Zones without a polygon config pass all detections through.
type ZoneFilter struct {
	zones map[string]ZonePolygonConfig
}

// NewZoneFilter builds a ZoneFilter from a map of zone ID → polygon config.
// Pass nil to get a no-op filter.
func NewZoneFilter(zones map[string]ZonePolygonConfig) *ZoneFilter {
	if zones == nil {
		zones = map[string]ZonePolygonConfig{}
	}
	return &ZoneFilter{zones: zones}
}

func (f *ZoneFilter) Filter(detections []inference.Detection) []inference.Detection {
	if f == nil || len(f.zones) == 0 {
		return detections
	}
	out := detections[:0:0] // zero-length slice backed by same array
	for _, d := range detections {
		cfg, ok := f.zones[d.ZoneID]
		if !ok || len(cfg.Polygon) == 0 {
			out = append(out, d)
			continue
		}
		minOverlap := cfg.MinOverlap
		if minOverlap == 0 {
			minOverlap = defaultMinBBoxOverlap
		}
		if zone.InZone(cfg.Polygon, d.BBox, minOverlap) {
			out = append(out, d)
		}
	}
	return out
}

type FrameTask struct {
	CameraID string
	ZoneID   string
	FrameB64 string
	Captured time.Time
}

type Health struct {
	Status     string `json:"status"`
	Ingest     string `json:"ingest"`
	Inference  string `json:"inference"`
	MCP        string `json:"mcp"`
	UptimeSecs int64  `json:"uptime_seconds"`
}

type Service struct {
	Registry   *camera.Registry
	Aggregator *state.Aggregator
	Inference  inference.Client
	Filter     *ZoneFilter
	Metrics    *telemetry.Collector

	queue       chan FrameTask
	queueCap    int
	start       time.Time
	dropped     atomic.Int64
	degraded    atomic.Bool
	lastFailure atomic.Int64
}

func New(registry *camera.Registry, aggregator *state.Aggregator, client inference.Client, queueSize int) *Service {
	if queueSize <= 0 {
		queueSize = 64
	}
	return &Service{
		Registry:   registry,
		Aggregator: aggregator,
		Inference:  client,
		Filter:     NewZoneFilter(nil),
		Metrics:    telemetry.New(0),
		queue:      make(chan FrameTask, queueSize),
		queueCap:   queueSize,
		start:      time.Now().UTC(),
	}
}

// QueueDepth returns the current number of frames waiting to be processed.
func (s *Service) QueueDepth() int { return len(s.queue) }

// QueueCapacity returns the maximum capacity of the ingest queue.
func (s *Service) QueueCapacity() int { return s.queueCap }

func (s *Service) Submit(task FrameTask) bool {
	select {
	case s.queue <- task:
		return true
	default:
		s.dropped.Add(1)
		s.Metrics.FrameDropped.Add(1)
		return false
	}
}

func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case task := <-s.queue:
				s.processTask(ctx, task)
			}
		}
	}()
}

func (s *Service) processTask(ctx context.Context, task FrameTask) {
	start := time.Now()
	resp, err := s.Inference.AnalyzeFrame(ctx, inference.FrameRequest{
		CameraID: task.CameraID,
		ZoneID:   task.ZoneID,
		FrameB64: task.FrameB64,
		Captured: task.Captured,
	})
	s.Metrics.RecordInferenceLatency(time.Since(start))
	s.Metrics.FrameTotal.Add(1)
	if err != nil {
		s.markDegraded(err)
		return
	}

	filtered := s.Filter.Filter(resp.Detections)
	normalized := make([]state.Detection, 0, len(filtered))
	for _, d := range filtered {
		normalized = append(normalized, state.Detection{
			CameraID:   d.CameraID,
			ZoneID:     d.ZoneID,
			Label:      d.Label,
			Confidence: d.Confidence,
			ObservedAt: d.Timestamp,
			BBox:       d.BBox,
		})
	}
	s.Aggregator.Ingest(normalized)
	s.degraded.Store(false)
}

func (s *Service) markDegraded(err error) {
	if err == nil {
		return
	}
	s.degraded.Store(true)
	s.lastFailure.Store(time.Now().UTC().Unix())
}

func (s *Service) WorkerHealthy(ctx context.Context) bool {
	resp, err := s.Inference.WorkerHealth(ctx)
	if err != nil {
		s.markDegraded(err)
		return false
	}
	healthy := resp.Status == "ok"
	s.degraded.Store(!healthy)
	return healthy
}

func (s *Service) IsDegraded() bool {
	return s.degraded.Load()
}

func (s *Service) LastFailureTime() time.Time {
	t := s.lastFailure.Load()
	if t == 0 {
		return time.Time{}
	}
	return time.Unix(t, 0).UTC()
}

func (s *Service) Health() Health {
	inferenceStatus := "ok"
	status := "ok"
	if s.IsDegraded() {
		inferenceStatus = "degraded"
		status = "degraded"
	}
	ingest := "ok"
	if s.dropped.Load() > 0 {
		ingest = "backpressure"
		if status == "ok" {
			status = "degraded"
		}
	}
	return Health{
		Status:     status,
		Ingest:     ingest,
		Inference:  inferenceStatus,
		MCP:        "ok",
		UptimeSecs: int64(time.Since(s.start).Seconds()),
	}
}

func (s *Service) ZoneState(zoneID string) state.ZoneState {
	return s.Aggregator.Zone(zoneID)
}

func (s *Service) DoorPackageState(cameraID string) (bool, float64, time.Time, error) {
	if cameraID == "" {
		cameraID = s.Registry.FrontDoorCameraID()
	}
	cameraInfo, ok := s.Registry.Get(cameraID)
	if !ok {
		return false, 0, time.Time{}, errors.New("camera not found")
	}
	z := s.Aggregator.Zone(cameraInfo.ZoneID)
	for _, entity := range z.Entities {
		if entity.Label == "package" {
			return entity.Present, entity.Confidence, entity.ObservedAt, nil
		}
	}
	return false, 0, time.Time{}, nil
}
