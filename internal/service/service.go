package service

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/inference"
	"github.com/barelabs/koala/internal/state"
)

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

	queue       chan FrameTask
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
		queue:      make(chan FrameTask, queueSize),
		start:      time.Now().UTC(),
	}
}

func (s *Service) Submit(task FrameTask) bool {
	select {
	case s.queue <- task:
		return true
	default:
		s.dropped.Add(1)
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
	resp, err := s.Inference.AnalyzeFrame(ctx, inference.FrameRequest{
		CameraID: task.CameraID,
		ZoneID:   task.ZoneID,
		FrameB64: task.FrameB64,
		Captured: task.Captured,
	})
	if err != nil {
		s.markDegraded(err)
		return
	}

	normalized := make([]state.Detection, 0, len(resp.Detections))
	for _, d := range resp.Detections {
		normalized = append(normalized, state.Detection{
			CameraID:   d.CameraID,
			ZoneID:     d.ZoneID,
			Label:      d.Label,
			Confidence: d.Confidence,
			ObservedAt: d.Timestamp,
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
