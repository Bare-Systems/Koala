package camera

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"
)

type ProbeResult struct {
	CameraID string
	Status   Status
	Error    string
}

type Prober struct {
	Timeout time.Duration
}

func (p Prober) Probe(ctx context.Context, camera Camera) ProbeResult {
	parsed, err := url.Parse(camera.RTSPURL)
	if err != nil || parsed.Host == "" {
		return ProbeResult{CameraID: camera.ID, Status: StatusUnavailable, Error: "invalid_rtsp_url"}
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", hostWithDefaultPort(parsed))
	if err != nil {
		return ProbeResult{CameraID: camera.ID, Status: StatusUnavailable, Error: err.Error()}
	}
	_ = conn.Close()
	return ProbeResult{CameraID: camera.ID, Status: StatusAvailable}
}

func hostWithDefaultPort(parsed *url.URL) string {
	if parsed.Port() != "" {
		return parsed.Host
	}
	return fmt.Sprintf("%s:%d", parsed.Hostname(), 554)
}
