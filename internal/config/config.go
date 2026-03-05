package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr string         `yaml:"listen_addr"`
	MCPToken   string         `yaml:"mcp_token"`
	Worker     WorkerConfig   `yaml:"worker"`
	Runtime    RuntimeConfig  `yaml:"runtime"`
	Cameras    []CameraConfig `yaml:"cameras"`
	Zones      []ZoneConfig   `yaml:"zones"`
}

type WorkerConfig struct {
	URL string `yaml:"url"`
}

type RuntimeConfig struct {
	QueueSize       int `yaml:"queue_size"`
	FreshnessWindow int `yaml:"freshness_window_seconds"`
}

type CameraConfig struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	RTSPURL     string `yaml:"rtsp_url"`
	ZoneID      string `yaml:"zone_id"`
	FrontDoor   bool   `yaml:"front_door"`
	ProbeAtBoot bool   `yaml:"probe_at_boot"`
}

type ZoneConfig struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
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
	if c.ListenAddr == "" {
		c.ListenAddr = ":8080"
	}
	if c.Runtime.QueueSize <= 0 {
		c.Runtime.QueueSize = 64
	}
	if c.Runtime.FreshnessWindow <= 0 {
		c.Runtime.FreshnessWindow = 90
	}
}

func (c Config) Validate() error {
	if c.MCPToken == "" {
		return fmt.Errorf("mcp_token is required")
	}
	if c.Worker.URL == "" {
		return fmt.Errorf("worker.url is required")
	}
	if len(c.Cameras) == 0 {
		return fmt.Errorf("at least one camera is required")
	}
	for _, camera := range c.Cameras {
		if camera.ID == "" {
			return fmt.Errorf("camera.id is required")
		}
		if camera.ZoneID == "" {
			return fmt.Errorf("camera.zone_id is required")
		}
	}
	return nil
}
