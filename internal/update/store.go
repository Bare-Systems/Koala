package update

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DeviceStore is the persistence backend for update Manager state.
// All methods must be safe for concurrent use.
type DeviceStore interface {
	UpsertDevice(device Device) error
	GetDevice(id string) (Device, bool)
	ListDevices() []Device
	DeleteDevice(id string) error

	SetStagedManifest(deviceID string, manifest Manifest) error
	GetStagedManifest(deviceID string) (Manifest, bool)
	ClearStagedManifest(deviceID string) error

	UpsertRollout(rollout Rollout) error
	GetRollout(id string) (Rollout, bool)
	ListRollouts() []Rollout

	Close() error
}

// ─── MemoryDeviceStore ────────────────────────────────────────────────────────

// MemoryDeviceStore is an in-memory DeviceStore used in tests and when no
// persistence path is configured.
type MemoryDeviceStore struct {
	mu       sync.RWMutex
	devices  map[string]Device
	staged   map[string]Manifest
	rollouts map[string]Rollout
}

func NewMemoryDeviceStore() *MemoryDeviceStore {
	return &MemoryDeviceStore{
		devices:  map[string]Device{},
		staged:   map[string]Manifest{},
		rollouts: map[string]Rollout{},
	}
}

func (s *MemoryDeviceStore) UpsertDevice(device Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[device.ID] = device
	return nil
}

func (s *MemoryDeviceStore) GetDevice(id string) (Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[id]
	return d, ok
}

func (s *MemoryDeviceStore) ListDevices() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Device, 0, len(s.devices))
	for _, d := range s.devices {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *MemoryDeviceStore) DeleteDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices, id)
	delete(s.staged, id)
	return nil
}

func (s *MemoryDeviceStore) SetStagedManifest(deviceID string, manifest Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staged[deviceID] = manifest
	return nil
}

func (s *MemoryDeviceStore) GetStagedManifest(deviceID string) (Manifest, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.staged[deviceID]
	return m, ok
}

func (s *MemoryDeviceStore) ClearStagedManifest(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.staged, deviceID)
	return nil
}

func (s *MemoryDeviceStore) UpsertRollout(rollout Rollout) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollouts[rollout.ID] = rollout
	return nil
}

func (s *MemoryDeviceStore) GetRollout(id string) (Rollout, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rollouts[id]
	return r, ok
}

func (s *MemoryDeviceStore) ListRollouts() []Rollout {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Rollout, 0, len(s.rollouts))
	for _, r := range s.rollouts {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *MemoryDeviceStore) Close() error { return nil }

// ─── SQLiteDeviceStore ────────────────────────────────────────────────────────

// SQLiteDeviceStore persists update Manager state to a SQLite database.
// It survives process restarts and is the recommended store for production.
type SQLiteDeviceStore struct {
	db *sql.DB
}

// NewSQLiteDeviceStore opens (or creates) the SQLite database at path and runs
// schema migrations.  The directory is created if it does not exist.
func NewSQLiteDeviceStore(path string) (*SQLiteDeviceStore, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	s := &SQLiteDeviceStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteDeviceStore) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS update_devices (
    id TEXT PRIMARY KEY,
    address TEXT NOT NULL DEFAULT '',
    current_version TEXT NOT NULL DEFAULT '',
    previous_version TEXT NOT NULL DEFAULT '',
    target_version TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'idle',
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS update_staged_manifests (
    device_id TEXT PRIMARY KEY,
    manifest_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS update_rollouts (
    id TEXT PRIMARY KEY,
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    manifest_json TEXT NOT NULL DEFAULT '{}',
    batches_json TEXT NOT NULL DEFAULT '[]',
    devices_json TEXT NOT NULL DEFAULT '{}',
    events_json TEXT NOT NULL DEFAULT '[]',
    max_failures INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    rollback_scope TEXT NOT NULL DEFAULT 'failed'
);
`)
	if err != nil {
		return fmt.Errorf("migrate update sqlite: %w", err)
	}
	return nil
}

func (s *SQLiteDeviceStore) UpsertDevice(device Device) error {
	_, err := s.db.Exec(`
INSERT INTO update_devices (id, address, current_version, previous_version, target_version, state, last_error, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    address=excluded.address,
    current_version=excluded.current_version,
    previous_version=excluded.previous_version,
    target_version=excluded.target_version,
    state=excluded.state,
    last_error=excluded.last_error,
    updated_at=excluded.updated_at`,
		device.ID, device.Address, device.CurrentVersion, device.PreviousVersion,
		device.TargetVersion, string(device.State), device.LastError,
		device.UpdatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteDeviceStore) GetDevice(id string) (Device, bool) {
	row := s.db.QueryRow(`SELECT id, address, current_version, previous_version, target_version, state, last_error, updated_at FROM update_devices WHERE id = ?`, id)
	d, err := scanDevice(row)
	if err != nil {
		return Device{}, false
	}
	return d, true
}

func (s *SQLiteDeviceStore) ListDevices() []Device {
	rows, err := s.db.Query(`SELECT id, address, current_version, previous_version, target_version, state, last_error, updated_at FROM update_devices ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err == nil {
			out = append(out, d)
		}
	}
	return out
}

func (s *SQLiteDeviceStore) DeleteDevice(id string) error {
	_, err := s.db.Exec(`DELETE FROM update_devices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM update_staged_manifests WHERE device_id = ?`, id)
	return nil
}

func (s *SQLiteDeviceStore) SetStagedManifest(deviceID string, manifest Manifest) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
INSERT INTO update_staged_manifests (device_id, manifest_json) VALUES (?, ?)
ON CONFLICT(device_id) DO UPDATE SET manifest_json=excluded.manifest_json`,
		deviceID, string(raw))
	return err
}

func (s *SQLiteDeviceStore) GetStagedManifest(deviceID string) (Manifest, bool) {
	var raw string
	if err := s.db.QueryRow(`SELECT manifest_json FROM update_staged_manifests WHERE device_id = ?`, deviceID).Scan(&raw); err != nil {
		return Manifest{}, false
	}
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return Manifest{}, false
	}
	return m, true
}

func (s *SQLiteDeviceStore) ClearStagedManifest(deviceID string) error {
	_, err := s.db.Exec(`DELETE FROM update_staged_manifests WHERE device_id = ?`, deviceID)
	return err
}

func (s *SQLiteDeviceStore) UpsertRollout(rollout Rollout) error {
	manifestJSON, _ := json.Marshal(rollout.Manifest)
	batchesJSON, _ := json.Marshal(rollout.Batches)
	devicesJSON, _ := json.Marshal(rollout.Devices)
	eventsJSON, _ := json.Marshal(rollout.Events)
	_, err := s.db.Exec(`
INSERT INTO update_rollouts (id, mode, status, created_at, updated_at, manifest_json, batches_json, devices_json, events_json, max_failures, failure_count, rollback_scope)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    status=excluded.status,
    updated_at=excluded.updated_at,
    batches_json=excluded.batches_json,
    devices_json=excluded.devices_json,
    events_json=excluded.events_json,
    failure_count=excluded.failure_count`,
		rollout.ID, string(rollout.Mode), string(rollout.Status),
		rollout.CreatedAt.UTC().Format(time.RFC3339),
		rollout.UpdatedAt.UTC().Format(time.RFC3339),
		string(manifestJSON), string(batchesJSON), string(devicesJSON), string(eventsJSON),
		rollout.MaxFailures, rollout.FailureCount, rollout.RollbackScope,
	)
	return err
}

func (s *SQLiteDeviceStore) GetRollout(id string) (Rollout, bool) {
	row := s.db.QueryRow(`SELECT id, mode, status, created_at, updated_at, manifest_json, batches_json, devices_json, events_json, max_failures, failure_count, rollback_scope FROM update_rollouts WHERE id = ?`, id)
	r, err := scanRollout(row)
	if err != nil {
		return Rollout{}, false
	}
	return r, true
}

func (s *SQLiteDeviceStore) ListRollouts() []Rollout {
	rows, err := s.db.Query(`SELECT id, mode, status, created_at, updated_at, manifest_json, batches_json, devices_json, events_json, max_failures, failure_count, rollback_scope FROM update_rollouts ORDER BY created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Rollout
	for rows.Next() {
		r, err := scanRollout(rows)
		if err == nil {
			out = append(out, r)
		}
	}
	return out
}

func (s *SQLiteDeviceStore) Close() error { return s.db.Close() }

// ─── scan helpers ─────────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(...any) error
}

func scanDevice(row rowScanner) (Device, error) {
	var d Device
	var updatedAt, state string
	if err := row.Scan(&d.ID, &d.Address, &d.CurrentVersion, &d.PreviousVersion,
		&d.TargetVersion, &state, &d.LastError, &updatedAt); err != nil {
		return Device{}, err
	}
	d.State = DeviceState(state)
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		d.UpdatedAt = t
	}
	return d, nil
}

func scanRollout(row rowScanner) (Rollout, error) {
	var r Rollout
	var createdAt, updatedAt, mode, status string
	var manifestJSON, batchesJSON, devicesJSON, eventsJSON string
	if err := row.Scan(&r.ID, &mode, &status, &createdAt, &updatedAt,
		&manifestJSON, &batchesJSON, &devicesJSON, &eventsJSON,
		&r.MaxFailures, &r.FailureCount, &r.RollbackScope); err != nil {
		return Rollout{}, err
	}
	r.Mode = RolloutMode(mode)
	r.Status = RolloutStatus(status)
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		r.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		r.UpdatedAt = t
	}
	_ = json.Unmarshal([]byte(manifestJSON), &r.Manifest)
	_ = json.Unmarshal([]byte(batchesJSON), &r.Batches)
	_ = json.Unmarshal([]byte(devicesJSON), &r.Devices)
	_ = json.Unmarshal([]byte(eventsJSON), &r.Events)
	return r, nil
}
