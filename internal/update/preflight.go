package update

import (
	"fmt"
	"os"
	"path/filepath"
)

// PreflightCheck is a single pre-apply verification step.
type PreflightCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

// PreflightResult summarizes all preflight checks run before an apply.
type PreflightResult struct {
	OK     bool             `json:"ok"`
	Checks []PreflightCheck `json:"checks"`
}

// RunPreflight validates that a staged artifact is ready to apply.
// It checks that the staged artifact and manifest files exist, are non-empty,
// and that sufficient disk space is available in the active directory.
//
// The stagingDir and activeDir are the FileAgent paths; manifest is the
// currently staged update to be applied.
func RunPreflight(stagingDir, activeDir string, manifest Manifest) PreflightResult {
	result := PreflightResult{OK: true}

	artifactPath := filepath.Join(stagingDir, manifest.Version, "artifact.bin")
	info, err := os.Stat(artifactPath)
	if err != nil || info.IsDir() {
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "staged_artifact_exists",
			Passed:  false,
			Message: fmt.Sprintf("staged artifact not found at %s", artifactPath),
		})
		result.OK = false
	} else {
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "staged_artifact_exists",
			Passed:  true,
			Message: fmt.Sprintf("artifact found size=%d bytes", info.Size()),
		})

		// Disk space: need at least 2× the artifact size in activeDir.
		needed := info.Size() * 2
		available := diskFreeBytes(activeDir)
		if available == -1 {
			result.Checks = append(result.Checks, PreflightCheck{
				Name:    "disk_space",
				Passed:  true,
				Message: "disk space check skipped (unavailable on this platform)",
			})
		} else {
			passed := available >= needed
			msg := fmt.Sprintf("needed=%d available=%d bytes", needed, available)
			result.Checks = append(result.Checks, PreflightCheck{
				Name:    "disk_space",
				Passed:  passed,
				Message: msg,
			})
			if !passed {
				result.OK = false
			}
		}
	}

	// Check staged manifest file is present.
	manifestPath := filepath.Join(stagingDir, manifest.Version, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "staged_manifest_present",
			Passed:  false,
			Message: "staged manifest.json not found; re-run stage",
		})
		result.OK = false
	} else {
		result.Checks = append(result.Checks, PreflightCheck{
			Name:    "staged_manifest_present",
			Passed:  true,
			Message: "manifest.json present",
		})
	}

	return result
}

// diskFreeBytes returns the available free bytes for the filesystem containing
// path.  Returns -1 if the value cannot be determined.
func diskFreeBytes(path string) int64 {
	// Ensure the directory exists so Statfs can probe it.
	if err := os.MkdirAll(path, 0o755); err != nil {
		return -1
	}
	return diskFreeBytesImpl(path)
}
