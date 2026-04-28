package runtimecfg

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"orchestrator/internal/config"
)

func TestApplyPatchUpdatesTimeoutsAndPermissionProfile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	manager := NewManager(path, config.Default())
	profile := "full_send"
	cfg, changed, err := manager.ApplyPatch(Patch{
		PermissionProfile: &profile,
		Timeouts: TimeoutPatch{
			ExecutorTurnTimeout: OptionalString{Set: true, Value: "4h"},
			HumanWaitTimeout:    OptionalString{Set: true, Value: "unlimited"},
		},
	})
	if err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if cfg.Timeouts.ExecutorTurnTimeout != "4h" {
		t.Fatalf("ExecutorTurnTimeout = %q, want 4h", cfg.Timeouts.ExecutorTurnTimeout)
	}
	if cfg.Permissions.Profile != "full_send" {
		t.Fatalf("Permissions.Profile = %q, want full_send", cfg.Permissions.Profile)
	}
	if cfg.Permissions.AskBeforeInstallingDependencies || cfg.Permissions.AskBeforeRunningTests {
		t.Fatalf("full_send preset should allow routine dependency/test work: %#v", cfg.Permissions)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Timeouts.ExecutorTurnTimeout != "4h" {
		t.Fatalf("persisted ExecutorTurnTimeout = %q, want 4h", loaded.Timeouts.ExecutorTurnTimeout)
	}
}

func TestApplyPatchAllowsFineGrainedPermissionOverride(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	manager := NewManager(path, config.Default())
	profile := "full_send"
	askBeforePush := true
	cfg, changed, err := manager.ApplyPatch(Patch{
		PermissionProfile: &profile,
		Permissions: PermissionPatch{
			AskBeforeGitPushes: &askBeforePush,
		},
	})
	if err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if cfg.Permissions.Profile != "full_send" {
		t.Fatalf("Permissions.Profile = %q, want full_send", cfg.Permissions.Profile)
	}
	if !cfg.Permissions.AskBeforeGitPushes {
		t.Fatalf("AskBeforeGitPushes = false, want override true: %#v", cfg.Permissions)
	}
	if cfg.Permissions.AskBeforeRunningTests {
		t.Fatalf("AskBeforeRunningTests = true, want full_send preset value false")
	}
}

func TestApplyPatchUpdatesNTFYConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	manager := NewManager(path, config.Default())
	serverURL := "https://ntfy.example.test"
	topic := "aurora-mobile"
	authToken := "secret-token"
	cfg, changed, err := manager.ApplyPatch(Patch{
		NTFY: NTFYPatch{
			ServerURL: &serverURL,
			Topic:     &topic,
			AuthToken: &authToken,
		},
	})
	if err != nil {
		t.Fatalf("ApplyPatch() error = %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if cfg.NTFY.ServerURL != serverURL || cfg.NTFY.Topic != topic || cfg.NTFY.AuthToken != authToken {
		t.Fatalf("NTFY config = %#v", cfg.NTFY)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.NTFY.Topic != topic {
		t.Fatalf("persisted NTFY topic = %q, want %q", loaded.NTFY.Topic, topic)
	}
}

func TestTimeoutPatchNullMeansUnlimited(t *testing.T) {
	t.Parallel()

	var patch Patch
	if err := json.Unmarshal([]byte(`{"timeouts":{"executor_turn_timeout":null}}`), &patch); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !patch.Timeouts.ExecutorTurnTimeout.Set {
		t.Fatal("ExecutorTurnTimeout.Set = false, want true")
	}
	if patch.Timeouts.ExecutorTurnTimeout.Value != "unlimited" {
		t.Fatalf("ExecutorTurnTimeout.Value = %q, want unlimited", patch.Timeouts.ExecutorTurnTimeout.Value)
	}
}
