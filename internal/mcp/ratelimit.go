package mcp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter is a per-IP token-bucket rate limiter safe for concurrent use.
// Each IP gets its own bucket that refills at `rate` tokens/second up to `cap`
// tokens. Idle buckets are purged after `idleTTL` to bound memory growth.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64       // tokens added per second
	cap     float64       // maximum burst capacity
	idleTTL time.Duration // how long before an idle bucket is evicted
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter returns a RateLimiter. rate is tokens/second; cap is the
// maximum burst. A typical setting for the MCP server is rate=2, cap=20.
func NewRateLimiter(rate, cap float64) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		cap:     cap,
		idleTTL: 5 * time.Minute,
	}
	return rl
}

// Allow returns true if the request from ip is within the rate limit.
// It refills the bucket by the elapsed time since the last call, then
// consumes one token.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[ip]
	if !ok {
		// New IP: start with a full bucket minus the current request.
		rl.buckets[ip] = &bucket{tokens: rl.cap - 1, last: now}
		return true
	}

	// Refill tokens proportional to elapsed time.
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(rl.cap, b.tokens+elapsed*rl.rate)
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Purge removes idle buckets that have not been accessed in idleTTL. Call
// periodically (e.g. in a goroutine) to prevent unbounded map growth.
func (rl *RateLimiter) Purge() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.idleTTL)
	for ip, b := range rl.buckets {
		if b.last.Before(cutoff) {
			delete(rl.buckets, ip)
		}
	}
}

// remoteIP extracts the IP portion from r.RemoteAddr, falling back to the
// full address string on parse error.
func remoteIP(r *http.Request) string {
	// Honour X-Forwarded-For for reverse-proxy deployments.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First address in the list is the originating client.
		if idx := len(xff); idx > 0 {
			ip := xff
			for i := range xff {
				if xff[i] == ',' {
					ip = xff[:i]
					break
				}
			}
			if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
				return parsed.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
