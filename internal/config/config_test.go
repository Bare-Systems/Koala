package config

import (
	"strings"
	"testing"
)

// frontDoorCamera is a minimal valid camera for tests that don't care about camera specifics.
var frontDoorCamera = CameraConfig{ID: "cam1", ZoneID: "front_door", FrontDoor: true}

func TestValidateUpdateKeyRequiredWhenEnabled(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update:   UpdateConfig{Enabled: true, PublicKeyBase64: "", EncryptionKeyBase64: ""},
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for missing update key")
	}
}

func TestValidateUpdateEncryptionKeyRequiredWhenEnabled(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update:   UpdateConfig{Enabled: true, PublicKeyBase64: "abc", EncryptionKeyBase64: ""},
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for missing update encryption key")
	}
}

func TestValidateUpdateKeyNotRequiredWhenDisabled(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update:   UpdateConfig{Enabled: false},
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateUpdateRotationConfig(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update: UpdateConfig{
			Enabled:             true,
			ActiveKeyID:         "key-2026-03",
			PreviousKeys:        []string{"key-2026-02"},
			PublicKeys:          map[string]string{"key-2026-03": "abc", "key-2026-02": "def"},
			EncryptionKeyBase64: "xyz",
		},
		Cameras: []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateUpdateRotationPreviousMissingKey(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update: UpdateConfig{
			Enabled:             true,
			ActiveKeyID:         "key-2026-03",
			PreviousKeys:        []string{"key-2026-02"},
			PublicKeys:          map[string]string{"key-2026-03": "abc"},
			EncryptionKeyBase64: "xyz",
		},
		Cameras: []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for missing previous key")
	}
}

func TestValidateRotationOnlyRejectsLegacyKey(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update: UpdateConfig{
			Enabled:             true,
			RotationOnlyMode:    true,
			PublicKeyBase64:     "legacy",
			ActiveKeyID:         "key-2026-03",
			PublicKeys:          map[string]string{"key-2026-03": "abc"},
			EncryptionKeyBase64: "xyz",
		},
		Cameras: []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for legacy key in rotation-only mode")
	}
}

func TestValidateLegacyModeAllowsPublicKeyBase64(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update: UpdateConfig{
			Enabled:             true,
			RotationOnlyMode:    false,
			PublicKeyBase64:     "legacy",
			EncryptionKeyBase64: "xyz",
		},
		Cameras: []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected legacy mode to allow public_key_base64: %v", err)
	}
}

func TestValidatePollRequiresManifestURL(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update: UpdateConfig{
			Enabled:             true,
			RotationOnlyMode:    false,
			PublicKeyBase64:     "legacy",
			EncryptionKeyBase64: "xyz",
			PollEnabled:         true,
			ManifestURL:         "",
		},
		Cameras: []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for missing poll manifest_url")
	}
}

// ─── Config version tests ─────────────────────────────────────────────────────

func TestValidateConfigVersionDefault(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected default config_version to pass: %v", err)
	}
}

func TestValidateConfigVersionUnsupported(t *testing.T) {
	cfg := Config{
		ConfigVersion: "99",
		MCPToken:      "token",
		Worker:        WorkerConfig{URL: "http://worker:8090"},
		Cameras:       []CameraConfig{frontDoorCamera},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for unsupported config_version")
	}
}

// ─── Camera validation tests ──────────────────────────────────────────────────

func TestValidateDuplicateCameraID(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Cameras: []CameraConfig{
			{ID: "cam1", ZoneID: "z1", FrontDoor: true},
			{ID: "cam1", ZoneID: "z1"},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for duplicate camera id")
	}
}

func TestValidateNoFrontDoorCamera(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Cameras:  []CameraConfig{{ID: "cam1", ZoneID: "z1", FrontDoor: false}},
	}
	cfg.applyDefaults()
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for missing front_door camera")
	}
	if !strings.Contains(err.Error(), "front_door") {
		t.Fatalf("expected front_door in error, got: %v", err)
	}
}

func TestValidateCameraUnknownZoneRef(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Zones:    []ZoneConfig{{ID: "front_door", Name: "Front Door"}},
		Cameras:  []CameraConfig{{ID: "cam1", ZoneID: "does_not_exist", FrontDoor: true}},
	}
	cfg.applyDefaults()
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error for unknown zone_id reference")
	}
	if !strings.Contains(err.Error(), "does_not_exist") {
		t.Fatalf("expected zone id in error, got: %v", err)
	}
}

func TestValidateCameraConfidenceThresholdOutOfRange(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Cameras:  []CameraConfig{{ID: "cam1", ZoneID: "z1", FrontDoor: true, ConfidenceThreshold: 1.5}},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for confidence_threshold > 1")
	}
}

func TestValidateCameraMaxFPSNegative(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Cameras:  []CameraConfig{{ID: "cam1", ZoneID: "z1", FrontDoor: true, MaxFPS: -1}},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for negative max_fps")
	}
}

// ─── Zone validation tests ────────────────────────────────────────────────────

func TestValidateDuplicateZoneID(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Zones: []ZoneConfig{
			{ID: "front_door", Name: "Front Door"},
			{ID: "front_door", Name: "Duplicate"},
		},
		Cameras: []CameraConfig{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for duplicate zone id")
	}
}

func TestValidateZoneConfidenceThresholdOutOfRange(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Zones:    []ZoneConfig{{ID: "front_door", Name: "Front Door", ConfidenceThreshold: -0.1}},
		Cameras:  []CameraConfig{{ID: "cam1", ZoneID: "front_door", FrontDoor: true}},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for negative zone confidence_threshold")
	}
}

// ─── Worker protocol tests ────────────────────────────────────────────────────

func TestWorkerProtocol_GRPCExplicit_RequiresGRPCAddr(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{Protocol: "grpc"}, // no GRPCAddr
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	// applyDefaults sets default grpc_addr, so this should still pass.
	if err := cfg.Validate(); err != nil {
		t.Fatalf("grpc with default addr should be valid: %v", err)
	}
	if cfg.Worker.GRPCAddr == "" {
		t.Fatal("expected default GRPCAddr to be set")
	}
}

func TestWorkerProtocol_GRPCExplicit_CustomAddr(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{Protocol: "grpc", GRPCAddr: "worker.local:50051"},
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("explicit grpc+addr should be valid: %v", err)
	}
}

func TestWorkerProtocol_HTTPExplicit_RequiresURL(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{Protocol: "http"}, // no URL
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for http protocol without url")
	}
}

func TestWorkerProtocol_AutoDetect_URLOnly_BecomesHTTP(t *testing.T) {
	// Existing config style: only worker.url set — must auto-detect as "http".
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if cfg.Worker.Protocol != "http" {
		t.Fatalf("expected auto-detected protocol=http, got %q", cfg.Worker.Protocol)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("url-only config must still validate: %v", err)
	}
}

func TestWorkerProtocol_AutoDetect_GRPCAddrOnly_BecomesGRPC(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{GRPCAddr: "worker:50051"},
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if cfg.Worker.Protocol != "grpc" {
		t.Fatalf("expected auto-detected protocol=grpc, got %q", cfg.Worker.Protocol)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("grpc_addr-only config must validate: %v", err)
	}
}

func TestWorkerProtocol_Unknown_FailsValidation(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{Protocol: "websocket", URL: "ws://worker"},
		Cameras:  []CameraConfig{frontDoorCamera},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for unknown protocol")
	}
}

func TestValidateValidWithZonesAndCameras(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Zones: []ZoneConfig{
			{ID: "front_door", Name: "Front Door"},
			{ID: "garage", Name: "Garage"},
		},
		Cameras: []CameraConfig{
			{ID: "cam1", ZoneID: "front_door", FrontDoor: true, ConfidenceThreshold: 0.5, MaxFPS: 2},
			{ID: "cam2", ZoneID: "garage", ConfidenceThreshold: 0.4},
		},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
