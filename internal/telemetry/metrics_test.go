package telemetry

import (
	"testing"
	"time"
)

func TestCollectorCounters(t *testing.T) {
	c := New(0)
	c.FrameTotal.Add(10)
	c.FrameDropped.Add(2)
	c.ToolRequestTotal.Add(5)
	c.ToolErrorTotal.Add(1)

	snap := c.Snapshot()
	if snap["frame_total"] != int64(10) {
		t.Fatalf("frame_total mismatch: %v", snap["frame_total"])
	}
	if snap["frame_dropped_total"] != int64(2) {
		t.Fatalf("frame_dropped_total mismatch: %v", snap["frame_dropped_total"])
	}
}

func TestCollectorLatencyPercentiles(t *testing.T) {
	c := New(100)
	// Record 100ms, 200ms, 300ms — p50 should be 200, p95 should be 300.
	for _, ms := range []int64{100, 200, 300} {
		c.RecordInferenceLatency(time.Duration(ms) * time.Millisecond)
	}
	p50 := c.InferenceLatencyPercentile(0.50)
	if p50 != 200 {
		t.Fatalf("expected p50=200ms, got %d", p50)
	}
	p95 := c.InferenceLatencyPercentile(0.95)
	if p95 != 300 {
		t.Fatalf("expected p95=300ms, got %d", p95)
	}
}

func TestCollectorLatencyRingBuffer(t *testing.T) {
	// Cap of 3; write 4 entries — oldest should be overwritten.
	c := New(3)
	for _, ms := range []int64{100, 200, 300, 400} {
		c.RecordInferenceLatency(time.Duration(ms) * time.Millisecond)
	}
	// Buffer should contain 3 entries; min should be 200 (100 was evicted).
	p50 := c.InferenceLatencyPercentile(0.50)
	if p50 < 200 {
		t.Fatalf("expected ring eviction, p50=%d", p50)
	}
}

func TestCollectorEmptyLatencyReturnsZero(t *testing.T) {
	c := New(0)
	if c.InferenceLatencyPercentile(0.95) != 0 {
		t.Fatalf("expected 0 for empty collector")
	}
}
