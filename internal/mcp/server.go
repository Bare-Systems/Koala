package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/baresystems/koala/internal/audit"
	"github.com/baresystems/koala/internal/ingest"
	"github.com/baresystems/koala/internal/service"
	"github.com/baresystems/koala/internal/update"
)

const (
	// defaultMaxBodyBytes caps request bodies at 5 MiB. Large enough for a
	// base64-encoded JPEG frame; small enough to reject garbage payloads.
	defaultMaxBodyBytes int64 = 5 << 20 // 5 MiB
)

type Server struct {
	token          atomic.Pointer[string] // rotatable bearer token
	service        *service.Service
	updater        *update.Manager
	agent          update.Agent
	poller         *update.Poller
	ingest         *ingest.Manager
	auditStore     audit.Store
	rateLimiter    *RateLimiter
	maxBodyBytes   int64
	allowlist      *IPAllowlist
	configSnapshot map[string]any // redacted runtime config for /admin/config
}

func NewServer(token string, svc *service.Service, updater *update.Manager, agent update.Agent, poller *update.Poller, ingestManager *ingest.Manager, auditStore audit.Store) *Server {
	s := &Server{
		service:      svc,
		updater:      updater,
		agent:        agent,
		poller:       poller,
		ingest:       ingestManager,
		auditStore:   auditStore,
		rateLimiter:  NewRateLimiter(2, 20), // 2 req/s, burst 20
		maxBodyBytes: defaultMaxBodyBytes,
	}
	s.token.Store(&token)
	return s
}

// WithConfigSnapshot attaches a pre-built, redacted config snapshot that is
// served from /admin/config. Call this after NewServer in main.go.
func (s *Server) WithConfigSnapshot(snap map[string]any) *Server {
	s.configSnapshot = snap
	return s
}

// WithAllowlist restricts access to IPs matching the provided allowlist.
// Passing nil (or calling without this method) allows all IPs.
func (s *Server) WithAllowlist(al *IPAllowlist) *Server {
	s.allowlist = al
	return s
}

// WithRateLimiter replaces the default per-IP rate limiter. Pass nil to
// disable rate limiting entirely (useful in test environments).
func (s *Server) WithRateLimiter(rl *RateLimiter) *Server {
	s.rateLimiter = rl
	return s
}

// currentToken returns the current bearer token value.
func (s *Server) currentToken() string {
	if p := s.token.Load(); p != nil {
		return *p
	}
	return ""
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/tools/koala.get_system_health", s.wrapAuth(s.getSystemHealth))
	mux.HandleFunc("/mcp/tools/koala.list_cameras", s.wrapAuth(s.listCameras))
	mux.HandleFunc("/mcp/tools/koala.get_zone_state", s.wrapAuth(s.getZoneState))
	mux.HandleFunc("/mcp/tools/koala.check_package_at_door", s.wrapAuth(s.checkPackageAtDoor))
	mux.HandleFunc("/ingest/frame", s.wrapAuth(s.ingestFrame))
	mux.HandleFunc("/metrics", s.wrapAuth(s.getMetrics))
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/readyz", s.readyz)

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
	mux.HandleFunc("/admin/ingest/status", s.wrapAuth(s.ingestStatus))
	mux.HandleFunc("/admin/cameras/{id}/snapshot", s.cameraSnapshot)
	mux.HandleFunc("/admin/config", s.wrapAuth(s.getConfig))
	mux.HandleFunc("/admin/auth/rotate-token", s.wrapAuth(s.rotateToken))

	// Fleet device management.
	mux.HandleFunc("/admin/fleet/devices/register", s.wrapAuth(s.fleetRegisterDevice))
	mux.HandleFunc("/admin/fleet/devices/list", s.wrapAuth(s.fleetListDevices))
	mux.HandleFunc("/admin/fleet/devices/deregister", s.wrapAuth(s.fleetDeregisterDevice))

	mux.HandleFunc("/agent/updates/health", s.wrapAuth(s.agentHealth))
	mux.HandleFunc("/agent/updates/stage", s.wrapAuth(s.agentStage))
	mux.HandleFunc("/agent/updates/apply", s.wrapAuth(s.agentApply))
	mux.HandleFunc("/agent/updates/rollback", s.wrapAuth(s.agentRollback))
	return corsMiddleware(mux)
}

// corsMiddleware adds CORS headers so the live-ui (served on a different port)
// can call the orchestrator API from the browser.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) wrapAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			s.writeToolError(w, http.StatusMethodNotAllowed, ErrCodeInvalidInput, "method not allowed", "")
			return
		}

		// ── IP Allowlist ──────────────────────────────────────────────────
		if s.allowlist != nil && !s.allowlist.Allows(remoteIP(r)) {
			s.writeToolError(w, http.StatusForbidden, ErrCodeUnauthorized,
				"source IP not in allowlist",
				"contact your Koala administrator to add your IP address")
			return
		}

		// ── Rate limiting ─────────────────────────────────────────────────
		if s.rateLimiter != nil && !s.rateLimiter.Allow(remoteIP(r)) {
			s.writeToolError(w, http.StatusTooManyRequests, ErrCodeRateLimited,
				"rate limit exceeded; slow down and retry",
				"wait 1–2 seconds before retrying")
			return
		}

		// ── Auth ──────────────────────────────────────────────────────────
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != s.currentToken() {
			s.writeToolError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "missing or invalid bearer token", "provide a valid Authorization: Bearer <token> header")
			return
		}

		// ── Body size limit ───────────────────────────────────────────────
		if r.Body != nil && s.maxBodyBytes > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
		}

		// ── Request ID + structured log ───────────────────────────────────
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)
		log.Printf("service=mcp request_id=%s method=%s path=%s", reqID, r.Method, r.URL.Path)
		if s.service != nil {
			s.service.Metrics.ToolRequestTotal.Add(1)
		}

		next(w, r)
	}
}

// handleDecodeError inspects a decodeToolRequest error and writes the
// appropriate HTTP error response. Returns true if an error was written.
func (s *Server) handleDecodeError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if ptle, ok := err.(interface{ IsPayloadTooLarge() bool }); ok && ptle.IsPayloadTooLarge() {
		s.writeToolError(w, http.StatusRequestEntityTooLarge, ErrCodePayloadTooLarge,
			fmt.Sprintf("request body exceeds %d byte limit", s.maxBodyBytes),
			"reduce payload size or split into multiple requests")
		return true
	}
	s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
	return true
}

// writeToolError writes a structured JSON error response with the given HTTP status code.
func (s *Server) writeToolError(w http.ResponseWriter, httpCode int, errCode, explanation, nextAction string) {
	s.writeJSON(w, httpCode, ToolResponse{
		Status:      "error",
		Explanation: explanation,
		ErrorCode:   errCode,
		NextAction:  nextAction,
	})
}

// bodyTooLarge is the sentinel error type from http.MaxBytesReader.
type bodyTooLargeError interface{ Error() string }

func (s *Server) decodeToolRequest(r *http.Request) (ToolRequest, error) {
	if r.Method == http.MethodGet {
		return ToolRequest{Input: map[string]any{}}, nil
	}
	var req ToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// http.MaxBytesReader wraps its error; check the message directly.
		if strings.Contains(err.Error(), "http: request body too large") {
			return ToolRequest{}, &payloadTooLargeErr{}
		}
		return ToolRequest{}, err
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}
	return req, nil
}

type payloadTooLargeErr struct{}

func (e *payloadTooLargeErr) Error() string { return "request body too large" }
func (e *payloadTooLargeErr) IsPayloadTooLarge() bool { return true }

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
	if s.handleDecodeError(w, err) {
		return
	}

	zoneID, err := readRequiredString(req.Input, "zone_id")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "provide zone_id in the input object")
		return
	}
	zoneState := s.service.ZoneState(zoneID)
	status := "ok"
	explanation := "zone state available"
	nextAction := ""
	switch {
	case s.service.IsDegraded():
		status = "degraded"
		explanation = "inference unavailable; returning last-known zone state"
		nextAction = "call koala.get_system_health for details"
	case zoneState.Stale:
		status = "stale"
		explanation = fmt.Sprintf("no recent observations for zone %q; ensure camera is active and streaming", zoneID)
		nextAction = "call koala.get_system_health to check camera and inference status"
	}
	resp := map[string]any{
		"status":            status,
		"freshness_seconds": zoneState.FreshnessSec,
		"explanation":       explanation,
		"risk_level":        RiskLow,
		"data": map[string]any{
			"zone_id":     zoneState.ZoneID,
			"observed_at": zoneState.ObservedAt.UTC().Format(time.RFC3339),
			"stale":       zoneState.Stale,
			"entities":    zoneState.Entities,
		},
	}
	if nextAction != "" {
		resp["next_action"] = nextAction
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) checkPackageAtDoor(w http.ResponseWriter, r *http.Request) {
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	cameraID, _ := readOptionalString(req.Input, "camera_id")
	present, confidence, observedAt, err := s.service.DoorPackageState(cameraID)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "provide a valid camera_id or omit to use the default front door camera")
		return
	}
	stale := observedAt.IsZero()
	status := "ok"
	explanation := "front door package state computed"
	nextAction := ""
	switch {
	case s.service.IsDegraded():
		status = "degraded"
		explanation = "inference unavailable; returning last-known package state"
		nextAction = "call koala.get_system_health for details"
	case stale:
		status = "stale"
		explanation = "no observations yet for the front door camera; state is unknown"
		nextAction = "ensure the front door camera is online and check koala.get_system_health"
	}
	freshness := int64(0)
	if !observedAt.IsZero() {
		freshness = int64(time.Since(observedAt).Seconds())
	}
	resp := map[string]any{
		"status":            status,
		"freshness_seconds": freshness,
		"explanation":       explanation,
		"risk_level":        RiskLow,
		"data": map[string]any{
			"package_present": present,
			"confidence":      confidence,
			"observed_at":     observedAt.UTC().Format(time.RFC3339),
			"stale":           stale,
		},
	}
	if nextAction != "" {
		resp["next_action"] = nextAction
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) ingestFrame(w http.ResponseWriter, r *http.Request) {
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	cameraID, err := readRequiredString(req.Input, "camera_id")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "provide camera_id in the input object")
		return
	}
	zoneID, err := readRequiredString(req.Input, "zone_id")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "provide zone_id in the input object")
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
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
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

func (s *Server) ingestStatus(w http.ResponseWriter, _ *http.Request) {
	if s.ingest == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "ingest workers are not configured", "enable runtime.enable_stream_workers in the server config")
		return
	}
	status := s.ingest.Status()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "ingest worker status snapshot",
		"data":        status,
	})
}

// cameraSnapshot serves the latest captured JPEG frame for a camera.
// Auth is accepted via Authorization: Bearer header or ?token= query param
// so that <img src="..."> tags in the live UI can load frames directly.
func (s *Server) cameraSnapshot(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" || token != s.currentToken() {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.ingest == nil {
		http.Error(w, "ingest not configured", http.StatusNotImplemented)
		return
	}
	id := r.PathValue("id")
	frame, ok := s.ingest.LatestFrame(id)
	if !ok {
		http.Error(w, "no frame available yet", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Content-Length", strconv.Itoa(len(frame)))
	_, _ = w.Write(frame)
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	manifest, deviceIDs, err := parseUpdateInput(req.Input)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	result, err := s.updater.Check(manifest, deviceIDs)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
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
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	manifest, deviceIDs, err := parseUpdateInput(req.Input)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	devices, err := s.updater.Stage(manifest, deviceIDs)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update staged",
		"data": map[string]any{
			"devices": devices,
		},
	})
	s.recordAuditEvent(r, audit.Event{
		Category:  "update",
		EventType: "update_staged",
		Severity:  "info",
		Message:   "update staged for devices",
		Payload:   map[string]any{"version": manifest.Version, "device_count": len(devices)},
	})
}

func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	deviceIDs, err := readDeviceIDs(req.Input)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	devices, err := s.updater.Apply(deviceIDs)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update applied",
		"data": map[string]any{
			"devices": devices,
		},
	})
	s.recordAuditEvent(r, audit.Event{
		Category:  "update",
		EventType: "update_applied",
		Severity:  "info",
		Message:   "update applied to devices",
		Payload:   map[string]any{"device_count": len(devices)},
	})
}

func (s *Server) updateRollback(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	deviceIDs, err := readDeviceIDs(req.Input)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	reason, _ := readOptionalString(req.Input, "reason")
	devices, err := s.updater.Rollback(deviceIDs, reason)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "update rolled back",
		"data": map[string]any{
			"devices": devices,
		},
	})
	s.recordAuditEvent(r, audit.Event{
		Category:  "update",
		EventType: "update_rolled_back",
		Severity:  "info",
		Message:   "update rolled back for devices",
		Payload:   map[string]any{"reason": reason, "device_count": len(devices)},
	})
}

func (s *Server) updateSecurity(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	health, err := s.agent.Health(r.Context())
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
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
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "audit store is not configured", "set update.audit_db_path in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	category, _ := readOptionalString(req.Input, "category")
	limit, err := readOptionalInt(req.Input, "limit")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	events, err := s.auditStore.List(r.Context(), audit.ListOptions{Category: category, Limit: limit})
	if err != nil {
		s.writeToolError(w, http.StatusInternalServerError, ErrCodeInternalError, err.Error(), "check koala.get_system_health for service state")
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
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	manifest, deviceIDs, err := parseUpdateInput(req.Input)
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	modeRaw, _ := readOptionalString(req.Input, "mode")
	batchSize, err := readOptionalInt(req.Input, "batch_size")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	maxFailures, err := readOptionalInt(req.Input, "max_failures")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	pauseMs, err := readOptionalInt(req.Input, "pause_between_batches_ms")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
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
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
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
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	rolloutID, err := readRequiredString(req.Input, "rollout_id")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	rollout, ok := s.updater.GetRollout(rolloutID)
	if !ok {
		s.writeToolError(w, http.StatusNotFound, ErrCodeInvalidInput, "rollout not found", "use /admin/updates/rollouts/list to get valid rollout IDs")
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
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
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
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "agent not configured", "enable update.enabled in the server config")
		return
	}
	health, err := s.agent.Health(r.Context())
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
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
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "agent not configured", "enable update.enabled in the server config")
		return
	}
	var payload struct {
		Manifest update.Manifest `json:"manifest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	if err := s.agent.Stage(r.Context(), payload.Manifest); err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "agent staged update",
	})
}

func (s *Server) agentApply(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "agent not configured", "enable update.enabled in the server config")
		return
	}
	if err := s.agent.Apply(r.Context()); err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "agent applied update; watchdog monitoring health",
	})
	// Launch a background watchdog: polls agent health and auto-rolls back if
	// the agent does not reach "healthy" within two minutes.
	go func() {
		result := update.Watch(context.Background(), s.agent, 2*time.Minute, 5*time.Second, true)
		if result.RollbackTriggered && s.auditStore != nil {
			_ = s.auditStore.Record(context.Background(), audit.Event{
				Category:  "update",
				EventType: "watchdog_rollback",
				Severity:  "warning",
				Message:   "watchdog triggered automatic rollback after apply",
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				Payload: map[string]any{
					"status":      result.Status,
					"check_count": result.CheckCount,
					"elapsed_ms":  result.Elapsed.Milliseconds(),
				},
			})
		}
	}()
}

func (s *Server) agentRollback(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "agent not configured", "enable update.enabled in the server config")
		return
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if err := s.agent.Rollback(r.Context(), payload.Reason); err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "agent rolled back update",
	})
}

// ─── Fleet device management endpoints ────────────────────────────────────────

func (s *Server) fleetRegisterDevice(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	deviceID, err := readRequiredString(req.Input, "device_id")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "provide device_id in the input object")
		return
	}
	address, _ := readOptionalString(req.Input, "address")
	currentVersion, _ := readOptionalString(req.Input, "current_version")
	s.updater.RegisterDevice(deviceID, address, currentVersion)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "device registered in fleet",
		"data": map[string]any{
			"device_id": deviceID,
		},
	})
}

func (s *Server) fleetListDevices(w http.ResponseWriter, _ *http.Request) {
	if s.updater == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	devices := s.updater.Status()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "fleet device list",
		"data": map[string]any{
			"devices": devices,
			"count":   len(devices),
		},
	})
}

func (s *Server) fleetDeregisterDevice(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		s.writeToolError(w, http.StatusNotImplemented, ErrCodeUnavailable, "updates are not configured", "enable update.enabled in the server config")
		return
	}
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	deviceID, err := readRequiredString(req.Input, "device_id")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "provide device_id in the input object")
		return
	}
	if err := s.updater.DeregisterDevice(deviceID); err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(), "use /admin/fleet/devices/list to verify the device_id")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "device deregistered from fleet",
		"data": map[string]any{
			"device_id": deviceID,
		},
	})
}

// ─── Observability endpoints ──────────────────────────────────────────────────

func (s *Server) getMetrics(w http.ResponseWriter, _ *http.Request) {
	snap := s.service.Metrics.Snapshot()
	snap["ingest_queue_depth"] = s.service.QueueDepth()
	snap["ingest_queue_capacity"] = s.service.QueueCapacity()
	snap["camera_count"] = len(s.service.Registry.List())
	availableCount := 0
	for _, cam := range s.service.Registry.List() {
		if cam.Status == "available" {
			availableCount++
		}
	}
	snap["camera_available_count"] = availableCount
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "runtime metrics snapshot",
		"data":        snap,
	})
}

// rotateToken atomically replaces the bearer token. The caller must present the
// current valid token in the Authorization header (as with all auth-gated
// endpoints). The new token is applied immediately with no restart required.
func (s *Server) rotateToken(w http.ResponseWriter, r *http.Request) {
	req, err := s.decodeToolRequest(r)
	if s.handleDecodeError(w, err) {
		return
	}
	newToken, err := readRequiredString(req.Input, "new_token")
	if err != nil {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput, err.Error(),
			"provide new_token in the input object")
		return
	}
	if len(newToken) < 32 {
		s.writeToolError(w, http.StatusBadRequest, ErrCodeInvalidInput,
			"new_token must be at least 32 characters",
			"generate a cryptographically random token of at least 32 characters")
		return
	}
	s.token.Store(&newToken)
	reqID, _ := w.Header()["X-Request-Id"]
	log.Printf("service=mcp event=token_rotated request_id=%v", reqID)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "bearer token rotated; update your client configuration immediately",
	})
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	snap := s.configSnapshot
	if snap == nil {
		snap = map[string]any{"note": "no config snapshot registered"}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"explanation": "current runtime configuration snapshot (sensitive fields redacted)",
		"data":        snap,
	})
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.service != nil && s.service.IsDegraded() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready","reason":"inference_degraded"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
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
