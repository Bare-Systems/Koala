package mcp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Bare-Systems/Koala/internal/camera"
	"github.com/Bare-Systems/Koala/internal/service"
	"github.com/Bare-Systems/Koala/internal/state"
)

func newRateLimitTestServer(t *testing.T) *Server {
	t.Helper()
	registry := camera.NewRegistry([]camera.Camera{
		{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true},
	})
	agg := state.NewAggregator(time.Minute)
	svc := service.New(registry, agg, fakeInferenceClient{}, 2)
	return NewServer("test-token", svc, nil, nil, nil, nil, nil)
}

// ─── Token bucket unit tests ───────────────────────────────────────────────────

func TestRateLimiter_BurstAllowed(t *testing.T) {
	rl := NewRateLimiter(1, 3) // 1 token/s, burst 3
	ip := "10.0.0.1"
	for i := range 3 {
		if !rl.Allow(ip) {
			t.Fatalf("request %d should be allowed within burst", i+1)
		}
	}
}

func TestRateLimiter_BlockedAfterBurst(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	ip := "10.0.0.1"
	for range 3 {
		rl.Allow(ip)
	}
	if rl.Allow(ip) {
		t.Fatal("4th request should be blocked after burst is exhausted")
	}
}

func TestRateLimiter_NewIPStartsFull(t *testing.T) {
	rl := NewRateLimiter(1, 5)
	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		if !rl.Allow(ip) {
			t.Fatalf("first request from new IP %s should be allowed", ip)
		}
	}
}

func TestRateLimiter_Purge(t *testing.T) {
	rl := NewRateLimiter(1, 5)
	rl.idleTTL = 0 // expire immediately
	rl.Allow("10.0.0.1")
	rl.Purge()
	rl.mu.Lock()
	remaining := len(rl.buckets)
	rl.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected 0 buckets after purge, got %d", remaining)
	}
}

// ─── HTTP-level rate limit enforcement ────────────────────────────────────────

func TestSecurity_RateLimitEndpoint_Returns429(t *testing.T) {
	s := newRateLimitTestServer(t)
	// Burst=1, very slow refill → second request will be blocked.
	s.rateLimiter = NewRateLimiter(0.001, 1)
	h := s.Routes()

	doReq := func() int {
		req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res.Code
	}

	if first := doReq(); first != http.StatusOK {
		t.Fatalf("first request should succeed, got %d", first)
	}
	if second := doReq(); second != http.StatusTooManyRequests {
		t.Fatalf("second request should be rate-limited, got %d", second)
	}
}

func TestSecurity_RateLimitResponse_IsValidToolResponse(t *testing.T) {
	s := newRateLimitTestServer(t)
	s.rateLimiter = NewRateLimiter(0.001, 1)
	h := s.Routes()

	// Exhaust the burst.
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	h.ServeHTTP(httptest.NewRecorder(), req)

	// Second call should return a well-formed error ToolResponse.
	req = httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	tr := assertErrorResponse(t, res, http.StatusTooManyRequests, ErrCodeRateLimited)
	if tr.NextAction == "" {
		t.Error("rate-limited response must include next_action guidance")
	}
}

// ─── Input size limits ─────────────────────────────────────────────────────────

func TestSecurity_OversizedBody_Returns413(t *testing.T) {
	s := newRateLimitTestServer(t)
	s.maxBodyBytes = 32 // tiny limit to trigger quickly
	h := s.Routes()

	big := `{"input":{"zone_id":"` + strings.Repeat("x", 64) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_zone_state",
		bytes.NewBufferString(big))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	assertErrorResponse(t, res, http.StatusRequestEntityTooLarge, ErrCodePayloadTooLarge)
}

func TestSecurity_NormalSizedBody_Passes(t *testing.T) {
	s := newRateLimitTestServer(t)
	h := s.Routes()
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.list_cameras",
		bytes.NewBufferString(`{"input":{}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("normal request should succeed, got %d: %s", res.Code, res.Body.String())
	}
}

func TestSecurity_MalformedJSON_Returns400(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_zone_state",
		bytes.NewBufferString(`{not valid json`))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	assertErrorResponse(t, res, http.StatusBadRequest, ErrCodeInvalidInput)
}
