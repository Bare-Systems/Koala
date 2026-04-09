package update

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type failingExecutor struct {
	failApply bool
}

func (f failingExecutor) Stage(_ context.Context, _ Device, _ Manifest) error { return nil }
func (f failingExecutor) Apply(_ context.Context, _ Device) error {
	if f.failApply {
		return errors.New("apply failed")
	}
	return nil
}
func (f failingExecutor) Rollback(_ context.Context, _ Device, _ string) error { return nil }

func TestManagerAutoRollbackOnApplyFailure(t *testing.T) {
	m := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:6705", "0.1.0", failingExecutor{failApply: true})
	manifest := Manifest{KeyID: "key-2026-03", Version: "0.2.0", ArtifactURL: "http://x", SHA256: strings.Repeat("a", 64), Signature: "sig", CreatedAt: freshCreatedAt()}

	if _, err := m.Stage(manifest, []string{"dev1"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := m.Apply([]string{"dev1"}); err == nil {
		t.Fatalf("expected apply error")
	}

	status := m.Status()[0]
	if status.State != StateRolledBack {
		t.Fatalf("expected rolled_back after failed apply, got %s", status.State)
	}
	if status.CurrentVersion != "0.1.0" {
		t.Fatalf("expected previous version restored, got %s", status.CurrentVersion)
	}
}
