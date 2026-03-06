package update

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type PollEvent struct {
	Category  string
	EventType string
	Severity  string
	Message   string
	DeviceID  string
	Payload   map[string]any
}

type PollStatus struct {
	Enabled             bool   `json:"enabled"`
	Running             bool   `json:"running"`
	ManifestURL         string `json:"manifest_url,omitempty"`
	LastPollAt          string `json:"last_poll_at,omitempty"`
	NextPollAt          string `json:"next_poll_at,omitempty"`
	LastResult          string `json:"last_result,omitempty"`
	LastError           string `json:"last_error,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastDetectedVersion string `json:"last_detected_version,omitempty"`
	LastAppliedVersion  string `json:"last_applied_version,omitempty"`
}

type Poller struct {
	mu          sync.Mutex
	updater     *Manager
	downloader  Downloader
	deviceID    string
	manifestURL string
	interval    time.Duration
	jitter      time.Duration
	recorder    func(PollEvent)
	status      PollStatus
}

func NewPoller(updater *Manager, downloader Downloader, deviceID string, manifestURL string, interval time.Duration, jitter time.Duration, recorder func(PollEvent)) *Poller {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if jitter < 0 {
		jitter = 0
	}
	if downloader == nil {
		downloader = NewHTTPDownloader(10 * time.Second)
	}
	return &Poller{
		updater:     updater,
		downloader:  downloader,
		deviceID:    deviceID,
		manifestURL: manifestURL,
		interval:    interval,
		jitter:      jitter,
		recorder:    recorder,
		status: PollStatus{
			Enabled:     true,
			Running:     false,
			ManifestURL: manifestURL,
		},
	}
}

func (p *Poller) Start(ctx context.Context) {
	p.mu.Lock()
	p.status.Running = true
	next := p.nextInterval(0)
	p.status.NextPollAt = time.Now().UTC().Add(next).Format(time.RFC3339)
	p.mu.Unlock()

	go func() {
		failures := 0
		for {
			wait := p.nextInterval(failures)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				p.mu.Lock()
				p.status.Running = false
				p.status.NextPollAt = ""
				p.mu.Unlock()
				return
			case <-timer.C:
			}

			err := p.RunOnce(ctx)
			if err != nil {
				failures++
			} else {
				failures = 0
			}
			p.mu.Lock()
			p.status.NextPollAt = time.Now().UTC().Add(p.nextInterval(failures)).Format(time.RFC3339)
			p.mu.Unlock()
		}
	}()
}

func (p *Poller) RunOnce(ctx context.Context) error {
	if p.updater == nil {
		return fmt.Errorf("updater not configured")
	}
	raw, err := p.downloader.Download(ctx, p.manifestURL)
	if err != nil {
		p.setFailure("download_failed", err)
		p.record("poll_failed", "medium", "manifest download failed", map[string]any{"error": err.Error()})
		return err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		p.setFailure("invalid_manifest", err)
		p.record("poll_failed", "medium", "manifest parse failed", map[string]any{"error": err.Error()})
		return err
	}
	checks, err := p.updater.Check(manifest, []string{p.deviceID})
	if err != nil {
		p.setFailure("check_failed", err)
		p.record("poll_failed", "medium", "manifest eligibility check failed", map[string]any{"error": err.Error()})
		return err
	}
	if len(checks) == 0 {
		err := fmt.Errorf("no check result returned")
		p.setFailure("check_failed", err)
		p.record("poll_failed", "medium", "manifest eligibility check returned no result", nil)
		return err
	}
	result := checks[0]
	p.mu.Lock()
	p.status.LastDetectedVersion = manifest.Version
	p.mu.Unlock()
	if !result.UpdateAvailable {
		p.setSuccess("no_update", "", manifest.Version)
		return nil
	}
	if _, err := p.updater.Stage(manifest, []string{p.deviceID}); err != nil {
		p.setFailure("stage_failed", err)
		p.record("poll_failed", "high", "poll stage failed", map[string]any{"error": err.Error(), "version": manifest.Version})
		return err
	}
	if _, err := p.updater.Apply([]string{p.deviceID}); err != nil {
		p.setFailure("apply_failed", err)
		p.record("poll_failed", "high", "poll apply failed", map[string]any{"error": err.Error(), "version": manifest.Version})
		return err
	}
	p.setSuccess("updated", manifest.Version, manifest.Version)
	p.record("poll_updated", "info", "poll applied update", map[string]any{"version": manifest.Version})
	return nil
}

func (p *Poller) Status() PollStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Poller) setFailure(result string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.LastPollAt = time.Now().UTC().Format(time.RFC3339)
	p.status.LastResult = result
	if err != nil {
		p.status.LastError = err.Error()
	}
	p.status.ConsecutiveFailures++
}

func (p *Poller) setSuccess(result string, appliedVersion string, detectedVersion string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.LastPollAt = time.Now().UTC().Format(time.RFC3339)
	p.status.LastResult = result
	p.status.LastError = ""
	p.status.ConsecutiveFailures = 0
	if strings.TrimSpace(appliedVersion) != "" {
		p.status.LastAppliedVersion = appliedVersion
	}
	if strings.TrimSpace(detectedVersion) != "" {
		p.status.LastDetectedVersion = detectedVersion
	}
}

func (p *Poller) nextInterval(consecutiveFailures int) time.Duration {
	backoffMultiplier := 1
	if consecutiveFailures > 0 {
		backoffMultiplier = 1 << minInt(consecutiveFailures, 4)
	}
	interval := p.interval * time.Duration(backoffMultiplier)
	jitter := time.Duration(0)
	if p.jitter > 0 {
		jitter = time.Duration(rand.Int63n(int64(p.jitter)))
	}
	return interval + jitter
}

func (p *Poller) record(eventType string, severity string, message string, payload map[string]any) {
	if p.recorder == nil {
		return
	}
	p.recorder(PollEvent{
		Category:  "poll",
		EventType: eventType,
		Severity:  severity,
		Message:   message,
		DeviceID:  p.deviceID,
		Payload:   payload,
	})
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
