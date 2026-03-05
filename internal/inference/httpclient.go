package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"
)

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 1500 * time.Millisecond},
	}
}

func (c *HTTPClient) AnalyzeFrame(ctx context.Context, req FrameRequest) (FrameResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return FrameResponse{}, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/analyze/frame", body)
	if err != nil {
		return FrameResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return FrameResponse{}, readBodyErr("analyze", resp)
	}
	var out FrameResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return FrameResponse{}, err
	}
	return out, nil
}

func (c *HTTPClient) WorkerHealth(ctx context.Context) (HealthResponse, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/health", nil)
	if err != nil {
		return HealthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return HealthResponse{}, readBodyErr("health", resp)
	}
	var out HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return HealthResponse{}, err
	}
	return out, nil
}

func (c *HTTPClient) do(ctx context.Context, method string, route string, body []byte) (*http.Response, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse worker url: %w", err)
	}
	base.Path = path.Join(base.Path, route)

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, base.String(), reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.client.Do(req)
}

func readBodyErr(name string, resp *http.Response) error {
	payload, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("%s failed: status=%d body=%s", name, resp.StatusCode, string(payload))
}
