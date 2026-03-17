package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/baresystems/koala/internal/camera"
	"github.com/baresystems/koala/internal/inference"
	"github.com/baresystems/koala/internal/state"
	"github.com/baresystems/koala/internal/zone"
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
	poly := zone.Polygon{{X: 0.5, Y: 0.5}, {X: 1, Y: 0.5}, {X: 1, Y: 1}, {X: 0.5, Y: 1}}
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
	poly := zone.Polygon{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}}
	f := NewZoneFilter(map[string]ZonePolygonConfig{
		"z1": {Polygon: poly, MinOverlap: 0.5},
	})
	dets := []inference.Detection{{ZoneID: "z1", Label: "package", BBox: zone.BBox{X: 0.1, Y: 0.1, W: 0.5, H: 0.5}}}
	got := f.Filter(dets)
	if len(got) != 1 {
		t.Fatalf("in-zone detection should pass")
	}
}

func TestZoneFilter_ZoneConfidenceThreshold_FiltersLow(t *testing.T) {
	f := NewZoneFilter(map[string]ZonePolygonConfig{
		"z1": {ConfidenceThreshold: 0.8},
	})
	dets := []inference.Detection{
		{ZoneID: "z1", Label: "package", Confidence: 0.79},
	}
	if got := f.Filter(dets); len(got) != 0 {
		t.Fatalf("expected low-confidence detection to be filtered, got %d", len(got))
	}
}

func TestZoneFilter_ZoneConfidenceThreshold_PassesHigh(t *testing.T) {
	f := NewZoneFilter(map[string]ZonePolygonConfig{
		"z1": {ConfidenceThreshold: 0.8},
	})
	dets := []inference.Detection{
		{ZoneID: "z1", Label: "package", Confidence: 0.85},
	}
	if got := f.Filter(dets); len(got) != 1 {
		t.Fatalf("expected high-confidence detection to pass, got %d", len(got))
	}
}

func TestZoneFilter_CameraThreshold_OverridesZone(t *testing.T) {
	// Zone threshold is 0.5 (lenient); camera threshold is 0.9 (strict).
	// Detection at 0.7 should be rejected by the camera threshold.
	f := NewZoneFilter(map[string]ZonePolygonConfig{
		"z1": {ConfidenceThreshold: 0.5},
	}).WithCameraThresholds(map[string]float64{"cam1": 0.9})
	dets := []inference.Detection{
		{CameraID: "cam1", ZoneID: "z1", Label: "package", Confidence: 0.7},
	}
	if got := f.Filter(dets); len(got) != 0 {
		t.Fatalf("camera threshold should override zone threshold and filter 0.7 confidence")
	}
}

func TestZoneFilter_GlobalThreshold_Fallback(t *testing.T) {
	// No zone config for this detection; global threshold should apply.
	f := NewZoneFilter(nil).WithGlobalThreshold(0.6)
	pass := inference.Detection{ZoneID: "z1", Label: "package", Confidence: 0.65}
	fail := inference.Detection{ZoneID: "z1", Label: "person", Confidence: 0.55}
	got := f.Filter([]inference.Detection{pass, fail})
	if len(got) != 1 || got[0].Label != "package" {
		t.Fatalf("expected only high-confidence detection to pass global threshold, got %v", got)
	}
}

func TestZoneFilter_NoThreshold_PassesAll(t *testing.T) {
	// When no thresholds are configured, confidence should not be checked.
	f := NewZoneFilter(nil)
	dets := []inference.Detection{
		{ZoneID: "z1", Label: "package", Confidence: 0.01},
	}
	if got := f.Filter(dets); len(got) != 1 {
		t.Fatalf("expected detection to pass when no threshold configured")
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
// mu guards captured because AnalyzeFrame is called from a worker goroutine
// while the test goroutine reads captured after time.Sleep.
type capturingClient struct {
	mu       sync.Mutex
	captured []string
}

func (c *capturingClient) AnalyzeFrame(_ context.Context, req inference.FrameRequest) (inference.FrameResponse, error) {
	c.mu.Lock()
	c.captured = append(c.captured, req.FrameB64)
	c.mu.Unlock()
	return inference.FrameResponse{}, nil
}

// snapshot returns a copy of captured under the lock.
func (c *capturingClient) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.captured))
	copy(out, c.captured)
	return out
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

	captured := client.snapshot()
	if len(captured) == 0 {
		t.Fatal("expected at least one inference call")
	}
	if captured[0] != "" {
		t.Fatalf("expected frame_b64 to be stripped in metadata-only mode, got %q", captured[0])
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

	captured := client.snapshot()
	if len(captured) == 0 {
		t.Fatal("expected at least one inference call")
	}
	if captured[0] != "base64data" {
		t.Fatalf("expected frame_b64 to be forwarded when enabled, got %q", captured[0])
	}
}
