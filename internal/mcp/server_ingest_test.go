package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Bare-Systems/Koala/internal/camera"
	"github.com/Bare-Systems/Koala/internal/ingest"
	"github.com/Bare-Systems/Koala/internal/service"
	"github.com/Bare-Systems/Koala/internal/state"
)

type staticSnapshotter struct{}

func (staticSnapshotter) Capture(_ context.Context, _ string) ([]byte, error) {
	return []byte("frame"), nil
}

func TestIngestStatusEndpoint(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", RTSPURL: "rtsp://example", ZoneID: "front_door"}})
	svc := service.New(registry, state.NewAggregator(time.Minute), fakeInferenceClient{}, 2)
	ingestManager := ingest.NewManager(registry, svc, staticSnapshotter{}, 100*time.Millisecond, time.Second)
	h := NewServer("test-token", svc, nil, nil, nil, ingestManager, nil).Routes()

	req := httptest.NewRequest(http.MethodGet, "/admin/ingest/status", nil)
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
	if _, ok := data["cameras"]; !ok {
		t.Fatalf("missing cameras stats")
	}
	if _, ok := data["incidents"]; !ok {
		t.Fatalf("missing incidents")
	}
}
