package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	appName    = "orchestrator"
	configFile = "config.json"
)

type Config struct {
	Version      int    `json:"version"`
	LogLevel     string `json:"log_level"`
	Verbosity    string `json:"verbosity"`
	PlannerModel string `json:"planner_model"`
}

func Default() Config {
	return Config{
		Version:      1,
		LogLevel:     "info",
		Verbosity:    "normal",
		PlannerModel: "gpt-5.1",
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

	return cfg, nil
}

func Save(path string, cfg Config) error {
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
