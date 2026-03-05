package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/barelabs/koala/internal/service"
)

type Server struct {
	token   string
	service *service.Service
}

func NewServer(token string, svc *service.Service) *Server {
	return &Server{token: token, service: svc}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/tools/koala.get_system_health", s.wrapAuth(s.getSystemHealth))
	mux.HandleFunc("/mcp/tools/koala.list_cameras", s.wrapAuth(s.listCameras))
	mux.HandleFunc("/mcp/tools/koala.get_zone_state", s.wrapAuth(s.getZoneState))
	mux.HandleFunc("/mcp/tools/koala.check_package_at_door", s.wrapAuth(s.checkPackageAtDoor))
	mux.HandleFunc("/ingest/frame", s.wrapAuth(s.ingestFrame))
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
