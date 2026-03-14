package update

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// watchAgent is a configurable Agent stub for watchdog tests.
type watchAgent struct {
	// responses is a list of status strings returned in order.
	// The last element is repeated once exhausted.
	responses []string
	callCount atomic.Int32
	rolled    atomic.Bool
}

func (a *watchAgent) Stage(_ context.Context, _ Manifest) error { return nil }
func (a *watchAgent) Apply(_ context.Context) error             { return nil }

func (a *watchAgent) Rollback(_ context.Context, _ string) error {
	a.rolled.Store(true)
	return nil
}

func (a *watchAgent) Health(_ context.Context) (map[string]any, error) {
	idx := int(a.callCount.Add(1)) - 1
	if idx >= len(a.responses) {
		idx = len(a.responses) - 1
	}
	return map[string]any{"status": a.responses[idx]}, nil
}

func TestWatch_ImmediatelyHealthy(t *testing.T) {
	agent := &watchAgent{responses: []string{"healthy"}}
	result := Watch(context.Background(), agent, 100*time.Millisecond, 5*time.Millisecond, false)
	if !result.Healthy {
		t.Fatalf("expected Healthy=true, got %+v", result)
	}
	if result.CheckCount < 1 {
		t.Fatalf("expected at least 1 check, got %d", result.CheckCount)
	}
	if result.RollbackTriggered {
		t.Fatal("expected no rollback for healthy result")
	}
}

func TestWatch_HealthyAfterSeveralPolls(t *testing.T) {
	// First two polls return "applying", third returns "healthy".
	agent := &watchAgent{responses: []string{"applying", "applying", "healthy"}}
	result := Watch(context.Background(), agent, 500*time.Millisecond, 5*time.Millisecond, false)
	if !result.Healthy {
		t.Fatalf("expected Healthy=true after delay, got %+v", result)
	}
	if result.CheckCount < 3 {
		t.Fatalf("expected at least 3 checks, got %d", result.CheckCount)
	}
}

func TestWatch_TimeoutNoRollback(t *testing.T) {
	// Always returns "applying" — times out without becoming healthy.
	agent := &watchAgent{responses: []string{"applying"}}
	result := Watch(context.Background(), agent, 30*time.Millisecond, 5*time.Millisecond, false)
	if result.Healthy {
		t.Fatal("expected Healthy=false on timeout")
	}
	if result.RollbackTriggered {
		t.Fatal("expected no rollback when autoRollback=false")
	}
	if agent.rolled.Load() {
		t.Fatal("Rollback must not be called when autoRollback=false")
	}
}

func TestWatch_TimeoutTriggersAutoRollback(t *testing.T) {
	agent := &watchAgent{responses: []string{"failed"}}
	result := Watch(context.Background(), agent, 30*time.Millisecond, 5*time.Millisecond, true)
	if result.Healthy {
		t.Fatal("expected Healthy=false on timeout")
	}
	if !result.RollbackTriggered {
		t.Fatal("expected RollbackTriggered=true when autoRollback=true and timed out")
	}
	if !agent.rolled.Load() {
		t.Fatal("expected Rollback() to be called")
	}
}

func TestWatch_ContextCancelled(t *testing.T) {
	agent := &watchAgent{responses: []string{"applying"}}
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately.
	cancel()
	result := Watch(ctx, agent, 5*time.Second, 5*time.Millisecond, true)
	// Should return quickly; rollback may or may not have been called depending
	// on when the first health check runs, but the function must not block.
	if result.Elapsed > time.Second {
		t.Fatalf("Watch should return quickly after context cancel, got elapsed=%v", result.Elapsed)
	}
}

func TestWatch_RolledBackStatus_TriggersRollback(t *testing.T) {
	// A device that stays in "failed" after apply should trigger auto-rollback.
	agent := &watchAgent{responses: []string{"failed"}}
	result := Watch(context.Background(), agent, 30*time.Millisecond, 5*time.Millisecond, true)
	if !result.RollbackTriggered {
		t.Fatal("expected auto-rollback for persistent failed status")
	}
	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q", result.Status)
	}
}

func TestWatch_DefaultsApplied(t *testing.T) {
	// Zero poll interval and maxWait should not panic; defaults should apply.
	agent := &watchAgent{responses: []string{"healthy"}}
	// Use a very short context to avoid hanging in the default maxWait path.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result := Watch(ctx, agent, 0, 0, false)
	// Either healthy (if it polled in time) or ctx cancelled — but no panic.
	_ = result
}
