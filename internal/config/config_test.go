package config

import "testing"

func TestValidateUpdateKeyRequiredWhenEnabled(t *testing.T) {
	cfg := Config{
		MCPToken: "token",
		Worker:   WorkerConfig{URL: "http://worker:8090"},
		Update:   UpdateConfig{Enabled: true, PublicKeyBase64: "", EncryptionKeyBase64: ""},
		Cameras:  []CameraConfig{{ID: "cam1", ZoneID: "front_door"}},
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
		Cameras:  []CameraConfig{{ID: "cam1", ZoneID: "front_door"}},
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
		Cameras:  []CameraConfig{{ID: "cam1", ZoneID: "front_door"}},
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
		Cameras: []CameraConfig{{ID: "cam1", ZoneID: "front_door"}},
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
		Cameras: []CameraConfig{{ID: "cam1", ZoneID: "front_door"}},
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
		Cameras: []CameraConfig{{ID: "cam1", ZoneID: "front_door"}},
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
		Cameras: []CameraConfig{{ID: "cam1", ZoneID: "front_door"}},
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
		Cameras: []CameraConfig{{ID: "cam1", ZoneID: "front_door"}},
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for missing poll manifest_url")
	}
}
