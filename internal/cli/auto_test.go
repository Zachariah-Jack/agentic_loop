package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
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

func TestRunAutoStartReachesComplete(t *testing.T) {
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
						Pause:           &planner.PauseOutcome{Reason: "keep going in auto mode"},
					},
				},
				{
					ResponseID: "resp_complete",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "auto loop completed the run"},
					},
				},
			},
		},
		nil,
		nil,
	))
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runAuto(context.Background(), Invocation{
		Args:     []string{"start", "--goal", "finish through repeated bounded cycles"},
		Stdin:    strings.NewReader(""),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runAuto() error = %v", err)
	}

	for _, want := range []string{
		"auto.stop_flag_path: " + autoStopFlagPath(layout),
		"command: auto start",
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
	if countJournalEvents(events, "auto.started") != 1 {
		t.Fatalf("auto.started count = %d, want 1", countJournalEvents(events, "auto.started"))
	}
	if countJournalEvents(events, "auto.cycle.completed") != 2 {
		t.Fatalf("auto.cycle.completed count = %d, want 2", countJournalEvents(events, "auto.cycle.completed"))
	}
	stopEvent := latestJournalEvent(events, "auto.stopped")
	if stopEvent.StopReason != orchestration.StopReasonPlannerComplete {
		t.Fatalf("auto.stopped stop reason = %q, want %q", stopEvent.StopReason, orchestration.StopReasonPlannerComplete)
	}
}

func TestRunAutoContinueStopsOnAskHumanBoundary(t *testing.T) {
	layout, run := newContinueTestRuntime(t)

	restoreRunner := stubBoundedCycleRunner(func(_ context.Context, _ Invocation, _ *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		return orchestration.Result{
			Run: state.Run{
				ID:                 currentRun.ID,
				RepoPath:           currentRun.RepoPath,
				Goal:               currentRun.Goal,
				Status:             state.StatusInitialized,
				PreviousResponseID: "resp_followup_question",
				HumanReplies: []state.HumanReply{
					{
						ID:         "human_reply_1",
						Source:     "terminal",
						ReceivedAt: time.Now().UTC(),
						Payload:    "raw answer\n",
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
				ResponseID: "resp_first_question",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeAskHuman,
					AskHuman:        &planner.AskHumanOutcome{Question: "Need one raw reply?"},
				},
			},
			SecondPlannerTurn: &planner.Result{
				ResponseID: "resp_followup_question",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeAskHuman,
					AskHuman:        &planner.AskHumanOutcome{Question: "Need another raw reply?"},
				},
			},
		}, nil
	})
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runAuto(context.Background(), Invocation{
		Args:     []string{"continue"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runAuto() error = %v", err)
	}

	for _, want := range []string{
		"command: auto continue",
		"run_id: " + run.ID,
		"first_planner_outcome: ask_human",
		"second_planner_outcome: ask_human",
		"stop_reason: planner_ask_human",
		"latest_checkpoint.label: planner_turn_post_human_reply",
		"next_operator_action: continue_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	if countJournalEvents(events, "auto.waiting_for_human") != 1 {
		t.Fatalf("auto.waiting_for_human count = %d, want 1", countJournalEvents(events, "auto.waiting_for_human"))
	}
	stopEvent := latestJournalEvent(events, "auto.stopped")
	if stopEvent.StopReason != orchestration.StopReasonPlannerAskHuman {
		t.Fatalf("auto.stopped stop reason = %q, want %q", stopEvent.StopReason, orchestration.StopReasonPlannerAskHuman)
	}
}

func TestRunAutoStartContinuesAfterTerminalHumanReply(t *testing.T) {
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
						AskHuman:        &planner.AskHumanOutcome{Question: "Which exact file should we edit next?"},
					},
				},
				{
					ResponseID: "resp_pause",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomePause,
						Pause:           &planner.PauseOutcome{Reason: "cycle finished after the human reply"},
					},
				},
				{
					ResponseID: "resp_complete",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "auto mode finished after the follow-up cycle"},
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
	err := runAuto(context.Background(), Invocation{
		Args:     []string{"start", "--goal", "ask a human question and keep going automatically"},
		Stdin:    strings.NewReader("raw auto reply\n"),
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runAuto() error = %v", err)
	}

	for _, want := range []string{
		"planner_question:",
		"human_reply> ",
		"command: auto start",
		"cycle_number: 1",
		"cycle_number: 2",
		"stop_reason: planner_complete",
		"latest_checkpoint.label: planner_declared_complete",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	run := latestRunForLayout(t, layout)
	if run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", run.Status)
	}
	if len(run.HumanReplies) != 1 {
		t.Fatalf("HumanReplies len = %d, want 1", len(run.HumanReplies))
	}
	if run.HumanReplies[0].Source != "terminal" {
		t.Fatalf("HumanReplies[0].Source = %q, want terminal", run.HumanReplies[0].Source)
	}
	if run.HumanReplies[0].Payload != "raw auto reply\n" {
		t.Fatalf("HumanReplies[0].Payload = %q, want raw auto reply", run.HumanReplies[0].Payload)
	}

	events := readContinueEvents(t, layout, run.ID, 16)
	if countJournalEvents(events, "auto.waiting_for_human") != 1 {
		t.Fatalf("auto.waiting_for_human count = %d, want 1", countJournalEvents(events, "auto.waiting_for_human"))
	}
	if countJournalEvents(events, "auto.cycle.completed") != 2 {
		t.Fatalf("auto.cycle.completed count = %d, want 2", countJournalEvents(events, "auto.cycle.completed"))
	}
}

func TestRunAutoContinueStopsOnTransportProcessError(t *testing.T) {
	layout, run := newContinueTestRuntime(t)

	restoreRunner := stubBoundedCycleRunner(func(_ context.Context, _ Invocation, _ *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		return orchestration.Result{
			Run: state.Run{
				ID:                  currentRun.ID,
				RepoPath:            currentRun.RepoPath,
				Goal:                currentRun.Goal,
				Status:              state.StatusInitialized,
				LatestStopReason:    orchestration.StopReasonTransportProcessError,
				RuntimeIssueReason:  orchestration.StopReasonTransportProcessError,
				RuntimeIssueMessage: "planner transport failed",
				LatestCheckpoint: state.Checkpoint{
					Sequence:     currentRun.LatestCheckpoint.Sequence,
					Stage:        currentRun.LatestCheckpoint.Stage,
					Label:        currentRun.LatestCheckpoint.Label,
					SafePause:    currentRun.LatestCheckpoint.SafePause,
					PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn,
					ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
					CreatedAt:    currentRun.LatestCheckpoint.CreatedAt,
				},
			},
		}, errors.New("planner transport failed")
	})
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runAuto(context.Background(), Invocation{
		Args:     []string{"continue"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err == nil || err.Error() != "planner transport failed" {
		t.Fatalf("runAuto() error = %v, want planner transport failed", err)
	}

	for _, want := range []string{
		"command: auto continue",
		"run_id: " + run.ID,
		"cycle_error: planner transport failed",
		"stop_reason: transport_or_process_error",
		"next_operator_action: inspect_status",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	if countJournalEvents(events, "auto.cycle.completed") != 0 {
		t.Fatalf("auto.cycle.completed count = %d, want 0", countJournalEvents(events, "auto.cycle.completed"))
	}
	stopEvent := latestJournalEvent(events, "auto.stopped")
	if stopEvent.StopReason != orchestration.StopReasonTransportProcessError {
		t.Fatalf("auto.stopped stop reason = %q, want %q", stopEvent.StopReason, orchestration.StopReasonTransportProcessError)
	}
}

func TestRunAutoContinueStopsOnExecutorApprovalRequired(t *testing.T) {
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
					Kind:  string(executor.ApprovalKindCommandExecution),
				},
				LatestCheckpoint: state.Checkpoint{
					Sequence:     currentRun.LatestCheckpoint.Sequence,
					Stage:        currentRun.LatestCheckpoint.Stage,
					Label:        currentRun.LatestCheckpoint.Label,
					SafePause:    currentRun.LatestCheckpoint.SafePause,
					PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn,
					ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
					CreatedAt:    currentRun.LatestCheckpoint.CreatedAt,
				},
			},
			FirstPlannerResult: planner.Result{
				ResponseID: "resp_execute",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute:         &planner.ExecuteOutcome{Task: "attempt one executor turn"},
				},
			},
		}, nil
	})
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runAuto(context.Background(), Invocation{
		Args:     []string{"continue"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runAuto() error = %v", err)
	}

	for _, want := range []string{
		"command: auto continue",
		"run_id: " + run.ID,
		"stop_reason: executor_approval_required",
		"executor_approval_state: required",
		"next_operator_action: approve_or_deny_executor_request",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	stopEvent := latestJournalEvent(readContinueEvents(t, layout, run.ID, 12), "auto.stopped")
	if stopEvent.StopReason != orchestration.StopReasonExecutorApprovalReq {
		t.Fatalf("stopEvent.StopReason = %q, want %q", stopEvent.StopReason, orchestration.StopReasonExecutorApprovalReq)
	}
}

func TestRunAutoContinueHonorsOperatorStopFlag(t *testing.T) {
	layout, run := newContinueTestRuntime(t)
	stopFlagPath := autoStopFlagPath(layout)
	calls := 0

	restoreRunner := stubBoundedCycleRunner(func(_ context.Context, _ Invocation, _ *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		calls++
		if err := os.WriteFile(stopFlagPath, []byte("stop"), 0o644); err != nil {
			t.Fatalf("WriteFile(stop flag) error = %v", err)
		}

		return orchestration.Result{
			Run: state.Run{
				ID:                 currentRun.ID,
				RepoPath:           currentRun.RepoPath,
				Goal:               currentRun.Goal,
				Status:             state.StatusInitialized,
				PreviousResponseID: "resp_pause",
				LatestCheckpoint: state.Checkpoint{
					Sequence:     currentRun.LatestCheckpoint.Sequence + 1,
					Stage:        "planner",
					Label:        "planner_turn_completed_1",
					SafePause:    true,
					PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn + 1,
					ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
					CreatedAt:    time.Now().UTC(),
				},
			},
			FirstPlannerResult: planner.Result{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "pause after this cycle"},
				},
			},
		}, nil
	})
	defer restoreRunner()

	var stdout bytes.Buffer
	err := runAuto(context.Background(), Invocation{
		Args:     []string{"continue"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Config:   config.Default(),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runAuto() error = %v", err)
	}

	if calls != 1 {
		t.Fatalf("bounded cycle calls = %d, want 1", calls)
	}
	if pathExists(stopFlagPath) {
		t.Fatalf("stop flag %q still exists, want consumed", stopFlagPath)
	}

	for _, want := range []string{
		"command: auto continue",
		"run_id: " + run.ID,
		"cycle_number: 1",
		"stop_reason: operator_stop_requested",
		"latest_checkpoint.label: planner_turn_completed_1",
		"next_operator_action: continue_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	latestRun := latestRunForLayout(t, layout)
	if latestRun.LatestStopReason != orchestration.StopReasonOperatorStopRequested {
		t.Fatalf("LatestStopReason = %q, want %q", latestRun.LatestStopReason, orchestration.StopReasonOperatorStopRequested)
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	if countJournalEvents(events, "auto.cycle.completed") != 1 {
		t.Fatalf("auto.cycle.completed count = %d, want 1", countJournalEvents(events, "auto.cycle.completed"))
	}
	stopEvent := latestJournalEvent(events, "auto.stopped")
	if stopEvent.StopReason != orchestration.StopReasonOperatorStopRequested {
		t.Fatalf("auto.stopped stop reason = %q, want %q", stopEvent.StopReason, orchestration.StopReasonOperatorStopRequested)
	}
}
