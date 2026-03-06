package update

import (
	"context"
	"strings"
	"testing"
)

type selectiveFailExecutor struct {
	failApplyDevice string
}

func (e selectiveFailExecutor) Stage(_ context.Context, _ Device, _ Manifest) error { return nil }
func (e selectiveFailExecutor) Apply(_ context.Context, device Device) error {
	if device.ID == e.failApplyDevice {
		return errApplyFailed
	}
	return nil
}
func (e selectiveFailExecutor) Rollback(_ context.Context, _ Device, _ string) error { return nil }

var errApplyFailed = &rolloutErr{"apply failed"}

type rolloutErr struct{ msg string }

func (e *rolloutErr) Error() string { return e.msg }

func rolloutManifest() Manifest {
	return Manifest{
		KeyID:                  "key-2026-03",
		Version:                "0.2.0",
		ArtifactURL:            "http://updates.local/koala-0.2.0.bundle.json",
		SHA256:                 strings.Repeat("a", 64),
		Signature:              "sig",
		CreatedAt:              "2026-03-06T00:00:00Z",
		MinOrchestratorVersion: "0.1.0-dev",
		MinWorkerVersion:       "0.1.0-dev",
	}
}

func TestStartRolloutBatchCompleted(t *testing.T) {
	m := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:8080", "0.1.0", NoopExecutor{})
	m.RegisterDevice("dev2", "http://127.0.0.1:8081", "0.1.0")
	m.RegisterDevice("dev3", "http://127.0.0.1:8082", "0.1.0")

	r, err := m.StartRollout(RolloutRequest{
		Manifest:    rolloutManifest(),
		Mode:        RolloutModeBatch,
		BatchSize:   2,
		MaxFailures: 0,
	})
	if err != nil {
		t.Fatalf("start rollout: %v", err)
	}
	if r.Status != RolloutStatusCompleted {
		t.Fatalf("expected completed, got %s", r.Status)
	}
	if len(r.Batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(r.Batches))
	}
}

func TestStartRolloutStopsOnFailureThreshold(t *testing.T) {
	m := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:8080", "0.1.0", selectiveFailExecutor{failApplyDevice: "dev2"})
	m.RegisterDevice("dev2", "http://127.0.0.1:8081", "0.1.0")
	m.RegisterDevice("dev3", "http://127.0.0.1:8082", "0.1.0")

	r, err := m.StartRollout(RolloutRequest{
		Manifest:      rolloutManifest(),
		Mode:          RolloutModeCanary,
		BatchSize:     1,
		MaxFailures:   0,
		RollbackScope: "batch",
	})
	if err != nil {
		t.Fatalf("start rollout: %v", err)
	}
	if r.Status != RolloutStatusStopped {
		t.Fatalf("expected stopped, got %s", r.Status)
	}
	if r.FailureCount == 0 {
		t.Fatalf("expected failures to be recorded")
	}
}

func TestGetAndListRollouts(t *testing.T) {
	m := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:8080", "0.1.0", NoopExecutor{})
	r, err := m.StartRollout(RolloutRequest{Manifest: rolloutManifest(), Mode: RolloutModeAll})
	if err != nil {
		t.Fatalf("start rollout: %v", err)
	}
	got, ok := m.GetRollout(r.ID)
	if !ok {
		t.Fatalf("expected rollout to exist")
	}
	if got.ID != r.ID {
		t.Fatalf("expected rollout id %s, got %s", r.ID, got.ID)
	}
	if len(m.ListRollouts()) == 0 {
		t.Fatalf("expected rollout list not empty")
	}
}
