package camera

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCapabilityCache_FreshStart_NoError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache, err := LoadCapabilityCache(path)
	if err != nil {
		t.Fatalf("load on non-existent file: %v", err)
	}
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
}

func TestCapabilityCache_CorruptFile_StartsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(path, []byte("not valid json!!!"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	cache, err := LoadCapabilityCache(path)
	if err != nil {
		t.Fatalf("corrupt file must not fail startup, got: %v", err)
	}
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
}

func TestCapabilityCache_WarmPrimesUnknownCamera(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")

	// Snapshot a registry with a known-available camera.
	reg1 := NewRegistry([]Camera{{ID: "cam1", ZoneID: "z1", Status: StatusAvailable}})
	cache1, err := LoadCapabilityCache(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cache1.Snapshot(reg1); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Reload cache into a registry that starts at StatusUnknown.
	reg2 := NewRegistry([]Camera{{ID: "cam1", ZoneID: "z1", Status: StatusUnknown}})
	cache2, err := LoadCapabilityCache(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	cache2.Warm(reg2)

	cam, ok := reg2.Get("cam1")
	if !ok {
		t.Fatal("camera not found after Warm")
	}
	if cam.Status != StatusAvailable {
		t.Fatalf("expected available after Warm, got %s", cam.Status)
	}
}

func TestCapabilityCache_WarmDoesNotOverrideLiveStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")

	// Cache records an unavailable camera.
	reg1 := NewRegistry([]Camera{{ID: "cam1", ZoneID: "z1", Status: StatusUnavailable}})
	cache1, _ := LoadCapabilityCache(path)
	_ = cache1.Snapshot(reg1)

	// Live registry already resolved the camera as available (probe succeeded).
	reg2 := NewRegistry([]Camera{{ID: "cam1", ZoneID: "z1", Status: StatusAvailable}})
	cache2, _ := LoadCapabilityCache(path)
	cache2.Warm(reg2) // must not overwrite StatusAvailable

	cam, _ := reg2.Get("cam1")
	if cam.Status != StatusAvailable {
		t.Fatalf("Warm must not overwrite live probe result; got %s", cam.Status)
	}
}

func TestCapabilityCache_RoundTrip_CapabilityPreserved(t *testing.T) {
	dir := t.TempDir()
	// Use a sub-directory that doesn't exist yet to test MkdirAll.
	path := filepath.Join(dir, "sub", "cache.json")

	wantCap := Capability{
		RTSPReachable:  true,
		SelectedSource: "rtsp",
		LastProbedAt:   "2026-03-01T00:00:00Z",
	}
	reg1 := NewRegistry([]Camera{{
		ID:         "cam1",
		ZoneID:     "z1",
		Status:     StatusAvailable,
		Capability: wantCap,
	}})
	cache1, err := LoadCapabilityCache(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cache1.Snapshot(reg1); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Reload and warm a fresh registry.
	reg2 := NewRegistry([]Camera{{ID: "cam1", ZoneID: "z1", Status: StatusUnknown}})
	cache2, err := LoadCapabilityCache(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	cache2.Warm(reg2)

	cam, ok := reg2.Get("cam1")
	if !ok {
		t.Fatal("camera not found after round-trip")
	}
	if cam.Status != StatusAvailable {
		t.Fatalf("expected available, got %s", cam.Status)
	}
	if !cam.Capability.RTSPReachable {
		t.Fatal("expected RTSPReachable=true after round-trip")
	}
	if cam.Capability.SelectedSource != "rtsp" {
		t.Fatalf("expected source=rtsp, got %q", cam.Capability.SelectedSource)
	}
}

func TestCapabilityCache_UnknownCameraInCache_Ignored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")

	// Cache has cam1, but new registry only has cam2.
	reg1 := NewRegistry([]Camera{{ID: "cam1", ZoneID: "z1", Status: StatusAvailable}})
	cache1, _ := LoadCapabilityCache(path)
	_ = cache1.Snapshot(reg1)

	reg2 := NewRegistry([]Camera{{ID: "cam2", ZoneID: "z2", Status: StatusUnknown}})
	cache2, _ := LoadCapabilityCache(path)
	cache2.Warm(reg2) // cam1 entry should have no effect

	cam2, _ := reg2.Get("cam2")
	if cam2.Status != StatusUnknown {
		t.Fatalf("cam2 should remain unknown (no cache entry), got %s", cam2.Status)
	}
}
