package update

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// storeFactory is a function that creates a DeviceStore for tests.
// The returned cleanup func removes any on-disk state.
type storeFactory func(t *testing.T) (DeviceStore, func())

func memFactory(t *testing.T) (DeviceStore, func()) {
	t.Helper()
	return NewMemoryDeviceStore(), func() {}
}

func sqliteFactory(t *testing.T) (DeviceStore, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "update_test.db")
	s, err := NewSQLiteDeviceStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteDeviceStore: %v", err)
	}
	return s, func() { _ = s.Close() }
}

var factories = []struct {
	name string
	fn   storeFactory
}{
	{"memory", memFactory},
	{"sqlite", sqliteFactory},
}

// ─── Device CRUD ──────────────────────────────────────────────────────────────

func TestDeviceStore_UpsertAndGet(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			d := Device{
				ID:             "dev-1",
				Address:        "http://10.0.0.1:6705",
				CurrentVersion: "1.0.0",
				State:          StateHealthy,
				UpdatedAt:      time.Now().UTC().Truncate(time.Second),
			}
			if err := s.UpsertDevice(d); err != nil {
				t.Fatalf("UpsertDevice: %v", err)
			}
			got, ok := s.GetDevice("dev-1")
			if !ok {
				t.Fatal("expected to find device dev-1")
			}
			if got.ID != d.ID || got.CurrentVersion != d.CurrentVersion || got.State != d.State {
				t.Fatalf("device mismatch: %+v", got)
			}
		})
	}
}

func TestDeviceStore_Upsert_UpdatesExisting(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			d := Device{ID: "dev-1", CurrentVersion: "1.0.0", State: StateIdle, UpdatedAt: time.Now().UTC()}
			_ = s.UpsertDevice(d)

			d.State = StateHealthy
			d.CurrentVersion = "2.0.0"
			_ = s.UpsertDevice(d)

			got, _ := s.GetDevice("dev-1")
			if got.State != StateHealthy || got.CurrentVersion != "2.0.0" {
				t.Fatalf("expected updated device, got %+v", got)
			}
		})
	}
}

func TestDeviceStore_GetDevice_NotFound(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()
			_, ok := s.GetDevice("nonexistent")
			if ok {
				t.Fatal("expected not found")
			}
		})
	}
}

func TestDeviceStore_ListDevices(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			_ = s.UpsertDevice(Device{ID: "b", CurrentVersion: "1.0.0", State: StateIdle, UpdatedAt: time.Now().UTC()})
			_ = s.UpsertDevice(Device{ID: "a", CurrentVersion: "1.0.0", State: StateHealthy, UpdatedAt: time.Now().UTC()})
			_ = s.UpsertDevice(Device{ID: "c", CurrentVersion: "1.0.0", State: StateIdle, UpdatedAt: time.Now().UTC()})

			devices := s.ListDevices()
			if len(devices) != 3 {
				t.Fatalf("expected 3 devices, got %d", len(devices))
			}
			// Should be sorted by ID.
			ids := make([]string, len(devices))
			for i, d := range devices {
				ids[i] = d.ID
			}
			if !sort.StringsAreSorted(ids) {
				t.Fatalf("expected devices sorted by ID, got %v", ids)
			}
		})
	}
}

func TestDeviceStore_DeleteDevice(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			_ = s.UpsertDevice(Device{ID: "dev-1", State: StateIdle, UpdatedAt: time.Now().UTC()})
			// Staged manifest should also be cleared on delete.
			_ = s.SetStagedManifest("dev-1", validTestManifest(t))

			if err := s.DeleteDevice("dev-1"); err != nil {
				t.Fatalf("DeleteDevice: %v", err)
			}
			if _, ok := s.GetDevice("dev-1"); ok {
				t.Fatal("expected device to be deleted")
			}
			if _, ok := s.GetStagedManifest("dev-1"); ok {
				t.Fatal("expected staged manifest to be cleared on device delete")
			}
		})
	}
}

// ─── Staged manifest ──────────────────────────────────────────────────────────

func TestDeviceStore_StagedManifest_RoundTrip(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			m := validTestManifest(t)
			if err := s.SetStagedManifest("dev-1", m); err != nil {
				t.Fatalf("SetStagedManifest: %v", err)
			}
			got, ok := s.GetStagedManifest("dev-1")
			if !ok {
				t.Fatal("expected staged manifest")
			}
			if got.Version != m.Version || got.KeyID != m.KeyID {
				t.Fatalf("manifest mismatch: %+v", got)
			}
		})
	}
}

func TestDeviceStore_StagedManifest_Clear(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			_ = s.SetStagedManifest("dev-1", validTestManifest(t))
			if err := s.ClearStagedManifest("dev-1"); err != nil {
				t.Fatalf("ClearStagedManifest: %v", err)
			}
			if _, ok := s.GetStagedManifest("dev-1"); ok {
				t.Fatal("expected no staged manifest after clear")
			}
		})
	}
}

// ─── Rollout persistence ──────────────────────────────────────────────────────

func TestDeviceStore_Rollout_RoundTrip(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			now := time.Now().UTC().Truncate(time.Second)
			r := Rollout{
				ID:            "r-001",
				Mode:          RolloutModeCanary,
				Status:        RolloutStatusCompleted,
				CreatedAt:     now,
				UpdatedAt:     now,
				Manifest:      validTestManifest(t),
				MaxFailures:   2,
				FailureCount:  0,
				RollbackScope: "failed",
				Events:        []string{"rollout_started", "batch_0_completed"},
				Batches:       []RolloutBatch{{Index: 0, DeviceIDs: []string{"dev-1"}, Completed: true}},
				Devices:       map[string]DeviceRolloutState{"dev-1": {DeviceID: "dev-1", State: StateHealthy}},
			}
			if err := s.UpsertRollout(r); err != nil {
				t.Fatalf("UpsertRollout: %v", err)
			}
			got, ok := s.GetRollout("r-001")
			if !ok {
				t.Fatal("expected rollout")
			}
			if got.ID != r.ID || got.Mode != r.Mode || got.Status != r.Status {
				t.Fatalf("rollout mismatch: %+v", got)
			}
			if len(got.Events) != len(r.Events) {
				t.Fatalf("events mismatch: got %v", got.Events)
			}
		})
	}
}

func TestDeviceStore_Rollout_UpdateStatus(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			now := time.Now().UTC()
			r := Rollout{ID: "r-002", Mode: RolloutModeAll, Status: RolloutStatusRunning, CreatedAt: now, UpdatedAt: now, RollbackScope: "failed", Devices: map[string]DeviceRolloutState{}, Batches: []RolloutBatch{}, Events: []string{"rollout_started"}}
			_ = s.UpsertRollout(r)

			r.Status = RolloutStatusCompleted
			r.FailureCount = 0
			_ = s.UpsertRollout(r)

			got, _ := s.GetRollout("r-002")
			if got.Status != RolloutStatusCompleted {
				t.Fatalf("expected completed, got %s", got.Status)
			}
		})
	}
}

func TestDeviceStore_Rollout_List(t *testing.T) {
	for _, f := range factories {
		t.Run(f.name, func(t *testing.T) {
			s, cleanup := f.fn(t)
			defer cleanup()

			now := time.Now().UTC()
			for i, id := range []string{"r-001", "r-002", "r-003"} {
				_ = s.UpsertRollout(Rollout{
					ID:            id,
					Mode:          RolloutModeAll,
					Status:        RolloutStatusCompleted,
					CreatedAt:     now.Add(time.Duration(i) * time.Second),
					UpdatedAt:     now,
					RollbackScope: "failed",
					Devices:       map[string]DeviceRolloutState{},
					Batches:       []RolloutBatch{},
					Events:        []string{},
				})
			}
			rollouts := s.ListRollouts()
			if len(rollouts) != 3 {
				t.Fatalf("expected 3 rollouts, got %d", len(rollouts))
			}
		})
	}
}

// ─── SQLite restart persistence ───────────────────────────────────────────────

func TestSQLiteDeviceStore_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.db")

	// Write state.
	s1, err := NewSQLiteDeviceStore(path)
	if err != nil {
		t.Fatalf("open store 1: %v", err)
	}
	d := Device{ID: "dev-jetson", Address: "http://192.168.1.10:6705", CurrentVersion: "1.0.0", State: StateHealthy, UpdatedAt: time.Now().UTC()}
	_ = s1.UpsertDevice(d)
	_ = s1.SetStagedManifest("dev-jetson", validTestManifest(t))
	_ = s1.Close()

	// Re-open and verify state survived.
	s2, err := NewSQLiteDeviceStore(path)
	if err != nil {
		t.Fatalf("open store 2: %v", err)
	}
	defer s2.Close()

	got, ok := s2.GetDevice("dev-jetson")
	if !ok {
		t.Fatal("device not found after restart")
	}
	if got.CurrentVersion != "1.0.0" || got.State != StateHealthy {
		t.Fatalf("device state wrong after restart: %+v", got)
	}
	_, ok = s2.GetStagedManifest("dev-jetson")
	if !ok {
		t.Fatal("staged manifest not found after restart")
	}
}

func TestSQLiteDeviceStore_CorruptPath_ReturnsError(t *testing.T) {
	// Directory path as db path should fail.
	dir := t.TempDir()
	badPath := filepath.Join(dir, "not-a-dir", "sub", "update.db")
	// Create a file at a parent path to cause MkdirAll to fail.
	if err := os.WriteFile(filepath.Join(dir, "not-a-dir"), []byte("file"), 0o644); err != nil {
		t.Skip("cannot create blocking file")
	}
	_, err := NewSQLiteDeviceStore(badPath)
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

// ─── helper ───────────────────────────────────────────────────────────────────

func validTestManifest(t *testing.T) Manifest {
	t.Helper()
	return Manifest{
		KeyID:       "key-2026-03",
		Version:     "2.0.0",
		ArtifactURL: "http://update.local/bundles/2.0.0.bin",
		SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab", // 63×'a' + 'b' = 64 hex chars
		Signature:   "dGVzdHNpZ25hdHVyZQ==",
		CreatedAt:   time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339),
	}
}
