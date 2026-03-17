package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/baresystems/koala/internal/camera"
	"github.com/baresystems/koala/internal/service"
	"github.com/baresystems/koala/internal/state"
	"github.com/baresystems/koala/internal/update"
	"time"
)

// newFleetServer returns an MCP server with the updater wired up.
func newFleetServer(t *testing.T) http.Handler {
	t.Helper()
	registry := camera.NewRegistry([]camera.Camera{
		{ID: "cam1", ZoneID: "front_door", FrontDoor: true},
	})
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	updater := update.NewManager("0.1.0-dev", "0.1.0-dev", "koala-local", "http://127.0.0.1:8080", "0.1.0", update.NoopExecutor{})
	return NewServer("test-token", svc, updater, nil, nil, nil, nil).Routes()
}

func doFleetRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ─── /admin/fleet/devices/list ────────────────────────────────────────────────

func TestFleet_ListDevices_InitiallyHasLocalDevice(t *testing.T) {
	h := newFleetServer(t)
	rec := doFleetRequest(h, http.MethodGet, "/admin/fleet/devices/list", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := resp["data"].(map[string]any)
	devices := data["devices"].([]any)
	if len(devices) < 1 {
		t.Fatal("expected at least the local device in the fleet list")
	}
	count := data["count"].(float64)
	if int(count) != len(devices) {
		t.Fatalf("count mismatch: count=%v devices=%d", count, len(devices))
	}
}

func TestFleet_ListDevices_NoUpdater_Returns501(t *testing.T) {
	registry := camera.NewRegistry(nil)
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	h := NewServer("test-token", svc, nil, nil, nil, nil, nil).Routes()
	rec := doFleetRequest(h, http.MethodGet, "/admin/fleet/devices/list", "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rec.Code)
	}
}

// ─── /admin/fleet/devices/register ───────────────────────────────────────────

func TestFleet_RegisterDevice_AddsDevice(t *testing.T) {
	h := newFleetServer(t)

	body := `{"input":{"device_id":"jetson-01","address":"http://10.0.0.10:8080","current_version":"1.0.0"}}`
	rec := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/register", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("register: expected 200, got %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("register: invalid JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("register: expected status=ok, got %v", resp["status"])
	}
	data := resp["data"].(map[string]any)
	if data["device_id"] != "jetson-01" {
		t.Fatalf("register: expected device_id=jetson-01, got %v", data["device_id"])
	}

	// Verify device appears in the fleet list.
	listRec := doFleetRequest(h, http.MethodGet, "/admin/fleet/devices/list", "")
	var listResp map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	listData := listResp["data"].(map[string]any)
	devices := listData["devices"].([]any)
	found := false
	for _, d := range devices {
		dm := d.(map[string]any)
		if dm["id"] == "jetson-01" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("registered device not found in fleet list")
	}
}

func TestFleet_RegisterDevice_MissingDeviceID_Returns400(t *testing.T) {
	h := newFleetServer(t)
	rec := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/register",
		`{"input":{"address":"http://10.0.0.10:8080"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestFleet_RegisterDevice_NoUpdater_Returns501(t *testing.T) {
	registry := camera.NewRegistry(nil)
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	h := NewServer("test-token", svc, nil, nil, nil, nil, nil).Routes()
	rec := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/register",
		`{"input":{"device_id":"jetson-01"}}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rec.Code)
	}
}

// ─── /admin/fleet/devices/deregister ─────────────────────────────────────────

func TestFleet_DeregisterDevice_RemovesDevice(t *testing.T) {
	h := newFleetServer(t)

	// Register a device first.
	doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/register",
		`{"input":{"device_id":"jetson-02","address":"http://10.0.0.11:8080","current_version":"1.0.0"}}`)

	// Deregister it.
	rec := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/deregister",
		`{"input":{"device_id":"jetson-02"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("deregister: expected 200, got %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("deregister: invalid JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("deregister: expected status=ok, got %v", resp["status"])
	}

	// Confirm it no longer appears in the list.
	listRec := doFleetRequest(h, http.MethodGet, "/admin/fleet/devices/list", "")
	var listResp map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	listData := listResp["data"].(map[string]any)
	devices := listData["devices"].([]any)
	for _, d := range devices {
		dm := d.(map[string]any)
		if dm["id"] == "jetson-02" {
			t.Fatal("deregistered device still appears in fleet list")
		}
	}
}

func TestFleet_DeregisterDevice_UnknownID_Returns400(t *testing.T) {
	h := newFleetServer(t)
	rec := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/deregister",
		`{"input":{"device_id":"no-such-device"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown device, got %d: %s", rec.Code, rec.Body)
	}
}

func TestFleet_DeregisterDevice_MissingDeviceID_Returns400(t *testing.T) {
	h := newFleetServer(t)
	rec := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/deregister",
		`{"input":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestFleet_DeregisterDevice_NoUpdater_Returns501(t *testing.T) {
	registry := camera.NewRegistry(nil)
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	h := NewServer("test-token", svc, nil, nil, nil, nil, nil).Routes()
	rec := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/deregister",
		`{"input":{"device_id":"jetson-02"}}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rec.Code)
	}
}

// ─── Auth gate ────────────────────────────────────────────────────────────────

func TestFleet_Endpoints_RequireAuth(t *testing.T) {
	h := newFleetServer(t)
	paths := []string{
		"/admin/fleet/devices/list",
		"/admin/fleet/devices/register",
		"/admin/fleet/devices/deregister",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"input":{}}`))
		// No Authorization header.
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401 without auth, got %d", path, rec.Code)
		}
	}
}

// ─── Round-trip: register → list → deregister → list ─────────────────────────

func TestFleet_FullRoundTrip(t *testing.T) {
	h := newFleetServer(t)

	const devID = "rpi-42"

	// 1. Register.
	reg := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/register",
		`{"input":{"device_id":"`+devID+`","address":"http://192.168.1.42:8080","current_version":"0.9.0"}}`)
	if reg.Code != http.StatusOK {
		t.Fatalf("register: %d %s", reg.Code, reg.Body)
	}

	// 2. List — device must be present.
	list1 := doFleetRequest(h, http.MethodGet, "/admin/fleet/devices/list", "")
	if list1.Code != http.StatusOK {
		t.Fatalf("list1: %d", list1.Code)
	}
	assertDeviceInList(t, list1.Body.Bytes(), devID, true)

	// 3. Deregister.
	dereg := doFleetRequest(h, http.MethodPost, "/admin/fleet/devices/deregister",
		`{"input":{"device_id":"`+devID+`"}}`)
	if dereg.Code != http.StatusOK {
		t.Fatalf("deregister: %d %s", dereg.Code, dereg.Body)
	}

	// 4. List — device must be gone.
	list2 := doFleetRequest(h, http.MethodGet, "/admin/fleet/devices/list", "")
	assertDeviceInList(t, list2.Body.Bytes(), devID, false)
}

// assertDeviceInList checks that device with the given ID is (or is not) in the
// fleet list response body.
func assertDeviceInList(t *testing.T, body []byte, deviceID string, wantPresent bool) {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data field")
	}
	devices, ok := data["devices"].([]any)
	if !ok {
		t.Fatalf("devices is not an array")
	}
	found := false
	for _, d := range devices {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		if dm["id"] == deviceID {
			found = true
			break
		}
	}
	if wantPresent && !found {
		t.Fatalf("device %q expected in list but not found", deviceID)
	}
	if !wantPresent && found {
		t.Fatalf("device %q expected absent but still in list", deviceID)
	}
}
