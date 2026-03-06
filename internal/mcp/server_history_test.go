package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/barelabs/koala/internal/audit"
	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/service"
	"github.com/barelabs/koala/internal/state"
)

func TestUpdateHistoryEndpoint(t *testing.T) {
	store := audit.NewMemoryStore()
	_ = store.Record(context.Background(), audit.Event{Category: "security", EventType: "unknown_key_id", Severity: "high", Message: "unknown key", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	_ = store.Record(context.Background(), audit.Event{Category: "rollout", EventType: "rollout_started", Severity: "info", Message: "rollout", CreatedAt: time.Now().UTC().Format(time.RFC3339)})

	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}})
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	h := NewServer("test-token", svc, nil, nil, nil, nil, store).Routes()

	req := httptest.NewRequest(http.MethodGet, "/admin/updates/history", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", res.Code, res.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	data := payload["data"].(map[string]any)
	events := data["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}
