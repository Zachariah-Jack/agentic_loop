package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"orchestrator/internal/control"
	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/state"
	workerctl "orchestrator/internal/workers"
)

type controlWorkerCreateSnapshot struct {
	Created bool                  `json:"created"`
	RunID   string                `json:"run_id,omitempty"`
	Message string                `json:"message,omitempty"`
	Worker  controlWorkerSnapshot `json:"worker"`
}

type controlWorkerDispatchSnapshot struct {
	Dispatched bool                  `json:"dispatched"`
	RunID      string                `json:"run_id,omitempty"`
	Message    string                `json:"message,omitempty"`
	Worker     controlWorkerSnapshot `json:"worker"`
}

type controlWorkerRemoveSnapshot struct {
	Removed      bool   `json:"removed"`
	RunID        string `json:"run_id,omitempty"`
	WorkerID     string `json:"worker_id,omitempty"`
	WorkerName   string `json:"worker_name,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Message      string `json:"message,omitempty"`
}

type controlWorkerIntegrateSnapshot struct {
	RunID              string                    `json:"run_id,omitempty"`
	WorkerIDs          []string                  `json:"worker_ids,omitempty"`
	WorkerCount        int                       `json:"worker_count"`
	ArtifactPath       string                    `json:"artifact_path,omitempty"`
	ArtifactPreview    string                    `json:"artifact_preview,omitempty"`
	IntegrationPreview string                    `json:"integration_preview,omitempty"`
	ConflictCount      int                       `json:"conflict_count"`
	ConflictCandidates []state.ConflictCandidate `json:"conflict_candidates,omitempty"`
	Message            string                    `json:"message,omitempty"`
}

func createWorkerForControl(ctx context.Context, inv Invocation, request control.CreateWorkerRequest) (controlWorkerCreateSnapshot, error) {
	if strings.TrimSpace(request.Name) == "" {
		return controlWorkerCreateSnapshot{}, errors.New("name is required")
	}
	if strings.TrimSpace(request.Scope) == "" {
		return controlWorkerCreateSnapshot{}, errors.New("scope is required")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return controlWorkerCreateSnapshot{}, err
	}
	defer store.Close()

	run, found, err := resolveWorkerRun(ctx, store, strings.TrimSpace(request.RunID))
	if err != nil {
		return controlWorkerCreateSnapshot{}, err
	}
	if !found {
		return controlWorkerCreateSnapshot{}, errors.New("no unfinished run is available for worker creation")
	}

	manager := workerctl.NewManager(run.RepoPath, inv.Layout.WorkersDir)
	plannedPath, err := manager.PlannedPath(request.Name)
	if err != nil {
		appendWorkerEvent(journalWriter, "worker.create.failed", run, nil, err.Error())
		return controlWorkerCreateSnapshot{}, err
	}

	worker, err := store.CreateWorker(ctx, state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    strings.TrimSpace(request.Name),
		WorkerStatus:  state.WorkerStatusCreating,
		AssignedScope: strings.TrimSpace(request.Scope),
		WorktreePath:  plannedPath,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		appendWorkerEvent(journalWriter, "worker.create.failed", run, &state.Worker{
			RunID:         run.ID,
			WorkerName:    strings.TrimSpace(request.Name),
			WorkerStatus:  state.WorkerStatusCreating,
			AssignedScope: strings.TrimSpace(request.Scope),
			WorktreePath:  plannedPath,
		}, err.Error())
		return controlWorkerCreateSnapshot{}, err
	}

	if _, err := manager.Create(ctx, worker.WorkerName); err != nil {
		_ = store.DeleteWorker(ctx, worker.ID)
		appendWorkerEvent(journalWriter, "worker.create.failed", run, &worker, err.Error())
		return controlWorkerCreateSnapshot{}, err
	}

	worker.WorkerStatus = state.WorkerStatusIdle
	worker.UpdatedAt = time.Now().UTC()
	if err := store.SaveWorker(ctx, worker); err != nil {
		return controlWorkerCreateSnapshot{}, err
	}

	worker, found, err = store.GetWorker(ctx, worker.ID)
	if err != nil {
		return controlWorkerCreateSnapshot{}, err
	}
	if !found {
		return controlWorkerCreateSnapshot{}, fmt.Errorf("worker %s could not be reloaded", worker.ID)
	}

	appendWorkerEvent(journalWriter, "worker.created", run, &worker, "isolated worker worktree created")
	emitEngineEvent(inv, "worker_created", eventPayloadForRun(run, map[string]any{
		"worker_id":     worker.ID,
		"worker_name":   worker.WorkerName,
		"worker_status": string(worker.WorkerStatus),
		"worker_scope":  worker.AssignedScope,
		"worker_path":   worker.WorktreePath,
	}))

	return controlWorkerCreateSnapshot{
		Created: true,
		RunID:   run.ID,
		Message: "isolated worker worktree created",
		Worker:  controlWorkerSnapshotFromWorker(worker),
	}, nil
}

func dispatchWorkerForControl(ctx context.Context, inv Invocation, request control.DispatchWorkerRequest) (controlWorkerDispatchSnapshot, error) {
	if strings.TrimSpace(request.WorkerID) == "" {
		return controlWorkerDispatchSnapshot{}, errors.New("worker_id is required")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return controlWorkerDispatchSnapshot{}, errors.New("prompt is required")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return controlWorkerDispatchSnapshot{}, err
	}
	defer store.Close()

	worker, found, err := store.GetWorker(ctx, strings.TrimSpace(request.WorkerID))
	if err != nil {
		return controlWorkerDispatchSnapshot{}, err
	}
	if !found {
		return controlWorkerDispatchSnapshot{}, errors.New("worker not found")
	}

	run, found, err := store.GetRun(ctx, worker.RunID)
	if err != nil {
		return controlWorkerDispatchSnapshot{}, err
	}
	if !found {
		return controlWorkerDispatchSnapshot{}, fmt.Errorf("worker run %s not found", worker.RunID)
	}
	if state.IsWorkerActive(worker.WorkerStatus) {
		return controlWorkerDispatchSnapshot{}, errors.New("worker already has an active executor turn")
	}
	if _, err := os.Stat(worker.WorktreePath); err != nil {
		return controlWorkerDispatchSnapshot{}, fmt.Errorf("worker worktree path is unavailable: %w", err)
	}

	worker.WorkerStatus = state.WorkerStatusExecutorActive
	worker.WorkerTaskSummary = previewString(strings.TrimSpace(request.Prompt), 240)
	worker.WorkerExecutorPromptSummary = previewString(strings.TrimSpace(request.Prompt), 240)
	worker.WorkerResultSummary = ""
	worker.WorkerErrorSummary = ""
	worker.ExecutorTurnStatus = string(executor.TurnStatusInProgress)
	worker.ExecutorApprovalState = ""
	worker.ExecutorApprovalKind = ""
	worker.ExecutorApprovalPreview = ""
	worker.ExecutorInterruptible = true
	worker.ExecutorSteerable = true
	worker.ExecutorFailureStage = ""
	worker.ExecutorLastControlAction = ""
	worker.ExecutorApproval = nil
	worker.ExecutorLastControl = nil
	worker.StartedAt = time.Now().UTC()
	worker.CompletedAt = time.Time{}
	worker.UpdatedAt = time.Now().UTC()
	if err := store.SaveWorker(ctx, worker); err != nil {
		return controlWorkerDispatchSnapshot{}, err
	}

	execClient, err := newWorkerExecutorClient(inv.Version)
	if err != nil {
		return controlWorkerDispatchSnapshot{}, err
	}

	executorResult, execErr := execClient.Execute(ctx, executor.TurnRequest{
		RunID:    run.ID,
		RepoPath: worker.WorktreePath,
		Prompt:   strings.TrimSpace(request.Prompt),
	})

	switch {
	case executorResult.ApprovalState == executor.ApprovalStateRequired || executorResult.TurnStatus == executor.TurnStatusApprovalRequired:
		worker.WorkerStatus = state.WorkerStatusApprovalRequired
		worker.WorkerResultSummary = workerExecutorApprovalMessage(executorResult)
		worker.WorkerErrorSummary = ""
	case execErr != nil || executorResult.TurnStatus == executor.TurnStatusFailed || executorResult.TurnStatus == executor.TurnStatusInterrupted:
		worker.WorkerStatus = state.WorkerStatusFailed
		worker.WorkerResultSummary = workerDispatchResultMessage(executorResult, execErr)
		worker.WorkerErrorSummary = worker.WorkerResultSummary
		worker.CompletedAt = time.Now().UTC()
	default:
		worker.WorkerStatus = state.WorkerStatusCompleted
		worker.WorkerResultSummary = workerDispatchResultMessage(executorResult, nil)
		worker.WorkerErrorSummary = ""
		if executorResult.CompletedAt.IsZero() {
			worker.CompletedAt = time.Now().UTC()
		} else {
			worker.CompletedAt = executorResult.CompletedAt.UTC()
		}
	}
	worker.ExecutorThreadID = strings.TrimSpace(executorResult.ThreadID)
	worker.ExecutorTurnID = strings.TrimSpace(executorResult.TurnID)
	worker.ExecutorTurnStatus = string(executorResult.TurnStatus)
	worker.ExecutorApprovalState = string(executorResult.ApprovalState)
	worker.ExecutorApprovalKind = workerExecutorApprovalKind(executorResult)
	worker.ExecutorApprovalPreview = workerExecutorApprovalPreview(executorResult)
	worker.ExecutorInterruptible = executorResult.Interruptible
	worker.ExecutorSteerable = executorResult.Steerable
	worker.ExecutorFailureStage = workerExecutorFailureStage(executorResult)
	worker.ExecutorApproval = workerExecutorApproval(executorResult)
	if strings.TrimSpace(worker.ExecutorTurnStatus) == "" {
		switch worker.WorkerStatus {
		case state.WorkerStatusCompleted:
			worker.ExecutorTurnStatus = string(executor.TurnStatusCompleted)
		case state.WorkerStatusApprovalRequired:
			worker.ExecutorTurnStatus = string(executor.TurnStatusApprovalRequired)
		default:
			worker.ExecutorTurnStatus = string(executor.TurnStatusFailed)
		}
	}
	worker.UpdatedAt = time.Now().UTC()
	if err := store.SaveWorker(ctx, worker); err != nil {
		return controlWorkerDispatchSnapshot{}, err
	}

	appendWorkerDispatchEvent(journalWriter, run, worker, executorResult, execErr)
	emitEngineEvent(inv, "worker_dispatch_completed", eventPayloadForRun(run, map[string]any{
		"worker_id":            worker.ID,
		"worker_name":          worker.WorkerName,
		"worker_status":        string(worker.WorkerStatus),
		"executor_thread_id":   worker.ExecutorThreadID,
		"executor_turn_id":     worker.ExecutorTurnID,
		"executor_turn_status": worker.ExecutorTurnStatus,
		"approval_required":    workerApprovalRequired(worker),
	}))

	message := firstNonEmptyValue(worker.WorkerResultSummary, "worker executor dispatch completed")
	return controlWorkerDispatchSnapshot{
		Dispatched: true,
		RunID:      run.ID,
		Message:    message,
		Worker:     controlWorkerSnapshotFromWorker(worker),
	}, nil
}

func removeWorkerForControl(ctx context.Context, inv Invocation, request control.RemoveWorkerRequest) (controlWorkerRemoveSnapshot, error) {
	if strings.TrimSpace(request.WorkerID) == "" {
		return controlWorkerRemoveSnapshot{}, errors.New("worker_id is required")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return controlWorkerRemoveSnapshot{}, err
	}
	defer store.Close()

	worker, found, err := store.GetWorker(ctx, strings.TrimSpace(request.WorkerID))
	if err != nil {
		return controlWorkerRemoveSnapshot{}, err
	}
	if !found {
		return controlWorkerRemoveSnapshot{}, errors.New("worker not found")
	}

	run, _, err := store.GetRun(ctx, worker.RunID)
	if err != nil {
		return controlWorkerRemoveSnapshot{}, err
	}
	if state.IsWorkerActive(worker.WorkerStatus) {
		message := "active workers cannot be removed until they are idle or completed"
		appendWorkerEvent(journalWriter, "worker.remove.failed", run, &worker, message)
		return controlWorkerRemoveSnapshot{}, errors.New(message)
	}

	manager := workerctl.NewManager(run.RepoPath, inv.Layout.WorkersDir)
	if err := manager.Remove(ctx, worker.WorktreePath); err != nil {
		appendWorkerEvent(journalWriter, "worker.remove.failed", run, &worker, err.Error())
		return controlWorkerRemoveSnapshot{}, err
	}
	if err := store.DeleteWorker(ctx, worker.ID); err != nil {
		appendWorkerEvent(journalWriter, "worker.remove.failed", run, &worker, err.Error())
		return controlWorkerRemoveSnapshot{}, err
	}

	appendWorkerEvent(journalWriter, "worker.removed", run, &worker, "worker registry entry and isolated worktree removed")
	emitEngineEvent(inv, "worker_removed", eventPayloadForRun(run, map[string]any{
		"worker_id":    worker.ID,
		"worker_name":  worker.WorkerName,
		"worker_path":  worker.WorktreePath,
		"worker_scope": worker.AssignedScope,
	}))

	return controlWorkerRemoveSnapshot{
		Removed:      true,
		RunID:        run.ID,
		WorkerID:     worker.ID,
		WorkerName:   worker.WorkerName,
		WorktreePath: worker.WorktreePath,
		Message:      "worker registry entry and isolated worktree removed",
	}, nil
}

func integrateWorkersForControl(ctx context.Context, inv Invocation, request control.IntegrateWorkersRequest) (controlWorkerIntegrateSnapshot, error) {
	workerIDs := make([]string, 0, len(request.WorkerIDs))
	for _, workerID := range request.WorkerIDs {
		if trimmed := strings.TrimSpace(workerID); trimmed != "" {
			workerIDs = append(workerIDs, trimmed)
		}
	}
	if len(workerIDs) == 0 {
		return controlWorkerIntegrateSnapshot{}, errors.New("worker_ids is required")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return controlWorkerIntegrateSnapshot{}, err
	}
	defer store.Close()

	selected := make([]state.Worker, 0, len(workerIDs))
	var run state.Run
	for idx, workerID := range workerIDs {
		worker, found, err := store.GetWorker(ctx, workerID)
		if err != nil {
			return controlWorkerIntegrateSnapshot{}, err
		}
		if !found {
			return controlWorkerIntegrateSnapshot{}, fmt.Errorf("worker %s not found", workerID)
		}
		loadedRun, found, err := store.GetRun(ctx, worker.RunID)
		if err != nil {
			return controlWorkerIntegrateSnapshot{}, err
		}
		if !found {
			return controlWorkerIntegrateSnapshot{}, fmt.Errorf("worker run %s not found", worker.RunID)
		}
		if idx == 0 {
			run = loadedRun
		} else if loadedRun.ID != run.ID {
			return controlWorkerIntegrateSnapshot{}, errors.New("integrate_workers requires workers from the same run")
		}
		selected = append(selected, worker)
	}

	if journalWriter != nil {
		_ = journalWriter.Append(journal.Event{
			Type:     "integration.started",
			RunID:    run.ID,
			RepoPath: run.RepoPath,
			Goal:     run.Goal,
			Status:   string(run.Status),
			Message:  fmt.Sprintf("building integration preview from %d worker(s)", len(selected)),
		})
	}

	summary, err := workerctl.BuildIntegrationSummary(run.RepoPath, selected)
	if err != nil {
		if journalWriter != nil {
			_ = journalWriter.Append(journal.Event{
				Type:     "integration.failed",
				RunID:    run.ID,
				RepoPath: run.RepoPath,
				Goal:     run.Goal,
				Status:   string(run.Status),
				Message:  err.Error(),
			})
		}
		return controlWorkerIntegrateSnapshot{}, err
	}

	artifactPath, artifactPreview := orchestration.WriteIntegrationArtifact(run, summary)
	if strings.TrimSpace(artifactPath) == "" {
		message := valueOrUnavailable(strings.TrimSpace(artifactPreview))
		if journalWriter != nil {
			_ = journalWriter.Append(journal.Event{
				Type:     "integration.failed",
				RunID:    run.ID,
				RepoPath: run.RepoPath,
				Goal:     run.Goal,
				Status:   string(run.Status),
				Message:  message,
			})
		}
		return controlWorkerIntegrateSnapshot{}, errors.New(message)
	}

	if journalWriter != nil {
		_ = journalWriter.Append(journal.Event{
			Type:            "integration.completed",
			RunID:           run.ID,
			RepoPath:        run.RepoPath,
			Goal:            run.Goal,
			Status:          string(run.Status),
			Message:         summary.IntegrationPreview,
			ArtifactPath:    artifactPath,
			ArtifactPreview: artifactPreview,
		})
	}
	emitEngineEvent(inv, "worker_integration_completed", eventPayloadForRun(run, map[string]any{
		"worker_ids":               summary.WorkerIDs,
		"worker_count":             len(selected),
		"artifact_path":            artifactPath,
		"integration_preview":      summary.IntegrationPreview,
		"conflict_candidate_count": len(summary.ConflictCandidates),
	}))

	return controlWorkerIntegrateSnapshot{
		RunID:              run.ID,
		WorkerIDs:          append([]string(nil), summary.WorkerIDs...),
		WorkerCount:        len(selected),
		ArtifactPath:       artifactPath,
		ArtifactPreview:    artifactPreview,
		IntegrationPreview: summary.IntegrationPreview,
		ConflictCount:      len(summary.ConflictCandidates),
		ConflictCandidates: append([]state.ConflictCandidate(nil), summary.ConflictCandidates...),
		Message:            summary.IntegrationPreview,
	}, nil
}
