package camera

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ProbeResult struct {
	CameraID          string
	Status            Status
	Error             string
	Capability        Capability
	DiscoveredRTSPURL string
}

type Prober struct {
	Timeout time.Duration
}

func (p Prober) Probe(ctx context.Context, camera Camera) ProbeResult {
	result := ProbeResult{
		CameraID: camera.ID,
		Capability: Capability{
			LastProbedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}
	result.Status, result.Error, result.Capability.RTSPReachable = p.probeRTSP(ctx, camera.RTSPURL)
	if result.Status == StatusAvailable {
		result.Capability.SelectedSource = "rtsp"
		return result
	}

	onvifOK, onvifErr := p.probeONVIF(ctx, camera)
	result.Capability.ONVIFReachable = onvifOK
	if onvifOK {
		result.Status = StatusDegraded
		result.Capability.SelectedSource = "onvif_fallback"
		if camera.RTSPURL == "" {
			host := hostFromURLs(camera.RTSPURL, camera.ONVIFURL)
			if host != "" {
				result.DiscoveredRTSPURL = fmt.Sprintf("rtsp://%s:554/Streaming/Channels/101", host)
			}
		}
		if result.Error == "" {
			result.Error = "rtsp unavailable, onvif reachable"
		}
		return result
	}
	if onvifErr != "" {
		if result.Error == "" {
			result.Error = onvifErr
		} else {
			result.Error = result.Error + "; " + onvifErr
		}
	}
	result.Status = StatusUnavailable
	result.Capability.LastError = result.Error
	return result
}

func (p Prober) probeRTSP(ctx context.Context, rtspURL string) (Status, string, bool) {
	parsed, err := url.Parse(rtspURL)
	if err != nil || parsed.Host == "" {
		return StatusUnavailable, "invalid_rtsp_url", false
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
		return StatusUnavailable, err.Error(), false
	}
	_ = conn.Close()
	return StatusAvailable, "", true
}

func (p Prober) probeONVIF(ctx context.Context, camera Camera) (bool, string) {
	host := hostFromURLs(camera.RTSPURL, camera.ONVIFURL)
	if host == "" {
		return false, "missing_camera_host_for_onvif"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	dialer := net.Dialer{}
	tryPorts := []int{}
	if parsed, err := url.Parse(camera.ONVIFURL); err == nil && parsed.Port() != "" {
		if port := parsed.Port(); port != "" {
			if p, perr := strconv.Atoi(port); perr == nil {
				tryPorts = append(tryPorts, p)
			}
		}
	}
	tryPorts = append(tryPorts, 80, 8899)
	var lastErr string
	for _, port := range tryPorts {
		dialCtx, cancel := context.WithTimeout(ctx, timeout)
		conn, err := dialer.DialContext(dialCtx, "tcp", fmt.Sprintf("%s:%d", host, port))
		cancel()
		if err == nil {
			_ = conn.Close()
			return true, ""
		}
		lastErr = err.Error()
	}
	return false, lastErr
}

func hostFromURLs(rtspURL string, onvifURL string) string {
	for _, raw := range []string{rtspURL, onvifURL} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err == nil && parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	return ""
}

func hostWithDefaultPort(parsed *url.URL) string {
	if parsed.Port() != "" {
		return parsed.Host
	}
	return fmt.Sprintf("%s:%d", parsed.Hostname(), 554)
}
