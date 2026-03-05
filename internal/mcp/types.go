package mcp

type ToolRequest struct {
	Input map[string]any `json:"input"`
}

type ToolResponse struct {
	Status           string `json:"status"`
	FreshnessSeconds int64  `json:"freshness_seconds,omitempty"`
	Explanation      string `json:"explanation"`
	Data             any    `json:"data,omitempty"`
}
