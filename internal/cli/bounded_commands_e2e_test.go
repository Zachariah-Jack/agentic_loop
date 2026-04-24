package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestRunCommandFirstTurnCompleteEndToEnd(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_complete",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "the bounded run is complete"},
					},
				},
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "complete on the first planner turn", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runRun() error = %v", err)
	}

	for _, want := range []string{
		"first_planner_outcome: complete",
		"cycle_number: 1",
		"status: completed",
		"stop_reason: planner_complete",
		"latest_checkpoint.label: planner_declared_complete",
		"next_operator_action: no_action_required_run_completed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", run.Status)
	}
	if run.PreviousResponseID != "resp_complete" {
		t.Fatalf("PreviousResponseID = %q, want resp_complete", run.PreviousResponseID)
	}
	if run.LatestStopReason != orchestration.StopReasonPlannerComplete {
		t.Fatalf("LatestStopReason = %q, want %q", run.LatestStopReason, orchestration.StopReasonPlannerComplete)
	}
	if run.LatestCheckpoint.Sequence != 2 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 2", run.LatestCheckpoint.Sequence)
	}
	if run.LatestCheckpoint.Stage != "planner" {
		t.Fatalf("LatestCheckpoint.Stage = %q, want planner", run.LatestCheckpoint.Stage)
	}
	if run.LatestCheckpoint.Label != "planner_declared_complete" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_declared_complete", run.LatestCheckpoint.Label)
	}

	store, err := openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()

	if _, found, err := store.LatestResumableRun(context.Background()); err != nil {
		t.Fatalf("LatestResumableRun() error = %v", err)
	} else if found {
		t.Fatal("LatestResumableRun() found = true, want completed run to be non-resumable")
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	if countJournalEvents(events, "run.completed") != 1 {
		t.Fatalf("run.completed count = %d, want 1", countJournalEvents(events, "run.completed"))
	}
}

func TestRunCommandDefaultsToForegroundProgressUntilCompleteEndToEnd(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_pause",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomePause,
						Pause:           &planner.PauseOutcome{Reason: "keep going without operator babysitting"},
					},
				},
				{
					ResponseID: "resp_complete",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "the unattended run completed the requested slice"},
					},
				},
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "keep advancing until the run really stops"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runRun() error = %v", err)
	}

	for _, want := range []string{
		"run.stop_flag_path: " + autoStopFlagPath(layout),
		"command: run",
		"cycle_number: 1",
		"cycle_number: 2",
		"status: completed",
		"stop_reason: planner_complete",
		"latest_checkpoint.label: planner_declared_complete",
		"next_operator_action: no_action_required_run_completed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", run.Status)
	}
	if run.PreviousResponseID != "resp_complete" {
		t.Fatalf("PreviousResponseID = %q, want resp_complete", run.PreviousResponseID)
	}
	if run.LatestStopReason != orchestration.StopReasonPlannerComplete {
		t.Fatalf("LatestStopReason = %q, want %q", run.LatestStopReason, orchestration.StopReasonPlannerComplete)
	}

	events := readContinueEvents(t, layout, run.ID, 16)
	if countJournalEvents(events, "run.started") != 1 {
		t.Fatalf("run.started count = %d, want 1", countJournalEvents(events, "run.started"))
	}
	if countJournalEvents(events, "run.cycle.completed") != 2 {
		t.Fatalf("run.cycle.completed count = %d, want 2", countJournalEvents(events, "run.cycle.completed"))
	}
	stopEvent := latestJournalEvent(events, "run.stopped")
	if stopEvent.StopReason != orchestration.StopReasonPlannerComplete {
		t.Fatalf("run.stopped stop reason = %q, want %q", stopEvent.StopReason, orchestration.StopReasonPlannerComplete)
	}
}

func TestRunCommandNonExecuteOutcomeEndToEnd(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_pause",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomePause,
						Pause:           &planner.PauseOutcome{Reason: "pause after persisting planner state"},
					},
				},
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "persist one non-execute planner outcome", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runRun() error = %v", err)
	}

	for _, want := range []string{
		"command: run",
		"run_action: created_new_run",
		"cycle_number: 1",
		"first_planner_outcome: pause",
		"executor_dispatched: false",
		"stop_reason: planner_pause",
		"latest_checkpoint.label: planner_turn_completed",
		"next_operator_action: resume_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.PreviousResponseID != "resp_pause" {
		t.Fatalf("PreviousResponseID = %q, want resp_pause", run.PreviousResponseID)
	}
	if run.LatestCheckpoint.Label != "planner_turn_completed" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_completed", run.LatestCheckpoint.Label)
	}
}

func TestRunCommandCollectContextThenSecondPlannerTurnEndToEnd(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	mustWriteFile(t, filepath.Join(repoRoot, "docs", "note.txt"), "bounded context preview")
	mustWriteFile(t, filepath.Join(repoRoot, "internal", "keep.go"), "package internal\n")

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_collect",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeCollectContext,
						CollectContext: &planner.CollectContextOutcome{
							Focus:     "inspect exact repo paths",
							Questions: []string{"what is in docs and internal?"},
							Paths:     []string{"docs/note.txt", "internal", "missing.txt"},
						},
					},
				},
				{
					ResponseID: "resp_pause",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomePause,
						Pause:           &planner.PauseOutcome{Reason: "context is enough for now"},
					},
				},
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "collect context once and stop", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runRun() error = %v", err)
	}

	for _, want := range []string{
		"cycle_number: 1",
		"first_planner_outcome: collect_context",
		"second_planner_outcome: pause",
		"stop_reason: planner_pause",
		"latest_checkpoint.label: planner_turn_post_collect_context",
		"next_operator_action: resume_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.CollectedContext == nil {
		t.Fatal("CollectedContext = nil, want persisted collected context")
	}
	if len(run.CollectedContext.Results) != 3 {
		t.Fatalf("CollectedContext.Results len = %d, want 3", len(run.CollectedContext.Results))
	}
	if run.CollectedContext.Results[2].Detail != "path_not_found" {
		t.Fatalf("CollectedContext.Results[2].Detail = %q, want path_not_found", run.CollectedContext.Results[2].Detail)
	}
}

func TestRunCommandCollectContextThenCompleteEndToEnd(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	mustWriteFile(t, filepath.Join(repoRoot, "docs", "note.txt"), "bounded context preview")

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_collect",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeCollectContext,
						CollectContext: &planner.CollectContextOutcome{
							Focus: "inspect one repo file before completing",
							Paths: []string{"docs/note.txt"},
						},
					},
				},
				{
					ResponseID: "resp_complete",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "context was enough to complete the run"},
					},
				},
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "collect context and complete", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runRun() error = %v", err)
	}

	for _, want := range []string{
		"cycle_number: 1",
		"first_planner_outcome: collect_context",
		"second_planner_outcome: complete",
		"status: completed",
		"stop_reason: planner_complete",
		"latest_checkpoint.label: planner_declared_complete",
		"next_operator_action: no_action_required_run_completed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", run.Status)
	}
	if run.PreviousResponseID != "resp_complete" {
		t.Fatalf("PreviousResponseID = %q, want resp_complete", run.PreviousResponseID)
	}
	if run.LatestStopReason != orchestration.StopReasonPlannerComplete {
		t.Fatalf("LatestStopReason = %q, want %q", run.LatestStopReason, orchestration.StopReasonPlannerComplete)
	}
	if run.LatestCheckpoint.Sequence != 3 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 3", run.LatestCheckpoint.Sequence)
	}
	if run.LatestCheckpoint.Stage != "planner" {
		t.Fatalf("LatestCheckpoint.Stage = %q, want planner", run.LatestCheckpoint.Stage)
	}
	if run.LatestCheckpoint.Label != "planner_declared_complete" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_declared_complete", run.LatestCheckpoint.Label)
	}
	if run.CollectedContext == nil || len(run.CollectedContext.Results) != 1 {
		t.Fatalf("CollectedContext = %#v, want one persisted result", run.CollectedContext)
	}
	if run.CollectedContext.Results[0].Kind != "file" {
		t.Fatalf("CollectedContext.Results[0].Kind = %q, want file", run.CollectedContext.Results[0].Kind)
	}
	if !strings.Contains(run.CollectedContext.Results[0].Preview, "bounded context preview") {
		t.Fatalf("CollectedContext.Results[0].Preview = %q", run.CollectedContext.Results[0].Preview)
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	if countJournalEvents(events, "run.completed") != 1 {
		t.Fatalf("run.completed count = %d, want 1", countJournalEvents(events, "run.completed"))
	}
}

func TestRunCommandAskHumanTerminalPathEndToEnd(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_question",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeAskHuman,
						AskHuman: &planner.AskHumanOutcome{
							Question: "Which exact file should the next slice edit?",
						},
					},
				},
				{
					ResponseID: "resp_pause",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomePause,
						Pause:           &planner.PauseOutcome{Reason: "stopping after the post-human planner turn"},
					},
				},
			},
		},
		nil,
		func(inv Invocation, _ *journal.Journal) orchestration.HumanInteractor {
			return terminalHumanInteractor{input: inv.Stdin, output: inv.Stdout}
		},
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "ask one human question and stop", "--bounded"},
		Stdin:    strings.NewReader("raw terminal reply\n"),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runRun() error = %v", err)
	}

	for _, want := range []string{
		"planner_question:",
		"human_reply> ",
		"cycle_number: 1",
		"first_planner_outcome: ask_human",
		"second_planner_outcome: pause",
		"stop_reason: planner_ask_human",
		"latest_checkpoint.label: planner_turn_post_human_reply",
		"next_operator_action: resume_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if len(run.HumanReplies) != 1 {
		t.Fatalf("HumanReplies len = %d, want 1", len(run.HumanReplies))
	}
	if run.HumanReplies[0].Source != "terminal" {
		t.Fatalf("HumanReplies[0].Source = %q, want terminal", run.HumanReplies[0].Source)
	}
	if run.HumanReplies[0].Payload != "raw terminal reply\n" {
		t.Fatalf("HumanReplies[0].Payload = %q, want raw terminal reply", run.HumanReplies[0].Payload)
	}
}

func TestRunCommandExecuteThenCompleteEndToEnd(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	executorCompletedAt := time.Date(2026, 4, 19, 18, 2, 0, 0, time.UTC)
	executorStub := &commandExecutor{
		result: executor.TurnResult{
			Transport:    executor.TransportAppServer,
			ThreadID:     "thr_execute",
			ThreadPath:   filepath.FromSlash("C:/Users/test/.codex/threads/thr_execute.jsonl"),
			TurnID:       "turn_execute",
			TurnStatus:   executor.TurnStatusCompleted,
			CompletedAt:  executorCompletedAt,
			FinalMessage: "implemented the bounded executor slice",
		},
	}

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_execute",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeExecute,
						Execute: &planner.ExecuteOutcome{
							Task:               "make one real executor edit",
							AcceptanceCriteria: []string{"persist executor result", "stop after second planner turn"},
							WriteScope:         []string{"internal/orchestration/cycle.go"},
						},
					},
				},
				{
					ResponseID: "resp_complete",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "executor result satisfied the bounded goal"},
					},
				},
			},
		},
		executorStub,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "execute once then complete", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runRun() error = %v", err)
	}

	for _, want := range []string{
		"cycle_number: 1",
		"first_planner_outcome: execute",
		"second_planner_outcome: complete",
		"executor_dispatched: true",
		"status: completed",
		"stop_reason: planner_complete",
		"latest_checkpoint.label: planner_declared_complete",
		"next_operator_action: no_action_required_run_completed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", run.Status)
	}
	if run.PreviousResponseID != "resp_complete" {
		t.Fatalf("PreviousResponseID = %q, want resp_complete", run.PreviousResponseID)
	}
	if run.LatestStopReason != orchestration.StopReasonPlannerComplete {
		t.Fatalf("LatestStopReason = %q, want %q", run.LatestStopReason, orchestration.StopReasonPlannerComplete)
	}
	if run.LatestCheckpoint.Sequence != 4 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 4", run.LatestCheckpoint.Sequence)
	}
	if run.LatestCheckpoint.Stage != "planner" {
		t.Fatalf("LatestCheckpoint.Stage = %q, want planner", run.LatestCheckpoint.Stage)
	}
	if run.LatestCheckpoint.Label != "planner_declared_complete" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_declared_complete", run.LatestCheckpoint.Label)
	}
	if run.ExecutorThreadID != "thr_execute" {
		t.Fatalf("ExecutorThreadID = %q, want thr_execute", run.ExecutorThreadID)
	}
	if run.ExecutorTurnID != "turn_execute" {
		t.Fatalf("ExecutorTurnID = %q, want turn_execute", run.ExecutorTurnID)
	}
	if run.ExecutorTurnStatus != string(executor.TurnStatusCompleted) {
		t.Fatalf("ExecutorTurnStatus = %q, want completed", run.ExecutorTurnStatus)
	}
	if run.ExecutorLastSuccess == nil || !*run.ExecutorLastSuccess {
		t.Fatalf("ExecutorLastSuccess = %#v, want true", run.ExecutorLastSuccess)
	}
	if run.ExecutorLastMessage != "implemented the bounded executor slice" {
		t.Fatalf("ExecutorLastMessage = %q", run.ExecutorLastMessage)
	}
	if len(executorStub.requests) != 1 {
		t.Fatalf("executor requests = %d, want 1", len(executorStub.requests))
	}

	events := readContinueEvents(t, layout, run.ID, 16)
	if countJournalEvents(events, "executor.turn.completed") != 1 {
		t.Fatalf("executor.turn.completed count = %d, want 1", countJournalEvents(events, "executor.turn.completed"))
	}
	if countJournalEvents(events, "run.completed") != 1 {
		t.Fatalf("run.completed count = %d, want 1", countJournalEvents(events, "run.completed"))
	}
}

func TestResumeCommandRunsOneBoundedCycleOnExistingRun(t *testing.T) {
	layout, existingRun := newContinueTestRuntime(t)

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_pause",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomePause,
						Pause:           &planner.PauseOutcome{Reason: "resume performed one bounded cycle"},
					},
				},
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runResume(context.Background(), Invocation{
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runResume() error = %v", err)
	}

	for _, want := range []string{
		"command: resume",
		"run_action: resumed_existing_run",
		"cycle_number: 1",
		"run_id: " + existingRun.ID,
		"stop_reason: planner_pause",
		"next_operator_action: continue_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.ID != existingRun.ID {
		t.Fatalf("Run.ID = %q, want %q", run.ID, existingRun.ID)
	}
	if run.PreviousResponseID != "resp_pause" {
		t.Fatalf("PreviousResponseID = %q, want resp_pause", run.PreviousResponseID)
	}
	if run.LatestStopReason != orchestration.StopReasonPlannerPause {
		t.Fatalf("LatestStopReason = %q, want %q", run.LatestStopReason, orchestration.StopReasonPlannerPause)
	}
	if run.LatestCheckpoint.Label != "planner_turn_completed" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_completed", run.LatestCheckpoint.Label)
	}
}

func TestContinueCommandStopsOnCompleteEndToEnd(t *testing.T) {
	layout, existingRun := newContinueTestRuntime(t)

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_complete",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "the bounded goal is complete"},
					},
				},
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runContinue(context.Background(), Invocation{
		Args:     []string{"--max-cycles", "3"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runContinue() error = %v", err)
	}

	for _, want := range []string{
		"command: continue",
		"run_action: continued_existing_run",
		"cycle_number: 1",
		"run_id: " + existingRun.ID,
		"stop_reason: planner_complete",
		"latest_checkpoint.label: planner_declared_complete",
		"next_operator_action: no_action_required_run_completed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", run.Status)
	}
	if run.PreviousResponseID != "resp_complete" {
		t.Fatalf("PreviousResponseID = %q, want resp_complete", run.PreviousResponseID)
	}
	if run.LatestStopReason != orchestration.StopReasonPlannerComplete {
		t.Fatalf("LatestStopReason = %q, want %q", run.LatestStopReason, orchestration.StopReasonPlannerComplete)
	}
	if run.LatestCheckpoint.Sequence != 2 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 2", run.LatestCheckpoint.Sequence)
	}

	events := readContinueEvents(t, layout, existingRun.ID, 10)
	stopEvent := latestJournalEvent(events, "continue.stopped")
	if stopEvent.StopReason != orchestration.StopReasonPlannerComplete {
		t.Fatalf("stopEvent.StopReason = %q, want %q", stopEvent.StopReason, orchestration.StopReasonPlannerComplete)
	}
}

func TestRunCommandFallsBackToTerminalWhenNTFYWaitFailsEndToEnd(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_question","event":"message","topic":"orchestrator-reply","message":"published"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/orchestrator-reply/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"open_1","event":"open","topic":"orchestrator-reply"}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_question",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeAskHuman,
						AskHuman:        &planner.AskHumanOutcome{Question: "What should we do next?"},
					},
				},
				{
					ResponseID: "resp_pause",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomePause,
						Pause:           &planner.PauseOutcome{Reason: "stopping after fallback reply"},
					},
				},
			},
		},
		nil,
		func(inv Invocation, journalWriter *journal.Journal) orchestration.HumanInteractor {
			return newHumanInteractor(inv, journalWriter)
		},
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "use terminal fallback when ntfy wait fails", "--bounded"},
		Stdin:    strings.NewReader("fallback terminal reply\n"),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config: config.Config{
			NTFY: config.NTFYConfig{
				ServerURL: server.URL,
				Topic:     "orchestrator-reply",
			},
		},
		Version: "test",
	})
	if err != nil {
		t.Fatalf("runRun() error = %v", err)
	}

	for _, want := range []string{
		"planner_question_delivery: terminal_fallback",
		"planner_question:",
		"human_reply> ",
		"stop_reason: planner_ask_human",
		"next_operator_action: resume_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if len(run.HumanReplies) != 1 {
		t.Fatalf("HumanReplies len = %d, want 1", len(run.HumanReplies))
	}
	if run.HumanReplies[0].Source != "terminal" {
		t.Fatalf("HumanReplies[0].Source = %q, want terminal", run.HumanReplies[0].Source)
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	if countJournalEvents(events, "ntfy.wait.failed") == 0 {
		t.Fatal("journal missing ntfy.wait.failed event")
	}
}

func TestRunCommandMissingPlannerAPIKeyPersistsMechanicalFailure(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	var stdout bytes.Buffer
	cfg := config.Default()
	cfg.Verbosity = "verbose"

	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "fail immediately without planner api key", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   cfg,
		Version:  "test",
	})
	if err == nil {
		t.Fatal("runRun() unexpectedly succeeded")
	}

	for _, want := range []string{
		"stop_reason: missing_required_config",
		"cycle_error: OPENAI_API_KEY is required for live planner calls",
		"latest_checkpoint.label: run_initialized",
		"runtime_issue.reason: missing_required_config",
		"next_operator_action: inspect_status",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.RuntimeIssueReason != orchestration.StopReasonMissingRequiredConfig {
		t.Fatalf("RuntimeIssueReason = %q, want %q", run.RuntimeIssueReason, orchestration.StopReasonMissingRequiredConfig)
	}
	if !strings.Contains(run.RuntimeIssueMessage, "OPENAI_API_KEY is required") {
		t.Fatalf("RuntimeIssueMessage = %q", run.RuntimeIssueMessage)
	}

	events := readContinueEvents(t, layout, run.ID, 10)
	failureEvent := latestJournalEvent(events, "planner.turn.failed")
	if failureEvent.StopReason != orchestration.StopReasonMissingRequiredConfig {
		t.Fatalf("failureEvent.StopReason = %q, want %q", failureEvent.StopReason, orchestration.StopReasonMissingRequiredConfig)
	}
}

func TestRunCommandPlannerValidationFailurePersistsArtifactAndStopReason(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			errs: []error{
				planner.NewValidationError(
					errors.New("planner output failed planner.v1 validation: complete.summary is required"),
					`{"id":"resp_invalid","output":[{"type":"message","content":[{"type":"output_text","text":"{\"contract_version\":\"planner.v1\",\"outcome\":\"complete\"}"}]}]}`,
					`{"contract_version":"planner.v1","outcome":"complete"}`,
					"resp_invalid",
				),
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	cfg := config.Default()
	cfg.Verbosity = "verbose"

	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "surface planner validation failures mechanically", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   cfg,
		Version:  "test",
	})
	if err == nil {
		t.Fatal("runRun() unexpectedly succeeded")
	}

	for _, want := range []string{
		"stop_reason: planner_validation_failed",
		"cycle_error: planner output failed planner.v1 validation: complete.summary is required",
		"latest_checkpoint.label: run_initialized",
		"runtime_issue.reason: planner_validation_failed",
		"next_operator_action: inspect_status",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.LatestStopReason != orchestration.StopReasonPlannerValidationFailed {
		t.Fatalf("LatestStopReason = %q, want %q", run.LatestStopReason, orchestration.StopReasonPlannerValidationFailed)
	}
	if run.RuntimeIssueReason != orchestration.StopReasonPlannerValidationFailed {
		t.Fatalf("RuntimeIssueReason = %q, want %q", run.RuntimeIssueReason, orchestration.StopReasonPlannerValidationFailed)
	}
	if run.PreviousResponseID != "" {
		t.Fatalf("PreviousResponseID = %q, want empty after validation failure", run.PreviousResponseID)
	}
	if run.LatestCheckpoint.Label != "run_initialized" {
		t.Fatalf("LatestCheckpoint.Label = %q, want run_initialized", run.LatestCheckpoint.Label)
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	failureEvent := latestJournalEvent(events, "planner.turn.failed")
	if failureEvent.StopReason != orchestration.StopReasonPlannerValidationFailed {
		t.Fatalf("failureEvent.StopReason = %q, want %q", failureEvent.StopReason, orchestration.StopReasonPlannerValidationFailed)
	}
	if failureEvent.ArtifactPath == "" {
		t.Fatal("failureEvent.ArtifactPath = empty, want planner validation artifact")
	}
	if !strings.Contains(failureEvent.ArtifactPreview, `"outcome":"complete"`) {
		t.Fatalf("failureEvent.ArtifactPreview = %q", failureEvent.ArtifactPreview)
	}

	artifactPath := filepath.Join(repoRoot, filepath.FromSlash(failureEvent.ArtifactPath))
	if _, statErr := os.Stat(artifactPath); statErr != nil {
		t.Fatalf("planner validation artifact missing at %s: %v", artifactPath, statErr)
	}
}

func TestRunCommandExecutorFailurePersistsStopReasonWithoutSecondPlannerTurn(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	executorStub := &commandExecutor{
		result: executor.TurnResult{
			Transport:  executor.TransportAppServer,
			ThreadID:   "thr_failed",
			ThreadPath: filepath.FromSlash("C:/Users/test/.codex/threads/thr_failed.jsonl"),
			TurnID:     "turn_failed",
			TurnStatus: executor.TurnStatusFailed,
			Error: &executor.Failure{
				Stage:   "turn_timeout",
				Message: "executor turn exceeded app-server wait deadline",
				Detail:  "context deadline exceeded",
			},
			FinalMessage: "partial executor output before failure",
		},
		err: errors.New("context deadline exceeded"),
	}

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_execute",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeExecute,
						Execute: &planner.ExecuteOutcome{
							Task:               "attempt one executor turn",
							AcceptanceCriteria: []string{"persist failure honestly"},
						},
					},
				},
			},
		},
		executorStub,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	cfg := config.Default()
	cfg.Verbosity = "verbose"

	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "surface executor failures honestly", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   cfg,
		Version:  "test",
	})
	if err == nil || err.Error() != "context deadline exceeded" {
		t.Fatalf("runRun() error = %v, want context deadline exceeded", err)
	}

	for _, want := range []string{
		"first_planner_outcome: execute",
		"executor_dispatched: true",
		"stop_reason: executor_failed",
		"cycle_error: context deadline exceeded",
		"latest_checkpoint.label: planner_turn_completed",
		"runtime_issue.reason: executor_failed",
		"next_operator_action: inspect_status",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.PreviousResponseID != "resp_execute" {
		t.Fatalf("PreviousResponseID = %q, want resp_execute", run.PreviousResponseID)
	}
	if run.LatestStopReason != orchestration.StopReasonExecutorFailed {
		t.Fatalf("LatestStopReason = %q, want %q", run.LatestStopReason, orchestration.StopReasonExecutorFailed)
	}
	if run.RuntimeIssueReason != orchestration.StopReasonExecutorFailed {
		t.Fatalf("RuntimeIssueReason = %q, want %q", run.RuntimeIssueReason, orchestration.StopReasonExecutorFailed)
	}
	if run.LatestCheckpoint.Sequence != 2 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 2", run.LatestCheckpoint.Sequence)
	}
	if run.LatestCheckpoint.Stage != "planner" {
		t.Fatalf("LatestCheckpoint.Stage = %q, want planner", run.LatestCheckpoint.Stage)
	}
	if run.LatestCheckpoint.Label != "planner_turn_completed" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_completed", run.LatestCheckpoint.Label)
	}
	if run.ExecutorTurnStatus != string(executor.TurnStatusFailed) {
		t.Fatalf("ExecutorTurnStatus = %q, want failed", run.ExecutorTurnStatus)
	}
	if run.ExecutorLastFailureStage != "turn_timeout" {
		t.Fatalf("ExecutorLastFailureStage = %q, want turn_timeout", run.ExecutorLastFailureStage)
	}
	if run.ExecutorLastMessage != "partial executor output before failure" {
		t.Fatalf("ExecutorLastMessage = %q", run.ExecutorLastMessage)
	}

	events := readContinueEvents(t, layout, run.ID, 16)
	if countJournalEvents(events, "planner.turn.completed") != 1 {
		t.Fatalf("planner.turn.completed count = %d, want 1", countJournalEvents(events, "planner.turn.completed"))
	}
	if countJournalEvents(events, "executor.turn.failed") != 1 {
		t.Fatalf("executor.turn.failed count = %d, want 1", countJournalEvents(events, "executor.turn.failed"))
	}
	failureEvent := latestJournalEvent(events, "executor.turn.failed")
	if failureEvent.StopReason != orchestration.StopReasonExecutorFailed {
		t.Fatalf("failureEvent.StopReason = %q, want %q", failureEvent.StopReason, orchestration.StopReasonExecutorFailed)
	}
	if failureEvent.Checkpoint != nil {
		t.Fatalf("failureEvent.Checkpoint = %#v, want nil for incomplete executor failure", failureEvent.Checkpoint)
	}
}

func TestRunCommandTerminalAskHumanReadFailurePersistsMechanicalFailure(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_question",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeAskHuman,
						AskHuman:        &planner.AskHumanOutcome{Question: "Need one raw reply"},
					},
				},
			},
		},
		nil,
		func(inv Invocation, _ *journal.Journal) orchestration.HumanInteractor {
			return terminalHumanInteractor{input: inv.Stdin, output: inv.Stdout}
		},
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	cfg := config.Default()
	cfg.Verbosity = "verbose"

	err := runRun(context.Background(), Invocation{
		Args:     []string{"--goal", "surface terminal input failure honestly", "--bounded"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   cfg,
		Version:  "test",
	})
	if err == nil {
		t.Fatal("runRun() unexpectedly succeeded")
	}

	for _, want := range []string{
		"planner_question:",
		"human_reply> ",
		"stop_reason: transport_or_process_error",
		"cycle_error: terminal input closed before human reply was received",
		"latest_checkpoint.label: planner_turn_completed",
		"runtime_issue.reason: transport_or_process_error",
		"next_operator_action: inspect_status",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.RuntimeIssueReason != orchestration.StopReasonTransportProcessError {
		t.Fatalf("RuntimeIssueReason = %q, want %q", run.RuntimeIssueReason, orchestration.StopReasonTransportProcessError)
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	failureEvent := latestJournalEvent(events, "human.reply.failed")
	if failureEvent.StopReason != orchestration.StopReasonTransportProcessError {
		t.Fatalf("failureEvent.StopReason = %q, want %q", failureEvent.StopReason, orchestration.StopReasonTransportProcessError)
	}
}

type commandPlanner struct {
	results             []planner.Result
	errs                []error
	calls               int
	inputs              []planner.InputEnvelope
	previousResponseIDs []string
}

func (p *commandPlanner) Plan(_ context.Context, input planner.InputEnvelope, previousResponseID string) (planner.Result, error) {
	p.calls++
	p.inputs = append(p.inputs, input)
	p.previousResponseIDs = append(p.previousResponseIDs, previousResponseID)

	index := p.calls - 1
	if index < len(p.errs) && p.errs[index] != nil {
		return planner.Result{}, p.errs[index]
	}
	if index >= len(p.results) {
		return planner.Result{}, errors.New("commandPlanner called more times than configured")
	}
	return p.results[index], nil
}

type commandExecutor struct {
	result   executor.TurnResult
	err      error
	requests []executor.TurnRequest
}

func (e *commandExecutor) Execute(_ context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	e.requests = append(e.requests, req)
	return e.result, e.err
}

func realCycleRunner(
	pl orchestration.Planner,
	ex orchestration.Executor,
	humanFactory func(Invocation, *journal.Journal) orchestration.HumanInteractor,
) boundedCycleRunnerFunc {
	return func(ctx context.Context, inv Invocation, store *state.Store, journalWriter *journal.Journal, run state.Run) (orchestration.Result, error) {
		var human orchestration.HumanInteractor
		if humanFactory != nil {
			human = humanFactory(inv, journalWriter)
		}

		cycle := orchestration.Cycle{
			Store:           store,
			Journal:         journalWriter,
			Planner:         pl,
			Executor:        ex,
			HumanInteractor: human,
			Events:          inv.Events,
		}
		return cycle.RunOnce(ctx, run)
	}
}

func latestRunForLayout(t *testing.T, layout state.Layout) state.Run {
	t.Helper()

	store, err := openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()

	run, found, err := store.LatestRun(context.Background())
	if err != nil {
		t.Fatalf("LatestRun() error = %v", err)
	}
	if !found {
		t.Fatal("LatestRun() found = false, want run")
	}

	return run
}
