package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/inference"
	"github.com/barelabs/koala/internal/state"
)

type staticInferenceClient struct {
	err error
}

func (s staticInferenceClient) AnalyzeFrame(_ context.Context, req inference.FrameRequest) (inference.FrameResponse, error) {
	if s.err != nil {
		return inference.FrameResponse{}, s.err
	}
	return inference.FrameResponse{Detections: []inference.Detection{{
		CameraID:   req.CameraID,
		ZoneID:     req.ZoneID,
		Label:      "person",
		Confidence: 0.9,
		Timestamp:  req.Captured,
	}}}, nil
}

func (s staticInferenceClient) WorkerHealth(_ context.Context) (inference.HealthResponse, error) {
	if s.err != nil {
		return inference.HealthResponse{}, s.err
	}
	return inference.HealthResponse{Status: "ok"}, nil
}

func TestServiceQueueBackpressure(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}})
	svc := New(registry, state.NewAggregator(time.Minute), staticInferenceClient{}, 1)

	if !svc.Submit(FrameTask{CameraID: "cam1", ZoneID: "front_door", Captured: time.Now().UTC()}) {
		t.Fatalf("first submit should succeed")
	}
	if svc.Submit(FrameTask{CameraID: "cam1", ZoneID: "front_door", Captured: time.Now().UTC()}) {
		t.Fatalf("second submit should be dropped when queue is full")
	}
}

func TestServiceDegradedOnInferenceError(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}})
	svc := New(registry, state.NewAggregator(time.Minute), staticInferenceClient{err: errors.New("down")}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	svc.Submit(FrameTask{CameraID: "cam1", ZoneID: "front_door", Captured: time.Now().UTC()})
	time.Sleep(20 * time.Millisecond)
	if !svc.IsDegraded() {
		t.Fatalf("expected degraded state")
	}
}
