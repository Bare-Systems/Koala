package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ConfigVersion string         `yaml:"config_version"`
	ListenAddr    string         `yaml:"listen_addr"`
	MCPToken      string         `yaml:"mcp_token"`
	// AllowedIPs is an optional list of IP addresses or CIDR ranges
	// permitted to call the MCP server. Empty = allow all (default).
	AllowedIPs []string      `yaml:"allowed_ips"`
	Service    ServiceConfig `yaml:"service"`
	Worker     WorkerConfig  `yaml:"worker"`
	Update     UpdateConfig  `yaml:"update"`
	Runtime    RuntimeConfig `yaml:"runtime"`
	Privacy    PrivacyConfig `yaml:"privacy"`
	Cameras    []CameraConfig `yaml:"cameras"`
	Zones      []ZoneConfig   `yaml:"zones"`
}

// PrivacyConfig controls data retention and frame buffer behaviour.
// Koala's default stance is "metadata-only": only detection labels, confidence
// scores, and timestamps are retained — never raw pixel data. Enable
// frame_buffer_enabled only when live clip analysis is explicitly needed.
type PrivacyConfig struct {
	// FrameBufferEnabled controls whether raw frame data (frame_b64) is
	// forwarded to the inference worker. Default false (metadata-only).
	FrameBufferEnabled bool `yaml:"frame_buffer_enabled"`
	// MetadataRetentionSeconds caps how long detection metadata is kept in
	// the sliding-window aggregator. 0 means use runtime.freshness_window_seconds.
	MetadataRetentionSeconds int `yaml:"metadata_retention_seconds"`
}

type ServiceConfig struct {
	DeviceID string `yaml:"device_id"`
	Version  string `yaml:"version"`
	Address  string `yaml:"address"`
}

type WorkerConfig struct {
	// URL is the HTTP endpoint for the private worker inference transport.
	URL string `yaml:"url"`
}

type UpdateConfig struct {
	Enabled             bool              `yaml:"enabled"`
	RotationOnlyMode    bool              `yaml:"rotation_only_mode"`
	PollEnabled         bool              `yaml:"poll_enabled"`
	PollIntervalSeconds int               `yaml:"poll_interval_seconds"`
	PollJitterSeconds   int               `yaml:"poll_jitter_seconds"`
	ManifestURL         string            `yaml:"manifest_url"`
	PublicKeyBase64     string            `yaml:"public_key_base64"`
	ActiveKeyID         string            `yaml:"active_key_id"`
	PreviousKeys        []string          `yaml:"previous_keys"`
	PublicKeys          map[string]string `yaml:"public_keys"`
	EncryptionKeyBase64 string            `yaml:"encryption_key_base64"`
	AuditDBPath         string            `yaml:"audit_db_path"`
	StagingDir          string            `yaml:"staging_dir"`
	ActiveDir           string            `yaml:"active_dir"`
}

type RuntimeConfig struct {
	QueueSize       int `yaml:"queue_size"`
	FreshnessWindow int `yaml:"freshness_window_seconds"`
	// MinDetections is the temporal smoothing threshold for the state aggregator.
	// An entity is declared present only when at least this many detections exist
	// in the freshness window. 0 (default) disables smoothing.
	MinDetections int `yaml:"min_detections"`
	// ConfidenceThreshold is the global fallback minimum detection confidence.
	// Applied when no per-camera or per-zone threshold is configured.
	// 0 disables the global threshold (default). Valid range: 0–1.
	ConfidenceThreshold   float64 `yaml:"confidence_threshold"`
	EnableStreamWorkers   bool    `yaml:"enable_stream_workers"`
	StreamSampleFPS       int     `yaml:"stream_sample_fps"`
	StreamCaptureTimeoutS int     `yaml:"stream_capture_timeout_seconds"`
	// CapabilityCachePath is the path to the JSON file used to persist
	// last-known camera probe results across restarts. Empty = no caching.
	CapabilityCachePath string `yaml:"capability_cache_path"`
}

type CameraConfig struct {
	ID                  string  `yaml:"id"`
	Name                string  `yaml:"name"`
	RTSPURL             string  `yaml:"rtsp_url"`
	ONVIFURL            string  `yaml:"onvif_url"`
	ZoneID              string  `yaml:"zone_id"`
	FrontDoor           bool    `yaml:"front_door"`
	ProbeAtBoot         bool    `yaml:"probe_at_boot"`
	ConfidenceThreshold float64 `yaml:"confidence_threshold"` // overrides zone/global default; 0 = use default
	MaxFPS              int     `yaml:"max_fps"`              // per-camera frame rate cap; 0 = use runtime default
}

type ZoneConfig struct {
	ID                  string      `yaml:"id"`
	Name                string      `yaml:"name"`
	ConfidenceThreshold float64     `yaml:"confidence_threshold"` // overrides global default for all cameras in zone; 0 = use global
	// Polygon defines the region of interest in normalized (0–1) frame coordinates.
	// Each element is [x, y]. If empty, no polygon filtering is applied.
	Polygon        [][]float64 `yaml:"polygon"`
	MinBBoxOverlap float64     `yaml:"min_bbox_overlap"` // minimum bbox overlap fraction; 0 uses default of 0.3
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode yaml: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.ConfigVersion == "" {
		c.ConfigVersion = "1"
	}
	if c.ListenAddr == "" {
		c.ListenAddr = ":6705"
	}
	if c.Runtime.QueueSize <= 0 {
		c.Runtime.QueueSize = 64
	}
	if c.Runtime.FreshnessWindow <= 0 {
		c.Runtime.FreshnessWindow = 90
	}
	if c.Runtime.StreamSampleFPS <= 0 {
		c.Runtime.StreamSampleFPS = 1
	}
	if c.Runtime.StreamCaptureTimeoutS <= 0 {
		c.Runtime.StreamCaptureTimeoutS = 5
	}
	if c.Service.DeviceID == "" {
		c.Service.DeviceID = "koala-local"
	}
	if c.Service.Version == "" {
		c.Service.Version = "0.1.0-dev"
	}
	if c.Service.Address == "" {
		c.Service.Address = "http://127.0.0.1:6705"
	}
	if c.Update.StagingDir == "" {
		c.Update.StagingDir = "/tmp/koala/staging"
	}
	if c.Update.ActiveDir == "" {
		c.Update.ActiveDir = "/tmp/koala/active"
	}
	if c.Update.AuditDBPath == "" {
		c.Update.AuditDBPath = "/tmp/koala/audit/events.db"
	}
	if c.Update.PollIntervalSeconds <= 0 {
		c.Update.PollIntervalSeconds = 300
	}
	if c.Update.PollJitterSeconds < 0 {
		c.Update.PollJitterSeconds = 0
	}
}

func (c Config) Validate() error {
	if c.ConfigVersion != "1" {
		return fmt.Errorf("config_version %q is not supported; expected \"1\"", c.ConfigVersion)
	}
	if c.MCPToken == "" {
		return fmt.Errorf("mcp_token is required")
	}
	if c.Runtime.MinDetections < 0 {
		return fmt.Errorf("runtime.min_detections must be >= 0")
	}
	if c.Runtime.ConfidenceThreshold < 0 || c.Runtime.ConfidenceThreshold > 1 {
		return fmt.Errorf("runtime.confidence_threshold must be between 0 and 1")
	}
	if c.Worker.URL == "" {
		return fmt.Errorf("worker.url is required")
	}
	if len(c.Cameras) == 0 {
		return fmt.Errorf("at least one camera is required")
	}

	// Build zone ID set for ref validation.
	zoneIDs := make(map[string]struct{}, len(c.Zones))
	for _, z := range c.Zones {
		if z.ID == "" {
			return fmt.Errorf("zone.id is required")
		}
		if _, dup := zoneIDs[z.ID]; dup {
			return fmt.Errorf("duplicate zone id %q", z.ID)
		}
		zoneIDs[z.ID] = struct{}{}
		if z.ConfidenceThreshold < 0 || z.ConfidenceThreshold > 1 {
			return fmt.Errorf("zone %q: confidence_threshold must be between 0 and 1", z.ID)
		}
		if len(z.Polygon) > 0 && len(z.Polygon) < 3 {
			return fmt.Errorf("zone %q: polygon must have at least 3 vertices or be empty", z.ID)
		}
		for i, pt := range z.Polygon {
			if len(pt) != 2 {
				return fmt.Errorf("zone %q: polygon[%d] must be [x, y]", z.ID, i)
			}
		}
		if z.MinBBoxOverlap < 0 || z.MinBBoxOverlap > 1 {
			return fmt.Errorf("zone %q: min_bbox_overlap must be between 0 and 1", z.ID)
		}
	}

	cameraIDs := make(map[string]struct{}, len(c.Cameras))
	hasFrontDoor := false
	for _, camera := range c.Cameras {
		if camera.ID == "" {
			return fmt.Errorf("camera.id is required")
		}
		if _, dup := cameraIDs[camera.ID]; dup {
			return fmt.Errorf("duplicate camera id %q", camera.ID)
		}
		cameraIDs[camera.ID] = struct{}{}

		if camera.ZoneID == "" {
			return fmt.Errorf("camera %q: zone_id is required", camera.ID)
		}
		if len(c.Zones) > 0 {
			if _, ok := zoneIDs[camera.ZoneID]; !ok {
				return fmt.Errorf("camera %q references unknown zone_id %q", camera.ID, camera.ZoneID)
			}
		}
		if camera.ConfidenceThreshold < 0 || camera.ConfidenceThreshold > 1 {
			return fmt.Errorf("camera %q: confidence_threshold must be between 0 and 1", camera.ID)
		}
		if camera.MaxFPS < 0 {
			return fmt.Errorf("camera %q: max_fps must be >= 0", camera.ID)
		}
		if camera.FrontDoor {
			hasFrontDoor = true
		}
	}
	if !hasFrontDoor {
		return fmt.Errorf("at least one camera must have front_door: true")
	}

	if c.Update.Enabled {
		if strings.TrimSpace(c.Update.EncryptionKeyBase64) == "" {
			return fmt.Errorf("update.encryption_key_base64 is required when update.enabled=true")
		}
		if c.Update.RotationOnlyMode {
			if strings.TrimSpace(c.Update.PublicKeyBase64) != "" {
				return fmt.Errorf("update.public_key_base64 is deprecated and not allowed when update.rotation_only_mode=true")
			}
			if err := validateRotationConfig(c.Update); err != nil {
				return err
			}
		} else if strings.TrimSpace(c.Update.PublicKeyBase64) == "" {
			if err := validateRotationConfig(c.Update); err != nil {
				return err
			}
		}
		if c.Update.PollEnabled {
			if strings.TrimSpace(c.Update.ManifestURL) == "" {
				return fmt.Errorf("update.manifest_url is required when update.poll_enabled=true")
			}
			if c.Update.PollIntervalSeconds <= 0 {
				return fmt.Errorf("update.poll_interval_seconds must be > 0 when update.poll_enabled=true")
			}
		}
	}
	return nil
}

func validateRotationConfig(updateCfg UpdateConfig) error {
	if strings.TrimSpace(updateCfg.ActiveKeyID) == "" {
		return fmt.Errorf("update.active_key_id is required when using key rotation config")
	}
	if len(updateCfg.PublicKeys) == 0 {
		return fmt.Errorf("update.public_keys is required when using key rotation config")
	}
	if _, ok := updateCfg.PublicKeys[updateCfg.ActiveKeyID]; !ok {
		return fmt.Errorf("update.public_keys must include update.active_key_id")
	}
	for _, previousKeyID := range updateCfg.PreviousKeys {
		if _, ok := updateCfg.PublicKeys[previousKeyID]; !ok {
			return fmt.Errorf("update.public_keys must include all update.previous_keys entries")
		}
	}
	return nil
}
