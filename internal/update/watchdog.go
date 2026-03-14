package update

import (
	"context"
	"time"
)

// WatchdogResult records the outcome of a post-apply health watch.
type WatchdogResult struct {
	// Healthy is true if the agent reported "healthy" within maxWait.
	Healthy bool
	// Status is the final agent status string at the point the watch ended.
	Status string
	// CheckCount is the number of health polls performed.
	CheckCount int
	// Elapsed is how long the watchdog ran before returning.
	Elapsed time.Duration
	// RollbackTriggered is true if the watchdog called Rollback() automatically.
	RollbackTriggered bool
}

// Watch polls agent.Health() at pollInterval until the agent reports "healthy"
// or maxWait elapses.  If autoRollback is true and the watch times out with a
// non-healthy status, Rollback is called automatically with reason
// "watchdog_timeout".
//
// Watch blocks until a result is available or ctx is cancelled.
func Watch(ctx context.Context, agent Agent, maxWait, pollInterval time.Duration, autoRollback bool) WatchdogResult {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	if maxWait <= 0 {
		maxWait = 60 * time.Second
	}

	startedAt := time.Now()
	deadline := startedAt.Add(maxWait)
	result := WatchdogResult{}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		health, err := agent.Health(ctx)
		result.CheckCount++
		status := ""
		if err == nil {
			if s, ok := health["status"].(string); ok {
				status = s
			}
		}
		result.Status = status
		result.Elapsed = time.Since(startedAt)

		if status == "healthy" {
			result.Healthy = true
			return result
		}

		if time.Now().After(deadline) {
			// Timed out without seeing "healthy".
			if autoRollback {
				reason := "watchdog_timeout"
				if status != "" {
					reason = "watchdog_timeout: status=" + status
				}
				_ = agent.Rollback(ctx, reason)
				result.RollbackTriggered = true
			}
			return result
		}

		select {
		case <-ctx.Done():
			result.Elapsed = time.Since(startedAt)
			return result
		case <-ticker.C:
		}
	}
}
