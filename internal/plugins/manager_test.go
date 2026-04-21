package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestLoadParsesEnabledSamplePlugin(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writePluginManifest(t, repoRoot, `{
  "name": "artifact_index",
  "version": "1.0.0",
  "enabled": true,
  "tools": [
    {
      "name": "artifact_index.write",
      "description": "Write an artifact index.",
      "input_schema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      }
    }
  ],
  "hooks": ["run.end"]
}`)

	manager, summary := Load(repoRoot)
	if manager == nil {
		t.Fatal("Load() returned nil manager")
	}
	if summary.Found != 1 {
		t.Fatalf("summary.Found = %d, want 1", summary.Found)
	}
	if summary.Loaded != 1 {
		t.Fatalf("summary.Loaded = %d, want 1", summary.Loaded)
	}
	if !summary.Enabled {
		t.Fatal("summary.Enabled = false, want true")
	}
	if len(summary.Failures) != 0 {
		t.Fatalf("summary.Failures = %#v, want none", summary.Failures)
	}

	descriptors := manager.ToolDescriptors()
	if len(descriptors) != 1 {
		t.Fatalf("ToolDescriptors len = %d, want 1", len(descriptors))
	}
	if descriptors[0].Name != "artifact_index.write" {
		t.Fatalf("ToolDescriptors[0].Name = %q, want artifact_index.write", descriptors[0].Name)
	}
}

func TestLoadIgnoresDisabledPlugin(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writePluginManifest(t, repoRoot, `{
  "name": "artifact_index",
  "version": "1.0.0",
  "enabled": false,
  "tools": [
    {
      "name": "artifact_index.write",
      "description": "Write an artifact index."
    }
  ]
}`)

	manager, summary := Load(repoRoot)
	if manager == nil {
		t.Fatal("Load() returned nil manager")
	}
	if summary.Found != 1 {
		t.Fatalf("summary.Found = %d, want 1", summary.Found)
	}
	if summary.Loaded != 0 {
		t.Fatalf("summary.Loaded = %d, want 0", summary.Loaded)
	}
	if summary.Enabled {
		t.Fatal("summary.Enabled = true, want false")
	}
	if len(summary.Failures) != 0 {
		t.Fatalf("summary.Failures = %#v, want none", summary.Failures)
	}
}

func TestExecuteToolCallReturnsStructuredData(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writePluginManifest(t, repoRoot, `{
  "name": "artifact_index",
  "version": "1.0.0",
  "enabled": true,
  "tools": [
    {
      "name": "artifact_index.write",
      "description": "Write an artifact index.",
      "input_schema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      }
    }
  ],
  "hooks": ["run.end"]
}`)

	seedPath := filepath.Join(repoRoot, ".orchestrator", "artifacts", "planner", "run_123", "seed.txt")
	if err := os.MkdirAll(filepath.Dir(seedPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(seedPath, []byte("seed"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, summary := Load(repoRoot)
	if summary.Loaded != 1 {
		t.Fatalf("summary.Loaded = %d, want 1", summary.Loaded)
	}

	result, err := manager.ExecuteToolCall(context.Background(), state.Run{
		ID:       "run_123",
		RepoPath: repoRoot,
	}, planner.PluginToolCall{Tool: "artifact_index.write"})
	if err != nil {
		t.Fatalf("ExecuteToolCall() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("result = %#v, want success", result)
	}
	if result.Tool != "artifact_index.write" {
		t.Fatalf("result.Tool = %q, want artifact_index.write", result.Tool)
	}
	if result.ArtifactPath == "" {
		t.Fatal("result.ArtifactPath = empty, want report artifact path")
	}
	if !strings.HasPrefix(result.ArtifactPath, ".orchestrator/artifacts/reports/run_123/") {
		t.Fatalf("result.ArtifactPath = %q, want report artifact path", result.ArtifactPath)
	}
	artifactPath := filepath.Join(repoRoot, filepath.FromSlash(result.ArtifactPath))
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("artifact path missing at %s: %v", artifactPath, err)
	}
}

func TestLoadRecordsManifestFailure(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writePluginManifest(t, repoRoot, `{
  "name": "artifact_index",
  "enabled": true,
  "tools": [
    {
      "name": "artifact_index.write",
      "description": "Write an artifact index."
    }
  ]
}`)

	_, summary := Load(repoRoot)
	if len(summary.Failures) != 1 {
		t.Fatalf("summary.Failures len = %d, want 1", len(summary.Failures))
	}
	if !strings.Contains(summary.Failures[0].Message, "plugin version is required") {
		t.Fatalf("summary.Failures[0].Message = %q, want version validation error", summary.Failures[0].Message)
	}
}

func writePluginManifest(t *testing.T, repoRoot string, contents string) {
	t.Helper()

	path := filepath.Join(repoRoot, DefaultDirectory, artifactIndexPluginName, ManifestFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
