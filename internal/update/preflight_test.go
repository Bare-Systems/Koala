package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunPreflight_AllChecksPass(t *testing.T) {
	stagingDir := t.TempDir()
	activeDir := t.TempDir()
	manifest := Manifest{
		Version:   "2.0.0",
		KeyID:     "key-1",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Create the staged artifact and manifest files.
	artifactDir := filepath.Join(stagingDir, manifest.Version)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "artifact.bin"), []byte("fake payload"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	manifestBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(artifactDir, "manifest.json"), manifestBytes, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	result := RunPreflight(stagingDir, activeDir, manifest)
	if !result.OK {
		for _, c := range result.Checks {
			if !c.Passed {
				t.Errorf("check %q failed: %s", c.Name, c.Message)
			}
		}
		t.Fatal("expected preflight to pass")
	}
	if len(result.Checks) == 0 {
		t.Fatal("expected at least one check to be recorded")
	}
}

func TestRunPreflight_MissingArtifact(t *testing.T) {
	stagingDir := t.TempDir()
	activeDir := t.TempDir()
	manifest := Manifest{Version: "3.0.0", KeyID: "key-1"}

	// Do not create any staged files — preflight should fail.
	result := RunPreflight(stagingDir, activeDir, manifest)
	if result.OK {
		t.Fatal("expected preflight to fail for missing artifact")
	}
	found := false
	for _, c := range result.Checks {
		if c.Name == "staged_artifact_exists" && !c.Passed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected staged_artifact_exists check to fail; checks: %+v", result.Checks)
	}
}

func TestRunPreflight_MissingManifest(t *testing.T) {
	stagingDir := t.TempDir()
	activeDir := t.TempDir()
	manifest := Manifest{Version: "4.0.0", KeyID: "key-1"}

	// Create artifact but not manifest.json.
	artifactDir := filepath.Join(stagingDir, manifest.Version)
	_ = os.MkdirAll(artifactDir, 0o755)
	_ = os.WriteFile(filepath.Join(artifactDir, "artifact.bin"), []byte("data"), 0o644)

	result := RunPreflight(stagingDir, activeDir, manifest)
	if result.OK {
		t.Fatal("expected preflight to fail for missing manifest")
	}
	found := false
	for _, c := range result.Checks {
		if c.Name == "staged_manifest_present" && !c.Passed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected staged_manifest_present check to fail; checks: %+v", result.Checks)
	}
}

func TestRunPreflight_RecordsAllCheckNames(t *testing.T) {
	stagingDir := t.TempDir()
	activeDir := t.TempDir()
	manifest := Manifest{Version: "5.0.0", KeyID: "key-1"}

	artifactDir := filepath.Join(stagingDir, manifest.Version)
	_ = os.MkdirAll(artifactDir, 0o755)
	_ = os.WriteFile(filepath.Join(artifactDir, "artifact.bin"), []byte("payload"), 0o644)
	_ = os.WriteFile(filepath.Join(artifactDir, "manifest.json"), []byte("{}"), 0o644)

	result := RunPreflight(stagingDir, activeDir, manifest)
	checkNames := map[string]bool{}
	for _, c := range result.Checks {
		checkNames[c.Name] = true
	}
	required := []string{"staged_artifact_exists", "staged_manifest_present"}
	for _, name := range required {
		if !checkNames[name] {
			t.Errorf("expected check %q to be present; got checks: %+v", name, result.Checks)
		}
	}
}
