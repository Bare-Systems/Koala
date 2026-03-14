package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/inference"
	"github.com/barelabs/koala/internal/mcp"
	"github.com/barelabs/koala/internal/service"
	"github.com/barelabs/koala/internal/state"
	"github.com/barelabs/koala/internal/zone"
)

type replayCase struct {
	Name            string `json:"name"`
	FrameTag        string `json:"frame_tag"`
	ExpectedPackage bool   `json:"expected_package"`
	ExpectedPerson  bool   `json:"expected_person"`
}

// confusionMatrix accumulates binary classification statistics for one label.
type confusionMatrix struct{ TP, FP, TN, FN int }

func (c *confusionMatrix) Record(expected, predicted bool) {
	switch {
	case expected && predicted:
		c.TP++
	case !expected && predicted:
		c.FP++
	case !expected && !predicted:
		c.TN++
	default: // expected && !predicted
		c.FN++
	}
}

func (c confusionMatrix) Precision() float64 {
	if c.TP+c.FP == 0 {
		return 1.0
	}
	return float64(c.TP) / float64(c.TP+c.FP)
}

func (c confusionMatrix) Recall() float64 {
	if c.TP+c.FN == 0 {
		return 1.0
	}
	return float64(c.TP) / float64(c.TP+c.FN)
}

func (c confusionMatrix) F1() float64 {
	p, r := c.Precision(), c.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

type replayInferenceClient struct{}

func (replayInferenceClient) AnalyzeFrame(_ context.Context, req inference.FrameRequest) (inference.FrameResponse, error) {
	decoded, _ := base64.StdEncoding.DecodeString(req.FrameB64)
	tag := string(decoded)
	detections := make([]inference.Detection, 0, 2)
	if strings.Contains(tag, "package") {
		detections = append(detections, inference.Detection{
			CameraID:   req.CameraID,
			ZoneID:     req.ZoneID,
			Label:      "package",
			Confidence: 0.95,
			Timestamp:  req.Captured,
		})
	}
	if strings.Contains(tag, "person") {
		detections = append(detections, inference.Detection{
			CameraID:   req.CameraID,
			ZoneID:     req.ZoneID,
			Label:      "person",
			Confidence: 0.91,
			Timestamp:  req.Captured,
		})
	}
	return inference.FrameResponse{Detections: detections}, nil
}

func (replayInferenceClient) WorkerHealth(_ context.Context) (inference.HealthResponse, error) {
	return inference.HealthResponse{Status: "ok"}, nil
}

func TestReplayHarnessPackageDoorLatencyAndAccuracyGates(t *testing.T) {
	cases, err := loadReplayCases("fixtures/replay/front_door_cases.json")
	if err != nil {
		t.Fatalf("load replay cases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("expected replay cases")
	}

	latencies := make([]time.Duration, 0, len(cases))
	var pkgCM, personCM confusionMatrix

	for _, tc := range cases {
		predPkg, predPerson, latency := runReplayCase(t, tc)
		latencies = append(latencies, latency)
		pkgCM.Record(tc.ExpectedPackage, predPkg)
		personCM.Record(tc.ExpectedPerson, predPerson)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95 := percentile(latencies, 0.95)
	if p95 > 2*time.Second {
		t.Fatalf("latency gate failed: p95=%s > 2s", p95)
	}

	t.Logf("package:  precision=%.2f recall=%.2f f1=%.2f  TP=%d FP=%d TN=%d FN=%d",
		pkgCM.Precision(), pkgCM.Recall(), pkgCM.F1(),
		pkgCM.TP, pkgCM.FP, pkgCM.TN, pkgCM.FN)
	t.Logf("person:   precision=%.2f recall=%.2f f1=%.2f  TP=%d FP=%d TN=%d FN=%d",
		personCM.Precision(), personCM.Recall(), personCM.F1(),
		personCM.TP, personCM.FP, personCM.TN, personCM.FN)

	if pkgCM.Precision() < 0.90 {
		t.Fatalf("accuracy gate failed: package precision=%.2f < 0.90", pkgCM.Precision())
	}
	if pkgCM.Recall() < 0.90 {
		t.Fatalf("accuracy gate failed: package recall=%.2f < 0.90", pkgCM.Recall())
	}
}

func runReplayCase(t *testing.T, tc replayCase) (predPkg bool, predPerson bool, latency time.Duration) {
	t.Helper()
	registry := camera.NewRegistry([]camera.Camera{{
		ID:        "cam_front_1",
		Name:      "Front Door",
		ZoneID:    "front_door",
		FrontDoor: true,
		Status:    camera.StatusAvailable,
	}})
	svc := service.New(registry, state.NewAggregator(time.Minute), replayInferenceClient{}, 8)
	svc.FrameBufferEnabled = true // replay test uses frame content for detection; enable forwarding
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)

	handler := mcp.NewServer("test-token", svc, nil, nil, nil, nil, nil).WithRateLimiter(nil).Routes()

	frameB64 := base64.StdEncoding.EncodeToString([]byte(tc.FrameTag))
	ingestReq := map[string]any{
		"input": map[string]any{
			"camera_id": "cam_front_1",
			"zone_id":   "front_door",
			"frame_b64": frameB64,
		},
	}
	if code := postJSON(t, handler, "/ingest/frame", ingestReq).Code; code != http.StatusAccepted {
		t.Fatalf("case %s ingest failed: status=%d", tc.Name, code)
	}

	start := time.Now()
	deadline := start.Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		// Query package state.
		res := postJSON(t, handler, "/mcp/tools/koala.check_package_at_door", map[string]any{"input": map[string]any{}})
		if res.Code != http.StatusOK {
			t.Fatalf("case %s check failed: status=%d", tc.Name, res.Code)
		}
		var payload map[string]any
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatalf("case %s decode payload: %v", tc.Name, err)
		}
		pkgData := payload["data"].(map[string]any)
		gotPkg := pkgData["package_present"].(bool)

		// Query zone for person state.
		zoneRes := postJSON(t, handler, "/mcp/tools/koala.get_zone_state",
			map[string]any{"input": map[string]any{"zone_id": "front_door"}})
		gotPerson := false
		if zoneRes.Code == http.StatusOK {
			var zonePayload map[string]any
			if err := json.Unmarshal(zoneRes.Body.Bytes(), &zonePayload); err == nil {
				if zd, ok := zonePayload["data"].(map[string]any); ok {
					if entities, ok := zd["entities"].([]any); ok {
						for _, e := range entities {
							em := e.(map[string]any)
							if em["label"] == "person" && em["present"].(bool) {
								gotPerson = true
							}
						}
					}
				}
			}
		}

		if gotPkg == tc.ExpectedPackage && gotPerson == tc.ExpectedPerson {
			return gotPkg, gotPerson, time.Since(start)
		}
		time.Sleep(25 * time.Millisecond)
	}

	return false, false, time.Since(start)
}

// TestReplayZonePolygonFiltering validates that the zone polygon filter correctly
// passes in-zone detections and rejects out-of-zone detections end-to-end.
//
// Zone covers the bottom-right quadrant (x≥0.5, y≥0.5).  MinOverlap=0.3.
func TestReplayZonePolygonFiltering(t *testing.T) {
	// Bottom-right quadrant polygon.
	zonePoly := zone.Polygon{{X: 0.5, Y: 0.5}, {X: 1, Y: 0.5}, {X: 1, Y: 1}, {X: 0.5, Y: 1}}

	type polygonCase struct {
		name        string
		bbox        zone.BBox
		wantPackage bool
	}
	// in_zone: BBox [0.6..0.9]×[0.6..0.9] fully inside zone → passes.
	// out_zone: BBox [0.0..0.3]×[0.0..0.3] fully outside zone → rejected.
	// edge: BBox [0.4..0.8]×[0.4..0.8]; intersection [0.5..0.8]×[0.5..0.8]=0.09
	//       bbox area=0.16, overlap=0.09/0.16=0.5625 ≥ 0.3 → passes.
	cases := []polygonCase{
		{"in_zone", zone.BBox{X: 0.6, Y: 0.6, W: 0.3, H: 0.3}, true},
		{"out_zone", zone.BBox{X: 0.0, Y: 0.0, W: 0.3, H: 0.3}, false},
		{"edge_majority_in", zone.BBox{X: 0.4, Y: 0.4, W: 0.4, H: 0.4}, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			registry := camera.NewRegistry([]camera.Camera{{
				ID: "cam_front_1", Name: "Front Door",
				ZoneID: "front_door", FrontDoor: true,
				Status: camera.StatusAvailable,
			}})
			agg := state.NewAggregator(time.Minute)
			bboxClient := &bboxInferenceClient{bbox: tc.bbox}
			svc := service.New(registry, agg, bboxClient, 8)
			svc.FrameBufferEnabled = true
			svc.Filter = service.NewZoneFilter(map[string]service.ZonePolygonConfig{
				"front_door": {Polygon: zonePoly, MinOverlap: 0.3},
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			svc.Start(ctx)

			handler := mcp.NewServer("test-token", svc, nil, nil, nil, nil, nil).WithRateLimiter(nil).Routes()

			frameB64 := base64.StdEncoding.EncodeToString([]byte("package_at_door"))
			ingestReq := map[string]any{
				"input": map[string]any{
					"camera_id": "cam_front_1",
					"zone_id":   "front_door",
					"frame_b64": frameB64,
				},
			}
			if code := postJSON(t, handler, "/ingest/frame", ingestReq).Code; code != http.StatusAccepted {
				t.Fatalf("ingest: unexpected status %d", code)
			}

			gotPkg := false
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				res := postJSON(t, handler, "/mcp/tools/koala.check_package_at_door",
					map[string]any{"input": map[string]any{}})
				if res.Code == http.StatusOK {
					var payload map[string]any
					if err := json.Unmarshal(res.Body.Bytes(), &payload); err == nil {
						if data, ok := payload["data"].(map[string]any); ok {
							gotPkg, _ = data["package_present"].(bool)
						}
					}
				}
				if gotPkg == tc.wantPackage {
					break
				}
				time.Sleep(15 * time.Millisecond)
			}

			if gotPkg != tc.wantPackage {
				t.Fatalf("case %s: package_present=%v want=%v", tc.name, gotPkg, tc.wantPackage)
			}
		})
	}
}

// bboxInferenceClient always returns a "package" detection with the configured BBox
// when the frame tag contains "package".
type bboxInferenceClient struct {
	bbox zone.BBox
}

func (c *bboxInferenceClient) AnalyzeFrame(_ context.Context, req inference.FrameRequest) (inference.FrameResponse, error) {
	decoded, _ := base64.StdEncoding.DecodeString(req.FrameB64)
	if !strings.Contains(string(decoded), "package") {
		return inference.FrameResponse{}, nil
	}
	return inference.FrameResponse{Detections: []inference.Detection{{
		CameraID:   req.CameraID,
		ZoneID:     req.ZoneID,
		Label:      "package",
		Confidence: 0.95,
		Timestamp:  req.Captured,
		BBox:       c.bbox,
	}}}, nil
}

func (c *bboxInferenceClient) WorkerHealth(_ context.Context) (inference.HealthResponse, error) {
	return inference.HealthResponse{Status: "ok"}, nil
}

func loadReplayCases(path string) ([]replayCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []replayCase
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func postJSON(t *testing.T, handler http.Handler, route string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		return values[0]
	}
	if p >= 1 {
		return values[len(values)-1]
	}
	idx := int(float64(len(values)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}
