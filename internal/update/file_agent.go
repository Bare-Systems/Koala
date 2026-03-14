package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Downloader interface {
	Download(ctx context.Context, artifactURL string) ([]byte, error)
}

type HTTPDownloader struct {
	client *http.Client
}

func NewHTTPDownloader(timeout time.Duration) *HTTPDownloader {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HTTPDownloader{client: &http.Client{Timeout: timeout}}
}

func (d *HTTPDownloader) Download(ctx context.Context, artifactURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artifact download failed status=%d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

type FileAgent struct {
	mu            sync.Mutex
	downloader    Downloader
	verifier      Verifier
	encryptionKey []byte
	stagingDir    string
	activeDir     string
	current       string
	previous      string
	staged        *Manifest
	status        string
	lastError     string
}

func NewFileAgent(currentVersion string, stagingDir string, activeDir string, downloader Downloader, verifier Verifier, encryptionKey []byte) *FileAgent {
	if currentVersion == "" {
		currentVersion = "0.1.0-dev"
	}
	if stagingDir == "" {
		stagingDir = "/tmp/koala/staging"
	}
	if activeDir == "" {
		activeDir = "/tmp/koala/active"
	}
	if downloader == nil {
		downloader = NewHTTPDownloader(10 * time.Second)
	}
	if verifier == nil {
		verifier = NoopVerifier{}
	}
	return &FileAgent{
		downloader:    downloader,
		verifier:      verifier,
		encryptionKey: encryptionKey,
		stagingDir:    stagingDir,
		activeDir:     activeDir,
		current:       currentVersion,
		status:        "healthy",
	}
}

func (a *FileAgent) Stage(ctx context.Context, manifest Manifest) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.verifier.VerifyManifest(manifest); err != nil {
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}

	rawBundle, err := a.downloader.Download(ctx, manifest.ArtifactURL)
	if err != nil {
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}

	var bundle Bundle
	if err := json.Unmarshal(rawBundle, &bundle); err != nil {
		a.status = "failed"
		a.lastError = "invalid bundle format"
		return fmt.Errorf("decode bundle: %w", err)
	}

	if bundle.Version != manifest.Version {
		err := fmt.Errorf("bundle version mismatch")
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}
	if bundle.KeyID != manifest.KeyID {
		err := fmt.Errorf("bundle key_id mismatch")
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}
	if strings.TrimSpace(bundle.CreatedAt) != strings.TrimSpace(manifest.CreatedAt) {
		err := fmt.Errorf("bundle created_at mismatch")
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}

	if err := a.verifier.VerifyBundle(bundle); err != nil {
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}

	artifact, err := DecryptBundle(bundle, a.encryptionKey)
	if err != nil {
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}
	sum := sha256.Sum256(artifact)
	artifactSHA := hex.EncodeToString(sum[:])
	if artifactSHA != strings.ToLower(strings.TrimSpace(manifest.SHA256)) {
		err := fmt.Errorf("manifest sha256 mismatch")
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}
	if artifactSHA != strings.ToLower(strings.TrimSpace(bundle.ArtifactSHA256)) {
		err := fmt.Errorf("bundle artifact sha256 mismatch")
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}

	artifactDir := filepath.Join(a.stagingDir, manifest.Version)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "artifact.bin"), artifact, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "bundle.json"), rawBundle, 0o644); err != nil {
		return err
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "manifest.json"), manifestBytes, 0o644); err != nil {
		return err
	}

	a.staged = &manifest
	a.status = "staged"
	a.lastError = ""
	return nil
}

func (a *FileAgent) Apply(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.staged == nil {
		return fmt.Errorf("no staged update")
	}

	// Run pre-apply checks: staged files present, disk space available.
	preResult := RunPreflight(a.stagingDir, a.activeDir, *a.staged)
	if !preResult.OK {
		for _, check := range preResult.Checks {
			if !check.Passed {
				a.status = "failed"
				a.lastError = "preflight: " + check.Name + ": " + check.Message
				return fmt.Errorf("preflight check %q failed: %s", check.Name, check.Message)
			}
		}
	}

	stagedArtifact := filepath.Join(a.stagingDir, a.staged.Version, "artifact.bin")
	payload, err := os.ReadFile(stagedArtifact)
	if err != nil {
		a.status = "failed"
		a.lastError = err.Error()
		return err
	}
	activeVersionDir := filepath.Join(a.activeDir, a.staged.Version)
	if err := os.MkdirAll(activeVersionDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(activeVersionDir, "artifact.bin"), payload, 0o644); err != nil {
		return err
	}

	a.previous = a.current
	a.current = a.staged.Version
	a.staged = nil
	a.status = "healthy"
	a.lastError = ""
	if err := os.MkdirAll(a.activeDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(a.activeDir, "current_version"), []byte(a.current), 0o644); err != nil {
		return err
	}
	return nil
}

func (a *FileAgent) Rollback(_ context.Context, reason string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.previous == "" {
		return fmt.Errorf("no previous version")
	}
	a.current = a.previous
	a.staged = nil
	a.status = "rolled_back"
	a.lastError = reason
	if err := os.MkdirAll(a.activeDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(a.activeDir, "current_version"), []byte(a.current), 0o644); err != nil {
		return err
	}
	return nil
}

func (a *FileAgent) Health(_ context.Context) (map[string]any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	staged := ""
	if a.staged != nil {
		staged = a.staged.Version
	}
	health := map[string]any{
		"status":           a.status,
		"current_version":  a.current,
		"previous_version": a.previous,
		"staged_version":   staged,
		"last_error":       a.lastError,
		"staging_dir":      a.stagingDir,
		"active_dir":       a.activeDir,
	}
	if telemetry, ok := a.verifier.(interface{ UnknownKeyStats() UnknownKeyStats }); ok {
		health["unknown_key_attempts"] = telemetry.UnknownKeyStats()
	}
	if alerts, ok := a.verifier.(interface {
		RecentUnknownKeyAlerts(limit int) []UnknownKeyAlert
	}); ok {
		health["unknown_key_alerts"] = alerts.RecentUnknownKeyAlerts(50)
	}
	return health, nil
}
