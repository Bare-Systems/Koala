package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/inference"
	"github.com/barelabs/koala/internal/service"
	"github.com/barelabs/koala/internal/state"
)

type fakeInferenceClient struct {
	healthStatus string
	err          error
}

func (f fakeInferenceClient) AnalyzeFrame(_ context.Context, req inference.FrameRequest) (inference.FrameResponse, error) {
	if f.err != nil {
		return inference.FrameResponse{}, f.err
	}
	return inference.FrameResponse{
		ModelVersion: "test",
		Detections: []inference.Detection{{
			CameraID:   req.CameraID,
			ZoneID:     req.ZoneID,
			Label:      "package",
			Confidence: 0.93,
			Timestamp:  req.Captured,
		}},
	}, nil
}

func (f fakeInferenceClient) WorkerHealth(_ context.Context) (inference.HealthResponse, error) {
	if f.err != nil {
		return inference.HealthResponse{}, f.err
	}
	status := f.healthStatus
	if status == "" {
		status = "ok"
	}
	return inference.HealthResponse{Status: status}, nil
}

func TestServer_CheckPackageContract(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true, Status: camera.StatusAvailable}})
	agg := state.NewAggregator(time.Minute)
	agg.Ingest([]state.Detection{{
		CameraID:   "cam_front_1",
		ZoneID:     "front_door",
		Label:      "package",
		Confidence: 0.95,
		ObservedAt: time.Now().UTC(),
	}})

	svc := service.New(registry, agg, fakeInferenceClient{}, 2)
	h := NewServer("test-token", svc, nil, nil, nil, nil, nil).Routes()

	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.check_package_at_door", bytes.NewBufferString(`{"input":{}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", res.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", payload["status"])
	}
	if _, ok := payload["freshness_seconds"]; !ok {
		t.Fatalf("missing freshness_seconds")
	}
	if _, ok := payload["explanation"]; !ok {
		t.Fatalf("missing explanation")
	}
}

func TestServer_AuthRequired(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true}})
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	h := NewServer("test-token", svc, nil, nil, nil, nil, nil).Routes()

	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", res.Code)
	}
}

func TestServer_DegradedResponse(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true}})
	agg := state.NewAggregator(time.Minute)
	svc := service.New(registry, agg, fakeInferenceClient{healthStatus: "degraded"}, 2)
	svc.Submit(service.FrameTask{CameraID: "cam_front_1", ZoneID: "front_door", Captured: time.Now().UTC()})
	svc.WorkerHealthy(context.Background())

	h := NewServer("test-token", svc, nil, nil, nil, nil, nil).Routes()
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_zone_state", bytes.NewBufferString(`{"input":{"zone_id":"front_door"}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "degraded" {
		t.Fatalf("expected degraded status, got %v", payload["status"])
	}
}
