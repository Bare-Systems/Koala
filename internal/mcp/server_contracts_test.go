package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Bare-Systems/Koala/internal/camera"
	"github.com/Bare-Systems/Koala/internal/service"
	"github.com/Bare-Systems/Koala/internal/state"
)

// assertToolResponse decodes the response body as a ToolResponse and returns it.
// It fails the test if the body cannot be decoded or if Status is empty.
func assertToolResponse(t *testing.T, res *httptest.ResponseRecorder) ToolResponse {
	t.Helper()
	var tr ToolResponse
	if err := json.Unmarshal(res.Body.Bytes(), &tr); err != nil {
		t.Fatalf("response body is not a valid ToolResponse: %v\nbody: %s", err, res.Body.String())
	}
	if tr.Status == "" {
		t.Fatalf("ToolResponse.Status must not be empty")
	}
	if tr.Explanation == "" {
		t.Fatalf("ToolResponse.Explanation must not be empty")
	}
	return tr
}

// assertErrorResponse verifies the response has status="error" and the expected error code.
func assertErrorResponse(t *testing.T, res *httptest.ResponseRecorder, wantHTTP int, wantCode string) ToolResponse {
	t.Helper()
	if res.Code != wantHTTP {
		t.Fatalf("expected HTTP %d, got %d; body: %s", wantHTTP, res.Code, res.Body.String())
	}
	tr := assertToolResponse(t, res)
	if tr.Status != "error" {
		t.Fatalf("expected status=error, got %q", tr.Status)
	}
	if tr.ErrorCode != wantCode {
		t.Fatalf("expected error_code=%q, got %q", wantCode, tr.ErrorCode)
	}
	return tr
}

func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	registry := camera.NewRegistry([]camera.Camera{
		{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true, Status: camera.StatusAvailable},
	})
	agg := state.NewAggregator(time.Minute)
	svc := service.New(registry, agg, fakeInferenceClient{}, 2)
	s := NewServer("test-token", svc, nil, nil, nil, nil, nil)
	return s, s.Routes()
}

// ─── Error code contract tests ────────────────────────────────────────────────

func TestContract_Unauthorized_JsonBody(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	tr := assertErrorResponse(t, res, http.StatusUnauthorized, ErrCodeUnauthorized)
	if tr.NextAction == "" {
		t.Fatalf("expected NextAction hint on unauthorized response")
	}
}

func TestContract_MethodNotAllowed_JsonBody(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/mcp/tools/koala.get_system_health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	assertErrorResponse(t, res, http.StatusMethodNotAllowed, ErrCodeInvalidInput)
}

func TestContract_GetZoneState_MissingZoneID(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_zone_state",
		bytes.NewBufferString(`{"input":{}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	tr := assertErrorResponse(t, res, http.StatusBadRequest, ErrCodeInvalidInput)
	if tr.NextAction == "" {
		t.Fatalf("expected NextAction hint for missing zone_id")
	}
}

func TestContract_CheckPackageAtDoor_InvalidCamera(t *testing.T) {
	_, h := newTestServer(t)
	body := `{"input":{"camera_id":"does_not_exist"}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.check_package_at_door",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	assertErrorResponse(t, res, http.StatusBadRequest, ErrCodeInvalidInput)
}

func TestContract_IngestFrame_MissingCameraID(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/ingest/frame",
		bytes.NewBufferString(`{"input":{"zone_id":"front_door"}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	assertErrorResponse(t, res, http.StatusBadRequest, ErrCodeInvalidInput)
}

func TestContract_IngestFrame_MissingZoneID(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/ingest/frame",
		bytes.NewBufferString(`{"input":{"camera_id":"cam_front_1"}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	assertErrorResponse(t, res, http.StatusBadRequest, ErrCodeInvalidInput)
}

func TestContract_AdminUpdates_NotConfigured(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/updates/status", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	tr := assertErrorResponse(t, res, http.StatusNotImplemented, ErrCodeUnavailable)
	if tr.NextAction == "" {
		t.Fatalf("expected NextAction hint for unavailable update service")
	}
}

// ─── Success response schema fixture tests ────────────────────────────────────

func TestContract_GetSystemHealth_Schema(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	tr := assertToolResponse(t, res)
	if tr.Status != "ok" && tr.Status != "degraded" {
		t.Fatalf("status must be ok or degraded, got %q", tr.Status)
	}
	if tr.Data == nil {
		t.Fatalf("get_system_health data must not be nil")
	}
}

func TestContract_ListCameras_Schema(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.list_cameras", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	tr := assertToolResponse(t, res)
	if tr.Status != "ok" && tr.Status != "degraded" {
		t.Fatalf("status must be ok or degraded, got %q", tr.Status)
	}
	data, ok := tr.Data.(map[string]any)
	if !ok {
		t.Fatalf("data must be an object")
	}
	if _, ok := data["cameras"]; !ok {
		t.Fatalf("data.cameras must be present")
	}
}

func TestContract_GetZoneState_Schema(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_zone_state",
		bytes.NewBufferString(`{"input":{"zone_id":"front_door"}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	tr := assertToolResponse(t, res)
	if tr.Status != "ok" && tr.Status != "degraded" && tr.Status != "stale" {
		t.Fatalf("status must be ok, degraded, or stale, got %q", tr.Status)
	}
	data, ok := tr.Data.(map[string]any)
	if !ok {
		t.Fatalf("data must be an object")
	}
	for _, field := range []string{"zone_id", "observed_at", "entities"} {
		if _, ok := data[field]; !ok {
			t.Fatalf("data.%s must be present in get_zone_state response", field)
		}
	}
}

func TestContract_CheckPackageAtDoor_Schema(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{
		{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true, Status: camera.StatusAvailable},
	})
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

	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.check_package_at_door",
		bytes.NewBufferString(`{"input":{}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	tr := assertToolResponse(t, res)
	if tr.Status != "ok" && tr.Status != "degraded" {
		t.Fatalf("status must be ok or degraded, got %q", tr.Status)
	}
	data, ok := tr.Data.(map[string]any)
	if !ok {
		t.Fatalf("data must be an object")
	}
	for _, field := range []string{"package_present", "confidence", "observed_at"} {
		if _, ok := data[field]; !ok {
			t.Fatalf("data.%s must be present in check_package_at_door response", field)
		}
	}
}
