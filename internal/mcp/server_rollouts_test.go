package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baresystems/koala/internal/camera"
	"github.com/baresystems/koala/internal/service"
	"github.com/baresystems/koala/internal/state"
	"github.com/baresystems/koala/internal/update"
)

func rolloutManifestInputJSON() string {
	return `{
"manifest": {
  "key_id": "key-2026-03",
  "version": "0.2.2",
  "artifact_url": "http://updates.local/koala-0.2.2.bundle.json",
  "sha256": "` + strings.Repeat("a", 64) + `",
  "signature": "sig-ed25519-1",
  "min_orchestrator_version": "0.1.0-dev",
  "min_worker_version": "0.1.0-dev",
  "created_at": "2026-03-06T00:00:00Z"
},
"mode": "batch",
"batch_size": 1,
"max_failures": 1
}`
}

func newServerWithRollouts() http.Handler {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}})
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	updater := update.NewManager("0.1.0-dev", "0.1.0-dev", "koala-local", "http://127.0.0.1:8080", "0.1.0", update.NoopExecutor{})
	return NewServer("test-token", svc, updater, nil, nil, nil, nil).Routes()
}

func TestRolloutEndpoints_StartGetList(t *testing.T) {
	h := newServerWithRollouts()

	startReq := httptest.NewRequest(http.MethodPost, "/admin/updates/rollouts/start", bytes.NewBufferString(`{"input":`+rolloutManifestInputJSON()+`}`))
	startReq.Header.Set("Authorization", "Bearer test-token")
	startRes := httptest.NewRecorder()
	h.ServeHTTP(startRes, startReq)
	if startRes.Code != http.StatusOK {
		t.Fatalf("start status: %d body=%s", startRes.Code, startRes.Body.String())
	}

	var startPayload map[string]any
	if err := json.Unmarshal(startRes.Body.Bytes(), &startPayload); err != nil {
		t.Fatalf("decode start payload: %v", err)
	}
	rollout := startPayload["data"].(map[string]any)
	rolloutID := rollout["id"].(string)

	getReq := httptest.NewRequest(http.MethodPost, "/admin/updates/rollouts/get", bytes.NewBufferString(`{"input":{"rollout_id":"`+rolloutID+`"}}`))
	getReq.Header.Set("Authorization", "Bearer test-token")
	getRes := httptest.NewRecorder()
	h.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status: %d body=%s", getRes.Code, getRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/updates/rollouts/list", nil)
	listReq.Header.Set("Authorization", "Bearer test-token")
	listRes := httptest.NewRecorder()
	h.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status: %d body=%s", listRes.Code, listRes.Body.String())
	}
	var listPayload map[string]any
	if err := json.Unmarshal(listRes.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	data := listPayload["data"].(map[string]any)
	rollouts := data["rollouts"].([]any)
	if len(rollouts) == 0 {
		t.Fatalf("expected rollouts list to be non-empty")
	}
}
