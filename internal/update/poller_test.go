package update

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type staticManifestDownloader struct {
	manifest Manifest
	err      error
}

func (d staticManifestDownloader) Download(_ context.Context, _ string) ([]byte, error) {
	if d.err != nil {
		return nil, d.err
	}
	return json.Marshal(d.manifest)
}

func TestPollerRunOnceNoUpdate(t *testing.T) {
	m := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:6705", "0.2.0", NoopExecutor{})
	manifest := Manifest{
		KeyID:                  "key-2026-03",
		Version:                "0.2.0",
		ArtifactURL:            "http://updates.local/m.json",
		SHA256:                 strings.Repeat("a", 64),
		Signature:              "sig",
		CreatedAt:              freshCreatedAt(),
		MinOrchestratorVersion: "0.1.0-dev",
		MinWorkerVersion:       "0.1.0-dev",
	}
	p := NewPoller(m, staticManifestDownloader{manifest: manifest}, "dev1", manifest.ArtifactURL, time.Second, 0, nil)
	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	status := p.Status()
	if status.LastResult != "no_update" {
		t.Fatalf("expected no_update, got %s", status.LastResult)
	}
}

func TestPollerRunOnceAppliesUpdate(t *testing.T) {
	m := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:6705", "0.1.0", NoopExecutor{})
	manifest := Manifest{
		KeyID:                  "key-2026-03",
		Version:                "0.2.0",
		ArtifactURL:            "http://updates.local/m.json",
		SHA256:                 strings.Repeat("a", 64),
		Signature:              "sig",
		CreatedAt:              freshCreatedAt(),
		MinOrchestratorVersion: "0.1.0-dev",
		MinWorkerVersion:       "0.1.0-dev",
	}
	events := []PollEvent{}
	p := NewPoller(m, staticManifestDownloader{manifest: manifest}, "dev1", manifest.ArtifactURL, time.Second, 0, func(e PollEvent) {
		events = append(events, e)
	})
	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	device := m.Status()[0]
	if device.CurrentVersion != "0.2.0" {
		t.Fatalf("expected device version 0.2.0, got %s", device.CurrentVersion)
	}
	status := p.Status()
	if status.LastResult != "updated" {
		t.Fatalf("expected updated result, got %s", status.LastResult)
	}
	if len(events) != 1 || events[0].EventType != "poll_updated" {
		t.Fatalf("expected poll_updated event")
	}
}

func TestPollerRunOnceFailureIncrementsCounter(t *testing.T) {
	m := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:6705", "0.1.0", NoopExecutor{})
	p := NewPoller(m, staticManifestDownloader{err: context.DeadlineExceeded}, "dev1", "http://updates.local/m.json", time.Second, 0, nil)
	if err := p.RunOnce(context.Background()); err == nil {
		t.Fatalf("expected poll failure")
	}
	status := p.Status()
	if status.ConsecutiveFailures != 1 {
		t.Fatalf("expected consecutive failures=1, got %d", status.ConsecutiveFailures)
	}
	if status.LastResult != "download_failed" {
		t.Fatalf("expected download_failed, got %s", status.LastResult)
	}
}
