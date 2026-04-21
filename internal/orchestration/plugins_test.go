package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/journal"
	"orchestrator/internal/planner"
	"orchestrator/internal/plugins"
	"orchestrator/internal/state"
)

func TestCycleRunOnceCollectContextPluginToolResultsPersistedAndFedToSecondPlannerTurn(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	writeSamplePluginManifest(t, run.RepoPath)
	manager, summary := plugins.Load(run.RepoPath)
	if summary.Loaded != 1 {
		t.Fatalf("summary.Loaded = %d, want 1", summary.Loaded)
	}

	seedArtifactPath := filepath.Join(run.RepoPath, ".orchestrator", "artifacts", "planner", run.ID, "seed.txt")
	if err := os.MkdirAll(filepath.Dir(seedArtifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(seedArtifactPath, []byte("seed"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "collect plugin-generated artifact metadata",
						ToolCalls: []planner.PluginToolCall{
							{Tool: "artifact_index.write"},
						},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after plugin tool data is persisted"},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
		Plugins: manager,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if len(fakePlanner.inputs[0].PluginTools) != 1 {
		t.Fatalf("first planner input plugin_tools len = %d, want 1", len(fakePlanner.inputs[0].PluginTools))
	}
	if fakePlanner.inputs[0].PluginTools[0].Name != "artifact_index.write" {
		t.Fatalf("first planner input plugin tool = %q, want artifact_index.write", fakePlanner.inputs[0].PluginTools[0].Name)
	}
	if result.Run.CollectedContext == nil {
		t.Fatal("CollectedContext = nil, want persisted collected context")
	}
	if len(result.Run.CollectedContext.ToolResults) != 1 {
		t.Fatalf("CollectedContext.ToolResults len = %d, want 1", len(result.Run.CollectedContext.ToolResults))
	}
	if !result.Run.CollectedContext.ToolResults[0].Success {
		t.Fatalf("CollectedContext.ToolResults[0] = %#v, want success", result.Run.CollectedContext.ToolResults[0])
	}
	if !strings.HasPrefix(result.Run.CollectedContext.ToolResults[0].ArtifactPath, ".orchestrator/artifacts/reports/"+run.ID+"/") {
		t.Fatalf("ToolResults[0].ArtifactPath = %q, want report artifact path", result.Run.CollectedContext.ToolResults[0].ArtifactPath)
	}
	if fakePlanner.inputs[1].CollectedContext == nil {
		t.Fatal("second planner input missing collected_context")
	}
	if len(fakePlanner.inputs[1].CollectedContext.ToolResults) != 1 {
		t.Fatalf("second planner collected_context.tool_results len = %d, want 1", len(fakePlanner.inputs[1].CollectedContext.ToolResults))
	}
	if fakePlanner.inputs[1].CollectedContext.ToolResults[0].Tool != "artifact_index.write" {
		t.Fatalf("second planner tool result tool = %q, want artifact_index.write", fakePlanner.inputs[1].CollectedContext.ToolResults[0].Tool)
	}

	events, err := journalWriter.ReadRecent(run.ID, 16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "plugin.tool.called") {
		t.Fatal("journal missing plugin.tool.called event")
	}
	if containsEventType(events, "plugin.tool.failed") {
		t.Fatal("journal unexpectedly recorded plugin.tool.failed for successful tool execution")
	}
}

func TestCycleRunOncePluginHookFailureIsJournaledWithoutChangingRunSemantics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := state.Open(filepath.Join(root, "orchestrator.db"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	journalWriter, err := journal.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatalf("journal.Open() error = %v", err)
	}

	repoFile := filepath.Join(root, "repo-file")
	if err := os.WriteFile(repoFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoFile,
		Goal:     "verify plugin hook failures stay sidecar data",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	pluginRoot := t.TempDir()
	writeSamplePluginManifest(t, pluginRoot)
	manager, summary := plugins.Load(pluginRoot)
	if summary.Loaded != 1 {
		t.Fatalf("summary.Loaded = %d, want 1", summary.Loaded)
	}

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after one planner turn"},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
		Plugins: manager,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.Status != state.StatusInitialized {
		t.Fatalf("Run.Status = %q, want initialized", result.Run.Status)
	}
	if result.Run.RuntimeIssueReason != "" {
		t.Fatalf("RuntimeIssueReason = %q, want empty", result.Run.RuntimeIssueReason)
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_completed" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_completed", result.Run.LatestCheckpoint.Label)
	}

	events, err := journalWriter.ReadRecent(run.ID, 12)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	hookFailure := latestEventType(events, "plugin.hook.failed")
	if hookFailure.Type != "plugin.hook.failed" {
		t.Fatal("journal missing plugin.hook.failed event")
	}
	if hookFailure.PluginName != "artifact_index" {
		t.Fatalf("hookFailure.PluginName = %q, want artifact_index", hookFailure.PluginName)
	}
	if hookFailure.PluginHook != plugins.HookRunEnd {
		t.Fatalf("hookFailure.PluginHook = %q, want %q", hookFailure.PluginHook, plugins.HookRunEnd)
	}
	if result.FirstPlannerResult.Output.Outcome != planner.OutcomePause {
		t.Fatalf("FirstPlannerResult.Output.Outcome = %q, want pause", result.FirstPlannerResult.Output.Outcome)
	}
}

func writeSamplePluginManifest(t *testing.T, repoRoot string) {
	t.Helper()

	manifestPath := filepath.Join(repoRoot, "plugins", "artifact_index", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	manifest := `{
  "name": "artifact_index",
  "version": "1.0.0",
  "enabled": true,
  "tools": [
    {
      "name": "artifact_index.write",
      "description": "Write a JSON index of .orchestrator/artifacts for the current run under .orchestrator/artifacts/reports/<run-id>/.",
      "input_schema": {
        "type": "object",
        "properties": {},
        "additionalProperties": false
      }
    }
  ],
  "hooks": [
    "run.end"
  ]
}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
