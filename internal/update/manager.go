package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type DeviceState string

const (
	StateIdle        DeviceState = "idle"
	StateDownloading DeviceState = "downloading"
	StateStaged      DeviceState = "staged"
	StateApplying    DeviceState = "applying"
	StateRestarting  DeviceState = "restarting"
	StateHealthy     DeviceState = "healthy"
	StateFailed      DeviceState = "failed"
	StateRolledBack  DeviceState = "rolled_back"
)

type Manifest struct {
	KeyID                  string `json:"key_id"`
	Version                string `json:"version"`
	ArtifactURL            string `json:"artifact_url"`
	SHA256                 string `json:"sha256"`
	Signature              string `json:"signature"`
	MinOrchestratorVersion string `json:"min_orchestrator_version"`
	MinWorkerVersion       string `json:"min_worker_version"`
	CreatedAt              string `json:"created_at"`
}

type Device struct {
	ID              string      `json:"id"`
	Address         string      `json:"address"`
	CurrentVersion  string      `json:"current_version"`
	PreviousVersion string      `json:"previous_version,omitempty"`
	TargetVersion   string      `json:"target_version,omitempty"`
	State           DeviceState `json:"state"`
	LastError       string      `json:"last_error,omitempty"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type CheckResult struct {
	DeviceID        string `json:"device_id"`
	UpdateAvailable bool   `json:"update_available"`
	Reason          string `json:"reason"`
}

type Manager struct {
	mu                  sync.RWMutex
	orchestratorVersion string
	workerVersion       string
	executor            Executor
	devices             map[string]Device
	staged              map[string]Manifest
	rollouts            map[string]Rollout
}

func NewManager(orchestratorVersion string, workerVersion string, localDeviceID string, localAddress string, currentVersion string, executor Executor) *Manager {
	if localDeviceID == "" {
		localDeviceID = "koala-local"
	}
	if currentVersion == "" {
		currentVersion = "0.1.0-dev"
	}
	if localAddress == "" {
		localAddress = "http://127.0.0.1:8080"
	}
	if executor == nil {
		executor = NoopExecutor{}
	}
	now := time.Now().UTC()
	device := Device{
		ID:             localDeviceID,
		Address:        localAddress,
		CurrentVersion: currentVersion,
		State:          StateHealthy,
		UpdatedAt:      now,
	}
	return &Manager{
		orchestratorVersion: orchestratorVersion,
		workerVersion:       workerVersion,
		executor:            executor,
		devices:             map[string]Device{localDeviceID: device},
		staged:              map[string]Manifest{},
		rollouts:            map[string]Rollout{},
	}
}

func (m *Manager) RegisterDevice(deviceID string, address string, currentVersion string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(deviceID) == "" {
		return
	}
	if currentVersion == "" {
		currentVersion = "unknown"
	}
	m.devices[deviceID] = Device{
		ID:             deviceID,
		Address:        address,
		CurrentVersion: currentVersion,
		State:          StateIdle,
		UpdatedAt:      time.Now().UTC(),
	}
}

func (m *Manager) Status() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Device, 0, len(m.devices))
	for _, d := range m.devices {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) Check(manifest Manifest, deviceIDs []string) ([]CheckResult, error) {
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	selected, err := m.selectDevicesLocked(deviceIDs)
	if err != nil {
		return nil, err
	}

	results := make([]CheckResult, 0, len(selected))
	for _, d := range selected {
		result := CheckResult{DeviceID: d.ID}
		if d.CurrentVersion == manifest.Version {
			result.UpdateAvailable = false
			result.Reason = "already_on_target_version"
			results = append(results, result)
			continue
		}
		if manifest.MinOrchestratorVersion != "" && manifest.MinOrchestratorVersion != m.orchestratorVersion {
			result.UpdateAvailable = false
			result.Reason = "orchestrator_version_not_compatible"
			results = append(results, result)
			continue
		}
		if manifest.MinWorkerVersion != "" && manifest.MinWorkerVersion != m.workerVersion {
			result.UpdateAvailable = false
			result.Reason = "worker_version_not_compatible"
			results = append(results, result)
			continue
		}
		result.UpdateAvailable = true
		result.Reason = "ok"
		results = append(results, result)
	}
	return results, nil
}

func (m *Manager) Stage(manifest Manifest, deviceIDs []string) ([]Device, error) {
	if _, err := m.Check(manifest, deviceIDs); err != nil {
		return nil, err
	}

	m.mu.Lock()
	selected, err := m.selectDevicesLocked(deviceIDs)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}

	updated := make([]Device, 0, len(selected))
	for _, d := range selected {
		d.State = StateDownloading
		d.TargetVersion = manifest.Version
		d.LastError = ""
		d.UpdatedAt = time.Now().UTC()
		m.devices[d.ID] = d
		updated = append(updated, d)
	}
	m.mu.Unlock()

	for _, d := range updated {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := m.executor.Stage(ctx, d, manifest)
		cancel()

		m.mu.Lock()
		current := m.devices[d.ID]
		if err != nil {
			current.State = StateFailed
			current.LastError = err.Error()
			current.UpdatedAt = time.Now().UTC()
			m.devices[d.ID] = current
			m.mu.Unlock()
			return m.Status(), fmt.Errorf("stage failed for %s: %w", d.ID, err)
		}
		current.State = StateStaged
		current.UpdatedAt = time.Now().UTC()
		m.devices[d.ID] = current
		m.staged[d.ID] = manifest
		m.mu.Unlock()
	}
	return m.Status(), nil
}

func (m *Manager) Apply(deviceIDs []string) ([]Device, error) {
	m.mu.Lock()
	selected, err := m.selectDevicesLocked(deviceIDs)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}

	for _, d := range selected {
		if _, ok := m.staged[d.ID]; !ok {
			m.mu.Unlock()
			return nil, fmt.Errorf("device %s has no staged update", d.ID)
		}
		d.State = StateApplying
		d.PreviousVersion = d.CurrentVersion
		d.UpdatedAt = time.Now().UTC()
		m.devices[d.ID] = d
	}
	m.mu.Unlock()

	for _, d := range selected {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := m.executor.Apply(ctx, d)
		cancel()

		m.mu.Lock()
		current := m.devices[d.ID]
		manifest := m.staged[d.ID]
		if err != nil {
			current.State = StateFailed
			current.LastError = err.Error()
			current.UpdatedAt = time.Now().UTC()
			m.devices[d.ID] = current
			m.mu.Unlock()

			_, _ = m.Rollback([]string{d.ID}, "auto_rollback_after_apply_failure")
			return m.Status(), fmt.Errorf("apply failed for %s: %w", d.ID, err)
		}
		current.CurrentVersion = manifest.Version
		current.TargetVersion = ""
		current.State = StateHealthy
		current.LastError = ""
		current.UpdatedAt = time.Now().UTC()
		m.devices[d.ID] = current
		delete(m.staged, d.ID)
		m.mu.Unlock()
	}
	return m.Status(), nil
}

func (m *Manager) Rollback(deviceIDs []string, reason string) ([]Device, error) {
	m.mu.Lock()
	selected, err := m.selectDevicesLocked(deviceIDs)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if reason == "" {
		reason = "manual_rollback"
	}
	for _, d := range selected {
		if d.PreviousVersion == "" {
			m.mu.Unlock()
			return nil, fmt.Errorf("device %s has no previous version to roll back to", d.ID)
		}
	}
	m.mu.Unlock()

	for _, d := range selected {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := m.executor.Rollback(ctx, d, reason)
		cancel()

		m.mu.Lock()
		current := m.devices[d.ID]
		if err != nil {
			current.State = StateFailed
			current.LastError = err.Error()
			current.UpdatedAt = time.Now().UTC()
			m.devices[d.ID] = current
			m.mu.Unlock()
			return m.Status(), fmt.Errorf("rollback failed for %s: %w", d.ID, err)
		}
		current.CurrentVersion = current.PreviousVersion
		current.TargetVersion = ""
		current.State = StateRolledBack
		current.LastError = reason
		current.UpdatedAt = time.Now().UTC()
		m.devices[d.ID] = current
		delete(m.staged, d.ID)
		m.mu.Unlock()
	}
	return m.Status(), nil
}

func (m *Manager) selectDevicesLocked(deviceIDs []string) ([]Device, error) {
	if len(deviceIDs) == 0 {
		out := make([]Device, 0, len(m.devices))
		for _, d := range m.devices {
			out = append(out, d)
		}
		return out, nil
	}
	out := make([]Device, 0, len(deviceIDs))
	for _, id := range deviceIDs {
		d, ok := m.devices[id]
		if !ok {
			return nil, fmt.Errorf("unknown device_id: %s", id)
		}
		out = append(out, d)
	}
	return out, nil
}

func validateManifest(m Manifest) error {
	if strings.TrimSpace(m.KeyID) == "" {
		return fmt.Errorf("manifest.key_id is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("manifest.version is required")
	}
	if strings.TrimSpace(m.ArtifactURL) == "" {
		return fmt.Errorf("manifest.artifact_url is required")
	}
	if strings.TrimSpace(m.SHA256) == "" {
		return fmt.Errorf("manifest.sha256 is required")
	}
	if len(m.SHA256) != sha256.Size*2 {
		return fmt.Errorf("manifest.sha256 must be 64 hex chars")
	}
	if _, err := hex.DecodeString(m.SHA256); err != nil {
		return fmt.Errorf("manifest.sha256 must be valid hex")
	}
	if strings.TrimSpace(m.Signature) == "" {
		return fmt.Errorf("manifest.signature is required")
	}
	if strings.TrimSpace(m.CreatedAt) == "" {
		return fmt.Errorf("manifest.created_at is required")
	}
	createdAt, err := time.Parse(time.RFC3339, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("manifest.created_at must be RFC3339: %w", err)
	}
	now := time.Now().UTC()
	if createdAt.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("manifest.created_at is in the future")
	}
	if now.Sub(createdAt) > 30*24*time.Hour {
		return fmt.Errorf("manifest.created_at is too old")
	}
	artifactURL, err := url.Parse(m.ArtifactURL)
	if err != nil || artifactURL.Scheme == "" || artifactURL.Host == "" {
		return fmt.Errorf("manifest.artifact_url must be an absolute URL")
	}
	if artifactURL.Scheme != "http" && artifactURL.Scheme != "https" {
		return fmt.Errorf("manifest.artifact_url scheme must be http or https")
	}
	return nil
}
