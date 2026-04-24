package runtimecfg

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"

	"orchestrator/internal/config"
)

type Patch struct {
	Verbosity *string `json:"verbosity,omitempty"`
}

type Manager struct {
	path string

	mu  sync.RWMutex
	cfg config.Config
}

func NewManager(path string, initial config.Config) *Manager {
	return &Manager{
		path: strings.TrimSpace(path),
		cfg:  config.WithDefaults(initial),
	}
}

func (m *Manager) Path() string {
	if m == nil {
		return ""
	}
	return m.path
}

func (m *Manager) Snapshot() config.Config {
	if m == nil {
		return config.Default()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) ReloadFromDisk() (config.Config, bool, error) {
	if m == nil {
		return config.Default(), false, nil
	}

	if strings.TrimSpace(m.path) == "" {
		cfg := m.Snapshot()
		return cfg, false, nil
	}

	loaded, err := config.Load(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := m.Snapshot()
			return cfg, false, nil
		}
		return config.Config{}, false, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	changed := !reflect.DeepEqual(m.cfg, loaded)
	m.cfg = loaded
	return m.cfg, changed, nil
}

func (m *Manager) SetVerbosity(value string) (config.Config, bool, error) {
	normalized, err := config.NormalizeVerbosity(value)
	if err != nil {
		return config.Config{}, false, err
	}

	m.mu.Lock()
	cfg := m.cfg
	changed := cfg.Verbosity != normalized
	cfg.Verbosity = normalized
	cfg = config.WithDefaults(cfg)
	m.cfg = cfg
	m.mu.Unlock()

	if err := m.persist(cfg); err != nil {
		return config.Config{}, false, err
	}

	return cfg, changed, nil
}

func (m *Manager) ApplyPatch(patch Patch) (config.Config, bool, error) {
	if patch.Verbosity == nil {
		cfg := m.Snapshot()
		return cfg, false, nil
	}

	return m.SetVerbosity(*patch.Verbosity)
}

func (m *Manager) persist(cfg config.Config) error {
	if m == nil || strings.TrimSpace(m.path) == "" {
		return nil
	}
	return config.Save(m.path, cfg)
}
