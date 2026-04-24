package orchestration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"orchestrator/internal/activity"
	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestShouldDispatchExecutor(t *testing.T) {
	t.Parallel()

	if !ShouldDispatchExecutor(planner.OutputEnvelope{
		Outcome: planner.OutcomeExecute,
		Execute: &planner.ExecuteOutcome{Task: "implement a bounded slice", AcceptanceCriteria: []string{"it works"}},
	}) {
		t.Fatal("execute outcome should dispatch")
	}

	if ShouldDispatchExecutor(planner.OutputEnvelope{
		Outcome: planner.OutcomeAskHuman,
		AskHuman: &planner.AskHumanOutcome{
			Question: "Need clarification?",
		},
	}) {
		t.Fatal("non-execute outcome should not dispatch")
	}
}

func TestRenderExecutorPrompt(t *testing.T) {
	t.Parallel()

	prompt, err := RenderExecutorPrompt("ship the smallest real cycle", planner.OutputEnvelope{
		Outcome: planner.OutcomeExecute,
		Execute: &planner.ExecuteOutcome{
			Task:               "wire one executor dispatch",
			AcceptanceCriteria: []string{"planner execute dispatches once", "non-execute does not dispatch"},
			WriteScope:         []string{"internal/cli/run.go", "internal/orchestration/cycle.go"},
		},
	})
	if err != nil {
		t.Fatalf("RenderExecutorPrompt() error = %v", err)
	}

	for _, want := range []string{
		"ship the smallest real cycle",
		"wire one executor dispatch",
		"planner execute dispatches once",
		"internal/cli/run.go",
		"Do not write repo-analysis files, orchestration summaries, or run reports into the repo root by default.",
		".orchestrator/artifacts/reports/",
		"Do not choose a new task.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n%s", want, prompt)
		}
	}
}

func TestRenderExecutorPromptRejectsNonExecute(t *testing.T) {
	t.Parallel()

	if _, err := RenderExecutorPrompt("goal", planner.OutputEnvelope{
		Outcome: planner.OutcomePause,
		Pause:   &planner.PauseOutcome{Reason: "wait"},
	}); err == nil {
		t.Fatal("expected non-execute output to be rejected")
	}
}

func TestBuildExecutorCheckpoint(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	checkpoint := BuildExecutorCheckpoint(state.Checkpoint{
		Sequence:     2,
		PlannerTurn:  1,
		ExecutorTurn: 0,
	}, "executor_turn_completed", at)

	if checkpoint.Sequence != 3 {
		t.Fatalf("Sequence = %d, want 3", checkpoint.Sequence)
	}
	if checkpoint.Stage != "executor" {
		t.Fatalf("Stage = %q, want executor", checkpoint.Stage)
	}
	if checkpoint.Label != "executor_turn_completed" {
		t.Fatalf("Label = %q", checkpoint.Label)
	}
	if !checkpoint.SafePause {
		t.Fatal("SafePause should be true")
	}
	if checkpoint.ExecutorTurn != 1 {
		t.Fatalf("ExecutorTurn = %d, want 1", checkpoint.ExecutorTurn)
	}
	if checkpoint.CreatedAt != at {
		t.Fatalf("CreatedAt = %s, want %s", checkpoint.CreatedAt, at)
	}
}

func TestBuildExecutorResultInput(t *testing.T) {
	t.Parallel()

	success := true
	summary := BuildExecutorResultInput(state.Run{
		ExecutorThreadID:    "thr_123",
		ExecutorLastSuccess: &success,
		ExecutorLastMessage: "Implemented the bounded slice.",
	})
	if summary == nil {
		t.Fatal("BuildExecutorResultInput() = nil, want summary")
	}
	if !summary.Success {
		t.Fatal("summary.Success = false, want true")
	}
	if summary.ThreadID != "thr_123" {
		t.Fatalf("summary.ThreadID = %q, want thr_123", summary.ThreadID)
	}
	if summary.FinalMessage != "Implemented the bounded slice." {
		t.Fatalf("summary.FinalMessage = %q", summary.FinalMessage)
	}
}

func TestBuildCollectedContextInput(t *testing.T) {
	t.Parallel()

	summary := BuildCollectedContextInput(state.Run{
		CollectedContext: &state.CollectedContextState{
			Focus:     "Inspect planner inputs",
			Questions: []string{"What did the collector read?"},
			Results: []state.CollectedContextResult{
				{
					RequestedPath: "internal/orchestration",
					ResolvedPath:  `D:\Projects\agentic_loop\internal\orchestration`,
					Kind:          "dir",
					Entries:       []string{"cycle.go", "cycle_test.go"},
				},
			},
		},
	})
	if summary == nil {
		t.Fatal("BuildCollectedContextInput() = nil, want summary")
	}
	if summary.Focus != "Inspect planner inputs" {
		t.Fatalf("summary.Focus = %q", summary.Focus)
	}
	if len(summary.Results) != 1 || summary.Results[0].Kind != "dir" {
		t.Fatalf("summary.Results = %#v", summary.Results)
	}
}

func TestCycleRunOnceAskHumanRecordsRawReplyAndPerformsSecondPlannerTurn(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_question",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeAskHuman,
					AskHuman: &planner.AskHumanOutcome{
						Question: "Which path should the next bounded slice edit?",
						Context:  "Reply with the exact repo-relative path.",
					},
				},
			},
			{
				ResponseID: "resp_followup",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeAskHuman,
					AskHuman: &planner.AskHumanOutcome{
						Question: "Should the next slice update tests too?",
					},
				},
			},
		},
	}
	fakeHuman := &stubHumanInteractor{
		reply: "  internal/orchestration/cycle.go  \r\n",
	}

	cycle := Cycle{
		Store:           store,
		Journal:         journalWriter,
		Planner:         fakePlanner,
		HumanInteractor: fakeHuman,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if fakeHuman.calls != 1 {
		t.Fatalf("human interactor calls = %d, want 1", fakeHuman.calls)
	}
	if fakeHuman.lastOutcome.Question != "Which path should the next bounded slice edit?" {
		t.Fatalf("lastOutcome.Question = %q", fakeHuman.lastOutcome.Question)
	}
	if result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = true, want false")
	}
	if result.SecondPlannerTurn == nil {
		t.Fatal("SecondPlannerTurn = nil, want second planner result")
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_post_human_reply" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_post_human_reply", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.PreviousResponseID != "resp_followup" {
		t.Fatalf("PreviousResponseID = %q, want resp_followup", result.Run.PreviousResponseID)
	}
	if len(result.Run.HumanReplies) != 1 {
		t.Fatalf("HumanReplies len = %d, want 1", len(result.Run.HumanReplies))
	}
	if result.Run.HumanReplies[0].Source != "terminal" {
		t.Fatalf("HumanReplies[0].Source = %q, want terminal", result.Run.HumanReplies[0].Source)
	}
	if result.Run.HumanReplies[0].Payload != fakeHuman.reply {
		t.Fatalf("HumanReplies[0].Payload = %q, want %q", result.Run.HumanReplies[0].Payload, fakeHuman.reply)
	}

	if fakePlanner.previousResponseIDs[1] != "resp_question" {
		t.Fatalf("second planner previous_response_id = %q, want resp_question", fakePlanner.previousResponseIDs[1])
	}
	if len(fakePlanner.inputs[1].RawHumanReplies) != 1 {
		t.Fatalf("second planner raw_human_replies len = %d, want 1", len(fakePlanner.inputs[1].RawHumanReplies))
	}
	if fakePlanner.inputs[1].RawHumanReplies[0].Payload != fakeHuman.reply {
		t.Fatalf("second planner raw reply payload = %q, want %q", fakePlanner.inputs[1].RawHumanReplies[0].Payload, fakeHuman.reply)
	}

	events, err := journalWriter.ReadRecent(run.ID, 12)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "human.question.presented") {
		t.Fatal("journal missing human.question.presented event")
	}
	if !containsEventType(events, "human.reply.recorded") {
		t.Fatal("journal missing human.reply.recorded event")
	}
}

func TestCycleRunOncePersistsPlannerOperatorStatusAndEmitsEvent(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	stream, cancel := events.Subscribe(0)
	defer cancel()

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_status",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "waiting at a safe boundary"},
					OperatorStatus: &planner.OperatorStatus{
						OperatorMessage:    "Implementing the next bounded slice.",
						CurrentFocus:       "planner operator-status persistence",
						NextIntendedStep:   "surface the safe status block through CLI and protocol",
						WhyThisStep:        "operators need a live planner-safe summary without exposing hidden reasoning.",
						ProgressPercent:    46,
						ProgressConfidence: planner.ProgressConfidenceMedium,
						ProgressBasis:      "cycle persistence and event routing already exist; this slice is wiring operator status through them.",
					},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
		Events:  events,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Run.PlannerOperatorStatus == nil {
		t.Fatal("PlannerOperatorStatus = nil, want persisted planner-safe status")
	}
	if result.Run.PlannerOperatorStatus.OperatorMessage != "Implementing the next bounded slice." {
		t.Fatalf("OperatorMessage = %q, want persisted operator message", result.Run.PlannerOperatorStatus.OperatorMessage)
	}

	seenPlannerCompleted := false
	seenOperatorMessage := false
	deadline := time.After(2 * time.Second)
	for !seenPlannerCompleted || !seenOperatorMessage {
		select {
		case event := <-stream:
			if event.Event == "planner_turn_completed" {
				seenPlannerCompleted = true
				if payload, _ := event.Payload["operator_status"].(map[string]any); payload == nil || payload["operator_message"] != "Implementing the next bounded slice." {
					t.Fatalf("planner_turn_completed operator_status = %#v, want live operator status payload", event.Payload["operator_status"])
				}
			}
			if event.Event == "planner_operator_message" {
				seenOperatorMessage = true
				if event.Payload["operator_message"] != "Implementing the next bounded slice." {
					t.Fatalf("planner_operator_message payload = %#v, want operator message", event.Payload)
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for planner operator events")
		}
	}
}

func TestCycleRunOnceFirstPlannerCompleteMarksRunCompleted(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_complete",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeComplete,
					Complete: &planner.CompleteOutcome{
						Summary: "The bounded orchestrator slice is complete.",
					},
				},
			},
		},
	}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: &stubExecutor{},
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", result.Run.Status)
	}
	if result.Run.LatestCheckpoint.Label != "planner_declared_complete" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_declared_complete", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.PreviousResponseID != "resp_complete" {
		t.Fatalf("PreviousResponseID = %q, want resp_complete", result.Run.PreviousResponseID)
	}
	if result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = true, want false")
	}
	if result.SecondPlannerTurn != nil {
		t.Fatal("SecondPlannerTurn should be nil for first-turn complete outcome")
	}

	resumableRun, found, err := store.LatestResumableRun(context.Background())
	if err != nil {
		t.Fatalf("LatestResumableRun() error = %v", err)
	}
	if found {
		t.Fatalf("LatestResumableRun() = %#v, want none", resumableRun)
	}

	events, err := journalWriter.ReadRecent(run.ID, 10)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "run.completed") {
		t.Fatal("journal missing run.completed event")
	}
}

func TestCycleRunOnceExecuteDispatchesExactlyOneExecutorTurnAndPerformsPostExecutorPlannerTurn(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_123",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "implement the bounded planner to executor slice",
						AcceptanceCriteria: []string{"dispatch one executor turn", "persist the result"},
						WriteScope:         []string{"internal/cli/run.go", "internal/orchestration/cycle.go"},
					},
				},
			},
			{
				ResponseID: "resp_456",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeAskHuman,
					AskHuman: &planner.AskHumanOutcome{
						Question: "Should the next slice add resume semantics for the second planner turn?",
						Context:  "The bounded run now persists a post-executor planner outcome.",
					},
				},
			},
		},
	}

	startedAt := time.Date(2026, 4, 19, 18, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Minute)
	fakeExecutor := &stubExecutor{
		result: executor.TurnResult{
			Transport:    executor.TransportAppServer,
			RunID:        run.ID,
			ThreadID:     "thr_123",
			ThreadPath:   filepath.FromSlash("C:/Users/test/.codex/threads/thr_123.jsonl"),
			TurnID:       "turn_123",
			TurnStatus:   executor.TurnStatusCompleted,
			StartedAt:    startedAt,
			CompletedAt:  completedAt,
			FinalMessage: "Implemented the bounded planner to executor slice and persisted the executor result.",
		},
	}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if !result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = false, want true")
	}
	if fakeExecutor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", fakeExecutor.calls)
	}
	if fakePlanner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", fakePlanner.calls)
	}
	if fakePlanner.inputs[0].Capabilities.Executor != planner.CapabilityAvailable {
		t.Fatalf("first planner input executor capability = %q, want available", fakePlanner.inputs[0].Capabilities.Executor)
	}
	if fakePlanner.inputs[0].ExecutorResult != nil {
		t.Fatal("first planner input unexpectedly included executor result")
	}
	if fakePlanner.previousResponseIDs[0] != "" {
		t.Fatalf("first planner previous_response_id = %q, want empty", fakePlanner.previousResponseIDs[0])
	}
	if fakePlanner.previousResponseIDs[1] != "resp_123" {
		t.Fatalf("second planner previous_response_id = %q, want resp_123", fakePlanner.previousResponseIDs[1])
	}
	if fakePlanner.inputs[1].ExecutorResult == nil {
		t.Fatal("second planner input missing executor result summary")
	}
	if !fakePlanner.inputs[1].ExecutorResult.Success {
		t.Fatal("second planner executor result success = false, want true")
	}
	if fakePlanner.inputs[1].ExecutorResult.ThreadID != "thr_123" {
		t.Fatalf("second planner executor result thread_id = %q, want thr_123", fakePlanner.inputs[1].ExecutorResult.ThreadID)
	}
	if fakePlanner.inputs[1].ExecutorResult.FinalMessage != fakeExecutor.result.FinalMessage {
		t.Fatalf("second planner executor result final_message = %q", fakePlanner.inputs[1].ExecutorResult.FinalMessage)
	}
	if !strings.Contains(fakeExecutor.lastRequest.Prompt, "implement the bounded planner to executor slice") {
		t.Fatalf("executor prompt missing task:\n%s", fakeExecutor.lastRequest.Prompt)
	}

	if result.PostExecutorPlannerTurn == nil {
		t.Fatal("PostExecutorPlannerTurn = nil, want second planner result")
	}
	if result.SecondPlannerTurn == nil {
		t.Fatal("SecondPlannerTurn = nil, want second planner result")
	}
	if result.Run.PreviousResponseID != "resp_456" {
		t.Fatalf("PreviousResponseID = %q, want resp_456", result.Run.PreviousResponseID)
	}
	if result.Run.LatestCheckpoint.Stage != "planner" {
		t.Fatalf("LatestCheckpoint.Stage = %q, want planner", result.Run.LatestCheckpoint.Stage)
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_post_executor" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_post_executor", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.LatestCheckpoint.Sequence != 4 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 4", result.Run.LatestCheckpoint.Sequence)
	}
	if result.Run.ExecutorTransport != string(executor.TransportAppServer) {
		t.Fatalf("ExecutorTransport = %q", result.Run.ExecutorTransport)
	}
	if result.Run.ExecutorTurnStatus != string(executor.TurnStatusCompleted) {
		t.Fatalf("ExecutorTurnStatus = %q", result.Run.ExecutorTurnStatus)
	}
	if result.Run.ExecutorLastSuccess == nil || !*result.Run.ExecutorLastSuccess {
		t.Fatalf("ExecutorLastSuccess = %#v, want true", result.Run.ExecutorLastSuccess)
	}
	if result.Run.ExecutorLastMessage != fakeExecutor.result.FinalMessage {
		t.Fatalf("ExecutorLastMessage = %q, want final message", result.Run.ExecutorLastMessage)
	}

	events, err := journalWriter.ReadRecent(run.ID, 12)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}

	for _, want := range []string{
		"planner.turn.completed",
		"executor.turn.dispatched",
		"executor.turn.completed",
	} {
		if !containsEventType(events, want) {
			t.Fatalf("journal missing %q event", want)
		}
	}
	if countEventType(events, "planner.turn.completed") != 2 {
		t.Fatalf("planner.turn.completed count = %d, want 2", countEventType(events, "planner.turn.completed"))
	}
}

func TestCycleRunOnceExecuteApprovalRequiredPersistsStateAndStopsBeforeSecondPlannerTurn(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_execute",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "attempt one executor turn that needs approval",
						AcceptanceCriteria: []string{"persist approval-required executor state"},
					},
				},
			},
		},
	}

	fakeExecutor := &stubExecutor{
		result: executor.TurnResult{
			Transport:     executor.TransportAppServer,
			RunID:         run.ID,
			ThreadID:      "thr_approval",
			ThreadPath:    filepath.FromSlash("C:/Users/test/.codex/threads/thr_approval.jsonl"),
			TurnID:        "turn_approval",
			TurnStatus:    executor.TurnStatusApprovalRequired,
			ApprovalState: executor.ApprovalStateRequired,
			Approval: &executor.ApprovalRequest{
				RequestID:  "req_approval",
				ApprovalID: "approval_123",
				ItemID:     "item_123",
				State:      executor.ApprovalStateRequired,
				Kind:       executor.ApprovalKindCommandExecution,
				Command:    "go test ./...",
				CWD:        run.RepoPath,
				Reason:     "Run the requested test command.",
				RawParams:  `{"approvalId":"approval_123"}`,
			},
			FinalMessage:  "waiting for approval before continuing",
			Interruptible: true,
		},
	}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if !result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = false, want true")
	}
	if result.SecondPlannerTurn != nil {
		t.Fatalf("SecondPlannerTurn = %#v, want nil when executor waits for approval", result.SecondPlannerTurn)
	}
	if result.PostExecutorPlannerTurn != nil {
		t.Fatalf("PostExecutorPlannerTurn = %#v, want nil when executor waits for approval", result.PostExecutorPlannerTurn)
	}
	if result.Run.LatestStopReason != StopReasonExecutorApprovalReq {
		t.Fatalf("LatestStopReason = %q, want %q", result.Run.LatestStopReason, StopReasonExecutorApprovalReq)
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_completed" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_completed", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.LatestCheckpoint.Sequence != 2 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 2", result.Run.LatestCheckpoint.Sequence)
	}
	if result.Run.ExecutorTurnStatus != string(executor.TurnStatusApprovalRequired) {
		t.Fatalf("ExecutorTurnStatus = %q, want approval_required", result.Run.ExecutorTurnStatus)
	}
	if result.Run.ExecutorApproval == nil {
		t.Fatal("ExecutorApproval = nil, want persisted approval request")
	}
	if result.Run.ExecutorApproval.State != string(executor.ApprovalStateRequired) {
		t.Fatalf("ExecutorApproval.State = %q, want required", result.Run.ExecutorApproval.State)
	}
	if result.Run.ExecutorApproval.Kind != string(executor.ApprovalKindCommandExecution) {
		t.Fatalf("ExecutorApproval.Kind = %q, want command_execution", result.Run.ExecutorApproval.Kind)
	}

	events, err := journalWriter.ReadRecent(run.ID, 16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "executor.turn.paused") {
		t.Fatal("journal missing executor.turn.paused event")
	}
	approvalEvent := latestEventType(events, "executor.approval.required")
	if approvalEvent.Type != "executor.approval.required" {
		t.Fatal("journal missing executor.approval.required event")
	}
	if approvalEvent.StopReason != StopReasonExecutorApprovalReq {
		t.Fatalf("approvalEvent.StopReason = %q, want %q", approvalEvent.StopReason, StopReasonExecutorApprovalReq)
	}
	if approvalEvent.Checkpoint == nil || approvalEvent.Checkpoint.Sequence != 2 {
		t.Fatalf("approvalEvent.Checkpoint = %#v, want last stable planner checkpoint", approvalEvent.Checkpoint)
	}
}

func TestCycleRunOnceQueuedControlMessageTriggersPlannerInterventionAtSafePoint(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	eventStream, cancel := events.Subscribe(0)
	defer cancel()

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_execute_initial",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "apply the blue wall change",
						AcceptanceCriteria: []string{"bounded executor handoff prepared"},
						WriteScope:         []string{"game/walls.go"},
					},
				},
			},
			{
				ResponseID: "resp_pause_after_intervention",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause: &planner.PauseOutcome{
						Reason: "operator intervention received; pause before any executor dispatch",
					},
				},
			},
		},
		afterPlan: func(call int, _ planner.InputEnvelope, _ string) {
			if call != 1 {
				return
			}
			if _, err := store.RecordControlMessage(context.Background(), state.CreateControlMessageParams{
				RunID:         run.ID,
				TargetBinding: "latest_unfinished_run",
				Source:        "control_chat",
				Reason:        "operator_intervention",
				RawText:       "Make that wall red, not blue.",
				CreatedAt:     time.Now().UTC(),
			}); err != nil {
				t.Fatalf("RecordControlMessage() error = %v", err)
			}
		},
	}

	fakeExecutor := &stubExecutor{}
	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
		Events:   events,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if fakePlanner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", fakePlanner.calls)
	}
	if fakeExecutor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0 after intervention pause", fakeExecutor.calls)
	}
	if result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = true, want false after intervention pause")
	}
	if result.SecondPlannerTurn == nil {
		t.Fatal("SecondPlannerTurn = nil, want intervention planner turn")
	}
	if result.SecondPlannerTurn.Output.Outcome != planner.OutcomePause {
		t.Fatalf("SecondPlannerTurn outcome = %q, want pause", result.SecondPlannerTurn.Output.Outcome)
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_post_control_intervention" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_post_control_intervention", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.PreviousResponseID != "resp_pause_after_intervention" {
		t.Fatalf("PreviousResponseID = %q, want resp_pause_after_intervention", result.Run.PreviousResponseID)
	}

	if fakePlanner.inputs[0].ControlIntervention != nil {
		t.Fatalf("first planner input control_intervention = %#v, want nil", fakePlanner.inputs[0].ControlIntervention)
	}
	if fakePlanner.inputs[1].ControlIntervention == nil {
		t.Fatal("second planner input missing control_intervention")
	}
	if fakePlanner.inputs[1].ControlIntervention.RawMessage != "Make that wall red, not blue." {
		t.Fatalf("control_intervention.raw_message = %q, want raw operator message", fakePlanner.inputs[1].ControlIntervention.RawMessage)
	}
	if fakePlanner.inputs[1].ControlIntervention.Source != "control_chat" {
		t.Fatalf("control_intervention.source = %q, want control_chat", fakePlanner.inputs[1].ControlIntervention.Source)
	}
	if fakePlanner.inputs[1].ControlIntervention.PauseReason != "operator_intervention_at_safe_point" {
		t.Fatalf("control_intervention.pause_reason = %q, want operator_intervention_at_safe_point", fakePlanner.inputs[1].ControlIntervention.PauseReason)
	}
	if fakePlanner.inputs[1].PendingAction == nil {
		t.Fatal("second planner input missing pending_action")
	}
	if !fakePlanner.inputs[1].PendingAction.Present {
		t.Fatal("second planner input pending_action.present = false, want true")
	}
	if fakePlanner.inputs[1].PendingAction.PlannerOutcome != "execute" {
		t.Fatalf("pending_action.planner_outcome = %q, want execute", fakePlanner.inputs[1].PendingAction.PlannerOutcome)
	}
	if fakePlanner.inputs[1].PendingAction.TurnType != "executor_dispatch" {
		t.Fatalf("pending_action.turn_type = %q, want executor_dispatch", fakePlanner.inputs[1].PendingAction.TurnType)
	}
	if !fakePlanner.inputs[1].PendingAction.Held {
		t.Fatal("pending_action.held = false, want true")
	}
	if fakePlanner.inputs[1].PendingAction.HoldReason != "control_message_queued" {
		t.Fatalf("pending_action.hold_reason = %q, want control_message_queued", fakePlanner.inputs[1].PendingAction.HoldReason)
	}
	if fakePlanner.inputs[1].PendingAction.PendingDispatchTarget == nil || fakePlanner.inputs[1].PendingAction.PendingDispatchTarget.Kind != "primary_executor" {
		t.Fatalf("pending_action.pending_dispatch_target = %#v, want primary_executor", fakePlanner.inputs[1].PendingAction.PendingDispatchTarget)
	}
	if !strings.Contains(fakePlanner.inputs[1].PendingAction.PendingExecutorPrompt, "apply the blue wall change") {
		t.Fatalf("pending_action.pending_executor_prompt missing execute task:\n%s", fakePlanner.inputs[1].PendingAction.PendingExecutorPrompt)
	}

	queued, err := store.ListControlMessages(context.Background(), run.ID, state.ControlMessageQueued, 10)
	if err != nil {
		t.Fatalf("ListControlMessages(queued) error = %v", err)
	}
	if len(queued) != 0 {
		t.Fatalf("queued control messages = %#v, want none after intervention consumption", queued)
	}
	consumed, err := store.ListControlMessages(context.Background(), run.ID, state.ControlMessageConsumed, 10)
	if err != nil {
		t.Fatalf("ListControlMessages(consumed) error = %v", err)
	}
	if len(consumed) != 1 {
		t.Fatalf("consumed control messages len = %d, want 1", len(consumed))
	}

	pendingAction, found, err := store.GetPendingAction(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetPendingAction() error = %v", err)
	}
	if found {
		t.Fatalf("pending action = %#v, want cleared after intervention pause turn", pendingAction)
	}

	journalEvents, err := journalWriter.ReadRecent(run.ID, 20)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(journalEvents, "control.intervention.pending") {
		t.Fatal("journal missing control.intervention.pending event")
	}

	requiredEvents := map[string]bool{
		"pending_action_updated":              false,
		"safe_point_intervention_pending":     false,
		"planner_intervention_turn_started":   false,
		"control_message_consumed":            false,
		"planner_intervention_turn_completed": false,
		"pending_action_cleared":              false,
	}
	deadline := time.After(2 * time.Second)
	for {
		allSeen := true
		for _, seen := range requiredEvents {
			if !seen {
				allSeen = false
				break
			}
		}
		if allSeen {
			break
		}

		select {
		case event := <-eventStream:
			if _, ok := requiredEvents[event.Event]; ok {
				requiredEvents[event.Event] = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for intervention events: %#v", requiredEvents)
		}
	}
}

func TestCycleRunOncePersistsAndSurfacesNonExecuteOutcomeWithoutDispatch(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause: &planner.PauseOutcome{
						Reason: "Pause after recording the planner outcome.",
					},
				},
			},
		},
	}
	fakeExecutor := &stubExecutor{}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = true, want false")
	}
	if fakeExecutor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", fakeExecutor.calls)
	}
	if result.Run.PreviousResponseID != "resp_pause" {
		t.Fatalf("PreviousResponseID = %q, want resp_pause", result.Run.PreviousResponseID)
	}
	if result.Run.LatestCheckpoint.Stage != "planner" {
		t.Fatalf("LatestCheckpoint.Stage = %q, want planner", result.Run.LatestCheckpoint.Stage)
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_completed" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_completed", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.LatestCheckpoint.Sequence != 2 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 2", result.Run.LatestCheckpoint.Sequence)
	}
	if result.Run.ExecutorTurnStatus != "" {
		t.Fatalf("ExecutorTurnStatus = %q, want empty", result.Run.ExecutorTurnStatus)
	}
	if result.PostExecutorPlannerTurn != nil {
		t.Fatal("PostExecutorPlannerTurn should be nil for non-execute outcome")
	}
	if result.SecondPlannerTurn != nil {
		t.Fatal("SecondPlannerTurn should be nil for non-execute outcome")
	}

	events, err := journalWriter.ReadRecent(run.ID, 10)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}

	if containsEventType(events, "executor.turn.dispatched") {
		t.Fatal("journal unexpectedly recorded executor dispatch for non-execute outcome")
	}
	if !containsEventType(events, "planner.turn.completed") {
		t.Fatal("journal missing planner.turn.completed event")
	}
}

func TestCycleRunOnceCollectContextPersistsResultsAndPerformsSecondPlannerTurn(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := os.Mkdir(filepath.Join(run.RepoPath, "docs"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.RepoPath, "docs", "note.txt"), []byte("bounded context preview"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(run.RepoPath, "internal"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.RepoPath, "internal", "keep.go"), []byte("package internal"), 0o600); err != nil {
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
						Focus:     "Inspect the requested repo paths.",
						Questions: []string{"What is inside docs and internal?"},
						Paths:     []string{"docs/note.txt", "internal", "missing.txt"},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause: &planner.PauseOutcome{
						Reason: "Collected context is enough for the next bounded slice.",
					},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = true, want false")
	}
	if fakePlanner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", fakePlanner.calls)
	}
	if result.SecondPlannerTurn == nil {
		t.Fatal("SecondPlannerTurn = nil, want second planner result")
	}
	if result.PostExecutorPlannerTurn != nil {
		t.Fatal("PostExecutorPlannerTurn should be nil for collect_context flow")
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_post_collect_context" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_post_collect_context", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.PreviousResponseID != "resp_pause" {
		t.Fatalf("PreviousResponseID = %q, want resp_pause", result.Run.PreviousResponseID)
	}
	if result.Run.CollectedContext == nil {
		t.Fatal("CollectedContext = nil, want persisted collected context")
	}
	if result.Run.CollectedContext.Focus != "Inspect the requested repo paths." {
		t.Fatalf("CollectedContext.Focus = %q", result.Run.CollectedContext.Focus)
	}
	if result.Run.CollectedContext.ArtifactPath == "" {
		t.Fatal("CollectedContext.ArtifactPath = empty, want run-scoped context artifact")
	}
	if !strings.HasPrefix(result.Run.CollectedContext.ArtifactPath, filepath.ToSlash(filepath.Join(contextArtifactDir, run.ID))+"/") {
		t.Fatalf("CollectedContext.ArtifactPath = %q, want context artifact path under %s", result.Run.CollectedContext.ArtifactPath, contextArtifactDir)
	}
	if _, err := os.Stat(filepath.Join(run.RepoPath, filepath.FromSlash(result.Run.CollectedContext.ArtifactPath))); err != nil {
		t.Fatalf("run-scoped context artifact missing at %s: %v", result.Run.CollectedContext.ArtifactPath, err)
	}
	if len(result.Run.CollectedContext.Results) != 3 {
		t.Fatalf("CollectedContext.Results len = %d, want 3", len(result.Run.CollectedContext.Results))
	}

	secondInput := fakePlanner.inputs[1]
	if secondInput.CollectedContext == nil {
		t.Fatal("second planner input missing collected_context summary")
	}
	if secondInput.ExecutorResult != nil {
		t.Fatal("second planner input unexpectedly included executor_result")
	}
	if secondInput.CollectedContext.Results[0].Kind != "file" {
		t.Fatalf("first collected result kind = %q, want file", secondInput.CollectedContext.Results[0].Kind)
	}
	if !strings.Contains(secondInput.CollectedContext.Results[0].Preview, "bounded context preview") {
		t.Fatalf("file preview = %q, want file contents", secondInput.CollectedContext.Results[0].Preview)
	}
	if secondInput.CollectedContext.Results[1].Kind != "dir" {
		t.Fatalf("second collected result kind = %q, want dir", secondInput.CollectedContext.Results[1].Kind)
	}
	if len(secondInput.CollectedContext.Results[1].Entries) == 0 || secondInput.CollectedContext.Results[1].Entries[0] != "keep.go" {
		t.Fatalf("directory entries = %#v, want keep.go listing", secondInput.CollectedContext.Results[1].Entries)
	}
	if secondInput.CollectedContext.Results[2].Kind != "missing" {
		t.Fatalf("third collected result kind = %q, want missing", secondInput.CollectedContext.Results[2].Kind)
	}
	if secondInput.CollectedContext.Results[2].Detail != "path_not_found" {
		t.Fatalf("third collected result detail = %q, want path_not_found", secondInput.CollectedContext.Results[2].Detail)
	}
	if fakePlanner.previousResponseIDs[1] != "resp_collect" {
		t.Fatalf("second planner previous_response_id = %q, want resp_collect", fakePlanner.previousResponseIDs[1])
	}

	events, err := journalWriter.ReadRecent(run.ID, 16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "context.collection.completed") {
		t.Fatal("journal missing context.collection.completed event")
	}
	completedEvent := latestEventType(events, "context.collection.completed")
	if completedEvent.ArtifactPath == "" {
		t.Fatal("context.collection.completed missing run-scoped artifact path")
	}
	if completedEvent.ArtifactPath != result.Run.CollectedContext.ArtifactPath {
		t.Fatalf("completedEvent.ArtifactPath = %q, want %q", completedEvent.ArtifactPath, result.Run.CollectedContext.ArtifactPath)
	}
	if countEventType(events, "context.collection.recorded") != 3 {
		t.Fatalf("context.collection.recorded count = %d, want 3", countEventType(events, "context.collection.recorded"))
	}
	if countEventType(events, "planner.turn.completed") != 2 {
		t.Fatalf("planner.turn.completed count = %d, want 2", countEventType(events, "planner.turn.completed"))
	}
}

func TestCycleRunOnceCollectContextWorkerCreatePersistsPendingStateAndFeedsPlanner(t *testing.T) {
	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})
	installFakeGitForCycleTests(t)

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect_worker",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "Create an isolated worker before deciding whether to dispatch it.",
						WorkerActions: []planner.WorkerAction{
							{
								Action:     planner.WorkerActionCreate,
								WorkerName: "code-survey",
								Scope:      "repo survey",
							},
						},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after recording the worker create result"},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.CollectedContext == nil {
		t.Fatal("CollectedContext = nil, want persisted worker action result")
	}
	if result.Run.CollectedContext.ArtifactPath == "" {
		t.Fatal("CollectedContext.ArtifactPath = empty, want run-scoped context artifact")
	}
	if len(result.Run.CollectedContext.WorkerResults) != 1 {
		t.Fatalf("WorkerResults len = %d, want 1", len(result.Run.CollectedContext.WorkerResults))
	}
	workerResult := result.Run.CollectedContext.WorkerResults[0]
	if workerResult.Worker == nil {
		t.Fatal("WorkerResults[0].Worker = nil, want worker summary")
	}
	if workerResult.Worker.WorkerStatus != string(state.WorkerStatusIdle) {
		t.Fatalf("WorkerResults[0].Worker.WorkerStatus = %q, want %q", workerResult.Worker.WorkerStatus, state.WorkerStatusIdle)
	}
	if !strings.Contains(workerResult.Message, "awaiting explicit dispatch") {
		t.Fatalf("WorkerResults[0].Message = %q, want explicit dispatch guidance", workerResult.Message)
	}

	secondInput := fakePlanner.inputs[1]
	if secondInput.CollectedContext == nil {
		t.Fatal("second planner input missing collected_context summary")
	}
	if len(secondInput.CollectedContext.WorkerResults) != 1 {
		t.Fatalf("second planner worker_results len = %d, want 1", len(secondInput.CollectedContext.WorkerResults))
	}
	if secondInput.CollectedContext.WorkerResults[0].Worker == nil {
		t.Fatal("second planner worker result missing worker summary")
	}
	if secondInput.CollectedContext.WorkerResults[0].Worker.WorkerStatus != string(state.WorkerStatusIdle) {
		t.Fatalf("second planner worker status = %q, want %q", secondInput.CollectedContext.WorkerResults[0].Worker.WorkerStatus, state.WorkerStatusIdle)
	}
	if !strings.Contains(secondInput.CollectedContext.WorkerResults[0].Message, "awaiting explicit dispatch") {
		t.Fatalf("second planner worker result message = %q, want explicit dispatch guidance", secondInput.CollectedContext.WorkerResults[0].Message)
	}

	workers, err := store.ListWorkers(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkers() error = %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("workers len = %d, want 1", len(workers))
	}
	if workers[0].WorkerStatus != state.WorkerStatusIdle {
		t.Fatalf("worker status = %q, want %q", workers[0].WorkerStatus, state.WorkerStatusIdle)
	}
	if strings.TrimSpace(workers[0].ExecutorThreadID) != "" || strings.TrimSpace(workers[0].ExecutorTurnID) != "" {
		t.Fatalf("worker executor ids = (%q,%q), want empty for created-but-not-dispatched worker", workers[0].ExecutorThreadID, workers[0].ExecutorTurnID)
	}

	events, err := journalWriter.ReadRecent(run.ID, 12)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	completedEvent := latestEventType(events, "context.collection.completed")
	if completedEvent.ArtifactPath == "" {
		t.Fatal("context.collection.completed missing run-scoped artifact path")
	}
}

func TestCycleRunOnceCollectContextSecondPlannerCompleteMarksRunCompleted(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := os.Mkdir(filepath.Join(run.RepoPath, "docs"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.RepoPath, "docs", "note.txt"), []byte("bounded context preview"), 0o600); err != nil {
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
						Focus: "Inspect repo state.",
						Paths: []string{"docs/note.txt"},
					},
				},
			},
			{
				ResponseID: "resp_complete",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeComplete,
					Complete: &planner.CompleteOutcome{
						Summary: "Context collection proved the task is complete.",
					},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", result.Run.Status)
	}
	if result.Run.LatestCheckpoint.Label != "planner_declared_complete" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_declared_complete", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.PreviousResponseID != "resp_complete" {
		t.Fatalf("PreviousResponseID = %q, want resp_complete", result.Run.PreviousResponseID)
	}
	if result.SecondPlannerTurn == nil || result.SecondPlannerTurn.Output.Outcome != planner.OutcomeComplete {
		t.Fatalf("SecondPlannerTurn = %#v, want complete outcome", result.SecondPlannerTurn)
	}
	if result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = true, want false")
	}

	events, err := journalWriter.ReadRecent(run.ID, 16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "run.completed") {
		t.Fatal("journal missing run.completed event")
	}
}

func TestCycleRunOnceExecuteSecondPlannerCompleteMarksRunCompleted(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_execute",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "implement one bounded slice",
						AcceptanceCriteria: []string{"persist executor result"},
					},
				},
			},
			{
				ResponseID: "resp_complete",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeComplete,
					Complete: &planner.CompleteOutcome{
						Summary: "Executor result satisfies the run goal.",
					},
				},
			},
		},
	}

	completedAt := time.Date(2026, 4, 19, 18, 2, 0, 0, time.UTC)
	fakeExecutor := &stubExecutor{
		result: executor.TurnResult{
			Transport:    executor.TransportAppServer,
			RunID:        run.ID,
			ThreadID:     "thr_123",
			ThreadPath:   filepath.FromSlash("C:/Users/test/.codex/threads/thr_123.jsonl"),
			TurnID:       "turn_123",
			TurnStatus:   executor.TurnStatusCompleted,
			CompletedAt:  completedAt,
			FinalMessage: "Implemented the requested bounded slice.",
		},
	}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.Status != state.StatusCompleted {
		t.Fatalf("Run.Status = %q, want completed", result.Run.Status)
	}
	if result.Run.LatestCheckpoint.Label != "planner_declared_complete" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_declared_complete", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.PreviousResponseID != "resp_complete" {
		t.Fatalf("PreviousResponseID = %q, want resp_complete", result.Run.PreviousResponseID)
	}
	if !result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = false, want true")
	}
	if result.PostExecutorPlannerTurn == nil || result.PostExecutorPlannerTurn.Output.Outcome != planner.OutcomeComplete {
		t.Fatalf("PostExecutorPlannerTurn = %#v, want complete outcome", result.PostExecutorPlannerTurn)
	}

	events, err := journalWriter.ReadRecent(run.ID, 16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "run.completed") {
		t.Fatal("journal missing run.completed event")
	}
}

func TestCycleRunOncePlannerValidationFailurePersistsRuntimeIssue(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		err: planner.NewValidationError(
			errors.New("planner output failed planner.v1 validation: complete.summary is required"),
			`{"id":"resp_invalid","output":[{"type":"message","content":[{"type":"output_text","text":"{\"contract_version\":\"planner.v1\",\"outcome\":\"complete\"}"}]}]}`,
			`{"contract_version":"planner.v1","outcome":"complete"}`,
			"resp_invalid",
		),
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err == nil {
		t.Fatal("RunOnce() unexpectedly succeeded")
	}

	if result.Run.ID != run.ID {
		t.Fatalf("Run.ID = %q, want %q", result.Run.ID, run.ID)
	}
	if result.Run.RuntimeIssueReason != StopReasonPlannerValidationFailed {
		t.Fatalf("RuntimeIssueReason = %q, want %q", result.Run.RuntimeIssueReason, StopReasonPlannerValidationFailed)
	}
	if result.Run.LatestStopReason != StopReasonPlannerValidationFailed {
		t.Fatalf("LatestStopReason = %q, want %q", result.Run.LatestStopReason, StopReasonPlannerValidationFailed)
	}
	if !strings.Contains(result.Run.RuntimeIssueMessage, "planner output failed planner.v1 validation") {
		t.Fatalf("RuntimeIssueMessage = %q", result.Run.RuntimeIssueMessage)
	}
	if result.Run.LatestCheckpoint.Label != "run_initialized" {
		t.Fatalf("LatestCheckpoint.Label = %q, want run_initialized", result.Run.LatestCheckpoint.Label)
	}

	events, err := journalWriter.ReadRecent(run.ID, 10)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	failureEvent := latestEventType(events, "planner.turn.failed")
	if failureEvent.StopReason != StopReasonPlannerValidationFailed {
		t.Fatalf("failureEvent.StopReason = %q, want %q", failureEvent.StopReason, StopReasonPlannerValidationFailed)
	}
	if failureEvent.ArtifactPath == "" {
		t.Fatal("failureEvent.ArtifactPath = empty, want planner validation artifact")
	}
	if !strings.HasPrefix(failureEvent.ArtifactPath, filepath.ToSlash(filepath.Join(plannerArtifactDir, run.ID))+"/") {
		t.Fatalf("failureEvent.ArtifactPath = %q, want planner artifact path under %s", failureEvent.ArtifactPath, plannerArtifactDir)
	}
	if !strings.Contains(failureEvent.ArtifactPreview, `"outcome":"complete"`) {
		t.Fatalf("failureEvent.ArtifactPreview = %q", failureEvent.ArtifactPreview)
	}
	if failureEvent.Checkpoint == nil || failureEvent.Checkpoint.Label != "run_initialized" {
		t.Fatalf("failureEvent.Checkpoint = %#v, want bootstrap checkpoint", failureEvent.Checkpoint)
	}
	artifactPath := filepath.Join(run.RepoPath, filepath.FromSlash(failureEvent.ArtifactPath))
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("planner validation artifact missing at %s: %v", artifactPath, err)
	}
}

func TestCycleRunOnceExecutorTransportFailurePersistsRuntimeIssueWithoutCheckpoint(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_execute",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "attempt one executor turn",
						AcceptanceCriteria: []string{"persist executor failure honestly"},
					},
				},
			},
		},
	}
	fakeExecutor := &stubExecutor{
		result: executor.TurnResult{
			Transport:  executor.TransportAppServer,
			RunID:      run.ID,
			ThreadID:   "thr_123",
			ThreadPath: "thread.jsonl",
			TurnID:     "turn_123",
			TurnStatus: executor.TurnStatusFailed,
			Error: &executor.Failure{
				Stage:   "turn_timeout",
				Message: "executor turn exceeded app-server wait deadline",
				Detail:  "context deadline exceeded",
			},
		},
		err: errors.New("context deadline exceeded"),
	}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err == nil {
		t.Fatal("RunOnce() unexpectedly succeeded")
	}

	if !result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = false, want true")
	}
	if result.Run.RuntimeIssueReason != StopReasonExecutorFailed {
		t.Fatalf("RuntimeIssueReason = %q, want %q", result.Run.RuntimeIssueReason, StopReasonExecutorFailed)
	}
	if result.Run.LatestStopReason != StopReasonExecutorFailed {
		t.Fatalf("LatestStopReason = %q, want %q", result.Run.LatestStopReason, StopReasonExecutorFailed)
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_completed" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_completed", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.ExecutorTurnStatus != string(executor.TurnStatusFailed) {
		t.Fatalf("ExecutorTurnStatus = %q, want failed", result.Run.ExecutorTurnStatus)
	}

	events, err := journalWriter.ReadRecent(run.ID, 12)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	failureEvent := latestEventType(events, "executor.turn.failed")
	if failureEvent.StopReason != StopReasonExecutorFailed {
		t.Fatalf("failureEvent.StopReason = %q, want %q", failureEvent.StopReason, StopReasonExecutorFailed)
	}
	if failureEvent.Checkpoint != nil {
		t.Fatalf("failureEvent.Checkpoint = %#v, want nil for incomplete executor failure", failureEvent.Checkpoint)
	}
}

func TestCycleRunOnceCollectContextMissingPathRecordsFailureData(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "inspect one missing path",
						Paths: []string{"missing.txt"},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after collecting failure data"},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.CollectedContext == nil || len(result.Run.CollectedContext.Results) != 1 {
		t.Fatalf("CollectedContext = %#v, want one result", result.Run.CollectedContext)
	}
	if result.Run.CollectedContext.Results[0].Detail != "path_not_found" {
		t.Fatalf("CollectedContext.Results[0].Detail = %q, want path_not_found", result.Run.CollectedContext.Results[0].Detail)
	}

	events, err := journalWriter.ReadRecent(run.ID, 12)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	recordedEvent := latestEventType(events, "context.collection.recorded")
	if recordedEvent.ContextDetail != "path_not_found" {
		t.Fatalf("recordedEvent.ContextDetail = %q, want path_not_found", recordedEvent.ContextDetail)
	}
}

func TestCycleRunOnceCollectContextLargeFilePersistsArtifactReference(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := os.Mkdir(filepath.Join(run.RepoPath, "docs"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	largeContent := strings.Repeat("bounded-context-preview-", 120)
	if err := os.WriteFile(filepath.Join(run.RepoPath, "docs", "large.txt"), []byte(largeContent), 0o600); err != nil {
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
						Focus: "inspect one large file",
						Paths: []string{"docs/large.txt"},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after collecting the large file"},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.CollectedContext == nil || len(result.Run.CollectedContext.Results) != 1 {
		t.Fatalf("CollectedContext = %#v, want one collected result", result.Run.CollectedContext)
	}
	if !result.Run.CollectedContext.Results[0].Truncated {
		t.Fatal("CollectedContext.Results[0].Truncated = false, want true for large file")
	}

	events, err := journalWriter.ReadRecent(run.ID, 12)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	recordedEvent := latestEventType(events, "context.collection.recorded")
	if recordedEvent.ArtifactPath == "" {
		t.Fatal("recordedEvent.ArtifactPath = empty, want externalized context artifact")
	}
	if !strings.HasPrefix(recordedEvent.ArtifactPath, filepath.ToSlash(filepath.Join(contextArtifactDir, run.ID))+"/") {
		t.Fatalf("recordedEvent.ArtifactPath = %q, want context artifact path under %s", recordedEvent.ArtifactPath, contextArtifactDir)
	}

	artifactPath := filepath.Join(run.RepoPath, filepath.FromSlash(recordedEvent.ArtifactPath))
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("context artifact missing at %s: %v", artifactPath, err)
	}

	foundArtifactSummary := false
	for _, preview := range fakePlanner.inputs[1].RecentEvents {
		if strings.Contains(preview.Summary, recordedEvent.ArtifactPath) {
			foundArtifactSummary = true
			break
		}
	}
	if !foundArtifactSummary {
		t.Fatalf("second planner input recent_events did not reference context artifact path %q", recordedEvent.ArtifactPath)
	}
}

func TestCycleRunOnceCollectContextIntegrationBuildsArtifactAndFeedsPlanner(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := os.MkdirAll(filepath.Join(run.RepoPath, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.RepoPath, "src", "shared.txt"), []byte("base shared\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(shared) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.RepoPath, "src", "main.txt"), []byte("base main\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(main) error = %v", err)
	}

	workerOnePath := filepath.Join(t.TempDir(), "worker-one")
	workerTwoPath := filepath.Join(t.TempDir(), "worker-two")
	for _, item := range []struct {
		root    string
		files   map[string]string
		name    string
		scope   string
		summary string
	}{
		{
			root: workerOnePath,
			files: map[string]string{
				filepath.Join("src", "shared.txt"): "worker one shared\n",
				filepath.Join("src", "ui.txt"):     "worker one ui\n",
			},
			name:    "worker-one",
			scope:   "ui shell",
			summary: "worker one ui output",
		},
		{
			root: workerTwoPath,
			files: map[string]string{
				filepath.Join("src", "shared.txt"): "worker two shared\n",
				filepath.Join("src", "api.txt"):    "worker two api\n",
			},
			name:    "worker-two",
			scope:   "api shell",
			summary: "worker two api output",
		},
	} {
		for relativePath, contents := range item.files {
			absolutePath := filepath.Join(item.root, relativePath)
			if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
				t.Fatalf("MkdirAll(%q) error = %v", absolutePath, err)
			}
			if err := os.WriteFile(absolutePath, []byte(contents), 0o600); err != nil {
				t.Fatalf("WriteFile(%q) error = %v", absolutePath, err)
			}
		}
	}

	workerOne, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "worker-one",
		WorkerStatus:  state.WorkerStatusCompleted,
		AssignedScope: "ui shell",
		WorktreePath:  workerOnePath,
		CreatedAt:     time.Date(2026, 4, 20, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateWorker(worker one) error = %v", err)
	}
	workerOne.WorkerResultSummary = "worker one ui output"
	workerOne.UpdatedAt = time.Date(2026, 4, 20, 13, 2, 0, 0, time.UTC)
	if err := store.SaveWorker(context.Background(), workerOne); err != nil {
		t.Fatalf("SaveWorker(worker one) error = %v", err)
	}

	workerTwo, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "worker-two",
		WorkerStatus:  state.WorkerStatusCompleted,
		AssignedScope: "api shell",
		WorktreePath:  workerTwoPath,
		CreatedAt:     time.Date(2026, 4, 20, 13, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateWorker(worker two) error = %v", err)
	}
	workerTwo.WorkerResultSummary = "worker two api output"
	workerTwo.UpdatedAt = time.Date(2026, 4, 20, 13, 3, 0, 0, time.UTC)
	if err := store.SaveWorker(context.Background(), workerTwo); err != nil {
		t.Fatalf("SaveWorker(worker two) error = %v", err)
	}

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect_integrate",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "build a read-only integration preview",
						WorkerActions: []planner.WorkerAction{
							{
								Action:    planner.WorkerActionIntegrate,
								WorkerIDs: []string{workerOne.ID, workerTwo.ID},
							},
						},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after the integration preview"},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.CollectedContext == nil || len(result.Run.CollectedContext.WorkerResults) != 1 {
		t.Fatalf("CollectedContext.WorkerResults = %#v, want one integration result", result.Run.CollectedContext)
	}
	integrationResult := result.Run.CollectedContext.WorkerResults[0]
	if integrationResult.Action != string(planner.WorkerActionIntegrate) {
		t.Fatalf("integrationResult.Action = %q, want integrate", integrationResult.Action)
	}
	if !integrationResult.Success {
		t.Fatalf("integrationResult.Success = false, want true: %#v", integrationResult)
	}
	if integrationResult.Integration == nil {
		t.Fatalf("integrationResult.Integration = nil, want structured integration summary: %#v", integrationResult)
	}
	if integrationResult.ArtifactPath == "" {
		t.Fatal("integrationResult.ArtifactPath = empty, want integration artifact path")
	}
	if !strings.HasPrefix(integrationResult.ArtifactPath, filepath.ToSlash(filepath.Join(integrationArtifactDir, run.ID))+"/") {
		t.Fatalf("integration artifact path = %q, want under %s", integrationResult.ArtifactPath, integrationArtifactDir)
	}
	if _, err := os.Stat(filepath.Join(run.RepoPath, filepath.FromSlash(integrationResult.ArtifactPath))); err != nil {
		t.Fatalf("integration artifact missing: %v", err)
	}
	if len(integrationResult.Integration.Workers) != 2 {
		t.Fatalf("len(integrationResult.Integration.Workers) = %d, want 2", len(integrationResult.Integration.Workers))
	}
	if !containsIntegrationConflict(integrationResult.Integration.ConflictCandidates, "src/shared.txt", "same_file_touched") {
		t.Fatalf("integration conflicts missing same-file candidate: %#v", integrationResult.Integration.ConflictCandidates)
	}

	if fakePlanner.inputs[1].CollectedContext == nil || len(fakePlanner.inputs[1].CollectedContext.WorkerResults) != 1 {
		t.Fatalf("second planner input missing worker integration result: %#v", fakePlanner.inputs[1].CollectedContext)
	}
	if fakePlanner.inputs[1].CollectedContext.WorkerResults[0].Integration == nil {
		t.Fatalf("second planner input missing structured integration summary: %#v", fakePlanner.inputs[1].CollectedContext.WorkerResults[0])
	}
	if len(fakePlanner.inputs[1].CollectedContext.WorkerResults[0].Integration.Workers) != 2 {
		t.Fatalf("second planner integration workers = %#v", fakePlanner.inputs[1].CollectedContext.WorkerResults[0].Integration.Workers)
	}
	if _, err := os.Stat(filepath.Join(run.RepoPath, "src", "ui.txt")); !os.IsNotExist(err) {
		t.Fatalf("main repo unexpectedly changed by integration preview: %v", err)
	}
	if _, err := os.Stat(filepath.Join(run.RepoPath, "src", "api.txt")); !os.IsNotExist(err) {
		t.Fatalf("main repo unexpectedly changed by integration preview: %v", err)
	}

	events, err := journalWriter.ReadRecent(run.ID, 20)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "integration.started") {
		t.Fatal("journal missing integration.started event")
	}
	completedEvent := latestEventType(events, "integration.completed")
	if completedEvent.Type != "integration.completed" {
		t.Fatal("journal missing integration.completed event")
	}
	if completedEvent.ArtifactPath == "" {
		t.Fatal("integration.completed missing artifact path")
	}
}

func TestCycleRunOnceCollectContextApplyWritesNonConflictingFilesAndFeedsPlanner(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	writeIntegrationFile(t, filepath.Join(run.RepoPath, "src", "shared.txt"), "base shared\n")

	layout := state.ResolveLayout(run.RepoPath)
	workerOnePath := filepath.Join(layout.WorkersDir, "worker-one")
	workerTwoPath := filepath.Join(layout.WorkersDir, "worker-two")
	writeIntegrationFile(t, filepath.Join(workerOnePath, "src", "shared.txt"), "worker one shared\n")
	writeIntegrationFile(t, filepath.Join(workerOnePath, "src", "ui.txt"), "worker one ui\n")
	writeIntegrationFile(t, filepath.Join(workerTwoPath, "src", "shared.txt"), "worker two shared\n")
	writeIntegrationFile(t, filepath.Join(workerTwoPath, "src", "api.txt"), "worker two api\n")

	workerOne, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "worker-one",
		WorkerStatus:  state.WorkerStatusCompleted,
		AssignedScope: "ui shell",
		WorktreePath:  workerOnePath,
	})
	if err != nil {
		t.Fatalf("CreateWorker(worker one) error = %v", err)
	}
	workerTwo, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "worker-two",
		WorkerStatus:  state.WorkerStatusCompleted,
		AssignedScope: "api shell",
		WorktreePath:  workerTwoPath,
	})
	if err != nil {
		t.Fatalf("CreateWorker(worker two) error = %v", err)
	}

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect_apply",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "Apply only the non-conflicting worker outputs",
						WorkerActions: []planner.WorkerAction{
							{
								Action:    planner.WorkerActionApply,
								WorkerIDs: []string{workerOne.ID, workerTwo.ID},
								ApplyMode: string(planner.WorkerApplyModeNonConflicting),
							},
						},
					},
				},
			},
			{
				ResponseID: "resp_pause_after_apply",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after the integration apply preview result is recorded"},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.CollectedContext == nil || len(result.Run.CollectedContext.WorkerResults) != 1 {
		t.Fatalf("CollectedContext.WorkerResults = %#v, want one apply result", result.Run.CollectedContext)
	}
	applyResult := result.Run.CollectedContext.WorkerResults[0]
	if applyResult.Action != string(planner.WorkerActionApply) {
		t.Fatalf("applyResult.Action = %q, want apply", applyResult.Action)
	}
	if !applyResult.Success {
		t.Fatalf("applyResult.Success = false, want true: %#v", applyResult)
	}
	if applyResult.Apply == nil {
		t.Fatalf("applyResult.Apply = nil, want structured apply summary: %#v", applyResult)
	}
	if applyResult.Apply.Status != "completed" {
		t.Fatalf("applyResult.Apply.Status = %q, want completed", applyResult.Apply.Status)
	}
	if applyResult.Apply.ApplyMode != string(planner.WorkerApplyModeNonConflicting) {
		t.Fatalf("applyResult.Apply.ApplyMode = %q, want %q", applyResult.Apply.ApplyMode, planner.WorkerApplyModeNonConflicting)
	}
	if len(applyResult.Apply.FilesApplied) != 2 {
		t.Fatalf("len(applyResult.Apply.FilesApplied) = %d, want 2", len(applyResult.Apply.FilesApplied))
	}
	if !containsStateAppliedFile(applyResult.Apply.FilesApplied, "src/ui.txt", "added") {
		t.Fatalf("apply files missing src/ui.txt add: %#v", applyResult.Apply.FilesApplied)
	}
	if !containsStateAppliedFile(applyResult.Apply.FilesApplied, "src/api.txt", "added") {
		t.Fatalf("apply files missing src/api.txt add: %#v", applyResult.Apply.FilesApplied)
	}
	if !containsStateSkippedApplyFile(applyResult.Apply.FilesSkipped, "src/shared.txt", "conflict_candidate") {
		t.Fatalf("apply files missing shared conflict skip: %#v", applyResult.Apply.FilesSkipped)
	}
	if applyResult.ArtifactPath == "" {
		t.Fatal("applyResult.ArtifactPath = empty, want integration apply artifact path")
	}
	if _, err := os.Stat(filepath.Join(run.RepoPath, filepath.FromSlash(applyResult.ArtifactPath))); err != nil {
		t.Fatalf("integration apply artifact missing: %v", err)
	}
	if applyResult.Apply.SourceArtifactPath == "" {
		t.Fatal("applyResult.Apply.SourceArtifactPath = empty, want source integration artifact path")
	}
	if _, err := os.Stat(filepath.Join(run.RepoPath, filepath.FromSlash(applyResult.Apply.SourceArtifactPath))); err != nil {
		t.Fatalf("source integration artifact missing: %v", err)
	}

	if fakePlanner.inputs[1].CollectedContext == nil || len(fakePlanner.inputs[1].CollectedContext.WorkerResults) != 1 {
		t.Fatalf("second planner input missing worker apply result: %#v", fakePlanner.inputs[1].CollectedContext)
	}
	if fakePlanner.inputs[1].CollectedContext.WorkerResults[0].Apply == nil {
		t.Fatalf("second planner input missing structured apply result: %#v", fakePlanner.inputs[1].CollectedContext.WorkerResults[0])
	}
	if len(fakePlanner.inputs[1].CollectedContext.WorkerResults[0].Apply.FilesApplied) != 2 {
		t.Fatalf("second planner input apply files = %#v", fakePlanner.inputs[1].CollectedContext.WorkerResults[0].Apply.FilesApplied)
	}

	assertFileContents(t, filepath.Join(run.RepoPath, "src", "ui.txt"), "worker one ui\n")
	assertFileContents(t, filepath.Join(run.RepoPath, "src", "api.txt"), "worker two api\n")
	assertFileContents(t, filepath.Join(run.RepoPath, "src", "shared.txt"), "base shared\n")

	events, err := journalWriter.ReadRecent(run.ID, 24)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "integration.started") {
		t.Fatal("journal missing integration.started event")
	}
	if !containsEventType(events, "integration.completed") {
		t.Fatal("journal missing integration.completed event")
	}
	if !containsEventType(events, "integration.apply.started") {
		t.Fatal("journal missing integration.apply.started event")
	}
	if !containsEventType(events, "integration.apply.completed") {
		t.Fatal("journal missing integration.apply.completed event")
	}
}

func TestCycleRunOnceCollectContextWorkerPlanDispatchesConcurrentlyAndApplies(t *testing.T) {
	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})
	installFakeGitForCycleTests(t)

	writeIntegrationFile(t, filepath.Join(run.RepoPath, "src", "shared.txt"), "base shared\n")
	layout := state.ResolveLayout(run.RepoPath)

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect_worker_plan",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "Partition isolated worker work for one bounded turn",
						WorkerPlan: &planner.WorkerPlan{
							Workers: []planner.PlannedWorker{
								{
									Name:           "ui-worker",
									Scope:          "ui shell",
									TaskSummary:    "Implement the ui shell slice in an isolated worker.",
									ExecutorPrompt: "Implement the ui shell slice in this worker workspace only.",
								},
								{
									Name:           "api-worker",
									Scope:          "api slice",
									TaskSummary:    "Implement the api slice in an isolated worker.",
									ExecutorPrompt: "Implement the api slice in this worker workspace only.",
								},
							},
							IntegrationRequested: true,
							ApplyMode:            string(planner.WorkerApplyModeNonConflicting),
						},
					},
				},
			},
			{
				ResponseID: "resp_pause_after_worker_plan",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after the worker plan results are recorded"},
				},
			},
		},
	}

	var (
		dispatchedPaths []string
		dispatchMu      sync.Mutex
	)
	started := make(chan string, 2)
	release := make(chan struct{})

	fakeExecutor := newConcurrentScriptedExecutor(map[string]scriptedExecutorResult{
		"Implement the ui shell slice in this worker workspace only.": {
			beforeReturn: func(req executor.TurnRequest) {
				dispatchMu.Lock()
				dispatchedPaths = append(dispatchedPaths, req.RepoPath)
				dispatchMu.Unlock()
				writeIntegrationFile(t, filepath.Join(req.RepoPath, "src", "shared.txt"), "worker one shared\n")
				writeIntegrationFile(t, filepath.Join(req.RepoPath, "src", "ui.txt"), "worker one ui\n")
				started <- strings.TrimSpace(req.Prompt)
				<-release
			},
			result: executor.TurnResult{
				Transport:    executor.TransportAppServer,
				RunID:        run.ID,
				ThreadID:     "worker_thread_ui",
				ThreadPath:   filepath.FromSlash("C:/Users/test/.codex/threads/worker_thread_ui.jsonl"),
				TurnID:       "worker_turn_ui",
				TurnStatus:   executor.TurnStatusCompleted,
				StartedAt:    time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC),
				CompletedAt:  time.Date(2026, 4, 21, 12, 1, 0, 0, time.UTC),
				FinalMessage: "worker executor turn completed for ui-worker",
			},
		},
		"Implement the api slice in this worker workspace only.": {
			beforeReturn: func(req executor.TurnRequest) {
				dispatchMu.Lock()
				dispatchedPaths = append(dispatchedPaths, req.RepoPath)
				dispatchMu.Unlock()
				writeIntegrationFile(t, filepath.Join(req.RepoPath, "src", "shared.txt"), "worker two shared\n")
				writeIntegrationFile(t, filepath.Join(req.RepoPath, "src", "api.txt"), "worker two api\n")
				started <- strings.TrimSpace(req.Prompt)
				<-release
			},
			result: executor.TurnResult{
				Transport:    executor.TransportAppServer,
				RunID:        run.ID,
				ThreadID:     "worker_thread_api",
				ThreadPath:   filepath.FromSlash("C:/Users/test/.codex/threads/worker_thread_api.jsonl"),
				TurnID:       "worker_turn_api",
				TurnStatus:   executor.TurnStatusCompleted,
				StartedAt:    time.Date(2026, 4, 21, 12, 2, 0, 0, time.UTC),
				CompletedAt:  time.Date(2026, 4, 21, 12, 3, 0, 0, time.UTC),
				FinalMessage: "worker executor turn completed for api-worker",
			},
		},
	})

	cycle := Cycle{
		Store:                      store,
		Journal:                    journalWriter,
		Planner:                    fakePlanner,
		Executor:                   fakeExecutor,
		WorkerPlanConcurrencyLimit: 2,
	}

	resultCh := make(chan struct {
		result Result
		err    error
	}, 1)
	go func() {
		result, err := cycle.RunOnce(context.Background(), run)
		resultCh <- struct {
			result Result
			err    error
		}{result: result, err: err}
	}()

	waitForPrompts(t, started, map[string]bool{
		"Implement the ui shell slice in this worker workspace only.": true,
		"Implement the api slice in this worker workspace only.":      true,
	})
	close(release)

	outcome := <-resultCh
	if outcome.err != nil {
		t.Fatalf("RunOnce() error = %v", outcome.err)
	}
	result := outcome.result

	if fakePlanner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", fakePlanner.calls)
	}
	if fakeExecutor.Calls() != 2 {
		t.Fatalf("executor calls = %d, want 2", fakeExecutor.Calls())
	}
	if len(dispatchedPaths) != 2 {
		t.Fatalf("len(dispatchedPaths) = %d, want 2", len(dispatchedPaths))
	}
	if fakeExecutor.MaxActive() != 2 {
		t.Fatalf("MaxActive() = %d, want 2 to prove true concurrency", fakeExecutor.MaxActive())
	}
	if dispatchedPaths[0] == run.RepoPath || dispatchedPaths[1] == run.RepoPath {
		t.Fatalf("worker dispatch unexpectedly targeted main repo path: %#v", dispatchedPaths)
	}
	if dispatchedPaths[0] == dispatchedPaths[1] {
		t.Fatalf("worker dispatch paths should be distinct, got %#v", dispatchedPaths)
	}
	for _, path := range dispatchedPaths {
		if !strings.HasPrefix(strings.ToLower(path), strings.ToLower(layout.WorkersDir+string(filepath.Separator))) {
			t.Fatalf("worker dispatch path %q not under isolated workers dir %q", path, layout.WorkersDir)
		}
	}

	if result.Run.CollectedContext == nil {
		t.Fatal("CollectedContext = nil, want worker-plan results")
	}
	if len(result.Run.CollectedContext.WorkerResults) != 8 {
		t.Fatalf("len(CollectedContext.WorkerResults) = %d, want 8", len(result.Run.CollectedContext.WorkerResults))
	}
	planResult := result.Run.CollectedContext.WorkerPlan
	if planResult == nil {
		t.Fatal("CollectedContext.WorkerPlan = nil, want structured worker-plan result")
	}
	if planResult.Status != "completed" {
		t.Fatalf("WorkerPlan.Status = %q, want completed", planResult.Status)
	}
	if planResult.ConcurrencyLimit != 2 {
		t.Fatalf("WorkerPlan.ConcurrencyLimit = %d, want 2", planResult.ConcurrencyLimit)
	}
	if len(planResult.WorkerIDs) != 2 {
		t.Fatalf("len(WorkerPlan.WorkerIDs) = %d, want 2", len(planResult.WorkerIDs))
	}
	if len(planResult.Workers) != 2 {
		t.Fatalf("len(WorkerPlan.Workers) = %d, want 2", len(planResult.Workers))
	}
	for _, worker := range planResult.Workers {
		if worker.WorkerStatus != string(state.WorkerStatusCompleted) {
			t.Fatalf("worker status = %q, want completed: %#v", worker.WorkerStatus, worker)
		}
		if worker.WorktreePath == run.RepoPath {
			t.Fatalf("worker worktree path unexpectedly reused main repo path: %#v", worker)
		}
	}
	if !planResult.IntegrationRequested {
		t.Fatal("WorkerPlan.IntegrationRequested = false, want true")
	}
	if planResult.IntegrationArtifactPath == "" {
		t.Fatal("WorkerPlan.IntegrationArtifactPath = empty, want integration artifact path")
	}
	if planResult.ApplyMode != string(planner.WorkerApplyModeNonConflicting) {
		t.Fatalf("WorkerPlan.ApplyMode = %q, want %q", planResult.ApplyMode, planner.WorkerApplyModeNonConflicting)
	}
	if planResult.ApplyArtifactPath == "" {
		t.Fatal("WorkerPlan.ApplyArtifactPath = empty, want apply artifact path")
	}
	if planResult.Apply == nil {
		t.Fatal("WorkerPlan.Apply = nil, want structured apply result")
	}
	if planResult.Apply.Status != "completed" {
		t.Fatalf("WorkerPlan.Apply.Status = %q, want completed", planResult.Apply.Status)
	}
	if !containsStateAppliedFile(planResult.Apply.FilesApplied, "src/ui.txt", "added") {
		t.Fatalf("plan apply files missing src/ui.txt add: %#v", planResult.Apply.FilesApplied)
	}
	if !containsStateAppliedFile(planResult.Apply.FilesApplied, "src/api.txt", "added") {
		t.Fatalf("plan apply files missing src/api.txt add: %#v", planResult.Apply.FilesApplied)
	}
	if !containsStateSkippedApplyFile(planResult.Apply.FilesSkipped, "src/shared.txt", "conflict_candidate") {
		t.Fatalf("plan apply files missing shared conflict skip: %#v", planResult.Apply.FilesSkipped)
	}

	if fakePlanner.inputs[1].CollectedContext == nil {
		t.Fatal("second planner input missing collected_context")
	}
	if fakePlanner.inputs[1].CollectedContext.WorkerPlan == nil {
		t.Fatalf("second planner input missing worker_plan result: %#v", fakePlanner.inputs[1].CollectedContext)
	}
	if len(fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers) != 2 {
		t.Fatalf("second planner worker_plan workers = %#v", fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers)
	}
	if fakePlanner.inputs[1].CollectedContext.WorkerPlan.Apply == nil {
		t.Fatalf("second planner input missing worker_plan apply result: %#v", fakePlanner.inputs[1].CollectedContext.WorkerPlan)
	}

	assertFileContents(t, filepath.Join(run.RepoPath, "src", "ui.txt"), "worker one ui\n")
	assertFileContents(t, filepath.Join(run.RepoPath, "src", "api.txt"), "worker two api\n")
	assertFileContents(t, filepath.Join(run.RepoPath, "src", "shared.txt"), "base shared\n")

	events, err := journalWriter.ReadRecent(run.ID, 32)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	for _, want := range []string{
		"worker.plan.received",
		"worker.plan.started",
		"worker.plan.dispatch.started",
		"worker.plan.dispatch.completed",
		"worker.plan.completed",
		"worker.plan.finished",
		"worker.executor.started",
		"worker.executor.dispatched",
		"integration.completed",
		"integration.apply.completed",
	} {
		if !containsEventType(events, want) {
			t.Fatalf("journal missing %q event", want)
		}
	}
	if countEventType(events, "worker.executor.dispatched") != 2 {
		t.Fatalf("worker.executor.dispatched count = %d, want 2", countEventType(events, "worker.executor.dispatched"))
	}
	if countEventType(events, "worker.executor.started") != 2 {
		t.Fatalf("worker.executor.started count = %d, want 2", countEventType(events, "worker.executor.started"))
	}
}

func TestCycleRunOnceCollectContextWorkerPlanRespectsConcurrencyLimit(t *testing.T) {
	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})
	installFakeGitForCycleTests(t)

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect_worker_plan",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "Partition isolated worker work with a bounded limit",
						WorkerPlan: &planner.WorkerPlan{
							Workers: []planner.PlannedWorker{
								{Name: "worker-a", Scope: "scope-a", TaskSummary: "task a", ExecutorPrompt: "prompt a"},
								{Name: "worker-b", Scope: "scope-b", TaskSummary: "task b", ExecutorPrompt: "prompt b"},
								{Name: "worker-c", Scope: "scope-c", TaskSummary: "task c", ExecutorPrompt: "prompt c"},
							},
							IntegrationRequested: false,
							ApplyMode:            string(planner.WorkerApplyModeUnavailable),
						},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after bounded worker-plan execution"},
				},
			},
		},
	}

	started := make(chan string, 3)
	release := make(chan struct{})
	fakeExecutor := newConcurrentScriptedExecutor(map[string]scriptedExecutorResult{
		"prompt a": {
			beforeReturn: func(req executor.TurnRequest) {
				started <- req.Prompt
				<-release
			},
			result: completedWorkerTurnResult(run.ID, "thread_a", "turn_a", "worker a completed"),
		},
		"prompt b": {
			beforeReturn: func(req executor.TurnRequest) {
				started <- req.Prompt
				<-release
			},
			result: completedWorkerTurnResult(run.ID, "thread_b", "turn_b", "worker b completed"),
		},
		"prompt c": {
			beforeReturn: func(req executor.TurnRequest) {
				started <- req.Prompt
				<-release
			},
			result: completedWorkerTurnResult(run.ID, "thread_c", "turn_c", "worker c completed"),
		},
	})

	cycle := Cycle{
		Store:                      store,
		Journal:                    journalWriter,
		Planner:                    fakePlanner,
		Executor:                   fakeExecutor,
		WorkerPlanConcurrencyLimit: 2,
	}

	resultCh := make(chan error, 1)
	go func() {
		_, err := cycle.RunOnce(context.Background(), run)
		resultCh <- err
	}()

	waitForStartedCount(t, started, 2)
	close(release)

	if err := <-resultCh; err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if fakeExecutor.Calls() != 3 {
		t.Fatalf("executor calls = %d, want 3", fakeExecutor.Calls())
	}
	if fakeExecutor.MaxActive() != 2 {
		t.Fatalf("MaxActive() = %d, want 2", fakeExecutor.MaxActive())
	}
}

func TestCycleRunOnceCollectContextWorkerPlanFailureDoesNotDiscardSuccessfulResults(t *testing.T) {
	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})
	installFakeGitForCycleTests(t)

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect_worker_plan",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "Run two workers and return all results",
						WorkerPlan: &planner.WorkerPlan{
							Workers: []planner.PlannedWorker{
								{Name: "success-worker", Scope: "ui", TaskSummary: "task success", ExecutorPrompt: "prompt success"},
								{Name: "failed-worker", Scope: "api", TaskSummary: "task failed", ExecutorPrompt: "prompt failed"},
							},
							IntegrationRequested: false,
							ApplyMode:            string(planner.WorkerApplyModeUnavailable),
						},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after reviewing worker failures"},
				},
			},
		},
	}

	fakeExecutor := newConcurrentScriptedExecutor(map[string]scriptedExecutorResult{
		"prompt success": {
			result: completedWorkerTurnResult(run.ID, "thread_success", "turn_success", "success worker completed"),
		},
		"prompt failed": {
			result: executor.TurnResult{
				Transport:    executor.TransportAppServer,
				RunID:        run.ID,
				ThreadID:     "thread_failed",
				TurnID:       "turn_failed",
				TurnStatus:   executor.TurnStatusFailed,
				StartedAt:    time.Date(2026, 4, 21, 13, 0, 0, 0, time.UTC),
				CompletedAt:  time.Date(2026, 4, 21, 13, 0, 30, 0, time.UTC),
				FinalMessage: "failed worker error",
				Error: &executor.Failure{
					Stage:   "complete",
					Message: "failed worker error",
				},
			},
		},
	})

	cycle := Cycle{
		Store:                      store,
		Journal:                    journalWriter,
		Planner:                    fakePlanner,
		Executor:                   fakeExecutor,
		WorkerPlanConcurrencyLimit: 2,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.CollectedContext == nil || result.Run.CollectedContext.WorkerPlan == nil {
		t.Fatalf("CollectedContext.WorkerPlan = %#v, want structured plan result", result.Run.CollectedContext)
	}
	planResult := result.Run.CollectedContext.WorkerPlan
	if planResult.Status != "failed" {
		t.Fatalf("WorkerPlan.Status = %q, want failed", planResult.Status)
	}
	if len(planResult.Workers) != 2 {
		t.Fatalf("len(WorkerPlan.Workers) = %d, want 2", len(planResult.Workers))
	}
	if !containsWorkerStatus(planResult.Workers, string(state.WorkerStatusCompleted)) {
		t.Fatalf("worker plan missing completed worker: %#v", planResult.Workers)
	}
	if !containsWorkerStatus(planResult.Workers, string(state.WorkerStatusFailed)) {
		t.Fatalf("worker plan missing failed worker: %#v", planResult.Workers)
	}
	if fakePlanner.inputs[1].CollectedContext == nil || fakePlanner.inputs[1].CollectedContext.WorkerPlan == nil {
		t.Fatalf("second planner input missing worker plan result: %#v", fakePlanner.inputs[1].CollectedContext)
	}
	if !containsPlannerWorkerStatus(fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers, string(state.WorkerStatusCompleted)) {
		t.Fatalf("second planner input missing completed worker result: %#v", fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers)
	}
	if !containsPlannerWorkerStatus(fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers, string(state.WorkerStatusFailed)) {
		t.Fatalf("second planner input missing failed worker result: %#v", fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers)
	}
}

func TestCycleRunOnceCollectContextWorkerPlanApprovalRequiredPersistsState(t *testing.T) {
	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})
	installFakeGitForCycleTests(t)

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect_worker_plan",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus: "Run two workers and persist approval state",
						WorkerPlan: &planner.WorkerPlan{
							Workers: []planner.PlannedWorker{
								{Name: "completed-worker", Scope: "ui", TaskSummary: "task complete", ExecutorPrompt: "prompt complete"},
								{Name: "approval-worker", Scope: "api", TaskSummary: "task approval", ExecutorPrompt: "prompt approval"},
							},
							IntegrationRequested: true,
							ApplyMode:            string(planner.WorkerApplyModeUnavailable),
						},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after worker approval state is recorded"},
				},
			},
		},
	}

	fakeExecutor := newConcurrentScriptedExecutor(map[string]scriptedExecutorResult{
		"prompt complete": {
			result: completedWorkerTurnResult(run.ID, "thread_complete", "turn_complete", "completed worker finished"),
		},
		"prompt approval": {
			result: executor.TurnResult{
				Transport:     executor.TransportAppServer,
				RunID:         run.ID,
				ThreadID:      "thread_approval",
				TurnID:        "turn_approval",
				TurnStatus:    executor.TurnStatusApprovalRequired,
				ApprovalState: executor.ApprovalStateRequired,
				Approval: &executor.ApprovalRequest{
					State:  executor.ApprovalStateRequired,
					Kind:   executor.ApprovalKindCommandExecution,
					Reason: "approval needed for worker command",
				},
				StartedAt:    time.Date(2026, 4, 21, 14, 0, 0, 0, time.UTC),
				FinalMessage: "approval required for worker",
			},
		},
	})

	cycle := Cycle{
		Store:                      store,
		Journal:                    journalWriter,
		Planner:                    fakePlanner,
		Executor:                   fakeExecutor,
		WorkerPlanConcurrencyLimit: 2,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.Run.LatestStopReason != StopReasonExecutorApprovalReq {
		t.Fatalf("LatestStopReason = %q, want %q", result.Run.LatestStopReason, StopReasonExecutorApprovalReq)
	}
	if result.SecondPlannerTurn == nil || result.SecondPlannerTurn.Output.Outcome != planner.OutcomePause {
		t.Fatalf("SecondPlannerTurn = %#v, want pause result after worker approval data", result.SecondPlannerTurn)
	}
	if result.Run.CollectedContext == nil || result.Run.CollectedContext.WorkerPlan == nil {
		t.Fatalf("CollectedContext.WorkerPlan = %#v, want structured plan result", result.Run.CollectedContext)
	}
	planResult := result.Run.CollectedContext.WorkerPlan
	if planResult.Status != "approval_required" {
		t.Fatalf("WorkerPlan.Status = %q, want approval_required", planResult.Status)
	}
	if !containsWorkerStatus(planResult.Workers, string(state.WorkerStatusCompleted)) {
		t.Fatalf("worker plan missing completed sibling worker result: %#v", planResult.Workers)
	}
	if !containsWorkerStatus(planResult.Workers, string(state.WorkerStatusApprovalRequired)) {
		t.Fatalf("worker plan missing approval-required worker: %#v", planResult.Workers)
	}
	if fakePlanner.inputs[1].CollectedContext == nil || fakePlanner.inputs[1].CollectedContext.WorkerPlan == nil {
		t.Fatalf("second planner input missing worker plan result: %#v", fakePlanner.inputs[1].CollectedContext)
	}
	if !containsPlannerWorkerStatus(fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers, string(state.WorkerStatusCompleted)) {
		t.Fatalf("second planner input missing completed sibling worker result: %#v", fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers)
	}
	if !containsPlannerWorkerStatus(fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers, string(state.WorkerStatusApprovalRequired)) {
		t.Fatalf("second planner input missing approval-required worker: %#v", fakePlanner.inputs[1].CollectedContext.WorkerPlan.Workers)
	}

	events, err := journalWriter.ReadRecent(run.ID, 32)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	for _, want := range []string{
		"worker.executor.approval_required",
		"worker.plan.waiting_on_approval",
		"worker.plan.finished",
	} {
		if !containsEventType(events, want) {
			t.Fatalf("journal missing %q event", want)
		}
	}
}

func TestCycleRunOnceExecuteRelocatesKnownRootReportArtifact(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_execute",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "run one bounded smoke check",
						AcceptanceCriteria: []string{"persist the executor turn", "stop after the second planner turn"},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after relocating the report artifact"},
				},
			},
		},
	}

	rootReportName := "summary_of_contracts_and_prior_runs.md"
	fakeExecutor := &stubExecutor{
		beforeReturn: func(req executor.TurnRequest) {
			reportPath := filepath.Join(req.RepoPath, rootReportName)
			if err := os.WriteFile(reportPath, []byte("# bounded smoke report\n"), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		},
		result: executor.TurnResult{
			Transport:    executor.TransportAppServer,
			RunID:        run.ID,
			ThreadID:     "thr_report",
			ThreadPath:   filepath.FromSlash("C:/Users/test/.codex/threads/thr_report.jsonl"),
			TurnID:       "turn_report",
			TurnStatus:   executor.TurnStatusCompleted,
			CompletedAt:  time.Date(2026, 4, 19, 18, 2, 0, 0, time.UTC),
			FinalMessage: "Wrote summary_of_contracts_and_prior_runs.md as a bounded smoke report.",
		},
	}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.SecondPlannerTurn == nil || result.SecondPlannerTurn.Output.Outcome != planner.OutcomePause {
		t.Fatalf("SecondPlannerTurn = %#v, want pause", result.SecondPlannerTurn)
	}

	rootReportPath := filepath.Join(run.RepoPath, rootReportName)
	if _, err := os.Stat(rootReportPath); !os.IsNotExist(err) {
		t.Fatalf("root report path still exists or stat errored: %v", err)
	}

	events, err := journalWriter.ReadRecent(run.ID, 16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	recordedEvent := latestEventType(events, "report.artifact.recorded")
	if recordedEvent.ArtifactPath == "" {
		t.Fatal("report.artifact.recorded missing artifact path")
	}
	if !strings.HasPrefix(recordedEvent.ArtifactPath, filepath.ToSlash(filepath.Join(reportArtifactDir, run.ID))+"/") {
		t.Fatalf("recordedEvent.ArtifactPath = %q, want report artifact path under %s", recordedEvent.ArtifactPath, reportArtifactDir)
	}

	artifactPath := filepath.Join(run.RepoPath, filepath.FromSlash(recordedEvent.ArtifactPath))
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("report artifact missing at %s: %v", artifactPath, err)
	}

	foundArtifactSummary := false
	for _, preview := range fakePlanner.inputs[1].RecentEvents {
		if strings.Contains(preview.Summary, recordedEvent.ArtifactPath) {
			foundArtifactSummary = true
			break
		}
	}
	if !foundArtifactSummary {
		t.Fatalf("second planner input recent_events did not reference report artifact path %q", recordedEvent.ArtifactPath)
	}
}

func TestCycleRunOnceExecuteWithDriftWatcherRunsPlannerReconsiderationBeforeDispatch(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_execute_initial",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "write a generic summary file",
						AcceptanceCriteria: []string{"complete one executor turn"},
					},
				},
			},
			{
				ResponseID: "resp_execute_reconsidered",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "implement the roadmap-aligned slice under internal/orchestration",
						AcceptanceCriteria: []string{"use the reconsidered task"},
						WriteScope:         []string{"internal/orchestration/cycle.go"},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after the post-executor planner turn"},
				},
			},
		},
	}

	fakeExecutor := &stubExecutor{
		result: executor.TurnResult{
			Transport:    executor.TransportAppServer,
			RunID:        run.ID,
			ThreadID:     "thr_drift",
			ThreadPath:   "thread.jsonl",
			TurnID:       "turn_drift",
			TurnStatus:   executor.TurnStatusCompleted,
			StartedAt:    time.Date(2026, 4, 19, 18, 0, 0, 0, time.UTC),
			CompletedAt:  time.Date(2026, 4, 19, 18, 1, 0, 0, time.UTC),
			FinalMessage: "Implemented the reconsidered roadmap-aligned slice.",
		},
	}

	fakeDriftWatcher := &stubDriftWatcher{
		result: DriftReviewResult{
			Summary: planner.DriftReviewSummary{
				Reviewer:                      driftWatcherName,
				Aligned:                       false,
				Concerns:                      []string{"planner task summary has no obvious lexical overlap with brief, roadmap, or decisions"},
				MissingContext:                []string{".orchestrator/decisions.md"},
				RecommendedPlannerAdjustments: []string{"Restate roadmap alignment before executor dispatch."},
				EvidencePaths:                 []string{".orchestrator/brief.md", ".orchestrator/roadmap.md"},
			},
			Artifact: []byte("{\"reviewer\":\"drift_watcher\",\"aligned\":false}"),
		},
	}

	cycle := Cycle{
		Store:         store,
		Journal:       journalWriter,
		Planner:       fakePlanner,
		Executor:      fakeExecutor,
		DriftWatcher:  fakeDriftWatcher,
		DriftReviewOn: true,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if fakeDriftWatcher.calls != 1 {
		t.Fatalf("drift watcher calls = %d, want 1", fakeDriftWatcher.calls)
	}
	if fakePlanner.calls != 3 {
		t.Fatalf("planner calls = %d, want 3", fakePlanner.calls)
	}
	if fakePlanner.previousResponseIDs[1] != "resp_execute_initial" {
		t.Fatalf("reconsideration previous_response_id = %q, want resp_execute_initial", fakePlanner.previousResponseIDs[1])
	}
	if fakePlanner.previousResponseIDs[2] != "resp_execute_reconsidered" {
		t.Fatalf("post-executor previous_response_id = %q, want resp_execute_reconsidered", fakePlanner.previousResponseIDs[2])
	}
	if fakePlanner.inputs[1].DriftReview == nil {
		t.Fatal("reconsideration planner input missing drift_review")
	}
	if fakePlanner.inputs[1].DriftReview.Reviewer != driftWatcherName {
		t.Fatalf("drift_review.reviewer = %q, want %q", fakePlanner.inputs[1].DriftReview.Reviewer, driftWatcherName)
	}
	if !strings.Contains(fakeExecutor.lastRequest.Prompt, "implement the roadmap-aligned slice under internal/orchestration") {
		t.Fatalf("executor prompt missing reconsidered task:\n%s", fakeExecutor.lastRequest.Prompt)
	}
	if strings.Contains(fakeExecutor.lastRequest.Prompt, "write a generic summary file") {
		t.Fatalf("executor prompt unexpectedly used the pre-review task:\n%s", fakeExecutor.lastRequest.Prompt)
	}
	if result.ReconsiderationPlannerTurn == nil {
		t.Fatal("ReconsiderationPlannerTurn = nil, want persisted reconsideration turn")
	}
	if result.SecondPlannerTurn == nil || result.SecondPlannerTurn.Output.Outcome != planner.OutcomePause {
		t.Fatalf("SecondPlannerTurn = %#v, want pause", result.SecondPlannerTurn)
	}

	events, err := journalWriter.ReadRecent(run.ID, 24)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "review.drift.started") {
		t.Fatal("journal missing review.drift.started event")
	}
	if !containsEventType(events, "review.drift.completed") {
		t.Fatal("journal missing review.drift.completed event")
	}
	reviewEvent := latestEventType(events, "review.drift.completed")
	if reviewEvent.ArtifactPath == "" {
		t.Fatal("review.drift.completed missing artifact path")
	}
	if !strings.HasPrefix(reviewEvent.ArtifactPath, filepath.ToSlash(filepath.Join(reviewArtifactDir, run.ID))+"/") {
		t.Fatalf("review artifact path = %q, want path under %s", reviewEvent.ArtifactPath, reviewArtifactDir)
	}
	if _, err := os.Stat(filepath.Join(run.RepoPath, filepath.FromSlash(reviewEvent.ArtifactPath))); err != nil {
		t.Fatalf("review artifact missing at %s: %v", reviewEvent.ArtifactPath, err)
	}
}

func TestCycleRunOnceDriftWatcherFailureIsRecordedAndDoesNotStopExecuteFlow(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_execute",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "attempt one executor turn after review failure",
						AcceptanceCriteria: []string{"continue with the original execute task"},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause:           &planner.PauseOutcome{Reason: "stop after one executor turn"},
				},
			},
		},
	}

	fakeExecutor := &stubExecutor{
		result: executor.TurnResult{
			Transport:    executor.TransportAppServer,
			RunID:        run.ID,
			ThreadID:     "thr_review_fail",
			ThreadPath:   "thread.jsonl",
			TurnID:       "turn_review_fail",
			TurnStatus:   executor.TurnStatusCompleted,
			CompletedAt:  time.Date(2026, 4, 19, 18, 3, 0, 0, time.UTC),
			FinalMessage: "Completed the original execute task after reviewer failure.",
		},
	}

	fakeDriftWatcher := &stubDriftWatcher{
		err: errors.New("drift watcher input could not be prepared"),
	}

	cycle := Cycle{
		Store:         store,
		Journal:       journalWriter,
		Planner:       fakePlanner,
		Executor:      fakeExecutor,
		DriftWatcher:  fakeDriftWatcher,
		DriftReviewOn: true,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if fakePlanner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", fakePlanner.calls)
	}
	if fakeExecutor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", fakeExecutor.calls)
	}
	if result.ReconsiderationPlannerTurn != nil {
		t.Fatalf("ReconsiderationPlannerTurn = %#v, want nil after reviewer failure", result.ReconsiderationPlannerTurn)
	}
	if result.Run.Status != state.StatusInitialized {
		t.Fatalf("Run.Status = %q, want initialized", result.Run.Status)
	}
	if result.Run.RuntimeIssueReason != "" {
		t.Fatalf("RuntimeIssueReason = %q, want empty", result.Run.RuntimeIssueReason)
	}

	events, err := journalWriter.ReadRecent(run.ID, 20)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "review.drift.started") {
		t.Fatal("journal missing review.drift.started event")
	}
	if !containsEventType(events, "review.drift.failed") {
		t.Fatal("journal missing review.drift.failed event")
	}
	if containsEventType(events, "review.drift.completed") {
		t.Fatal("journal unexpectedly recorded review.drift.completed after watcher failure")
	}
}

type stubPlanner struct {
	results             []planner.Result
	err                 error
	calls               int
	inputs              []planner.InputEnvelope
	previousResponseIDs []string
	afterPlan           func(call int, input planner.InputEnvelope, previousResponseID string)
}

func (s *stubPlanner) Plan(_ context.Context, input planner.InputEnvelope, previousResponseID string) (planner.Result, error) {
	s.calls++
	s.inputs = append(s.inputs, input)
	s.previousResponseIDs = append(s.previousResponseIDs, previousResponseID)
	if s.afterPlan != nil {
		s.afterPlan(s.calls, input, previousResponseID)
	}
	if s.err != nil {
		return planner.Result{}, s.err
	}
	index := s.calls - 1
	if index >= len(s.results) {
		return planner.Result{}, errors.New("stubPlanner called more times than configured")
	}
	return s.results[index], nil
}

type stubExecutor struct {
	result       executor.TurnResult
	err          error
	calls        int
	lastRequest  executor.TurnRequest
	beforeReturn func(executor.TurnRequest)
}

func (s *stubExecutor) Execute(_ context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	s.calls++
	s.lastRequest = req
	if s.beforeReturn != nil {
		s.beforeReturn(req)
	}
	return s.result, s.err
}

type scriptedExecutorResult struct {
	result       executor.TurnResult
	err          error
	beforeReturn func(executor.TurnRequest)
}

type concurrentScriptedExecutor struct {
	mu        sync.Mutex
	scripts   map[string]scriptedExecutorResult
	calls     int
	active    int
	maxActive int
}

func newConcurrentScriptedExecutor(scripts map[string]scriptedExecutorResult) *concurrentScriptedExecutor {
	return &concurrentScriptedExecutor{scripts: scripts}
}

func (s *concurrentScriptedExecutor) Execute(_ context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	prompt := strings.TrimSpace(req.Prompt)

	s.mu.Lock()
	s.calls++
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	script, ok := s.scripts[prompt]
	s.mu.Unlock()
	if !ok {
		return executor.TurnResult{}, fmt.Errorf("unexpected worker executor prompt: %s", prompt)
	}

	if script.beforeReturn != nil {
		script.beforeReturn(req)
	}

	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	return script.result, script.err
}

func (s *concurrentScriptedExecutor) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *concurrentScriptedExecutor) MaxActive() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive
}

type stubHumanInteractor struct {
	reply       string
	source      string
	err         error
	calls       int
	lastRun     state.Run
	lastOutcome planner.AskHumanOutcome
}

func (s *stubHumanInteractor) Ask(_ context.Context, run state.Run, outcome planner.AskHumanOutcome) (HumanInput, error) {
	s.calls++
	s.lastRun = run
	s.lastOutcome = outcome
	if s.err != nil {
		return HumanInput{}, s.err
	}
	source := s.source
	if source == "" {
		source = "terminal"
	}
	return HumanInput{Source: source, Payload: s.reply}, nil
}

type stubDriftWatcher struct {
	result DriftReviewResult
	err    error
	calls  int
	last   DriftReviewRequest
}

func (s *stubDriftWatcher) Review(_ context.Context, req DriftReviewRequest) (DriftReviewResult, error) {
	s.calls++
	s.last = req
	if s.err != nil {
		return DriftReviewResult{}, s.err
	}
	return s.result, nil
}

func newTestRuntime(t *testing.T) (*state.Store, *journal.Journal, state.Run) {
	t.Helper()

	root := t.TempDir()
	store, err := state.Open(filepath.Join(root, "orchestrator.db"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	journalWriter, err := journal.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatalf("journal.Open() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: root,
		Goal:     "ship the smallest real bounded planner to executor cycle",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 19, 17, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	return store, journalWriter, run
}

func containsEventType(events []journal.Event, want string) bool {
	for _, event := range events {
		if event.Type == want {
			return true
		}
	}
	return false
}

func waitForPrompts(t *testing.T, started <-chan string, expected map[string]bool) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	seen := make(map[string]bool, len(expected))
	for len(seen) < len(expected) {
		select {
		case prompt := <-started:
			if !expected[prompt] {
				t.Fatalf("unexpected started prompt: %q", prompt)
			}
			seen[prompt] = true
		case <-deadline:
			t.Fatalf("timed out waiting for started prompts: %#v", expected)
		}
	}
}

func waitForStartedCount(t *testing.T, started <-chan string, count int) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	seen := 0
	for seen < count {
		select {
		case <-started:
			seen++
		case <-deadline:
			t.Fatalf("timed out waiting for %d started worker turn(s)", count)
		}
	}
}

func completedWorkerTurnResult(runID string, threadID string, turnID string, message string) executor.TurnResult {
	return executor.TurnResult{
		Transport:    executor.TransportAppServer,
		RunID:        runID,
		ThreadID:     threadID,
		TurnID:       turnID,
		TurnStatus:   executor.TurnStatusCompleted,
		StartedAt:    time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC),
		CompletedAt:  time.Date(2026, 4, 21, 12, 1, 0, 0, time.UTC),
		FinalMessage: message,
	}
}

func containsWorkerStatus(workers []state.WorkerResultSummary, want string) bool {
	for _, worker := range workers {
		if strings.TrimSpace(worker.WorkerStatus) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
}

func containsPlannerWorkerStatus(workers []planner.WorkerResultSummary, want string) bool {
	for _, worker := range workers {
		if strings.TrimSpace(worker.WorkerStatus) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
}

func countEventType(events []journal.Event, want string) int {
	count := 0
	for _, event := range events {
		if event.Type == want {
			count++
		}
	}
	return count
}

func latestEventType(events []journal.Event, want string) journal.Event {
	for idx := len(events) - 1; idx >= 0; idx-- {
		if events[idx].Type == want {
			return events[idx]
		}
	}
	return journal.Event{}
}

func containsIntegrationConflict(candidates []state.ConflictCandidate, wantPath string, wantReason string) bool {
	for _, candidate := range candidates {
		if candidate.Path == wantPath && candidate.Reason == wantReason {
			return true
		}
	}
	return false
}

func containsStateAppliedFile(files []state.IntegrationAppliedFile, wantPath string, wantChangeKind string) bool {
	for _, file := range files {
		if file.Path == wantPath && file.ChangeKind == wantChangeKind {
			return true
		}
	}
	return false
}

func containsStateSkippedApplyFile(files []state.IntegrationSkippedFile, wantPath string, wantReason string) bool {
	for _, file := range files {
		if file.Path == wantPath && file.Reason == wantReason {
			return true
		}
	}
	return false
}

func assertFileContents(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("contents(%q) = %q, want %q", path, string(content), want)
	}
}

func writeIntegrationFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func installFakeGitForCycleTests(t *testing.T) {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", binDir, err)
	}

	if runtime.GOOS == "windows" {
		script := `@echo off
setlocal EnableDelayedExpansion
if "%1"=="-C" (
  set REPO=%2
  shift
  shift
)
if "%1"=="rev-parse" (
  if "%FAKE_GIT_FAIL_REVPARSE%"=="1" (
    echo fake rev-parse failure 1>&2
    exit /b 1
  )
  echo true
  exit /b 0
)
if "%1"=="worktree" (
  if "%2"=="list" (
    echo !REPO!
    exit /b 0
  )
  if "%2"=="add" (
    if "%FAKE_GIT_FAIL_ADD%"=="1" (
      echo fake worktree add failure 1>&2
      exit /b 1
    )
    set TARGET=%4
    mkdir "!TARGET!" >nul 2>nul
    >"!TARGET!\.git" echo gitdir: fake
    exit /b 0
  )
  if "%2"=="remove" (
    if "%FAKE_GIT_FAIL_REMOVE%"=="1" (
      echo fake worktree remove failure 1>&2
      exit /b 1
    )
    set TARGET=%4
    if exist "!TARGET!" rmdir /s /q "!TARGET!"
    exit /b 0
  )
)
echo unsupported fake git invocation 1>&2
exit /b 1
`
		if err := os.WriteFile(filepath.Join(binDir, "git.cmd"), []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile(git.cmd) error = %v", err)
		}
	} else {
		script := `#!/bin/sh
if [ "$1" = "-C" ]; then
  REPO="$2"
  shift 2
fi
if [ "$1" = "rev-parse" ]; then
  if [ "${FAKE_GIT_FAIL_REVPARSE:-0}" = "1" ]; then
    echo "fake rev-parse failure" >&2
    exit 1
  fi
  echo true
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "list" ]; then
  echo "${REPO:-.}"
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "add" ]; then
  if [ "${FAKE_GIT_FAIL_ADD:-0}" = "1" ]; then
    echo "fake worktree add failure" >&2
    exit 1
  fi
  TARGET="$4"
  mkdir -p "$TARGET"
  printf 'gitdir: fake\n' > "$TARGET/.git"
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "remove" ]; then
  if [ "${FAKE_GIT_FAIL_REMOVE:-0}" = "1" ]; then
    echo "fake worktree remove failure" >&2
    exit 1
  fi
  TARGET="$4"
  rm -rf "$TARGET"
  exit 0
fi
echo "unsupported fake git invocation" >&2
exit 1
`
		if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile(git) error = %v", err)
		}
	}

	pathValue := binDir
	if existing := os.Getenv("PATH"); existing != "" {
		pathValue += string(os.PathListSeparator) + existing
	}
	t.Setenv("PATH", pathValue)
}
