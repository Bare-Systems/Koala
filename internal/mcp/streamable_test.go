package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPTransport_RequiresAuth(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestMCPTransport_Initialize(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"bearclaw","version":"1.0.0"}}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("MCP-Protocol-Version"); got != mcpProtocolVersion {
		t.Fatalf("expected protocol header %q, got %q", mcpProtocolVersion, got)
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	result := payload["result"].(map[string]any)
	if result["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("expected protocolVersion %q, got %v", mcpProtocolVersion, result["protocolVersion"])
	}
	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "koala" {
		t.Fatalf("expected server name koala, got %v", serverInfo["name"])
	}
}

func TestMCPTransport_ToolsList(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	result := payload["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	for _, rawTool := range tools {
		tool := rawTool.(map[string]any)
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("expected inputSchema object for tool %v", tool["name"])
		}
		if schema["type"] != "object" {
			t.Fatalf("expected tool %v schema type object, got %v", tool["name"], schema["type"])
		}
	}
}

func TestMCPTransport_ToolsCall(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"koala.get_zone_state","arguments":{"zone_id":"front_door"}}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tools/call response: %v", err)
	}
	result := payload["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("expected isError=false, got %v", result["isError"])
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["status"] == "" {
		t.Fatalf("expected structuredContent.status to be present")
	}
}

func TestMCPTransport_ToolsCallRejectsInvalidArgs(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"koala.get_zone_state","arguments":{}}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code == http.StatusInternalServerError {
		t.Fatalf("expected MCP invalid input response, got 500 body=%s", res.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tools/call invalid args response: %v", err)
	}
	result := payload["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected isError=true, got %v", result["isError"])
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["error_code"] != ErrCodeInvalidInput {
		t.Fatalf("expected error_code=%q, got %v", ErrCodeInvalidInput, structured["error_code"])
	}
}
