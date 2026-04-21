package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/state"
)

func TestRunExecutorApproveRecordsDurableApprovalAction(t *testing.T) {
	layout, run := newExecutorControlRuntime(t, state.ExecutorState{
		Transport:   string(executor.TransportAppServer),
		ThreadID:    "thr_approve",
		ThreadPath:  "thread.jsonl",
		TurnID:      "turn_approve",
		TurnStatus:  string(executor.TurnStatusApprovalRequired),
		LastMessage: "waiting for approval",
		Approval: &state.ExecutorApproval{
			State:      string(executor.ApprovalStateRequired),
			Kind:       string(executor.ApprovalKindCommandExecution),
			RequestID:  "req_approve",
			ApprovalID: "approval_approve",
			ItemID:     "item_approve",
			Command:    "go test ./...",
			CWD:        ".",
			RawParams:  `{"approvalId":"approval_approve"}`,
		},
	})

	restoreClient := stubExecutorControlClient(&fakeExecutorControlClient{})
	defer restoreClient()

	var stdout bytes.Buffer
	err := runExecutor(context.Background(), Invocation{
		Args:     []string{"approve"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runExecutor() error = %v", err)
	}

	updatedRun := latestRunForLayout(t, layout)
	if updatedRun.ExecutorApproval == nil {
		t.Fatal("ExecutorApproval = nil, want persisted approval state")
	}
	if updatedRun.ExecutorApproval.State != string(executor.ApprovalStateGranted) {
		t.Fatalf("ExecutorApproval.State = %q, want granted", updatedRun.ExecutorApproval.State)
	}
	if updatedRun.ExecutorLastControl == nil {
		t.Fatal("ExecutorLastControl = nil, want persisted control action")
	}
	if updatedRun.ExecutorLastControl.Action != string(executor.ControlActionApprove) {
		t.Fatalf("ExecutorLastControl.Action = %q, want approved", updatedRun.ExecutorLastControl.Action)
	}
	if updatedRun.LatestStopReason != "" {
		t.Fatalf("LatestStopReason = %q, want cleared", updatedRun.LatestStopReason)
	}
	if updatedRun.RuntimeIssueReason != "" {
		t.Fatalf("RuntimeIssueReason = %q, want empty", updatedRun.RuntimeIssueReason)
	}

	for _, want := range []string{
		"command: executor approve",
		"run_id: " + run.ID,
		"executor_approval_state: granted",
		"executor_control_action: approved",
		"next_operator_action: continue_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	events := readContinueEvents(t, layout, run.ID, 12)
	event := latestJournalEvent(events, "executor.approval.granted")
	if event.Type != "executor.approval.granted" {
		t.Fatal("journal missing executor.approval.granted event")
	}
	if event.ExecutorApprovalState != string(executor.ApprovalStateGranted) {
		t.Fatalf("event.ExecutorApprovalState = %q, want granted", event.ExecutorApprovalState)
	}
	if event.ExecutorControlAction != string(executor.ControlActionApprove) {
		t.Fatalf("event.ExecutorControlAction = %q, want approved", event.ExecutorControlAction)
	}
}

func TestRunExecutorDenyPersistsDataWithoutRuntimeFailure(t *testing.T) {
	layout, run := newExecutorControlRuntime(t, state.ExecutorState{
		Transport:   string(executor.TransportAppServer),
		ThreadID:    "thr_deny",
		ThreadPath:  "thread.jsonl",
		TurnID:      "turn_deny",
		TurnStatus:  string(executor.TurnStatusApprovalRequired),
		LastMessage: "waiting for approval",
		Approval: &state.ExecutorApproval{
			State:      string(executor.ApprovalStateRequired),
			Kind:       string(executor.ApprovalKindFileChange),
			RequestID:  "req_deny",
			ApprovalID: "approval_deny",
			ItemID:     "item_deny",
			GrantRoot:  ".",
			RawParams:  `{"itemId":"item_deny"}`,
		},
	})

	restoreClient := stubExecutorControlClient(&fakeExecutorControlClient{})
	defer restoreClient()

	var stdout bytes.Buffer
	err := runExecutor(context.Background(), Invocation{
		Args:     []string{"deny"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runExecutor() error = %v", err)
	}

	updatedRun := latestRunForLayout(t, layout)
	if updatedRun.ExecutorApproval == nil || updatedRun.ExecutorApproval.State != string(executor.ApprovalStateDenied) {
		t.Fatalf("ExecutorApproval = %#v, want denied", updatedRun.ExecutorApproval)
	}
	if updatedRun.ExecutorLastControl == nil || updatedRun.ExecutorLastControl.Action != string(executor.ControlActionDeny) {
		t.Fatalf("ExecutorLastControl = %#v, want denied action", updatedRun.ExecutorLastControl)
	}
	if updatedRun.RuntimeIssueReason != "" {
		t.Fatalf("RuntimeIssueReason = %q, want empty", updatedRun.RuntimeIssueReason)
	}
	if updatedRun.Status != state.StatusInitialized {
		t.Fatalf("Status = %q, want initialized", updatedRun.Status)
	}

	for _, want := range []string{
		"command: executor deny",
		"executor_approval_state: denied",
		"executor_control_action: denied",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout.String())
		}
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 12), "executor.approval.denied")
	if event.Type != "executor.approval.denied" {
		t.Fatal("journal missing executor.approval.denied event")
	}
	if event.ExecutorControlAction != string(executor.ControlActionDeny) {
		t.Fatalf("event.ExecutorControlAction = %q, want denied", event.ExecutorControlAction)
	}
}

func TestRunExecutorInterruptRecordsRequestedActionTruthfully(t *testing.T) {
	layout, run := newExecutorControlRuntime(t, state.ExecutorState{
		Transport:   string(executor.TransportAppServer),
		ThreadID:    "thr_interrupt",
		ThreadPath:  "thread.jsonl",
		TurnID:      "turn_interrupt",
		TurnStatus:  string(executor.TurnStatusInProgress),
		LastMessage: "executor is still running",
	})

	restoreClient := stubExecutorControlClient(&fakeExecutorControlClient{})
	defer restoreClient()

	var stdout bytes.Buffer
	err := runExecutor(context.Background(), Invocation{
		Args:     []string{"interrupt"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runExecutor() error = %v", err)
	}

	updatedRun := latestRunForLayout(t, layout)
	if updatedRun.ExecutorTurnStatus != string(executor.TurnStatusInProgress) {
		t.Fatalf("ExecutorTurnStatus = %q, want still in_progress after request", updatedRun.ExecutorTurnStatus)
	}
	if updatedRun.ExecutorLastControl == nil || updatedRun.ExecutorLastControl.Action != string(executor.ControlActionInterrupt) {
		t.Fatalf("ExecutorLastControl = %#v, want interrupted action", updatedRun.ExecutorLastControl)
	}
	if updatedRun.RuntimeIssueReason != "" {
		t.Fatalf("RuntimeIssueReason = %q, want empty", updatedRun.RuntimeIssueReason)
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 12), "executor.interrupt.requested")
	if event.Type != "executor.interrupt.requested" {
		t.Fatal("journal missing executor.interrupt.requested event")
	}
	if event.ExecutorControlAction != string(executor.ControlActionInterrupt) {
		t.Fatalf("event.ExecutorControlAction = %q, want interrupted", event.ExecutorControlAction)
	}
}

func TestRunExecutorSteerPersistsRawNote(t *testing.T) {
	layout, run := newExecutorControlRuntime(t, state.ExecutorState{
		Transport:   string(executor.TransportAppServer),
		ThreadID:    "thr_steer",
		ThreadPath:  "thread.jsonl",
		TurnID:      "turn_steer",
		TurnStatus:  string(executor.TurnStatusInProgress),
		LastMessage: "executor is still running",
	})

	fakeClient := &fakeExecutorControlClient{}
	restoreClient := stubExecutorControlClient(fakeClient)
	defer restoreClient()

	var stdout bytes.Buffer
	err := runExecutor(context.Background(), Invocation{
		Args:     []string{"steer", "please", "stay", "inside", "internal/orchestration"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runExecutor() error = %v", err)
	}

	if fakeClient.lastSteerNote != "please stay inside internal/orchestration" {
		t.Fatalf("lastSteerNote = %q", fakeClient.lastSteerNote)
	}

	updatedRun := latestRunForLayout(t, layout)
	if updatedRun.ExecutorLastControl == nil {
		t.Fatal("ExecutorLastControl = nil, want steered action")
	}
	if updatedRun.ExecutorLastControl.Action != string(executor.ControlActionSteer) {
		t.Fatalf("ExecutorLastControl.Action = %q, want steered", updatedRun.ExecutorLastControl.Action)
	}
	if updatedRun.ExecutorLastControl.Payload != "please stay inside internal/orchestration" {
		t.Fatalf("ExecutorLastControl.Payload = %q", updatedRun.ExecutorLastControl.Payload)
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 12), "executor.steer.sent")
	if event.Type != "executor.steer.sent" {
		t.Fatal("journal missing executor.steer.sent event")
	}
	if event.ExecutorControlAction != string(executor.ControlActionSteer) {
		t.Fatalf("event.ExecutorControlAction = %q, want steered", event.ExecutorControlAction)
	}
}

func TestRunExecutorSteerFailsMechanicallyWhenTurnIsNotSteerable(t *testing.T) {
	layout, run := newExecutorControlRuntime(t, state.ExecutorState{
		Transport:  string(executor.TransportAppServer),
		ThreadID:   "thr_not_steerable",
		ThreadPath: "thread.jsonl",
		TurnID:     "turn_not_steerable",
		TurnStatus: string(executor.TurnStatusApprovalRequired),
		Approval: &state.ExecutorApproval{
			State: string(executor.ApprovalStateRequired),
			Kind:  string(executor.ApprovalKindCommandExecution),
		},
	})

	restoreClient := stubExecutorControlClient(&fakeExecutorControlClient{})
	defer restoreClient()

	var stdout bytes.Buffer
	err := runExecutor(context.Background(), Invocation{
		Args:     []string{"steer", "raw", "note"},
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: layout.RepoRoot,
		Layout:   layout,
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("runExecutor() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "executor_steer: active executor turn is not currently steerable") {
		t.Fatalf("stdout = %q, want mechanical steer failure", stdout.String())
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 12), "executor.steer.failed")
	if event.Type != "executor.steer.failed" {
		t.Fatal("journal missing executor.steer.failed event")
	}
	if event.ExecutorControlAction != string(executor.ControlActionSteer) {
		t.Fatalf("event.ExecutorControlAction = %q, want steered", event.ExecutorControlAction)
	}
}

func newExecutorControlRuntime(t *testing.T, executorState state.ExecutorState) (state.Layout, state.Run) {
	t.Helper()

	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)

	store, journalWriter, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	defer store.Close()

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "control the active executor turn",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     2,
			Stage:        "planner",
			Label:        "planner_turn_post_executor",
			SafePause:    true,
			PlannerTurn:  1,
			ExecutorTurn: 1,
			CreatedAt:    time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := store.SaveExecutorState(context.Background(), run.ID, executorState); err != nil {
		t.Fatalf("SaveExecutorState() error = %v", err)
	}
	if executorState.Approval != nil {
		if err := store.SaveLatestStopReason(context.Background(), run.ID, orchestration.StopReasonExecutorApprovalReq); err != nil {
			t.Fatalf("SaveLatestStopReason() error = %v", err)
		}
		if err := journalWriter.Append(journal.Event{
			Type:                  "executor.approval.required",
			RunID:                 run.ID,
			RepoPath:              run.RepoPath,
			Goal:                  run.Goal,
			Status:                string(run.Status),
			Message:               "executor approval required",
			StopReason:            orchestration.StopReasonExecutorApprovalReq,
			ExecutorThreadID:      executorState.ThreadID,
			ExecutorThreadPath:    executorState.ThreadPath,
			ExecutorTurnID:        executorState.TurnID,
			ExecutorTurnStatus:    executorState.TurnStatus,
			ExecutorApprovalState: executorApprovalStateValueForState(executorState),
			ExecutorApprovalKind:  executorApprovalKindValueForState(executorState),
			Checkpoint:            journalCheckpointRef(run.LatestCheckpoint),
		}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	return layout, run
}

func stubExecutorControlClient(fake executorControlClient) func() {
	original := newExecutorControlClient
	newExecutorControlClient = func(string) (executorControlClient, error) {
		return fake, nil
	}
	return func() {
		newExecutorControlClient = original
	}
}

type fakeExecutorControlClient struct {
	lastAction    string
	lastRequest   executor.TurnRequest
	lastApproval  executor.ApprovalRequest
	lastSteerNote string
}

func (f *fakeExecutorControlClient) Approve(_ context.Context, req executor.TurnRequest, approval executor.ApprovalRequest) error {
	f.lastAction = "approve"
	f.lastRequest = req
	f.lastApproval = approval
	return nil
}

func (f *fakeExecutorControlClient) Deny(_ context.Context, req executor.TurnRequest, approval executor.ApprovalRequest) error {
	f.lastAction = "deny"
	f.lastRequest = req
	f.lastApproval = approval
	return nil
}

func (f *fakeExecutorControlClient) InterruptTurn(_ context.Context, req executor.TurnRequest) error {
	f.lastAction = "interrupt"
	f.lastRequest = req
	return nil
}

func (f *fakeExecutorControlClient) SteerTurn(_ context.Context, req executor.TurnRequest, note string) error {
	f.lastAction = "steer"
	f.lastRequest = req
	f.lastSteerNote = note
	return nil
}

func executorApprovalStateValueForState(executorState state.ExecutorState) string {
	if executorState.Approval == nil {
		return ""
	}
	return executorState.Approval.State
}

func executorApprovalKindValueForState(executorState state.ExecutorState) string {
	if executorState.Approval == nil {
		return ""
	}
	return executorState.Approval.Kind
}
