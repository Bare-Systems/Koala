package update

import (
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		KeyID:                  "key-2026-03",
		Version:                "0.2.0",
		ArtifactURL:            "http://updates.local/koala-0.2.0.tar.gz",
		SHA256:                 strings.Repeat("a", 64),
		Signature:              "sig-ed25519-1",
		MinOrchestratorVersion: "0.1.0-dev",
		MinWorkerVersion:       "0.1.0-dev",
		CreatedAt:              freshCreatedAt(),
	}
}

func TestManagerStageApplyRollback(t *testing.T) {
	manager := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://192.168.1.20:6705", "0.1.0", NoopExecutor{})
	manifest := validManifest()

	if _, err := manager.Stage(manifest, []string{"dev1"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	status := manager.Status()[0]
	if status.State != StateStaged {
		t.Fatalf("expected staged, got %s", status.State)
	}

	if _, err := manager.Apply([]string{"dev1"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	status = manager.Status()[0]
	if status.CurrentVersion != "0.2.0" {
		t.Fatalf("expected upgraded version, got %s", status.CurrentVersion)
	}
	if status.State != StateHealthy {
		t.Fatalf("expected healthy after apply, got %s", status.State)
	}

	if _, err := manager.Rollback([]string{"dev1"}, "healthcheck_failed"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	status = manager.Status()[0]
	if status.CurrentVersion != "0.1.0" {
		t.Fatalf("expected rollback to previous version, got %s", status.CurrentVersion)
	}
	if status.State != StateRolledBack {
		t.Fatalf("expected rolled_back state, got %s", status.State)
	}
}

func TestManagerCheckCompatibility(t *testing.T) {
	manager := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:6705", "0.1.0", NoopExecutor{})
	manifest := validManifest()
	manifest.MinWorkerVersion = "9.9.9"

	results, err := manager.Check(manifest, nil)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].UpdateAvailable {
		t.Fatalf("expected incompatible manifest to be unavailable")
	}
}

func TestManagerValidateManifest(t *testing.T) {
	manager := NewManager("0.1.0-dev", "0.1.0-dev", "dev1", "http://127.0.0.1:6705", "0.1.0", NoopExecutor{})
	bad := Manifest{Version: "0.2.0", ArtifactURL: "http://x", SHA256: "bad", Signature: "sig"}
	if _, err := manager.Check(bad, nil); err == nil {
		t.Fatalf("expected invalid sha256 error")
	}
}
