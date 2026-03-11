package mcp

// Error codes used in ToolResponse.ErrorCode.
// These are the only valid error codes in Koala MCP tool responses.
const (
	// ErrCodeInvalidInput is returned when the caller's request is malformed or
	// missing required fields.
	ErrCodeInvalidInput = "invalid_input"
	// ErrCodeUnauthorized is returned when the bearer token is missing or invalid.
	ErrCodeUnauthorized = "unauthorized"
	// ErrCodeUnavailable is returned when a required subsystem is unavailable.
	ErrCodeUnavailable = "unavailable"
	// ErrCodeInternalError is returned for unexpected server-side failures.
	ErrCodeInternalError = "internal_error"
)

// ToolRequest is the JSON body sent to any MCP tool endpoint.
type ToolRequest struct {
	Input map[string]any `json:"input"`
}

// Risk levels for action classification (BearClaw policy hook).
const (
	RiskLow    = "low"    // read-only queries; no side-effects
	RiskMedium = "medium" // writes to internal state; no external effects
	RiskHigh   = "high"   // external side-effects (updates, commands)
)

// ToolResponse is the canonical JSON envelope returned by all MCP tool endpoints.
// Agents MUST check the Status field before consuming Data.
//
// Status values:
//   - "ok"       – request succeeded, Data is populated.
//   - "degraded" – request partially succeeded; Data may contain stale values.
//   - "stale"    – no recent observations; Data reflects last-known state.
//   - "error"    – request failed; ErrorCode and Explanation describe the failure.
type ToolResponse struct {
	// Status is always present: "ok", "degraded", "stale", or "error".
	Status string `json:"status"`
	// FreshnessSeconds is seconds since the underlying observation was recorded.
	FreshnessSeconds int64 `json:"freshness_seconds,omitempty"`
	// Explanation is a human-readable sentence describing the status.
	Explanation string `json:"explanation"`
	// ErrorCode is set on error responses. One of the ErrCode* constants.
	ErrorCode string `json:"error_code,omitempty"`
	// NextAction is an optional hint for the caller about what to try next.
	NextAction string `json:"next_action,omitempty"`
	// RiskLevel classifies the action risk for policy enforcement (BearClaw hook).
	// One of RiskLow, RiskMedium, RiskHigh. Present on non-error responses.
	RiskLevel string `json:"risk_level,omitempty"`
	// Data holds tool-specific response payload. Absent on error responses.
	Data any `json:"data,omitempty"`
}
