package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	appName    = "orchestrator"
	configFile = "config.json"
)

type Config struct {
	Version                int        `json:"version"`
	LogLevel               string     `json:"log_level"`
	Verbosity              string     `json:"verbosity"`
	PlannerModel           string     `json:"planner_model"`
	WorkerConcurrencyLimit int        `json:"worker_concurrency_limit,omitempty"`
	DriftWatcherEnabled    bool       `json:"drift_watcher_enabled,omitempty"`
	RepoContractConfirmed  *bool      `json:"repo_contract_confirmed,omitempty"`
	NTFY                   NTFYConfig `json:"ntfy"`
}

type NTFYConfig struct {
	ServerURL string `json:"server_url"`
	Topic     string `json:"topic"`
	// AuthToken is stored in the plain JSON config file for v1 when set.
	AuthToken string `json:"auth_token,omitempty"`
}

func Default() Config {
	return Config{
		Version:                1,
		LogLevel:               "info",
		Verbosity:              "normal",
		PlannerModel:           "gpt-5.1",
		WorkerConcurrencyLimit: 2,
	}
}

func ResolvePath(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}

	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, appName, configFile), nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	if cfg.Version == 0 {
		return Config{}, errors.New("config version is required")
	}

	return WithDefaults(cfg), nil
}

func Save(path string, cfg Config) error {
	cfg = WithDefaults(cfg)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o600)
}

func WithDefaults(cfg Config) Config {
	defaults := Default()

	if cfg.Version == 0 {
		cfg.Version = defaults.Version
	}
	if strings.TrimSpace(cfg.LogLevel) == "" {
		cfg.LogLevel = defaults.LogLevel
	}
	if strings.TrimSpace(cfg.Verbosity) == "" {
		cfg.Verbosity = defaults.Verbosity
	}
	if strings.TrimSpace(cfg.PlannerModel) == "" {
		cfg.PlannerModel = defaults.PlannerModel
	}
	if cfg.WorkerConcurrencyLimit <= 0 {
		cfg.WorkerConcurrencyLimit = defaults.WorkerConcurrencyLimit
	}

	return cfg
}
