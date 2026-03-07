package camera

import (
	"context"
	"net"
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

func TestProberProbe_OnvifFallbackDegraded(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
	}()

	prober := Prober{Timeout: 200 * time.Millisecond}
	camera := Camera{
		ID:       "cam1",
		RTSPURL:  "rtsp://127.0.0.1:65534/stream",
		ONVIFURL: "http://" + ln.Addr().String() + "/onvif/device_service",
	}
	result := prober.Probe(context.Background(), camera)
	if result.Status != StatusDegraded {
		t.Fatalf("expected degraded, got %s", result.Status)
	}
	if !result.Capability.ONVIFReachable {
		t.Fatalf("expected ONVIF reachable")
	}
	<-done
}

func TestProberProbe_DiscoversDefaultRTSPFromOnvifHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
	}()
	camera := Camera{
		ID:       "cam1",
		ONVIFURL: "http://" + ln.Addr().String() + "/onvif/device_service",
	}
	result := (Prober{Timeout: 100 * time.Millisecond}).Probe(context.Background(), camera)
	if result.Status != StatusDegraded {
		t.Fatalf("expected degraded with fallback source, got %s", result.Status)
	}
	if result.DiscoveredRTSPURL == "" {
		t.Fatalf("expected discovered rtsp url")
	}
}
