package worker

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Worker       WorkerConfig       `yaml:"worker"`
	ControlPlane ControlPlaneConfig `yaml:"control_plane"`
	Heartbeat    HeartbeatConfig    `yaml:"heartbeat"`
	Polling      PollingConfig      `yaml:"polling"`
	Resources    ResourceConfig     `yaml:"resources"`
	Logging      LoggingConfig      `yaml:"logging"`

	// Runtime override (not loaded from YAML)
	OverrideHostname string `yaml:"-"`
}

type WorkerConfig struct {
	Hostname string `yaml:"hostname"`
	Version  string `yaml:"version"`
}

type ControlPlaneConfig struct {
	URL string `yaml:"url"`
}

type HeartbeatConfig struct {
	Interval time.Duration `yaml:"interval"`
}

type PollingConfig struct {
	Interval time.Duration `yaml:"interval"`
}

type ResourceConfig struct {
	CPUCount int `yaml:"cpu_count"`
	MemoryMB int `yaml:"memory_mb"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

func LoadConfig(path string) (*Config, error) {

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	// Runtime override from environment.
	cfg.OverrideHostname = os.Getenv("WORKER_HOSTNAME")

	if cfg.OverrideHostname != "" {
		cfg.Worker.Hostname = cfg.OverrideHostname
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {

	if c.Worker.Hostname == "" {
		return fmt.Errorf("worker.hostname is required")
	}

	if c.ControlPlane.URL == "" {
		return fmt.Errorf("control_plane.url is required")
	}

	if c.Heartbeat.Interval <= 0 {
		return fmt.Errorf("heartbeat.interval must be > 0")
	}

	if c.Polling.Interval <= 0 {
		return fmt.Errorf("polling.interval must be > 0")
	}

	if c.Resources.CPUCount <= 0 {
		return fmt.Errorf("resources.cpu_count must be > 0")
	}

	if c.Resources.MemoryMB <= 0 {
		return fmt.Errorf("resources.memory_mb must be > 0")
	}

	return nil
}