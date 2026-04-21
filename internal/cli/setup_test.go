package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"orchestrator/internal/config"
)

func TestRunSetupInteractiveKeepsCurrentValuesOnBlankInput(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	configPath := filepath.Join(repoRoot, "config.json")

	cfg := config.Default()
	cfg.PlannerModel = "gpt-5.2"
	cfg.DriftWatcherEnabled = true
	cfg.NTFY = config.NTFYConfig{
		ServerURL: "https://ntfy.example.com",
		Topic:     "orchestrator-reply",
		AuthToken: "tk_supersecretvalue",
	}
	cfg.RepoContractConfirmed = boolPtr(true)
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	var stdout bytes.Buffer
	err := runSetup(context.Background(), Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		Stdin:      strings.NewReader("\n\n\n\n\n\n"),
		ConfigPath: configPath,
		RepoRoot:   repoRoot,
	})
	if err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if loaded.PlannerModel != "gpt-5.2" {
		t.Fatalf("PlannerModel = %q, want gpt-5.2", loaded.PlannerModel)
	}
	if !loaded.DriftWatcherEnabled {
		t.Fatal("DriftWatcherEnabled = false, want true")
	}
	if loaded.NTFY.ServerURL != "https://ntfy.example.com" {
		t.Fatalf("ServerURL = %q, want existing value", loaded.NTFY.ServerURL)
	}
	if loaded.NTFY.Topic != "orchestrator-reply" {
		t.Fatalf("Topic = %q, want existing value", loaded.NTFY.Topic)
	}
	if loaded.NTFY.AuthToken != "tk_supersecretvalue" {
		t.Fatalf("AuthToken = %q, want existing value", loaded.NTFY.AuthToken)
	}
	if loaded.RepoContractConfirmed == nil || !*loaded.RepoContractConfirmed {
		t.Fatalf("RepoContractConfirmed = %#v, want true", loaded.RepoContractConfirmed)
	}

	for _, want := range []string{
		"planner model [gpt-5.2]: ",
		"drift watcher enabled [Y/n]: ",
		"ntfy server URL [https://ntfy.example.com]: ",
		"ntfy topic [orchestrator-reply]: ",
		"repo contract markers ready [Y/n]: ",
		"saved.planner_model: gpt-5.2",
		"saved.review.drift_watcher_enabled: true",
		"ntfy.configured: true",
		"repo_contract.markers_ready: true",
		"repo_contract.confirmed: true",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("setup output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunSetupYesWritesDefaultsWithoutPrompting(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	configPath := filepath.Join(repoRoot, "config.json")

	var stdout bytes.Buffer
	err := runSetup(context.Background(), Invocation{
		Args:       []string{"--yes"},
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		ConfigPath: configPath,
		RepoRoot:   repoRoot,
	})
	if err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if loaded.PlannerModel != config.Default().PlannerModel {
		t.Fatalf("PlannerModel = %q, want %q", loaded.PlannerModel, config.Default().PlannerModel)
	}
	if loaded.DriftWatcherEnabled {
		t.Fatal("DriftWatcherEnabled = true, want false default")
	}
	if loaded.RepoContractConfirmed == nil || !*loaded.RepoContractConfirmed {
		t.Fatalf("RepoContractConfirmed = %#v, want true", loaded.RepoContractConfirmed)
	}
	if loaded.NTFY.ServerURL != "" || loaded.NTFY.Topic != "" || loaded.NTFY.AuthToken != "" {
		t.Fatalf("NTFY config = %#v, want unset", loaded.NTFY)
	}

	for _, want := range []string{
		"setup.mode: non_interactive_yes",
		"config.state: created",
		"saved.planner_model: gpt-5.1",
		"saved.review.drift_watcher_enabled: false",
		"saved.ntfy.server_url: unset",
		"saved.ntfy.topic: unset",
		"saved.ntfy.auth_token: unset",
		"ntfy.configured: false",
		"repo_contract.markers_ready: true",
		"repo_contract.confirmed: true",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("setup output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunSetupMasksSensitiveTokenInOutput(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	configPath := filepath.Join(repoRoot, "config.json")

	cfg := config.Default()
	cfg.NTFY = config.NTFYConfig{
		ServerURL: "https://ntfy.example.com",
		Topic:     "orchestrator-reply",
		AuthToken: "tk_supersecretvalue",
	}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	var stdout bytes.Buffer
	err := runSetup(context.Background(), Invocation{
		Args:       []string{"--yes"},
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		ConfigPath: configPath,
		RepoRoot:   repoRoot,
	})
	if err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	if strings.Contains(stdout.String(), "tk_supersecretvalue") {
		t.Fatalf("setup output leaked raw token\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "saved.ntfy.auth_token: tk") ||
		!strings.Contains(stdout.String(), "ue (stored in config file)") {
		t.Fatalf("setup output missing masked token summary\n%s", stdout.String())
	}
}
