package update

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHTTPExecutor_CallsAgentEndpoints(t *testing.T) {
	calls := []string{}
	executor := &HTTPExecutor{
		token: "test-token",
		client: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			calls = append(calls, req.URL.Path)
			if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("unexpected auth header: %s", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	device := Device{ID: "dev1", Address: "http://device.local:6705"}
	manifest := Manifest{Version: "0.2.0", ArtifactURL: "http://x", SHA256: strings.Repeat("a", 64), Signature: "sig"}

	if err := executor.Stage(context.Background(), device, manifest); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if err := executor.Apply(context.Background(), device); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := executor.Rollback(context.Background(), device, "test"); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	if calls[0] != "/agent/updates/stage" || calls[1] != "/agent/updates/apply" || calls[2] != "/agent/updates/rollback" {
		t.Fatalf("unexpected paths: %#v", calls)
	}
}
