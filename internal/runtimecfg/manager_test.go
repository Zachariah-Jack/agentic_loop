package runtimecfg

import (
	"path/filepath"
	"testing"

	"orchestrator/internal/config"
)

func TestManagerSetVerbosityPersistsConfig(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	manager := NewManager(configPath, config.Default())

	cfg, changed, err := manager.SetVerbosity(config.VerbosityTrace)
	if err != nil {
		t.Fatalf("SetVerbosity() error = %v", err)
	}
	if !changed {
		t.Fatal("SetVerbosity() changed = false, want true")
	}
	if cfg.Verbosity != config.VerbosityTrace {
		t.Fatalf("cfg.Verbosity = %q, want %q", cfg.Verbosity, config.VerbosityTrace)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if loaded.Verbosity != config.VerbosityTrace {
		t.Fatalf("loaded.Verbosity = %q, want %q", loaded.Verbosity, config.VerbosityTrace)
	}
}

func TestManagerReloadFromDiskSeesExternalChange(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	initial := config.Default()
	if err := config.Save(configPath, initial); err != nil {
		t.Fatalf("config.Save(initial) error = %v", err)
	}

	manager := NewManager(configPath, initial)

	updated := initial
	updated.Verbosity = config.VerbosityVerbose
	if err := config.Save(configPath, updated); err != nil {
		t.Fatalf("config.Save(updated) error = %v", err)
	}

	cfg, changed, err := manager.ReloadFromDisk()
	if err != nil {
		t.Fatalf("ReloadFromDisk() error = %v", err)
	}
	if !changed {
		t.Fatal("ReloadFromDisk() changed = false, want true")
	}
	if cfg.Verbosity != config.VerbosityVerbose {
		t.Fatalf("cfg.Verbosity = %q, want %q", cfg.Verbosity, config.VerbosityVerbose)
	}
}

func TestManagerApplyPatchRejectsInvalidVerbosity(t *testing.T) {
	t.Parallel()

	manager := NewManager("", config.Default())
	value := "chatty"
	if _, _, err := manager.ApplyPatch(Patch{Verbosity: &value}); err == nil {
		t.Fatal("ApplyPatch() unexpectedly succeeded")
	}
}
