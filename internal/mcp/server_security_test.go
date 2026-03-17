package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baresystems/koala/internal/camera"
	"github.com/baresystems/koala/internal/service"
	"github.com/baresystems/koala/internal/state"
	"github.com/baresystems/koala/internal/update"
)

type fakeSecurityAgent struct{}

func (fakeSecurityAgent) Stage(context.Context, update.Manifest) error { return nil }
func (fakeSecurityAgent) Apply(context.Context) error                  { return nil }
func (fakeSecurityAgent) Rollback(context.Context, string) error       { return nil }
func (fakeSecurityAgent) Health(context.Context) (map[string]any, error) {
	return map[string]any{
		"unknown_key_attempts": update.UnknownKeyStats{
			ManifestUnknown: map[string]int{"key-old": 2},
			BundleUnknown:   map[string]int{"key-bad": 1},
		},
		"unknown_key_alerts": []update.UnknownKeyAlert{{
			Kind:       "manifest",
			KeyID:      "key-old",
			Count:      2,
			ObservedAt: time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC),
		}},
	}, nil
}

func TestUpdateSecurityEndpoint(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}})
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	h := NewServer("test-token", svc, nil, fakeSecurityAgent{}, nil, nil, nil).Routes()

	req := httptest.NewRequest(http.MethodGet, "/admin/updates/security", nil)
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
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", payload["status"])
	}
	data := payload["data"].(map[string]any)
	if _, ok := data["unknown_key_attempts"]; !ok {
		t.Fatalf("missing unknown_key_attempts")
	}
	if _, ok := data["recent_alerts"]; !ok {
		t.Fatalf("missing recent_alerts")
	}
}
