package mcp

// TestAgentHarness validates that each intent in the fixtures produces a
// schema-compliant MCP response. It simulates the tool-selection loop an AI
// agent (e.g. BearClaw) would perform: read intent → pick tool → call tool →
// parse response.
//
// The intents.json fixture IS the deterministic CI spec. Each entry records:
//   - the natural-language prompt ("intent") driving the tool choice,
//   - the exact tool/method/path/input the agent would send,
//   - structural invariants the response MUST satisfy.
//
// Running `go test ./internal/mcp/ -run TestAgentHarness -v` produces a
// readable transcript suitable for agent prompt-engineering review.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/baresystems/koala/internal/camera"
	"github.com/baresystems/koala/internal/service"
	"github.com/baresystems/koala/internal/state"
)

// intentFixture is one entry in tests/fixtures/agent/intents.json.
type intentFixture struct {
	ID                string         `json:"id"`
	Intent            string         `json:"intent"`
	Tool              string         `json:"tool"`
	Method            string         `json:"method"`
	Path              string         `json:"path"`
	Input             map[string]any `json:"input"`
	ExpectedStatusSet []string       `json:"expected_status_set"`
	ExpectedDataKeys  []string       `json:"expected_data_keys"`
	RequiredTopKeys   []string       `json:"required_top_keys"`
	// ExpectedHTTPStatus defaults to 200 when zero.
	ExpectedHTTPStatus int `json:"expected_http_status"`
}

// agentTranscript is the structured record produced for each intent run.
// It is logged via t.Log and can be serialised to disk for inspection.
type agentTranscript struct {
	ID             string         `json:"id"`
	Intent         string         `json:"intent"`
	Tool           string         `json:"tool"`
	Input          map[string]any `json:"input"`
	HTTPStatus     int            `json:"http_status"`
	ResponseStatus string         `json:"response_status"`
	DataKeys       []string       `json:"data_keys,omitempty"`
	SchemaValid    bool           `json:"schema_valid"`
	NextAction     string         `json:"next_action,omitempty"`
	Explanation    string         `json:"explanation"`
}

func loadIntentFixtures(t *testing.T) []intentFixture {
	t.Helper()
	data, err := os.ReadFile("../../tests/fixtures/agent/intents.json")
	if err != nil {
		t.Fatalf("load agent intents fixture: %v", err)
	}
	var fixtures []intentFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("parse agent intents fixture: %v", err)
	}
	return fixtures
}

// newSeededServer returns an MCP server with one package detection already
// ingested so happy-path tools return "ok" rather than "stale".
func newSeededServer(t *testing.T) http.Handler {
	t.Helper()
	registry := camera.NewRegistry([]camera.Camera{
		{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true, Status: camera.StatusAvailable},
	})
	agg := state.NewAggregator(time.Minute)
	agg.Ingest([]state.Detection{
		{
			CameraID:   "cam_front_1",
			ZoneID:     "front_door",
			Label:      "package",
			Confidence: 0.91,
			ObservedAt: time.Now().UTC(),
		},
	})
	svc := service.New(registry, agg, fakeInferenceClient{}, 4)
	return NewServer("test-token", svc, nil, nil, nil, nil, nil).Routes()
}

func TestAgentHarness(t *testing.T) {
	fixtures := loadIntentFixtures(t)
	h := newSeededServer(t)
	transcripts := make([]agentTranscript, 0, len(fixtures))

	for _, fix := range fixtures {
		fix := fix
		t.Run(fix.ID, func(t *testing.T) {
			wantHTTP := fix.ExpectedHTTPStatus
			if wantHTTP == 0 {
				wantHTTP = http.StatusOK
			}

			body, _ := json.Marshal(map[string]any{"input": fix.Input})
			req := httptest.NewRequest(fix.Method, fix.Path, bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)

			// ── HTTP status ───────────────────────────────────────────────
			if res.Code != wantHTTP {
				t.Fatalf("intent %q: expected HTTP %d, got %d\nbody: %s",
					fix.Intent, wantHTTP, res.Code, res.Body.String())
			}

			// ── Parse body ────────────────────────────────────────────────
			var resp map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
				t.Fatalf("intent %q: response is not valid JSON: %v\nbody: %s",
					fix.Intent, err, res.Body.String())
			}

			// ── Required top-level keys ───────────────────────────────────
			for _, key := range fix.RequiredTopKeys {
				if _, ok := resp[key]; !ok {
					t.Errorf("intent %q: required top-level key %q missing", fix.Intent, key)
				}
			}

			// ── Status must be in expected set ────────────────────────────
			status, _ := resp["status"].(string)
			if status == "" {
				t.Fatalf("intent %q: response has no \"status\" field", fix.Intent)
			}
			if !slices.Contains(fix.ExpectedStatusSet, status) {
				t.Fatalf("intent %q: status %q not in expected set %v", fix.Intent, status, fix.ExpectedStatusSet)
			}

			// ── Explanation always non-empty ──────────────────────────────
			explanation, _ := resp["explanation"].(string)
			if strings.TrimSpace(explanation) == "" {
				t.Errorf("intent %q: explanation must not be empty (status=%q)", fix.Intent, status)
			}

			// ── Data-level key checks (only for success responses) ────────
			var dataKeys []string
			if len(fix.ExpectedDataKeys) > 0 {
				data, ok := resp["data"].(map[string]any)
				if !ok {
					t.Fatalf("intent %q: expected data to be an object", fix.Intent)
				}
				for _, key := range fix.ExpectedDataKeys {
					if _, ok := data[key]; !ok {
						t.Errorf("intent %q: data.%s missing", fix.Intent, key)
					} else {
						dataKeys = append(dataKeys, key)
					}
				}
			}

			// ── next_action present on degraded/stale/error ───────────────
			nextAction, _ := resp["next_action"].(string)
			if (status == "degraded" || status == "stale" || status == "error") && nextAction == "" {
				t.Errorf("intent %q: status=%q should include next_action guidance", fix.Intent, status)
			}

			tr := agentTranscript{
				ID:             fix.ID,
				Intent:         fix.Intent,
				Tool:           fix.Tool,
				Input:          fix.Input,
				HTTPStatus:     res.Code,
				ResponseStatus: status,
				DataKeys:       dataKeys,
				SchemaValid:    !t.Failed(),
				NextAction:     nextAction,
				Explanation:    explanation,
			}
			transcripts = append(transcripts, tr)

			// Log structured transcript line for CI visibility.
			enc, _ := json.Marshal(tr)
			t.Logf("transcript: %s", enc)
		})
	}

	// Summary: print intent→status table for quick review.
	t.Log(formatTranscriptTable(transcripts))
}

// TestAgentHarness_Degraded verifies that a degraded inference worker still
// produces schema-valid, actionable MCP responses for every primary tool.
func TestAgentHarness_Degraded(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{
		{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true, Status: camera.StatusAvailable},
	})
	agg := state.NewAggregator(time.Minute)
	// Seed one detection so the aggregator has prior data; worker is still degraded.
	agg.Ingest([]state.Detection{{
		CameraID:   "cam_front_1",
		ZoneID:     "front_door",
		Label:      "package",
		Confidence: 0.88,
		ObservedAt: time.Now().UTC(),
	}})
	svc := service.New(registry, agg, fakeInferenceClient{healthStatus: "degraded"}, 4)
	h := NewServer("test-token", svc, nil, nil, nil, nil, nil).Routes()

	cases := []struct {
		name string
		path string
		body string
	}{
		{"get_zone_state", "/mcp/tools/koala.get_zone_state", `{"input":{"zone_id":"front_door"}}`},
		{"check_package_at_door", "/mcp/tools/koala.check_package_at_door", `{"input":{}}`},
		{"list_cameras", "/mcp/tools/koala.list_cameras", `{"input":{}}`},
		{"get_system_health", "/mcp/tools/koala.get_system_health", `{"input":{}}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer test-token")
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("degraded: expected HTTP 200 for %s, got %d", tc.name, res.Code)
			}

			var resp map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
				t.Fatalf("degraded: invalid JSON from %s: %v", tc.name, err)
			}

			status, _ := resp["status"].(string)
			if status == "" {
				t.Fatalf("degraded: %s response has no status", tc.name)
			}

			explanation, _ := resp["explanation"].(string)
			if strings.TrimSpace(explanation) == "" {
				t.Errorf("degraded: %s explanation must not be empty", tc.name)
			}

			// Tools that can be degraded must say so or provide clear stale guidance.
			validDegradedStatuses := []string{"ok", "degraded", "stale"}
			if !slices.Contains(validDegradedStatuses, status) {
				t.Errorf("degraded: %s unexpected status %q", tc.name, status)
			}

			// Degraded/stale responses must include next_action.
			if status == "degraded" || status == "stale" {
				nextAction, _ := resp["next_action"].(string)
				if nextAction == "" {
					t.Errorf("degraded: %s status=%q must include next_action", tc.name, status)
				}
			}

			t.Logf("degraded %s → status=%q explanation=%q", tc.name, status, explanation)
		})
	}
}

// formatTranscriptTable formats a compact summary table for test log output.
func formatTranscriptTable(ts []agentTranscript) string {
	var sb strings.Builder
	sb.WriteString("\n── Agent Harness Transcript ──────────────────────────\n")
	sb.WriteString(fmt.Sprintf("%-35s %-30s %-10s %s\n", "ID", "TOOL", "STATUS", "VALID"))
	sb.WriteString(strings.Repeat("─", 80) + "\n")
	for _, tr := range ts {
		valid := "✓"
		if !tr.SchemaValid {
			valid = "✗"
		}
		sb.WriteString(fmt.Sprintf("%-35s %-30s %-10s %s\n", tr.ID, tr.Tool, tr.ResponseStatus, valid))
	}
	return sb.String()
}
