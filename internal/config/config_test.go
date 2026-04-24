package config

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	confirmed := true
	cfg := Config{
		Version:                1,
		LogLevel:               "debug",
		Verbosity:              "verbose",
		PlannerModel:           "gpt-5.2",
		WorkerConcurrencyLimit: 4,
		DriftWatcherEnabled:    true,
		RepoContractConfirmed:  &confirmed,
		NTFY: NTFYConfig{
			ServerURL: "https://ntfy.example.com",
			Topic:     "orchestrator-reply",
			AuthToken: "tk_testtoken",
		},
	}

	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Version != 1 {
		t.Fatalf("Version = %d, want 1", loaded.Version)
	}
	if loaded.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q, want debug", loaded.LogLevel)
	}
	if loaded.Verbosity != "verbose" {
		t.Fatalf("Verbosity = %q, want verbose", loaded.Verbosity)
	}
	if loaded.PlannerModel != "gpt-5.2" {
		t.Fatalf("PlannerModel = %q, want gpt-5.2", loaded.PlannerModel)
	}
	if loaded.WorkerConcurrencyLimit != 4 {
		t.Fatalf("WorkerConcurrencyLimit = %d, want 4", loaded.WorkerConcurrencyLimit)
	}
	if !loaded.DriftWatcherEnabled {
		t.Fatal("DriftWatcherEnabled = false, want true")
	}
	if loaded.RepoContractConfirmed == nil || !*loaded.RepoContractConfirmed {
		t.Fatalf("RepoContractConfirmed = %#v, want true", loaded.RepoContractConfirmed)
	}
	if loaded.NTFY.ServerURL != "https://ntfy.example.com" {
		t.Fatalf("ServerURL = %q, want https://ntfy.example.com", loaded.NTFY.ServerURL)
	}
	if loaded.NTFY.Topic != "orchestrator-reply" {
		t.Fatalf("Topic = %q, want orchestrator-reply", loaded.NTFY.Topic)
	}
	if loaded.NTFY.AuthToken != "tk_testtoken" {
		t.Fatalf("AuthToken = %q, want tk_testtoken", loaded.NTFY.AuthToken)
	}
}

func TestWithDefaultsSetsWorkerConcurrencyLimit(t *testing.T) {
	t.Parallel()

	cfg := WithDefaults(Config{})
	if cfg.WorkerConcurrencyLimit != 2 {
		t.Fatalf("WorkerConcurrencyLimit = %d, want 2", cfg.WorkerConcurrencyLimit)
	}
}

func TestDefaultPlannerModelUsesLatestGPT5Alias(t *testing.T) {
	t.Parallel()

	if got := Default().PlannerModel; got != PlannerModelLatestGPT5 {
		t.Fatalf("Default().PlannerModel = %q, want %s", got, PlannerModelLatestGPT5)
	}
}

func TestNormalizeVerbosityAcceptsKnownLevels(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: "", want: VerbosityNormal},
		{input: " quiet ", want: VerbosityQuiet},
		{input: "NORMAL", want: VerbosityNormal},
		{input: "verbose", want: VerbosityVerbose},
		{input: "trace", want: VerbosityTrace},
	} {
		got, err := NormalizeVerbosity(tc.input)
		if err != nil {
			t.Fatalf("NormalizeVerbosity(%q) error = %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeVerbosity(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeVerbosityRejectsUnknownLevel(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeVerbosity("chatty"); err == nil {
		t.Fatal("NormalizeVerbosity(chatty) unexpectedly succeeded")
	}
}
