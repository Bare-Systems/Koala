package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const mcpProtocolVersion = "2025-11-25"

type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

func (s *Server) mcpTransport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("MCP-Protocol-Version", mcpProtocolVersion)
	if r.Method != http.MethodPost {
		s.writeJSONRPCError(w, http.StatusMethodNotAllowed, nil, -32000, "Method not allowed.")
		return
	}

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONRPCError(w, http.StatusBadRequest, nil, -32700, "Parse error")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		s.writeJSONRPCError(w, http.StatusBadRequest, req.ID, -32600, "Invalid Request")
		return
	}

	switch req.Method {
	case "initialize":
		s.writeJSONRPCResult(w, req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":        "koala",
				"title":       "Koala",
				"version":     s.serverVersion(),
				"description": "Koala exposes AI-first home-state tools over HTTP.",
			},
			"instructions": "Use tools to query zone state, package presence, cameras, and system health. The legacy /mcp/tools endpoints remain available for direct HTTP clients.",
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "ping":
		s.writeJSONRPCResult(w, req.ID, map[string]any{})
	case "tools/list":
		s.writeJSONRPCResult(w, req.ID, map[string]any{
			"tools": s.mcpTools(),
		})
	case "tools/call":
		name, _ := readOptionalString(req.Params, "name")
		if name == "" {
			s.writeJSONRPCError(w, http.StatusBadRequest, req.ID, -32602, "Invalid params")
			return
		}
		args := map[string]any{}
		if rawArgs, ok := req.Params["arguments"]; ok {
			asMap, ok := rawArgs.(map[string]any)
			if !ok {
				s.writeJSONRPCError(w, http.StatusBadRequest, req.ID, -32602, "Invalid params")
				return
			}
			args = asMap
		}
		toolResp, _ := s.callTool(name, args)
		respJSON, _ := json.Marshal(toolResp)
		s.writeJSONRPCResult(w, req.ID, map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": string(respJSON),
			}},
			"structuredContent": toolResp,
			"isError":           toolResp.Status == "error",
		})
	default:
		s.writeJSONRPCError(w, http.StatusNotFound, req.ID, -32601, "Method not found")
	}
}

func (s *Server) writeJSONRPCResult(w http.ResponseWriter, id any, result any) {
	s.writeJSON(w, http.StatusOK, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) writeJSONRPCError(w http.ResponseWriter, code int, id any, rpcCode int, message string) {
	s.writeJSON(w, code, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonRPCError{
			Code:    rpcCode,
			Message: message,
		},
	})
}

func (s *Server) serverVersion() string {
	if snap, ok := s.configSnapshot["service"].(map[string]any); ok {
		if version, ok := snap["version"].(string); ok && version != "" {
			return version
		}
	}
	return "0.1.0-dev"
}

func (s *Server) mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "koala.get_system_health",
			Title:       "System Health",
			Description: "Return Koala subsystem health and queue depth.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
		{
			Name:        "koala.list_cameras",
			Title:       "List Cameras",
			Description: "List configured cameras and their current status.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
		{
			Name:        "koala.get_zone_state",
			Title:       "Get Zone State",
			Description: "Return the latest zone state for tracked entities.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"zone_id": map[string]any{"type": "string"},
				},
				"required":             []string{"zone_id"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "koala.check_package_at_door",
			Title:       "Check Package At Door",
			Description: "Return the current package-present state for the front door.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"camera_id": map[string]any{"type": "string"},
				},
				"additionalProperties": false,
			},
		},
	}
}

func (s *Server) callTool(name string, input map[string]any) (ToolResponse, int) {
	switch name {
	case "koala.get_system_health":
		return s.getSystemHealthResponse(), http.StatusOK
	case "koala.list_cameras":
		return s.listCamerasResponse(), http.StatusOK
	case "koala.get_zone_state":
		return s.getZoneStateResponse(input)
	case "koala.check_package_at_door":
		return s.checkPackageAtDoorResponse(input)
	default:
		return ToolResponse{
			Status:      "error",
			Explanation: fmt.Sprintf("unknown tool %q", name),
			ErrorCode:   ErrCodeInvalidInput,
			NextAction:  "call tools/list to discover supported tool names",
		}, http.StatusNotFound
	}
}

func (s *Server) getSystemHealthResponse() ToolResponse {
	health := s.service.Health()
	status := "ok"
	explanation := "all subsystems healthy"
	if health.Status != "ok" {
		status = "degraded"
		explanation = "one or more subsystems degraded"
	}
	return ToolResponse{
		Status:      status,
		Explanation: explanation,
		RiskLevel:   RiskLow,
		Data:        health,
	}
}

func (s *Server) listCamerasResponse() ToolResponse {
	status := "ok"
	explanation := "camera registry loaded"
	if s.service.IsDegraded() {
		status = "degraded"
		explanation = "inference worker degraded; camera registry still available"
	}
	return ToolResponse{
		Status:      status,
		Explanation: explanation,
		RiskLevel:   RiskLow,
		Data: map[string]any{
			"cameras": s.service.Registry.List(),
		},
	}
}

func (s *Server) getZoneStateResponse(input map[string]any) (ToolResponse, int) {
	zoneID, err := readRequiredString(input, "zone_id")
	if err != nil {
		return ToolResponse{
			Status:      "error",
			Explanation: err.Error(),
			ErrorCode:   ErrCodeInvalidInput,
			NextAction:  "provide zone_id in the input object",
		}, http.StatusBadRequest
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
	return ToolResponse{
		Status:           status,
		FreshnessSeconds: zoneState.FreshnessSec,
		Explanation:      explanation,
		NextAction:       nextAction,
		RiskLevel:        RiskLow,
		Data: map[string]any{
			"zone_id":     zoneState.ZoneID,
			"observed_at": zoneState.ObservedAt.UTC().Format(time.RFC3339),
			"stale":       zoneState.Stale,
			"entities":    zoneState.Entities,
		},
	}, http.StatusOK
}

func (s *Server) checkPackageAtDoorResponse(input map[string]any) (ToolResponse, int) {
	cameraID, _ := readOptionalString(input, "camera_id")
	present, confidence, observedAt, err := s.service.DoorPackageState(cameraID)
	if err != nil {
		return ToolResponse{
			Status:      "error",
			Explanation: err.Error(),
			ErrorCode:   ErrCodeInvalidInput,
			NextAction:  "provide a valid camera_id or omit to use the default front door camera",
		}, http.StatusBadRequest
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
	return ToolResponse{
		Status:           status,
		FreshnessSeconds: freshness,
		Explanation:      explanation,
		NextAction:       nextAction,
		RiskLevel:        RiskLow,
		Data: map[string]any{
			"package_present": present,
			"confidence":      confidence,
			"observed_at":     observedAt.UTC().Format(time.RFC3339),
			"stale":           stale,
		},
	}, http.StatusOK
}
