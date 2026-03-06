package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/barelabs/koala/internal/audit"
	"github.com/barelabs/koala/internal/service"
	"github.com/barelabs/koala/internal/update"
)

type Server struct {
	token      string
	service    *service.Service
	updater    *update.Manager
	agent      update.Agent
	poller     *update.Poller
	auditStore audit.Store
}

func NewServer(token string, svc *service.Service, updater *update.Manager, agent update.Agent, poller *update.Poller, auditStore audit.Store) *Server {
	return &Server{token: token, service: svc, updater: updater, agent: agent, poller: poller, auditStore: auditStore}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/tools/koala.get_system_health", s.wrapAuth(s.getSystemHealth))
	mux.HandleFunc("/mcp/tools/koala.list_cameras", s.wrapAuth(s.listCameras))
	mux.HandleFunc("/mcp/tools/koala.get_zone_state", s.wrapAuth(s.getZoneState))
	mux.HandleFunc("/mcp/tools/koala.check_package_at_door", s.wrapAuth(s.checkPackageAtDoor))
	mux.HandleFunc("/ingest/frame", s.wrapAuth(s.ingestFrame))

	mux.HandleFunc("/admin/updates/status", s.wrapAuth(s.updateStatus))
	mux.HandleFunc("/admin/updates/check", s.wrapAuth(s.updateCheck))
	mux.HandleFunc("/admin/updates/stage", s.wrapAuth(s.updateStage))
	mux.HandleFunc("/admin/updates/apply", s.wrapAuth(s.updateApply))
	mux.HandleFunc("/admin/updates/rollback", s.wrapAuth(s.updateRollback))
	mux.HandleFunc("/admin/updates/security", s.wrapAuth(s.updateSecurity))
	mux.HandleFunc("/admin/updates/history", s.wrapAuth(s.updateHistory))
	mux.HandleFunc("/admin/updates/rollouts/start", s.wrapAuth(s.rolloutStart))
	mux.HandleFunc("/admin/updates/rollouts/get", s.wrapAuth(s.rolloutGet))
	mux.HandleFunc("/admin/updates/rollouts/list", s.wrapAuth(s.rolloutList))

	mux.HandleFunc("/agent/updates/health", s.wrapAuth(s.agentHealth))
	mux.HandleFunc("/agent/updates/stage", s.wrapAuth(s.agentStage))
	mux.HandleFunc("/agent/updates/apply", s.wrapAuth(s.agentApply))
	mux.HandleFunc("/agent/updates/rollback", s.wrapAuth(s.agentRollback))
	return mux
}

func (s *Server) wrapAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func (s *Server) decodeToolRequest(r *http.Request) (ToolRequest, error) {
	if r.Method == http.MethodGet {
		return ToolRequest{Input: map[string]any{}}, nil
	}
	var req ToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return ToolRequest{}, err
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}
	return req, nil
}

func (s *Server) writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) getSystemHealth(w http.ResponseWriter, _ *http.Request) {
	health := s.service.Health()
	status := "ok"
	explanation := "all subsystems healthy"
	if health.Status != "ok" {
		status = "degraded"
		explanation = "one or more subsystems degraded"
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      status,
		"explanation": explanation,
		"data":        health,
	})
}

func (s *Server) listCameras(w http.ResponseWriter, _ *http.Request) {
	status := "ok"
	explanation := "camera registry loaded"
	if s.service.IsDegraded() {
		status = "degraded"
		explanation = "inference worker degraded; camera registry still available"
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      status,
		"explanation": explanation,
		"data": map[string]any{
			"cameras": s.service.Registry.List(),
		},
	})
}

func (s *Server) getZoneState(w http.ResponseWriter, r *http.Request) {
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	zoneID, err := readRequiredString(req.Input, "zone_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	zone := s.service.ZoneState(zoneID)
	status := "ok"
	explanation := "zone state available"
	if s.service.IsDegraded() {
		status = "degraded"
		explanation = "inference unavailable; returning last-known zone state"
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":            status,
		"freshness_seconds": zone.FreshnessSec,
		"explanation":       explanation,
		"data": map[string]any{
			"zone_id":     zone.ZoneID,
			"observed_at": zone.ObservedAt.UTC().Format(time.RFC3339),
			"entities":    zone.Entities,
		},
	})
}

func (s *Server) checkPackageAtDoor(w http.ResponseWriter, r *http.Request) {
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cameraID, _ := readOptionalString(req.Input, "camera_id")
	present, confidence, observedAt, err := s.service.DoorPackageState(cameraID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := "ok"
	explanation := "front door package state computed"
	if s.service.IsDegraded() {
		status = "degraded"
		explanation = "inference unavailable; returning last-known package state"
	}
	freshness := int64(0)
	if !observedAt.IsZero() {
		freshness = int64(time.Since(observedAt).Seconds())
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":            status,
		"freshness_seconds": freshness,
		"explanation":       explanation,
		"data": map[string]any{
			"package_present": present,
			"confidence":      confidence,
			"observed_at":     observedAt.UTC().Format(time.RFC3339),
		},
	})
}

func (s *Server) ingestFrame(w http.ResponseWriter, r *http.Request) {
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cameraID, err := readRequiredString(req.Input, "camera_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	zoneID, err := readRequiredString(req.Input, "zone_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	frameB64, _ := readOptionalString(req.Input, "frame_b64")
	capturedAt := time.Now().UTC()
	if ts, ok := req.Input["captured_at"]; ok {
		if parsed, parseErr := parseTimestamp(ts); parseErr == nil {
			capturedAt = parsed
		}
	}
	accepted := s.service.Submit(service.FrameTask{CameraID: cameraID, ZoneID: zoneID, FrameB64: frameB64, Captured: capturedAt})
	if !accepted {
		s.writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"status":      "degraded",
			"explanation": "ingest queue is full",
		})
		return
	}
	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"status":      "ok",
		"explanation": "frame accepted",
	})
}

func (s *Server) updateStatus(w http.ResponseWriter, _ *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update device status list",
		"data": map[string]any{
			"devices": s.updater.Status(),
			"poller":  pollerStatus(s.poller),
		},
	})
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	manifest, deviceIDs, err := parseUpdateInput(req.Input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.updater.Check(manifest, deviceIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update eligibility evaluated",
		"data": map[string]any{
			"results": result,
		},
	})
}

func (s *Server) updateStage(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	manifest, deviceIDs, err := parseUpdateInput(req.Input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	devices, err := s.updater.Stage(manifest, deviceIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update staged",
		"data": map[string]any{
			"devices": devices,
		},
	})
}

func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	deviceIDs, err := readDeviceIDs(req.Input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	devices, err := s.updater.Apply(deviceIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update applied",
		"data": map[string]any{
			"devices": devices,
		},
	})
}

func (s *Server) updateRollback(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	deviceIDs, err := readDeviceIDs(req.Input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reason, _ := readOptionalString(req.Input, "reason")
	devices, err := s.updater.Rollback(deviceIDs, reason)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update rolled back",
		"data": map[string]any{
			"devices": devices,
		},
	})
}

func (s *Server) updateSecurity(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	health, err := s.agent.Health(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	attempts, _ := health["unknown_key_attempts"]
	alerts, _ := health["unknown_key_alerts"]
	if attempts == nil {
		attempts = map[string]any{"manifest_unknown": map[string]int{}, "bundle_unknown": map[string]int{}}
	}
	if alerts == nil {
		alerts = []any{}
	}
	history := []audit.Event{}
	if s.auditStore != nil {
		events, err := s.auditStore.List(r.Context(), audit.ListOptions{Category: "security", Limit: 50})
		if err == nil {
			history = events
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update security telemetry snapshot",
		"data": map[string]any{
			"unknown_key_attempts": attempts,
			"recent_alerts":        alerts,
			"history":              history,
		},
	})
}

func (s *Server) updateHistory(w http.ResponseWriter, r *http.Request) {
	if s.auditStore == nil {
		http.Error(w, "audit store is not configured", http.StatusNotImplemented)
		return
	}
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	category, _ := readOptionalString(req.Input, "category")
	limit, err := readOptionalInt(req.Input, "limit")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	events, err := s.auditStore.List(r.Context(), audit.ListOptions{Category: category, Limit: limit})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "historical update/security events",
		"data": map[string]any{
			"events": events,
		},
	})
}

func (s *Server) rolloutStart(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	manifest, deviceIDs, err := parseUpdateInput(req.Input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	modeRaw, _ := readOptionalString(req.Input, "mode")
	batchSize, err := readOptionalInt(req.Input, "batch_size")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	maxFailures, err := readOptionalInt(req.Input, "max_failures")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pauseMs, err := readOptionalInt(req.Input, "pause_between_batches_ms")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rollbackScope, _ := readOptionalString(req.Input, "rollback_scope")

	rollout, err := s.updater.StartRollout(update.RolloutRequest{
		Manifest:            manifest,
		DeviceIDs:           deviceIDs,
		Mode:                update.RolloutMode(strings.ToLower(strings.TrimSpace(modeRaw))),
		BatchSize:           batchSize,
		MaxFailures:         maxFailures,
		PauseBetweenBatches: time.Duration(pauseMs) * time.Millisecond,
		RollbackScope:       rollbackScope,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "rollout started",
		"data":        rollout,
	})
	s.recordAuditEvent(r, audit.Event{
		Category:  "rollout",
		EventType: "rollout_started",
		Severity:  "info",
		Message:   "rollout started",
		RolloutID: rollout.ID,
		Payload: map[string]any{
			"mode":          rollout.Mode,
			"status":        rollout.Status,
			"failure_count": rollout.FailureCount,
		},
	})
}

func (s *Server) rolloutGet(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	req, err := s.decodeToolRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rolloutID, err := readRequiredString(req.Input, "rollout_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rollout, ok := s.updater.GetRollout(rolloutID)
	if !ok {
		http.Error(w, "rollout not found", http.StatusNotFound)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "rollout details",
		"data":        rollout,
	})
}

func (s *Server) rolloutList(w http.ResponseWriter, _ *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not configured", http.StatusNotImplemented)
		return
	}
	rollouts := s.updater.ListRollouts()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "rollout list",
		"data": map[string]any{
			"rollouts": rollouts,
		},
	})
}

func (s *Server) agentHealth(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		http.Error(w, "agent not configured", http.StatusNotImplemented)
		return
	}
	health, err := s.agent.Health(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "agent health available",
		"data":        health,
	})
}

func (s *Server) agentStage(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		http.Error(w, "agent not configured", http.StatusNotImplemented)
		return
	}
	var payload struct {
		Manifest update.Manifest `json:"manifest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.agent.Stage(r.Context(), payload.Manifest); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "agent staged update",
	})
}

func (s *Server) agentApply(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		http.Error(w, "agent not configured", http.StatusNotImplemented)
		return
	}
	if err := s.agent.Apply(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "agent applied update",
	})
}

func (s *Server) agentRollback(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		http.Error(w, "agent not configured", http.StatusNotImplemented)
		return
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if err := s.agent.Rollback(r.Context(), payload.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "agent rolled back update",
	})
}

func parseUpdateInput(input map[string]any) (update.Manifest, []string, error) {
	raw, ok := input["manifest"]
	if !ok {
		return update.Manifest{}, nil, fmt.Errorf("manifest is required")
	}
	asMap, ok := raw.(map[string]any)
	if !ok {
		return update.Manifest{}, nil, fmt.Errorf("manifest must be an object")
	}
	manifest := update.Manifest{}
	manifest.KeyID, _ = readOptionalString(asMap, "key_id")
	manifest.Version, _ = readOptionalString(asMap, "version")
	manifest.ArtifactURL, _ = readOptionalString(asMap, "artifact_url")
	manifest.SHA256, _ = readOptionalString(asMap, "sha256")
	manifest.Signature, _ = readOptionalString(asMap, "signature")
	manifest.MinOrchestratorVersion, _ = readOptionalString(asMap, "min_orchestrator_version")
	manifest.MinWorkerVersion, _ = readOptionalString(asMap, "min_worker_version")
	manifest.CreatedAt, _ = readOptionalString(asMap, "created_at")
	deviceIDs, err := readDeviceIDs(input)
	if err != nil {
		return update.Manifest{}, nil, err
	}
	return manifest, deviceIDs, nil
}

func readDeviceIDs(values map[string]any) ([]string, error) {
	raw, ok := values["device_ids"]
	if !ok {
		return nil, nil
	}
	listRaw, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("device_ids must be an array")
	}
	ids := make([]string, 0, len(listRaw))
	for _, v := range listRaw {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("device_ids must contain non-empty strings")
		}
		ids = append(ids, s)
	}
	return ids, nil
}

func readOptionalInt(values map[string]any, key string) (int, error) {
	raw, ok := values[key]
	if !ok {
		return 0, nil
	}
	switch v := raw.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case json.Number:
		i, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, fmt.Errorf("%s must be numeric", key)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("%s must be numeric", key)
	}
}

func (s *Server) recordAuditEvent(r *http.Request, event audit.Event) {
	if s.auditStore == nil {
		return
	}
	if event.CreatedAt == "" {
		event.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_ = s.auditStore.Record(r.Context(), event)
}

func pollerStatus(poller *update.Poller) any {
	if poller == nil {
		return map[string]any{
			"enabled": false,
			"running": false,
		}
	}
	return poller.Status()
}

func readRequiredString(values map[string]any, key string) (string, error) {
	raw, ok := values[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	str, ok := raw.(string)
	if !ok || strings.TrimSpace(str) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return str, nil
}

func readOptionalString(values map[string]any, key string) (string, bool) {
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	str, ok := raw.(string)
	if !ok {
		return "", false
	}
	return str, true
}

func parseTimestamp(v any) (time.Time, error) {
	switch t := v.(type) {
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return time.Time{}, err
		}
		return parsed.UTC(), nil
	case float64:
		return time.Unix(int64(t), 0).UTC(), nil
	case json.Number:
		i, err := strconv.ParseInt(string(t), 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(i, 0).UTC(), nil
	default:
		return time.Time{}, errors.New("unsupported timestamp type")
	}
}
