package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Bare-Systems/Koala/internal/camera"
	"github.com/Bare-Systems/Koala/internal/service"
	"github.com/Bare-Systems/Koala/internal/state"
	"github.com/Bare-Systems/Koala/internal/update"
)

func updateManifestJSON() string {
	return `{
"manifest": {
  "key_id": "key-2026-03",
  "version": "0.2.1",
  "artifact_url": "http://updates.local/koala-0.2.1.tar.gz",
  "sha256": "` + strings.Repeat("a", 64) + `",
  "signature": "sig-ed25519-1",
  "min_orchestrator_version": "0.1.0-dev",
  "min_worker_version": "0.1.0-dev",
  "created_at": "2026-03-06T00:00:00Z"
},
"device_ids": ["koala-local"]
}`
}

func newServerWithUpdater() http.Handler {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}})
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	updater := update.NewManager("0.1.0-dev", "0.1.0-dev", "koala-local", "http://127.0.0.1:8080", "0.1.0", update.NoopExecutor{})
	agent := update.NewMemoryAgent("0.1.0")
	return NewServer("test-token", svc, updater, agent, nil, nil, nil).Routes()
}

func TestUpdateEndpoints_Flow(t *testing.T) {
	h := newServerWithUpdater()

	stageReq := httptest.NewRequest(http.MethodPost, "/admin/updates/stage", bytes.NewBufferString(`{"input":`+updateManifestJSON()+`}`))
	stageReq.Header.Set("Authorization", "Bearer test-token")
	stageRes := httptest.NewRecorder()
	h.ServeHTTP(stageRes, stageReq)
	if stageRes.Code != http.StatusOK {
		t.Fatalf("stage status: %d body=%s", stageRes.Code, stageRes.Body.String())
	}

	applyReq := httptest.NewRequest(http.MethodPost, "/admin/updates/apply", bytes.NewBufferString(`{"input":{"device_ids":["koala-local"]}}`))
	applyReq.Header.Set("Authorization", "Bearer test-token")
	applyRes := httptest.NewRecorder()
	h.ServeHTTP(applyRes, applyReq)
	if applyRes.Code != http.StatusOK {
		t.Fatalf("apply status: %d body=%s", applyRes.Code, applyRes.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/admin/updates/status", nil)
	statusReq.Header.Set("Authorization", "Bearer test-token")
	statusRes := httptest.NewRecorder()
	h.ServeHTTP(statusRes, statusReq)
	if statusRes.Code != http.StatusOK {
		t.Fatalf("status status: %d body=%s", statusRes.Code, statusRes.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(statusRes.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status payload: %v", err)
	}
	data := payload["data"].(map[string]any)
	devices := data["devices"].([]any)
	first := devices[0].(map[string]any)
	if first["current_version"] != "0.2.1" {
		t.Fatalf("expected current_version 0.2.1, got %v", first["current_version"])
	}
}

func TestUpdateEndpoints_Rollback(t *testing.T) {
	h := newServerWithUpdater()

	stageReq := httptest.NewRequest(http.MethodPost, "/admin/updates/stage", bytes.NewBufferString(`{"input":`+updateManifestJSON()+`}`))
	stageReq.Header.Set("Authorization", "Bearer test-token")
	stageRes := httptest.NewRecorder()
	h.ServeHTTP(stageRes, stageReq)
	if stageRes.Code != http.StatusOK {
		t.Fatalf("stage status: %d body=%s", stageRes.Code, stageRes.Body.String())
	}

	applyReq := httptest.NewRequest(http.MethodPost, "/admin/updates/apply", bytes.NewBufferString(`{"input":{"device_ids":["koala-local"]}}`))
	applyReq.Header.Set("Authorization", "Bearer test-token")
	applyRes := httptest.NewRecorder()
	h.ServeHTTP(applyRes, applyReq)
	if applyRes.Code != http.StatusOK {
		t.Fatalf("apply status: %d body=%s", applyRes.Code, applyRes.Body.String())
	}

	rollbackReq := httptest.NewRequest(http.MethodPost, "/admin/updates/rollback", bytes.NewBufferString(`{"input":{"device_ids":["koala-local"],"reason":"healthcheck_failed"}}`))
	rollbackReq.Header.Set("Authorization", "Bearer test-token")
	rollbackRes := httptest.NewRecorder()
	h.ServeHTTP(rollbackRes, rollbackReq)
	if rollbackRes.Code != http.StatusOK {
		t.Fatalf("rollback status: %d body=%s", rollbackRes.Code, rollbackRes.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rollbackRes.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode rollback payload: %v", err)
	}
	data := payload["data"].(map[string]any)
	devices := data["devices"].([]any)
	first := devices[0].(map[string]any)
	if first["state"] != string(update.StateRolledBack) {
		t.Fatalf("expected rolled_back, got %v", first["state"])
	}
}
