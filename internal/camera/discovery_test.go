package camera

import (
	"context"
	"testing"
	"time"
)

func TestProberProbe_InvalidURL(t *testing.T) {
	prober := Prober{Timeout: time.Second}
	camera := Camera{ID: "cam1", RTSPURL: "://bad-url"}
	result := prober.Probe(context.Background(), camera)
	if result.Status != StatusUnavailable {
		t.Fatalf("expected unavailable, got %s", result.Status)
	}
}

func TestProberProbe_Unavailable(t *testing.T) {
	prober := Prober{Timeout: 100 * time.Millisecond}
	camera := Camera{ID: "cam1", RTSPURL: "rtsp://127.0.0.1:65534/stream"}
	result := prober.Probe(context.Background(), camera)
	if result.Status != StatusUnavailable {
		t.Fatalf("expected unavailable, got %s", result.Status)
	}
}
