// Package telemetry provides lightweight in-process metrics collection for
// Koala's key operational signals.
package telemetry

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Collector accumulates runtime metrics. It is safe for concurrent use.
// Latency histograms are stored in a fixed-capacity ring buffer; when full,
// oldest entries are overwritten (sliding window semantics).
type Collector struct {
	// Counters — read with .Load().
	FrameTotal       atomic.Int64
	FrameDropped     atomic.Int64
	ToolRequestTotal atomic.Int64
	ToolErrorTotal   atomic.Int64

	mu         sync.Mutex
	latencies  []int64 // inference latency samples in milliseconds
	latencyPos int     // next write position (ring index)
	latencyCap int     // max capacity of the ring buffer
}

// New returns a Collector with a ring buffer capable of holding cap latency
// samples. Pass 0 to use the default capacity (200).
func New(cap int) *Collector {
	if cap <= 0 {
		cap = 200
	}
	return &Collector{
		latencies:  make([]int64, 0, cap),
		latencyCap: cap,
	}
}

// RecordInferenceLatency stores one inference round-trip measurement.
func (c *Collector) RecordInferenceLatency(d time.Duration) {
	ms := d.Milliseconds()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.latencies) < c.latencyCap {
		c.latencies = append(c.latencies, ms)
	} else {
		c.latencies[c.latencyPos%c.latencyCap] = ms
		c.latencyPos++
	}
}

// InferenceLatencyPercentile returns the p-th percentile of sampled inference
// latencies in milliseconds, where p is in [0, 1]. Returns 0 if no samples.
func (c *Collector) InferenceLatencyPercentile(p float64) int64 {
	c.mu.Lock()
	if len(c.latencies) == 0 {
		c.mu.Unlock()
		return 0
	}
	buf := make([]int64, len(c.latencies))
	copy(buf, c.latencies)
	c.mu.Unlock()

	sort.Slice(buf, func(i, j int) bool { return buf[i] < buf[j] })
	// Nearest-rank method: idx = ceil(p * n) - 1, clamped to [0, n-1].
	idx := int(math.Ceil(float64(len(buf))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(buf) {
		idx = len(buf) - 1
	}
	return buf[idx]
}

// Snapshot returns a point-in-time snapshot suitable for JSON serialisation.
func (c *Collector) Snapshot() map[string]any {
	c.mu.Lock()
	sampleCount := int64(len(c.latencies))
	c.mu.Unlock()
	return map[string]any{
		"frame_total":                c.FrameTotal.Load(),
		"frame_dropped_total":        c.FrameDropped.Load(),
		"tool_request_total":         c.ToolRequestTotal.Load(),
		"tool_error_total":           c.ToolErrorTotal.Load(),
		"inference_latency_samples":  sampleCount,
		"inference_latency_p50_ms":   c.InferenceLatencyPercentile(0.50),
		"inference_latency_p95_ms":   c.InferenceLatencyPercentile(0.95),
	}
}
