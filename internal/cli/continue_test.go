package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestRunContinueNoUnfinishedRunPrintsLookupMessage(t *testing.T) {
	layout := state.ResolveLayout(t.TempDir())
	writeRepoMarkerFiles(t, layout.RepoRoot)

	var stdout bytes.Buffer
	err := runContinue(context.Background(), Invocation{
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
	})
	if err != nil {
		t.Fatalf("runContinue() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "continue_lookup: no unfinished run found") {
		t.Fatalf("stdout = %q, want no unfinished run message", stdout.String())
	}
}

func TestRunContinueStopsAtMaxCycles(t *testing.T) {
	layout, run := newContinueTestRuntime(t)

	restoreRunner := stubBoundedCycleRunner(func(_ context.Context, _ Invocation, _ *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		cycleNumber := int(currentRun.LatestCheckpoint.Sequence)
		responseID := fmt.Sprintf("resp_%d", cycleNumber)
		return orchestration.Result{
			Run: state.Run{
				ID:                 currentRun.ID,
				RepoPath:           currentRun.RepoPath,
				Goal:               currentRun.Goal,
				Status:             state.StatusInitialized,
				PreviousResponseID: responseID,
				LatestCheckpoint: state.Checkpoint{
					Sequence:     currentRun.LatestCheckpoint.Sequence + 1,
					Stage:        "planner",
					Label:        fmt.Sprintf("planner_turn_completed_%d", cycleNumber),
					SafePause:    true,
					PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn + 1,
					ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
					CreatedAt:    time.Now().UTC(),
				},
			},
			FirstPlannerResult: planner.Result{
				ResponseID: responseID,
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "pause and continue later"},
				},
			},
		}, nil
	})
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runContinue(context.Background(), Invocation{
		Args:     []string{"--max-cycles", "2"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
	})
	if err != nil {
		t.Fatalf("runContinue() error = %v", err)
	}

	for _, want := range []string{
		"command: continue",
		"run_action: continued_existing_run",
		"cycle_number: 1",
		"cycle_number: 2",
		"run_id: " + run.ID,
		"stop_reason: max_cycles_reached",
		"latest_checkpoint.label: planner_turn_completed_2",
		"next_operator_action: continue_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	events := readContinueEvents(t, layout, run.ID, 10)
	if countJournalEvents(events, "continue.started") != 1 {
		t.Fatalf("continue.started count = %d, want 1", countJournalEvents(events, "continue.started"))
	}
	if countJournalEvents(events, "continue.cycle.completed") != 2 {
		t.Fatalf("continue.cycle.completed count = %d, want 2", countJournalEvents(events, "continue.cycle.completed"))
	}
	stopEvent := latestJournalEvent(events, "continue.stopped")
	if stopEvent.StopReason != string(continueStopReasonMaxCyclesReached) {
		t.Fatalf("stopEvent.StopReason = %q, want %q", stopEvent.StopReason, continueStopReasonMaxCyclesReached)
	}
	if stopEvent.CycleNumber != 2 {
		t.Fatalf("stopEvent.CycleNumber = %d, want 2", stopEvent.CycleNumber)
	}

	latestRun := latestRunForLayout(t, layout)
	if latestRun.LatestStopReason != orchestration.StopReasonMaxCyclesReached {
		t.Fatalf("LatestStopReason = %q, want %q", latestRun.LatestStopReason, orchestration.StopReasonMaxCyclesReached)
	}
}

func TestRunContinueStopsAfterAskHumanCycleBoundary(t *testing.T) {
	layout, run := newContinueTestRuntime(t)

	restoreRunner := stubBoundedCycleRunner(func(_ context.Context, _ Invocation, _ *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		return orchestration.Result{
			Run: state.Run{
				ID:                 currentRun.ID,
				RepoPath:           currentRun.RepoPath,
				Goal:               currentRun.Goal,
				Status:             state.StatusInitialized,
				PreviousResponseID: "resp_followup",
				HumanReplies: []state.HumanReply{
					{
						ID:         "human_reply_1",
						Source:     "terminal",
						ReceivedAt: time.Now().UTC(),
						Payload:    "raw reply\n",
					},
				},
				LatestCheckpoint: state.Checkpoint{
					Sequence:     currentRun.LatestCheckpoint.Sequence + 2,
					Stage:        "planner",
					Label:        "planner_turn_post_human_reply",
					SafePause:    true,
					PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn + 2,
					ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
					CreatedAt:    time.Now().UTC(),
				},
			},
			FirstPlannerResult: planner.Result{
				ResponseID: "resp_question",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeAskHuman,
					AskHuman:        &planner.AskHumanOutcome{Question: "Need input?"},
				},
			},
			SecondPlannerTurn: &planner.Result{
				ResponseID: "resp_followup",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after second planner turn"},
				},
			},
		}, nil
	})
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runContinue(context.Background(), Invocation{
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
	})
	if err != nil {
		t.Fatalf("runContinue() error = %v", err)
	}

	for _, want := range []string{
		"command: continue",
		"run_action: continued_existing_run",
		"cycle_number: 1",
		"run_id: " + run.ID,
		"first_planner_outcome: ask_human",
		"second_planner_outcome: pause",
		"stop_reason: planner_ask_human",
		"latest_checkpoint.label: planner_turn_post_human_reply",
		"next_operator_action: continue_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	events := readContinueEvents(t, layout, run.ID, 10)
	if countJournalEvents(events, "continue.cycle.completed") != 1 {
		t.Fatalf("continue.cycle.completed count = %d, want 1", countJournalEvents(events, "continue.cycle.completed"))
	}
	stopEvent := latestJournalEvent(events, "continue.stopped")
	if stopEvent.StopReason != string(continueStopReasonPlannerAskHuman) {
		t.Fatalf("stopEvent.StopReason = %q, want %q", stopEvent.StopReason, continueStopReasonPlannerAskHuman)
	}
}

func TestRunContinueStopsOnExecutorFailure(t *testing.T) {
	layout, run := newContinueTestRuntime(t)

	restoreRunner := stubBoundedCycleRunner(func(_ context.Context, _ Invocation, _ *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		return orchestration.Result{
			Run: state.Run{
				ID:                 currentRun.ID,
				RepoPath:           currentRun.RepoPath,
				Goal:               currentRun.Goal,
				Status:             state.StatusInitialized,
				LatestStopReason:   orchestration.StopReasonExecutorFailed,
				RuntimeIssueReason: orchestration.StopReasonExecutorFailed,
				PreviousResponseID: "resp_execute",
				LatestCheckpoint: state.Checkpoint{
					Sequence:     currentRun.LatestCheckpoint.Sequence + 1,
					Stage:        "executor",
					Label:        "executor_turn_failed",
					SafePause:    false,
					PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn + 1,
					ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn + 1,
					CreatedAt:    time.Now().UTC(),
				},
			},
			FirstPlannerResult: planner.Result{
				ResponseID: "resp_execute",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute:         &planner.ExecuteOutcome{Task: "make a real executor call"},
				},
			},
		}, errors.New("context deadline exceeded")
	})
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runContinue(context.Background(), Invocation{
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
	})
	if err == nil || err.Error() != "context deadline exceeded" {
		t.Fatalf("runContinue() error = %v, want context deadline exceeded", err)
	}

	for _, want := range []string{
		"command: continue",
		"run_action: continued_existing_run",
		"cycle_number: 1",
		"run_id: " + run.ID,
		"cycle_error: context deadline exceeded",
		"stop_reason: executor_failed",
		"latest_checkpoint.label: executor_turn_failed",
		"next_operator_action: inspect_status",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	events := readContinueEvents(t, layout, run.ID, 10)
	if countJournalEvents(events, "continue.cycle.completed") != 0 {
		t.Fatalf("continue.cycle.completed count = %d, want 0", countJournalEvents(events, "continue.cycle.completed"))
	}
	stopEvent := latestJournalEvent(events, "continue.stopped")
	if stopEvent.StopReason != string(continueStopReasonExecutorFailed) {
		t.Fatalf("stopEvent.StopReason = %q, want %q", stopEvent.StopReason, continueStopReasonExecutorFailed)
	}
}

func TestRunContinueStopsOnExecutorApprovalRequired(t *testing.T) {
	layout, run := newContinueTestRuntime(t)

	restoreRunner := stubBoundedCycleRunner(func(_ context.Context, _ Invocation, _ *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		return orchestration.Result{
			Run: state.Run{
				ID:                 currentRun.ID,
				RepoPath:           currentRun.RepoPath,
				Goal:               currentRun.Goal,
				Status:             state.StatusInitialized,
				LatestStopReason:   orchestration.StopReasonExecutorApprovalReq,
				ExecutorThreadID:   "thr_approval",
				ExecutorTurnID:     "turn_approval",
				ExecutorTurnStatus: string(executor.TurnStatusApprovalRequired),
				ExecutorApproval: &state.ExecutorApproval{
					State: string(executor.ApprovalStateRequired),
					Kind:  string(executor.ApprovalKindFileChange),
				},
				LatestCheckpoint: currentRun.LatestCheckpoint,
			},
			FirstPlannerResult: planner.Result{
				ResponseID: "resp_execute",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute:         &planner.ExecuteOutcome{Task: "wait for executor approval"},
				},
			},
		}, nil
	})
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runContinue(context.Background(), Invocation{
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
	})
	if err != nil {
		t.Fatalf("runContinue() error = %v", err)
	}

	for _, want := range []string{
		"command: continue",
		"run_id: " + run.ID,
		"stop_reason: executor_approval_required",
		"executor_approval_state: required",
		"next_operator_action: approve_or_deny_executor_request",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	stopEvent := latestJournalEvent(readContinueEvents(t, layout, run.ID, 10), "continue.stopped")
	if stopEvent.StopReason != string(continueStopReasonExecutorApprovalReq) {
		t.Fatalf("stopEvent.StopReason = %q, want %q", stopEvent.StopReason, continueStopReasonExecutorApprovalReq)
	}
}

func newContinueTestRuntime(t *testing.T) (state.Layout, state.Run) {
	t.Helper()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)

	store, journalWriter, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	defer store.Close()

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "advance the latest unfinished run",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 19, 20, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := journalWriter.Append(journal.Event{
		Type:     "run.created",
		RunID:    run.ID,
		RepoPath: run.RepoPath,
		Goal:     run.Goal,
		Status:   string(run.Status),
		Message:  "durable run record created",
	}); err != nil {
		t.Fatalf("Append(run.created) error = %v", err)
	}

	return layout, run
}

func stubBoundedCycleRunner(stub boundedCycleRunnerFunc) func() {
	original := boundedCycleRunner
	boundedCycleRunner = stub
	return func() {
		boundedCycleRunner = original
	}
}

func readContinueEvents(t *testing.T, layout state.Layout, runID string, limit int) []journal.Event {
	t.Helper()

	journalWriter, err := openExistingJournal(layout)
	if err != nil {
		t.Fatalf("openExistingJournal() error = %v", err)
	}

	events, err := journalWriter.ReadRecent(runID, limit)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	return events
}

func countJournalEvents(events []journal.Event, eventType string) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func latestJournalEvent(events []journal.Event, eventType string) journal.Event {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == eventType {
			return events[i]
		}
	}
	return journal.Event{}
}
