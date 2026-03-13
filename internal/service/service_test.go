package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/inference"
	"github.com/barelabs/koala/internal/state"
	"github.com/barelabs/koala/internal/zone"
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

func TestZoneFilterPassesWhenNoPoly(t *testing.T) {
	f := NewZoneFilter(nil)
	dets := []inference.Detection{{ZoneID: "z1", Label: "package", BBox: zone.BBox{X: 0, Y: 0, W: 1, H: 1}}}
	got := f.Filter(dets)
	if len(got) != 1 {
		t.Fatalf("no-poly filter should pass all detections")
	}
}

func TestZoneFilterRejectsOutOfZone(t *testing.T) {
	// Zone covers only bottom-right quarter.
	poly := zone.Polygon{{0.5, 0.5}, {1, 0.5}, {1, 1}, {0.5, 1}}
	f := NewZoneFilter(map[string]ZonePolygonConfig{
		"z1": {Polygon: poly, MinOverlap: 0.5},
	})
	// BBox is entirely in top-left → no overlap.
	dets := []inference.Detection{{ZoneID: "z1", Label: "package", BBox: zone.BBox{X: 0, Y: 0, W: 0.3, H: 0.3}}}
	got := f.Filter(dets)
	if len(got) != 0 {
		t.Fatalf("out-of-zone detection should be filtered")
	}
}

func TestZoneFilterAcceptsInZone(t *testing.T) {
	// Zone covers full frame.
	poly := zone.Polygon{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	f := NewZoneFilter(map[string]ZonePolygonConfig{
		"z1": {Polygon: poly, MinOverlap: 0.5},
	})
	dets := []inference.Detection{{ZoneID: "z1", Label: "package", BBox: zone.BBox{X: 0.1, Y: 0.1, W: 0.5, H: 0.5}}}
	got := f.Filter(dets)
	if len(got) != 1 {
		t.Fatalf("in-zone detection should pass")
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

// capturingClient records the FrameB64 values it receives.
type capturingClient struct {
	captured []string
}

func (c *capturingClient) AnalyzeFrame(_ context.Context, req inference.FrameRequest) (inference.FrameResponse, error) {
	c.captured = append(c.captured, req.FrameB64)
	return inference.FrameResponse{}, nil
}

func (c *capturingClient) WorkerHealth(_ context.Context) (inference.HealthResponse, error) {
	return inference.HealthResponse{Status: "ok"}, nil
}

func TestPrivacy_FrameStrippedByDefault(t *testing.T) {
	client := &capturingClient{}
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}})
	svc := New(registry, state.NewAggregator(time.Minute), client, 4)
	// default: FrameBufferEnabled = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	svc.Submit(FrameTask{CameraID: "cam1", ZoneID: "front_door", FrameB64: "base64data", Captured: time.Now().UTC()})
	time.Sleep(30 * time.Millisecond)

	if len(client.captured) == 0 {
		t.Fatal("expected at least one inference call")
	}
	if client.captured[0] != "" {
		t.Fatalf("expected frame_b64 to be stripped in metadata-only mode, got %q", client.captured[0])
	}
}

func TestPrivacy_FrameForwardedWhenEnabled(t *testing.T) {
	client := &capturingClient{}
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}})
	svc := New(registry, state.NewAggregator(time.Minute), client, 4)
	svc.FrameBufferEnabled = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	svc.Submit(FrameTask{CameraID: "cam1", ZoneID: "front_door", FrameB64: "base64data", Captured: time.Now().UTC()})
	time.Sleep(30 * time.Millisecond)

	if len(client.captured) == 0 {
		t.Fatal("expected at least one inference call")
	}
	if client.captured[0] != "base64data" {
		t.Fatalf("expected frame_b64 to be forwarded when enabled, got %q", client.captured[0])
	}
}
