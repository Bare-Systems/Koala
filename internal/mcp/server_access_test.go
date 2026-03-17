package mcp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/baresystems/koala/internal/camera"
	"github.com/baresystems/koala/internal/service"
	"github.com/baresystems/koala/internal/state"
)

// ─── IP Allowlist unit tests ───────────────────────────────────────────────────

func TestIPAllowlist_EmptyAllowsAll(t *testing.T) {
	al, err := NewIPAllowlist(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !al.Allows("1.2.3.4") {
		t.Fatal("empty allowlist should allow all IPs")
	}
}

func TestIPAllowlist_SpecificIP(t *testing.T) {
	al, err := NewIPAllowlist([]string{"192.168.1.5"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !al.Allows("192.168.1.5") {
		t.Fatal("exact IP should be allowed")
	}
	if al.Allows("192.168.1.6") {
		t.Fatal("different IP should not be allowed")
	}
}

func TestIPAllowlist_CIDRRange(t *testing.T) {
	al, err := NewIPAllowlist([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !al.Allows("10.1.2.3") {
		t.Fatal("IP within CIDR should be allowed")
	}
	if al.Allows("11.0.0.1") {
		t.Fatal("IP outside CIDR should not be allowed")
	}
}

func TestIPAllowlist_MultipleEntries(t *testing.T) {
	al, err := NewIPAllowlist([]string{"192.168.1.0/24", "10.0.0.1"})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, ip := range []string{"192.168.1.1", "192.168.1.254", "10.0.0.1"} {
		if !al.Allows(ip) {
			t.Errorf("expected %s to be allowed", ip)
		}
	}
	for _, ip := range []string{"192.168.2.1", "10.0.0.2"} {
		if al.Allows(ip) {
			t.Errorf("expected %s to be blocked", ip)
		}
	}
}

func TestIPAllowlist_InvalidEntry(t *testing.T) {
	if _, err := NewIPAllowlist([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected error for invalid IP")
	}
}

// ─── HTTP allowlist enforcement ────────────────────────────────────────────────

func newAccessTestServer(t *testing.T) *Server {
	t.Helper()
	registry := camera.NewRegistry([]camera.Camera{
		{ID: "cam_front_1", ZoneID: "front_door", FrontDoor: true},
	})
	agg := state.NewAggregator(time.Minute)
	svc := service.New(registry, agg, fakeInferenceClient{}, 2)
	return NewServer("test-token", svc, nil, nil, nil, nil, nil)
}

func TestSecurity_Allowlist_BlocksUnauthorizedIP(t *testing.T) {
	s := newAccessTestServer(t)
	al, _ := NewIPAllowlist([]string{"192.168.100.0/24"})
	s = s.WithAllowlist(al)
	h := s.Routes()

	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.RemoteAddr = "10.0.0.1:1234" // not in allowlist
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked IP, got %d", res.Code)
	}
}

func TestSecurity_Allowlist_AllowsAuthorizedIP(t *testing.T) {
	s := newAccessTestServer(t)
	al, _ := NewIPAllowlist([]string{"192.168.100.0/24"})
	s = s.WithAllowlist(al)
	h := s.Routes()

	req := httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.RemoteAddr = "192.168.100.5:1234" // in allowlist
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for allowed IP, got %d: %s", res.Code, res.Body.String())
	}
}

// ─── Token rotation tests ──────────────────────────────────────────────────────

func TestTokenRotation_RequiresLongToken(t *testing.T) {
	_, h := newTestServer(t)
	body := `{"input":{"new_token":"short"}}`
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/rotate-token",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	assertErrorResponse(t, res, http.StatusBadRequest, ErrCodeInvalidInput)
}

func TestTokenRotation_RotatesAndEnforcesNewToken(t *testing.T) {
	s := newAccessTestServer(t)
	h := s.Routes()

	newTok := "super-secret-new-token-that-is-long-enough-32chars"

	// Rotate with current token.
	body := `{"input":{"new_token":"` + newTok + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/rotate-token",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("rotate failed: %d %s", res.Code, res.Body.String())
	}

	// Old token should now be rejected.
	req = httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("old token should be rejected after rotation, got %d", res.Code)
	}

	// New token should be accepted.
	req = httptest.NewRequest(http.MethodPost, "/mcp/tools/koala.get_system_health", nil)
	req.Header.Set("Authorization", "Bearer "+newTok)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("new token should be accepted, got %d: %s", res.Code, res.Body.String())
	}
}
