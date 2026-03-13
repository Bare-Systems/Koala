package mcp

import (
	"fmt"
	"net"
	"strings"
)

// IPAllowlist gates access by source IP. An empty allowlist allows all IPs.
// Supports individual IPs (e.g. "192.168.1.10") and CIDR ranges
// (e.g. "10.0.0.0/8"). Call NewIPAllowlist to construct from config strings.
type IPAllowlist struct {
	entries []*net.IPNet
}

// NewIPAllowlist parses a slice of IP or CIDR strings into an IPAllowlist.
// Returns an error if any entry cannot be parsed.
func NewIPAllowlist(cidrs []string) (*IPAllowlist, error) {
	al := &IPAllowlist{}
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// If it looks like a plain IP (no slash), append /32 or /128.
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP address in allowlist: %q", raw)
			}
			if ip.To4() != nil {
				raw += "/32"
			} else {
				raw += "/128"
			}
		}
		_, cidr, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR in allowlist: %q: %w", raw, err)
		}
		al.entries = append(al.entries, cidr)
	}
	return al, nil
}

// Allows returns true if ip is permitted. An empty allowlist always returns
// true (open access — operator must explicitly configure restrictions).
func (al *IPAllowlist) Allows(ip string) bool {
	if al == nil || len(al.entries) == 0 {
		return true
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	for _, cidr := range al.entries {
		if cidr.Contains(parsed) {
			return true
		}
	}
	return false
}
