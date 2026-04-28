package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/buildinfo"
	"orchestrator/internal/config"
	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/state"
)

func TestRunStatusShowsLatestRunSummary(t *testing.T) {
	t.Parallel()

	layout := state.ResolveLayout(t.TempDir())
	store, journalWriter, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "inspect the latest run summary",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 19, 21, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := store.SaveCheckpoint(context.Background(), run.ID, state.Checkpoint{
		Sequence:     2,
		Stage:        "planner",
		Label:        "planner_turn_post_executor",
		SafePause:    true,
		PlannerTurn:  1,
		ExecutorTurn: 0,
		CreatedAt:    time.Date(2026, 4, 19, 21, 5, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveCheckpoint() error = %v", err)
	}
	success := true
	if err := store.SaveExecutorState(context.Background(), run.ID, state.ExecutorState{
		Transport:   "codex_app_server",
		ThreadID:    "thread_123",
		TurnID:      "turn_123",
		TurnStatus:  "completed",
		LastSuccess: &success,
		LastMessage: "Applied the requested change and updated the planner-facing notes.",
	}); err != nil {
		t.Fatalf("SaveExecutorState() error = %v", err)
	}
	if err := store.SavePlannerOperatorStatus(context.Background(), run.ID, &state.PlannerOperatorStatus{
		ContractVersion:    "planner.v1",
		OperatorMessage:    "Implementing the next bounded slice.",
		CurrentFocus:       "status summary rendering",
		NextIntendedStep:   "persist the safe operator-facing planner summary",
		WhyThisStep:        "the operator needs a concise live view of planner progress.",
		ProgressPercent:    37,
		ProgressConfidence: "medium",
		ProgressBasis:      "bounded executor work already ran; current visibility slice is wiring planner status into the CLI.",
	}); err != nil {
		t.Fatalf("SavePlannerOperatorStatus() error = %v", err)
	}

	if _, err := store.RecordHumanReply(context.Background(), run.ID, "ntfy", "raw human reply", time.Date(2026, 4, 19, 21, 6, 0, 0, time.UTC)); err != nil {
		t.Fatalf("RecordHumanReply() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(layout.WorkersDir, "frontend-worker"), 0o755); err != nil {
		t.Fatalf("MkdirAll(worker path) error = %v", err)
	}
	if _, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "frontend-worker",
		WorkerStatus:  state.WorkerStatusIdle,
		AssignedScope: "ui shell",
		WorktreePath:  filepath.Join(layout.WorkersDir, "frontend-worker"),
	}); err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}

	if err := journalWriter.Append(journal.Event{
		At:             time.Date(2026, 4, 19, 21, 7, 0, 0, time.UTC),
		Type:           "planner.turn.completed",
		RunID:          run.ID,
		PlannerOutcome: "execute",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := store.SaveLatestStopReason(context.Background(), run.ID, orchestration.StopReasonPlannerPause); err != nil {
		t.Fatalf("SaveLatestStopReason() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	cfg := config.Default()
	cfg.DriftWatcherEnabled = true
	err = runStatus(context.Background(), Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		RepoRoot:   layout.RepoRoot,
		Layout:     layout,
		Config:     cfg,
		ConfigPath: filepath.Join(layout.RepoRoot, "missing-config.json"),
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	for _, want := range []string{
		"latest_run:",
		"  present: true",
		"  summary.goal: inspect the latest run summary",
		"  summary.resumable: true",
		"  summary.completed: false",
		"  summary.next_operator_action: continue_existing_run",
		"  stop.reason: planner_pause",
		"  checkpoint.sequence: 2",
		"  checkpoint.stage: planner",
		"  checkpoint.label: planner_turn_post_executor",
		"  stop.stable_checkpoint: sequence=2 stage=planner label=planner_turn_post_executor safe_pause=true",
		"  review.drift_watcher_enabled: true",
		"  plugins.enabled: false",
		"  plugins.loaded: 0",
		"  plugins.load_failures: 0",
		"workers:",
		"  total: 1",
		"  active: 0",
		"  latest_run_count: 1",
		"  worker.1.name: frontend-worker",
		"  worker.1.status: idle",
		"  planner.outcome: execute",
		"  planner.operator_message: Implementing the next bounded slice.",
		"  planner.progress_percent: 37",
		"  executor.turn_status: completed",
		"  executor.preview: Applied the requested change and updated the planner-facing notes.",
		"  executor.thread_id: thread_123",
		"  executor.interruptible: false",
		"  executor.last_control_action: unavailable",
		"  human_reply.source: ntfy",
		"  human_reply.count: 1",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunHistoryListsRecentRunsInReverseChronologicalOrder(t *testing.T) {
	t.Parallel()

	layout := state.ResolveLayout(t.TempDir())
	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}

	olderRun, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "older completed run",
		Status:   state.StatusCompleted,
		Checkpoint: state.Checkpoint{
			Sequence:     2,
			Stage:        "planner",
			Label:        "planner_declared_complete",
			SafePause:    true,
			PlannerTurn:  1,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 19, 20, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun(older) error = %v", err)
	}
	if err := store.SaveLatestStopReason(context.Background(), olderRun.ID, orchestration.StopReasonPlannerComplete); err != nil {
		t.Fatalf("SaveLatestStopReason(older) error = %v", err)
	}

	newerRun, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "newer resumable run",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     3,
			Stage:        "planner",
			Label:        "planner_turn_post_executor",
			SafePause:    true,
			PlannerTurn:  2,
			ExecutorTurn: 1,
			CreatedAt:    time.Date(2026, 4, 19, 22, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun(newer) error = %v", err)
	}
	if err := store.SaveLatestStopReason(context.Background(), newerRun.ID, orchestration.StopReasonPlannerPause); err != nil {
		t.Fatalf("SaveLatestStopReason(newer) error = %v", err)
	}
	if err := store.SaveCollectedContext(context.Background(), newerRun.ID, &state.CollectedContextState{
		Focus:        "Inspect the latest collected context artifact",
		ArtifactPath: ".orchestrator/artifacts/context/" + newerRun.ID + "/collected_context_latest.json",
	}); err != nil {
		t.Fatalf("SaveCollectedContext(newer) error = %v", err)
	}
	if err := store.SaveLatestStopReason(context.Background(), newerRun.ID, orchestration.StopReasonPlannerPause); err != nil {
		t.Fatalf("SaveLatestStopReason(newer, after context) error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	err = runHistory(context.Background(), Invocation{
		Args:   []string{"--limit", "2"},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		Layout: layout,
	})
	if err != nil {
		t.Fatalf("runHistory() error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"history.limit: 2",
		"history.count: 2",
		"run.1.run_id: " + newerRun.ID,
		"run.1.goal: newer resumable run",
		"run.1.stop_reason: planner_pause",
		"run.1.checkpoint_label: planner_turn_post_executor",
		"run.1.artifact_path: .orchestrator/artifacts/context/" + newerRun.ID + "/collected_context_latest.json",
		"run.1.resumable: true",
		"run.1.next_operator_action: continue_existing_run",
		"run.2.run_id: " + olderRun.ID,
		"run.2.goal: older completed run",
		"run.2.stop_reason: planner_complete",
		"run.2.checkpoint_label: planner_declared_complete",
		"run.2.artifact_path: unavailable",
		"run.2.resumable: false",
		"run.2.next_operator_action: no_action_required_run_completed",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("history output missing %q\n%s", want, output)
		}
	}

	if strings.Index(output, "run.1.run_id: "+newerRun.ID) > strings.Index(output, "run.2.run_id: "+olderRun.ID) {
		t.Fatalf("history output not in reverse chronological order\n%s", output)
	}
}

func TestRunStatusFallsBackToCollectedContextArtifactPathFromRunState(t *testing.T) {
	t.Parallel()

	layout := state.ResolveLayout(t.TempDir())
	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "inspect collected context artifact fallback",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     2,
			Stage:        "planner",
			Label:        "planner_turn_post_collect_context",
			SafePause:    true,
			PlannerTurn:  1,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.SaveCollectedContext(context.Background(), run.ID, &state.CollectedContextState{
		Focus:           "Inspect one collected file",
		ArtifactPath:    ".orchestrator/artifacts/context/" + run.ID + "/collected_context_latest.json",
		ArtifactPreview: "{\"focus\":\"Inspect one collected file\"}",
		Results: []state.CollectedContextResult{
			{
				RequestedPath: "README.md",
				ResolvedPath:  filepath.Join(layout.RepoRoot, "README.md"),
				Kind:          "missing",
				Detail:        "path_not_found",
			},
		},
	}); err != nil {
		t.Fatalf("SaveCollectedContext() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	err = runStatus(context.Background(), Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		RepoRoot:   layout.RepoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: filepath.Join(layout.RepoRoot, "missing-config.json"),
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	want := "  artifact.path: .orchestrator/artifacts/context/" + run.ID + "/collected_context_latest.json"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("status output missing %q\n%s", want, stdout.String())
	}
}

func TestRunDoctorChecksCurrentV1Surfaces(t *testing.T) {
	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)
	writeRepoMarkerFiles(t, repoRoot)
	installFakeExecutorTooling(t)
	installFakeGitTooling(t)

	ntfyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"healthy":true}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer ntfyServer.Close()

	cfg := config.Default()
	cfg.NTFY = config.NTFYConfig{
		ServerURL: ntfyServer.URL,
		Topic:     "orchestrator-reply",
	}

	configPath := filepath.Join(repoRoot, "config.json")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	t.Setenv("OPENAI_API_KEY", "sk-test")
	originalVersion := buildinfo.Version
	originalRevision := buildinfo.Revision
	originalBuildTime := buildinfo.BuildTime
	t.Cleanup(func() {
		buildinfo.Version = originalVersion
		buildinfo.Revision = originalRevision
		buildinfo.BuildTime = originalBuildTime
	})
	buildinfo.Version = "1.5.0"
	buildinfo.Revision = "rev-test"
	buildinfo.BuildTime = "2026-04-21T18:00:00Z"

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	err = runDoctor(context.Background(), Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     cfg,
		ConfigPath: configPath,
		Version:    "1.5.0",
	})
	if err != nil {
		t.Fatalf("runDoctor() error = %v\n%s", err, stdout.String())
	}

	for _, want := range []string{
		"runtime:",
		"  [OK] binary version: 1.5.0",
		"  [OK] binary revision: rev-test",
		"  [OK] binary build time: 2026-04-21T18:00:00Z",
		"repo_contract:",
		"  [OK] AGENTS.md:",
		"  [OK] .orchestrator/brief.md:",
		"config:",
		"  [OK] config path: " + configPath + " (loadable)",
		"plugins:",
		"  [OK] plugin directory: " + filepath.Join(repoRoot, "plugins") + " (found=0 loaded=0 failures=0)",
		"planner:",
		"  [OK] planner transport: responses_api",
		"  [OK] planner API key: present",
		"executor:",
		"  [OK] codex app-server:",
		"workers:",
		"  [OK] workers directory: " + layout.WorkersDir,
		"  [OK] git worktree support: git worktree support available",
		"ntfy:",
		"  [OK] ntfy config: " + ntfyServer.URL + " topic=orchestrator-reply",
		"  [OK] ntfy readiness: /v1/health healthy=true",
		"persistence:",
		"  [OK] sqlite state path: " + layout.DBPath + " (read/write ready)",
		"  [OK] journal path: " + layout.JournalPath + " (read/write ready)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("doctor output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRootHelpShowsPrimeTimeWorkflow(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	app := NewApp(Options{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	})

	app.printRootHelp()

	for _, want := range []string{
		"Typical flow:",
		"setup -> init -> run -> continue/status/history/doctor",
		"auto",
		"control",
		"gui",
		"setup",
		"init",
		"run",
		"resume",
		"continue",
		"workers",
		"executor",
		"status",
		"history",
		"doctor",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("root help missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunGUIDryRunSelectsExplicitRepoAndUsesLauncherAssets(t *testing.T) {
	repoRoot := t.TempDir()
	mustMkdirAll(t, filepath.Join(repoRoot, ".git"))
	configPath := filepath.Join(t.TempDir(), "config.json")
	shellDir, err := filepath.Abs(filepath.Join("..", "..", "console", "v2-shell"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	t.Setenv("ORCHESTRATOR_GUI_SHELL_DIR", shellDir)

	var stdout bytes.Buffer
	err = runGUI(context.Background(), Invocation{
		Args:       []string{"--repo", repoRoot, "--dry-run"},
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		ConfigPath: configPath,
		RepoRoot:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("runGUI() error = %v\n%s", err, stdout.String())
	}

	for _, want := range []string{
		"gui.product: Aurora Orchestrator / AI Mission Control for Windows",
		"gui.repo: " + filepath.Clean(repoRoot) + " (explicit --repo)",
		"gui.status: launch plan ready",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("gui dry-run output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunStatusShowsLatestIntegrationArtifactReference(t *testing.T) {
	t.Parallel()

	layout := state.ResolveLayout(t.TempDir())
	store, journalWriter, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "inspect the latest integration artifact reference",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := journalWriter.Append(journal.Event{
		At:              time.Date(2026, 4, 20, 14, 5, 0, 0, time.UTC),
		Type:            "integration.completed",
		RunID:           run.ID,
		ArtifactPath:    ".orchestrator/artifacts/integration/" + run.ID + "/integration_preview.json",
		ArtifactPreview: "{\"integration_preview\":\"Read-only integration preview\"}",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	err = runStatus(context.Background(), Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		RepoRoot:   layout.RepoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: filepath.Join(layout.RepoRoot, "missing-config.json"),
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	for _, want := range []string{
		"  integration.present: true",
		"  integration.artifact_path: .orchestrator/artifacts/integration/" + run.ID + "/integration_preview.json",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunStatusShowsLatestIntegrationApplyReference(t *testing.T) {
	t.Parallel()

	layout := state.ResolveLayout(t.TempDir())
	store, journalWriter, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "inspect the latest integration apply reference",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 20, 14, 30, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := journalWriter.Append(journal.Event{
		At:           time.Date(2026, 4, 20, 14, 31, 0, 0, time.UTC),
		Type:         "integration.apply.completed",
		RunID:        run.ID,
		ArtifactPath: ".orchestrator/artifacts/integration/" + run.ID + "/integration_apply.json",
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	err = runStatus(context.Background(), Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		RepoRoot:   layout.RepoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: filepath.Join(layout.RepoRoot, "missing-config.json"),
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	for _, want := range []string{
		"  integration.apply_status: completed",
		"  integration.apply_artifact_path: .orchestrator/artifacts/integration/" + run.ID + "/integration_apply.json",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunStatusShowsLatestWorkerPlanSummary(t *testing.T) {
	t.Parallel()

	layout := state.ResolveLayout(t.TempDir())
	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "inspect the latest worker plan summary",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 21, 13, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := store.SaveCollectedContext(context.Background(), run.ID, &state.CollectedContextState{
		Focus: "Inspect the worker plan result",
		WorkerPlan: &state.WorkerPlanResult{
			Status:                  "completed",
			WorkerIDs:               []string{"worker_1", "worker_2"},
			IntegrationRequested:    true,
			IntegrationArtifactPath: ".orchestrator/artifacts/integration/" + run.ID + "/integration_preview.json",
			IntegrationPreview:      "Read-only integration preview for 2 worker(s): 2 changed file(s), 1 conflict candidate(s).",
			ApplyMode:               "apply_non_conflicting",
			ApplyArtifactPath:       ".orchestrator/artifacts/integration/" + run.ID + "/integration_apply.json",
			Apply: &state.IntegrationApplySummary{
				Status:             "completed",
				SourceArtifactPath: ".orchestrator/artifacts/integration/" + run.ID + "/integration_preview.json",
				ApplyMode:          "apply_non_conflicting",
				AfterSummary:       "integration apply completed: applied 1 file(s), skipped 1 file(s) using apply_non_conflicting.",
			},
			Workers: []state.WorkerResultSummary{
				{
					WorkerID:          "worker_1",
					WorkerName:        "ui-worker",
					WorkerStatus:      "completed",
					AssignedScope:     "ui shell",
					WorktreePath:      filepath.Join(layout.WorkersDir, "ui-worker"),
					WorkerTaskSummary: "Implement the ui shell slice in an isolated worker.",
				},
				{
					WorkerID:          "worker_2",
					WorkerName:        "api-worker",
					WorkerStatus:      "failed",
					AssignedScope:     "api slice",
					WorktreePath:      filepath.Join(layout.WorkersDir, "api-worker"),
					WorkerTaskSummary: "Implement the api slice in an isolated worker.",
				},
			},
			Message: "worker plan completed sequentially across 2 isolated worker(s)",
		},
	}); err != nil {
		t.Fatalf("SaveCollectedContext() error = %v", err)
	}

	for _, item := range []struct {
		name   string
		status state.WorkerStatus
		scope  string
	}{
		{name: "ui-worker", status: state.WorkerStatusCompleted, scope: "ui shell"},
		{name: "api-worker", status: state.WorkerStatusFailed, scope: "api slice"},
	} {
		if err := os.MkdirAll(filepath.Join(layout.WorkersDir, item.name), 0o755); err != nil {
			t.Fatalf("MkdirAll(worker path) error = %v", err)
		}
		if _, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
			RunID:         run.ID,
			WorkerName:    item.name,
			WorkerStatus:  item.status,
			AssignedScope: item.scope,
			WorktreePath:  filepath.Join(layout.WorkersDir, item.name),
		}); err != nil {
			t.Fatalf("CreateWorker(%s) error = %v", item.name, err)
		}
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	err = runStatus(context.Background(), Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		RepoRoot:   layout.RepoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: filepath.Join(layout.RepoRoot, "missing-config.json"),
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	for _, want := range []string{
		"  worker_plan.present: true",
		"  worker_plan.status: completed",
		"  worker_plan.worker_count: 2",
		"  worker_plan.integration_requested: true",
		"  worker_plan.integration_artifact_path: .orchestrator/artifacts/integration/" + run.ID + "/integration_preview.json",
		"  worker_plan.apply_mode: apply_non_conflicting",
		"  worker_plan.apply_status: completed",
		"  worker_plan.apply_artifact_path: .orchestrator/artifacts/integration/" + run.ID + "/integration_apply.json",
		"  latest_run_status_counts.completed: 1",
		"  latest_run_status_counts.failed: 1",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunStatusShowsWorkerApprovalSummary(t *testing.T) {
	layout := state.ResolveLayout(t.TempDir())
	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "inspect worker approval state",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     3,
			Stage:        "planner",
			Label:        "planner_turn_post_executor",
			SafePause:    true,
			PlannerTurn:  1,
			ExecutorTurn: 1,
			CreatedAt:    time.Date(2026, 4, 21, 13, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.SaveLatestStopReason(context.Background(), run.ID, orchestration.StopReasonExecutorApprovalReq); err != nil {
		t.Fatalf("SaveLatestStopReason() error = %v", err)
	}

	workerPath := filepath.Join(layout.WorkersDir, "approval-worker")
	if err := os.MkdirAll(workerPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(worker path) error = %v", err)
	}
	worker, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "approval-worker",
		WorkerStatus:  state.WorkerStatusApprovalRequired,
		AssignedScope: "api approvals",
		WorktreePath:  workerPath,
	})
	if err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}
	worker.ExecutorThreadID = "worker_thread_status"
	worker.ExecutorTurnID = "worker_turn_status"
	worker.ExecutorTurnStatus = string(executor.TurnStatusApprovalRequired)
	worker.ExecutorApprovalState = string(executor.ApprovalStateRequired)
	worker.ExecutorApprovalKind = string(executor.ApprovalKindCommandExecution)
	worker.ExecutorApprovalPreview = "approval required for worker status"
	worker.ExecutorInterruptible = true
	worker.ExecutorSteerable = false
	worker.ExecutorLastControlAction = string(executor.ControlActionInterrupt)
	worker.ExecutorApproval = &state.ExecutorApproval{
		State: string(executor.ApprovalStateRequired),
		Kind:  string(executor.ApprovalKindCommandExecution),
	}
	if err := store.SaveWorker(context.Background(), worker); err != nil {
		t.Fatalf("SaveWorker() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	err = runStatus(context.Background(), Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		RepoRoot:   layout.RepoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: filepath.Join(layout.RepoRoot, "missing-config.json"),
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("runStatus() error = %v", err)
	}

	for _, want := range []string{
		"  latest_run_status_counts.approval_required: 1",
		"  worker.1.approval_required: true",
		"  worker.1.approval_kind: command_execution",
		"  worker.1.executor_thread_id: worker_thread_status",
		"  worker.1.executor_turn_id: worker_turn_status",
		"  worker.1.executor_interruptible: true",
		"  worker.1.executor_steerable: false",
		"  worker.1.executor_last_control_action: interrupted",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status output missing %q\n%s", want, stdout.String())
		}
	}
}

func writeRepoMarkerFiles(t *testing.T, repoRoot string) {
	t.Helper()

	mustMkdirAll(t, filepath.Join(repoRoot, ".git"))
	mustWriteFile(t, filepath.Join(repoRoot, "AGENTS.md"), "# AGENTS\n")
	mustWriteFile(t, filepath.Join(repoRoot, "docs", "ORCHESTRATOR_CLI_UPDATED_SPEC.md"), "spec\n")
	mustWriteFile(t, filepath.Join(repoRoot, "docs", "ORCHESTRATOR_NON_NEGOTIABLES.md"), "non-negotiables\n")
	mustWriteFile(t, filepath.Join(repoRoot, "docs", "CLI_ENGINE_EXECPLAN.md"), "execplan\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".orchestrator", "brief.md"), "brief\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".orchestrator", "roadmap.md"), "roadmap\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".orchestrator", "constraints.md"), "constraints\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".orchestrator", "decisions.md"), "decisions\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".orchestrator", "human-notes.md"), "human notes\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".orchestrator", "goal.md"), "goal\n")
	mustMkdirAll(t, filepath.Join(repoRoot, ".orchestrator", "state"))
	mustMkdirAll(t, filepath.Join(repoRoot, ".orchestrator", "logs"))
	mustMkdirAll(t, filepath.Join(repoRoot, ".orchestrator", "artifacts"))
}

func installFakeExecutorTooling(t *testing.T) {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	mustMkdirAll(t, binDir)

	if runtime.GOOS == "windows" {
		mustWriteFile(t, filepath.Join(binDir, "codex.cmd"), "@echo off\r\nexit /b 0\r\n")
		mustWriteFile(t, filepath.Join(binDir, "node.cmd"), "@echo off\r\nexit /b 0\r\n")
		mustWriteFile(t, filepath.Join(binDir, "node_modules", "@openai", "codex", "bin", "codex.js"), "// test codex entrypoint\n")
	} else {
		writeExecutableFile(t, filepath.Join(binDir, "codex"), "#!/bin/sh\nexit 0\n")
		writeExecutableFile(t, filepath.Join(binDir, "node"), "#!/bin/sh\nexit 0\n")
	}

	pathValue := binDir
	if existing := os.Getenv("PATH"); existing != "" {
		pathValue += string(os.PathListSeparator) + existing
	}
	t.Setenv("PATH", pathValue)
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()

	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeExecutableFile(t *testing.T, path string, contents string) {
	t.Helper()

	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
