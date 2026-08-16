package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds every environment-driven setting used across the Vulcan
// services. Not every service needs every field — use the role-specific
// Load*Config function for the service you're wiring up, since each one
// validates only the subset it actually depends on.
type Config struct {
	Port               string
	DatabaseURL        string
	VictoriaMetricsURL string
	NATSURL            string
	WorkerCapacityRPS  int
	WorkerBinaryPath   string
	WorkerWorkingDir   string
	ControlPlaneURL    string
}

// load reads every known environment variable without validating any of
// them. Role-specific loaders below call this and then check only the
// fields they care about.
func load() *Config {
	_ = godotenv.Load("../../.env")

	return &Config{
		Port:               os.Getenv("PORT"),
		DatabaseURL:        os.Getenv("DB_URL"),
		VictoriaMetricsURL: os.Getenv("VICTORIA_METRICS_URL"),
		NATSURL:            os.Getenv("NATS_URL"),
		WorkerCapacityRPS:  parsePositiveInt(os.Getenv("WORKER_CAPACITY_RPS"), 100),
		WorkerBinaryPath:   os.Getenv("WORKER_BINARY_PATH"),
		WorkerWorkingDir:   os.Getenv("VULCAN_PROJECT_ROOT"),
		ControlPlaneURL:    os.Getenv("CONTROL_PLANE_URL"),
	}
}

// LoadServerConfig loads and validates the configuration required by the
// control-plane HTTP server (cmd/server). The control plane does not talk
// to NATS, so NATS_URL is intentionally not required here.
func LoadServerConfig() (*Config, error) {
	cfg := load()

	if cfg.Port == "" {
		return nil, errors.New("PORT environment variable is required")
	}

	if cfg.DatabaseURL == "" {
		return nil, errors.New("DB_URL environment variable is required")
	}

	if cfg.NATSURL == "" {
		return nil, errors.New("NATS_URL environment variable is required")
	}

	if cfg.ControlPlaneURL == "" {
		cfg.ControlPlaneURL = "http://localhost:" + cfg.Port
	}

	if cfg.WorkerBinaryPath == "" {
		cfg.WorkerBinaryPath = "./worker"
	}

	if cfg.WorkerWorkingDir == "" {
		cfg.WorkerWorkingDir = "."
	}

	if cfg.VictoriaMetricsURL == "" {
		return nil, errors.New(
			"VICTORIA_METRICS_URL environment variable is required",
		)
	}

	return cfg, nil
}

func parsePositiveInt(value string, fallback int) int {
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}

// LoadAggregatorConfig loads and validates the configuration required by
// the telemetry aggregator (cmd/aggregator). The aggregator has no HTTP
// port and no database, so only NATS and VictoriaMetrics are required.
func LoadAggregatorConfig() (*Config, error) {
	cfg := load()

	if cfg.NATSURL == "" {
		return nil, errors.New("NATS_URL environment variable is required")
	}

	if cfg.VictoriaMetricsURL == "" {
		return nil, errors.New(
			"VICTORIA_METRICS_URL environment variable is required",
		)
	}

	return cfg, nil
}

// LoadWorkerConfig loads and validates the configuration required by the
// worker process (cmd/worker). Worker-specific settings (hostname,
// control-plane URL, polling/heartbeat intervals, etc.) still come from
// configs/worker.yaml via worker.LoadConfig — this only covers the NATS
// connection, which is environment-driven like the other services.
func LoadWorkerConfig() (*Config, error) {
	cfg := load()

	if cfg.NATSURL == "" {
		return nil, errors.New("NATS_URL environment variable is required")
	}

	return cfg, nil
}
