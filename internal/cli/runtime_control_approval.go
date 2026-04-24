package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"orchestrator/internal/control"
	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

func approvalSnapshotFromRun(run state.Run, workers []state.Worker) controlApprovalSnapshot {
	workerApprovals := workerStatusCount(workers, state.WorkerStatusApprovalRequired)
	snapshot := controlApprovalSnapshot{
		Present:                false,
		RunID:                  strings.TrimSpace(run.ID),
		ExecutorThreadID:       strings.TrimSpace(run.ExecutorThreadID),
		ExecutorTurnID:         strings.TrimSpace(run.ExecutorTurnID),
		LastControlAction:      executorLastControlActionValue(run),
		WorkerApprovalRequired: workerApprovals,
		Message:                "no approval is currently required",
	}

	if run.ExecutorApproval != nil {
		snapshot.State = strings.TrimSpace(run.ExecutorApproval.State)
		snapshot.Kind = strings.TrimSpace(run.ExecutorApproval.Kind)
		snapshot.Reason = strings.TrimSpace(run.ExecutorApproval.Reason)
		snapshot.Command = strings.TrimSpace(run.ExecutorApproval.Command)
		snapshot.CWD = strings.TrimSpace(run.ExecutorApproval.CWD)
		snapshot.GrantRoot = strings.TrimSpace(run.ExecutorApproval.GrantRoot)
	}

	if snapshot.State == string(executor.ApprovalStateRequired) || strings.TrimSpace(run.ExecutorTurnStatus) == string(executor.TurnStatusApprovalRequired) {
		snapshot.Present = true
		snapshot.AvailableActions = []string{"approve", "deny"}
		snapshot.Summary = summarizeExecutorApproval(run)
		snapshot.Message = "primary executor turn is waiting on explicit approval"
		return snapshot
	}

	if workerApprovals > 0 {
		snapshot.Present = true
		snapshot.State = "worker_approval_required"
		snapshot.Summary = fmt.Sprintf("%d isolated worker(s) are waiting on approval", workerApprovals)
		snapshot.Message = "worker-specific approval controls are not wired into the shell yet"
		return snapshot
	}

	if snapshot.State != "" {
		snapshot.Summary = summarizeApprovalState(run)
	}
	return snapshot
}

func summarizeApprovalState(run state.Run) string {
	if run.ExecutorApproval == nil {
		return ""
	}
	switch strings.TrimSpace(run.ExecutorApproval.State) {
	case string(executor.ApprovalStateGranted):
		return "primary executor approval was granted"
	case string(executor.ApprovalStateDenied):
		return "primary executor approval was denied"
	default:
		return ""
	}
}

func summarizeExecutorApproval(run state.Run) string {
	if run.ExecutorApproval == nil {
		return "primary executor turn is waiting on approval"
	}
	switch executor.ApprovalKind(strings.TrimSpace(run.ExecutorApproval.Kind)) {
	case executor.ApprovalKindCommandExecution:
		if strings.TrimSpace(run.ExecutorApproval.Command) != "" {
			return "executor approval required for command: " + previewString(run.ExecutorApproval.Command, 160)
		}
	case executor.ApprovalKindFileChange:
		if strings.TrimSpace(run.ExecutorApproval.GrantRoot) != "" {
			return "executor approval required for file changes under: " + strings.TrimSpace(run.ExecutorApproval.GrantRoot)
		}
	case executor.ApprovalKindPermissions:
		if strings.TrimSpace(run.ExecutorApproval.Reason) != "" {
			return "executor approval required for permissions: " + previewString(run.ExecutorApproval.Reason, 160)
		}
	}
	if strings.TrimSpace(run.ExecutorApproval.Reason) != "" {
		return "executor approval required: " + previewString(run.ExecutorApproval.Reason, 160)
	}
	return "primary executor turn is waiting on approval"
}

func approveExecutorRequest(ctx context.Context, inv Invocation, request control.ExecutorApprovalActionRequest) (controlApprovalSnapshot, error) {
	return resolveExecutorApprovalRequest(ctx, inv, request, string(executor.ControlActionApprove))
}

func denyExecutorRequest(ctx context.Context, inv Invocation, request control.ExecutorApprovalActionRequest) (controlApprovalSnapshot, error) {
	return resolveExecutorApprovalRequest(ctx, inv, request, string(executor.ControlActionDeny))
}

func resolveExecutorApprovalRequest(
	ctx context.Context,
	inv Invocation,
	request control.ExecutorApprovalActionRequest,
	action string,
) (controlApprovalSnapshot, error) {
	store, journalWriter, run, err := activeExecutorRunForControl(ctx, inv, strings.TrimSpace(request.RunID))
	if err != nil {
		return controlApprovalSnapshot{}, err
	}
	defer store.Close()

	if run.ExecutorApproval == nil || strings.TrimSpace(run.ExecutorApproval.State) != string(executor.ApprovalStateRequired) {
		return controlApprovalSnapshot{}, errors.New("no persisted approval-required state found")
	}

	client, err := newExecutorControlClient(inv.Version)
	if err != nil {
		return controlApprovalSnapshot{}, err
	}

	switch action {
	case string(executor.ControlActionApprove):
		err = client.Approve(ctx, executorRequestFromRun(run), executorApprovalFromRun(run))
	case string(executor.ControlActionDeny):
		err = client.Deny(ctx, executorRequestFromRun(run), executorApprovalFromRun(run))
	default:
		err = fmt.Errorf("unsupported approval action %s", action)
	}
	if err != nil {
		return controlApprovalSnapshot{}, err
	}

	updatedState := executorStateFromRun(run)
	updatedState.TurnStatus = string(executor.TurnStatusInProgress)
	updatedState.LastError = ""
	updatedState.LastFailureStage = ""
	updatedState.Approval = &state.ExecutorApproval{
		State:      approvedStateForAction(action),
		Kind:       run.ExecutorApproval.Kind,
		RequestID:  run.ExecutorApproval.RequestID,
		ApprovalID: run.ExecutorApproval.ApprovalID,
		ItemID:     run.ExecutorApproval.ItemID,
		Reason:     run.ExecutorApproval.Reason,
		Command:    run.ExecutorApproval.Command,
		CWD:        run.ExecutorApproval.CWD,
		GrantRoot:  run.ExecutorApproval.GrantRoot,
		RawParams:  run.ExecutorApproval.RawParams,
	}
	updatedState.LastControl = &state.ExecutorControl{
		Action: action,
		At:     time.Now().UTC(),
	}
	if err := store.SaveExecutorState(ctx, run.ID, updatedState); err != nil {
		return controlApprovalSnapshot{}, err
	}

	latestRun, found, err := store.GetRun(ctx, run.ID)
	if err != nil {
		return controlApprovalSnapshot{}, err
	}
	if !found {
		return controlApprovalSnapshot{}, fmt.Errorf("run %s not found after approval action", run.ID)
	}

	if err := appendExecutorApprovalJournal(journalWriter, latestRun, action); err != nil {
		return controlApprovalSnapshot{}, err
	}

	workers, err := store.ListWorkers(ctx, latestRun.ID)
	if err != nil {
		return controlApprovalSnapshot{}, err
	}

	decision := "approved"
	eventType := "approval_cleared"
	if action == string(executor.ControlActionDeny) {
		decision = "denied"
	}
	emitEngineEvent(inv, eventType, eventPayloadForRun(latestRun, map[string]any{
		"approval_state":     approvedStateForAction(action),
		"approval_kind":      executorApprovalKindValue(latestRun),
		"executor_turn_id":   strings.TrimSpace(latestRun.ExecutorTurnID),
		"executor_thread_id": strings.TrimSpace(latestRun.ExecutorThreadID),
		"decision":           decision,
	}))

	return approvalSnapshotFromRun(latestRun, workers), nil
}

func approvedStateForAction(action string) string {
	switch action {
	case string(executor.ControlActionApprove):
		return string(executor.ApprovalStateGranted)
	case string(executor.ControlActionDeny):
		return string(executor.ApprovalStateDenied)
	default:
		return ""
	}
}

func appendExecutorApprovalJournal(journalWriter *journal.Journal, run state.Run, action string) error {
	if journalWriter == nil {
		return nil
	}

	eventType := "executor.approval.granted"
	message := "executor approval response sent"
	if action == string(executor.ControlActionDeny) {
		eventType = "executor.approval.denied"
		message = "executor denial response sent"
	}

	return journalWriter.Append(journal.Event{
		Type:                  eventType,
		RunID:                 run.ID,
		RepoPath:              run.RepoPath,
		Goal:                  run.Goal,
		Status:                string(run.Status),
		Message:               message,
		PreviousResponseID:    run.PreviousResponseID,
		ExecutorTransport:     run.ExecutorTransport,
		ExecutorThreadID:      run.ExecutorThreadID,
		ExecutorThreadPath:    run.ExecutorThreadPath,
		ExecutorTurnID:        run.ExecutorTurnID,
		ExecutorTurnStatus:    run.ExecutorTurnStatus,
		ExecutorApprovalState: executorApprovalStateValue(run),
		ExecutorApprovalKind:  executorApprovalKindValue(run),
		ExecutorControlAction: action,
		Checkpoint:            journalCheckpointRef(run.LatestCheckpoint),
	})
}

func activeExecutorRunForControl(ctx context.Context, inv Invocation, requestedRunID string) (*state.Store, *journal.Journal, state.Run, error) {
	if !pathExists(inv.Layout.DBPath) {
		return nil, nil, state.Run{}, errors.New("no unfinished run is available")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return nil, nil, state.Run{}, err
	}

	run, found, err := store.LatestResumableRun(ctx)
	if err != nil {
		store.Close()
		return nil, nil, state.Run{}, err
	}
	if !found {
		store.Close()
		return nil, nil, state.Run{}, errors.New("no unfinished run is available")
	}
	if requestedRunID != "" && !strings.EqualFold(strings.TrimSpace(requestedRunID), run.ID) {
		store.Close()
		return nil, nil, state.Run{}, errors.New("requested run is not the latest unfinished run in this slice")
	}
	if strings.TrimSpace(run.ExecutorThreadID) == "" || strings.TrimSpace(run.ExecutorTurnID) == "" {
		store.Close()
		return nil, nil, state.Run{}, errors.New("no active executor turn is available")
	}

	return store, journalWriter, run, nil
}
