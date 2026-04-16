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

func (s *Server) findMCPTool(name string) (mcpTool, bool) {
	for _, tool := range s.mcpTools() {
		if tool.Name == name {
			return tool, true
		}
	}
	return mcpTool{}, false
}

func schemaPath(path, child string) string {
	if path == "$" {
		return "$." + child
	}
	return path + "." + child
}

func matchesSchemaType(expected string, value any) bool {
	switch expected {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "number":
		_, ok := value.(float64)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func schemaNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func validateSchemaValue(schema map[string]any, value any, path string) error {
	if rawType, ok := schema["type"]; ok {
		switch typed := rawType.(type) {
		case string:
			if !matchesSchemaType(typed, value) {
				return fmt.Errorf("%s has the wrong JSON type", path)
			}
		case []any:
			matched := false
			for _, candidate := range typed {
				expected, ok := candidate.(string)
				if ok && matchesSchemaType(expected, value) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("%s has the wrong JSON type", path)
			}
		default:
			return fmt.Errorf("%s has an invalid tool schema", path)
		}
	}

	if rawAnyOf, ok := schema["anyOf"]; ok {
		candidates, ok := rawAnyOf.([]any)
		if !ok {
			return fmt.Errorf("%s has an invalid tool schema", path)
		}
		var lastErr error
		for _, candidate := range candidates {
			candidateSchema, ok := candidate.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			if err := validateSchemaValue(candidateSchema, value, path); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("%s does not match any allowed input shape", path)
	}

	if rawEnum, ok := schema["enum"]; ok {
		switch candidates := rawEnum.(type) {
		case []any:
			for _, candidate := range candidates {
				if fmt.Sprintf("%v", candidate) == fmt.Sprintf("%v", value) {
					goto enumMatched
				}
			}
		case []string:
			for _, candidate := range candidates {
				if candidate == fmt.Sprintf("%v", value) {
					goto enumMatched
				}
			}
		default:
			return fmt.Errorf("%s has an invalid tool schema", path)
		}
		return fmt.Errorf("%s must be one of the allowed enum values", path)
	enumMatched:
	}

	switch typed := value.(type) {
	case map[string]any:
		properties := map[string]any{}
		if rawProperties, ok := schema["properties"]; ok {
			asMap, ok := rawProperties.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			properties = asMap
		}

		if rawRequired, ok := schema["required"]; ok {
			switch required := rawRequired.(type) {
			case []any:
				for _, item := range required {
					key, ok := item.(string)
					if !ok {
						return fmt.Errorf("%s has an invalid tool schema", path)
					}
					if _, present := typed[key]; !present {
						return fmt.Errorf("%s.%s is required", path, key)
					}
				}
			case []string:
				for _, key := range required {
					if _, present := typed[key]; !present {
						return fmt.Errorf("%s.%s is required", path, key)
					}
				}
			default:
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
		}

		additionalProperties := true
		if rawAdditional, ok := schema["additionalProperties"]; ok {
			asBool, ok := rawAdditional.(bool)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			additionalProperties = asBool
		}
		if !additionalProperties {
			for key := range typed {
				if _, ok := properties[key]; !ok {
					return fmt.Errorf("%s contains unsupported field %q", path, key)
				}
			}
		}

		for key, childValue := range typed {
			childSchemaAny, ok := properties[key]
			if !ok {
				continue
			}
			childSchema, ok := childSchemaAny.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			if err := validateSchemaValue(childSchema, childValue, schemaPath(path, key)); err != nil {
				return err
			}
		}
	case []any:
		if rawMinItems, ok := schema["minItems"]; ok {
			minItems, ok := schemaNumber(rawMinItems)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			if len(typed) < int(minItems) {
				return fmt.Errorf("%s must contain at least %d item(s)", path, int(minItems))
			}
		}
		if rawItems, ok := schema["items"]; ok {
			itemSchema, ok := rawItems.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			for idx, item := range typed {
				if err := validateSchemaValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, idx)); err != nil {
					return err
				}
			}
		}
	case string:
		if rawMinLength, ok := schema["minLength"]; ok {
			minLength, ok := schemaNumber(rawMinLength)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			if len(typed) < int(minLength) {
				return fmt.Errorf("%s must not be empty", path)
			}
		}
	case float64:
		if rawMinimum, ok := schema["minimum"]; ok {
			minimum, ok := schemaNumber(rawMinimum)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			if typed < minimum {
				return fmt.Errorf("%s must be >= %v", path, minimum)
			}
		}
		if rawMaximum, ok := schema["maximum"]; ok {
			maximum, ok := schemaNumber(rawMaximum)
			if !ok {
				return fmt.Errorf("%s has an invalid tool schema", path)
			}
			if typed > maximum {
				return fmt.Errorf("%s must be <= %v", path, maximum)
			}
		}
	}

	return nil
}

func validateMCPToolInput(tool mcpTool, input map[string]any) error {
	if tool.InputSchema == nil {
		return nil
	}
	return validateSchemaValue(tool.InputSchema, input, "$")
}

func (s *Server) callTool(name string, input map[string]any) (ToolResponse, int) {
	tool, ok := s.findMCPTool(name)
	if !ok {
		return ToolResponse{
			Status:      "error",
			Explanation: fmt.Sprintf("unknown tool %q", name),
			ErrorCode:   ErrCodeInvalidInput,
			NextAction:  "call tools/list to discover supported tool names",
		}, http.StatusBadRequest
	}
	if err := validateMCPToolInput(tool, input); err != nil {
		return ToolResponse{
			Status:      "error",
			Explanation: err.Error(),
			ErrorCode:   ErrCodeInvalidInput,
			NextAction:  "call tools/list to inspect the expected inputSchema",
		}, http.StatusBadRequest
	}

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
		}, http.StatusBadRequest
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
