package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"orchestrator/internal/control"
	"orchestrator/internal/state"
)

const sideChatUnavailableMessage = "side chat backend is not implemented in this slice; the message was recorded only and did not affect the active run"

func sendSideChatMessage(ctx context.Context, inv Invocation, request control.SideChatRequest) (controlSideChatSendSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlSideChatSendSnapshot{}, err
	}
	if strings.TrimSpace(request.Message) == "" {
		return controlSideChatSendSnapshot{}, errors.New("side chat message is required")
	}
	if !pathExists(inv.Layout.DBPath) {
		return controlSideChatSendSnapshot{
			Available: false,
			Stored:    false,
			Message:   "side chat storage is unavailable because runtime state has not been initialized yet",
		}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlSideChatSendSnapshot{
				Available: false,
				Stored:    false,
				Message:   "side chat storage is unavailable because runtime state has not been initialized yet",
			}, nil
		}
		return controlSideChatSendSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlSideChatSendSnapshot{}, err
	}

	runID := ""
	if run, found, err := store.LatestRun(ctx); err != nil {
		return controlSideChatSendSnapshot{}, err
	} else if found && strings.EqualFold(strings.TrimSpace(run.RepoPath), strings.TrimSpace(repoRoot)) {
		runID = run.ID
	}

	recorded, err := store.RecordSideChatMessage(ctx, state.CreateSideChatMessageParams{
		RepoPath:        repoRoot,
		RunID:           runID,
		Source:          "side_chat",
		ContextPolicy:   strings.TrimSpace(request.ContextPolicy),
		RawText:         request.Message,
		BackendState:    "unavailable",
		ResponseMessage: sideChatUnavailableMessage,
	})
	if err != nil {
		return controlSideChatSendSnapshot{}, err
	}

	emitEngineEvent(inv, "side_chat_message_recorded", map[string]any{
		"repo_path":       repoRoot,
		"run_id":          runID,
		"side_chat_id":    recorded.ID,
		"context_policy":  strings.TrimSpace(recorded.ContextPolicy),
		"backend_state":   recorded.BackendState,
		"message_preview": previewString(recorded.RawText, 240),
	})

	entry := controlSideChatSnapshotFromState(recorded)
	return controlSideChatSendSnapshot{
		Available: false,
		Stored:    true,
		Message:   sideChatUnavailableMessage,
		Entry:     &entry,
	}, nil
}

func listSideChatMessages(ctx context.Context, inv Invocation, request control.ListSideChatMessagesRequest) (controlSideChatListSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlSideChatListSnapshot{}, err
	}
	if !pathExists(inv.Layout.DBPath) {
		return controlSideChatListSnapshot{
			Available: false,
			Count:     0,
			Message:   "side chat storage is unavailable because runtime state has not been initialized yet",
		}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlSideChatListSnapshot{
				Available: false,
				Count:     0,
				Message:   "side chat storage is unavailable because runtime state has not been initialized yet",
			}, nil
		}
		return controlSideChatListSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlSideChatListSnapshot{}, err
	}

	messages, err := store.ListSideChatMessages(ctx, repoRoot, request.Limit)
	if err != nil {
		return controlSideChatListSnapshot{}, err
	}

	items := make([]controlSideChatMessageSnapshot, 0, len(messages))
	for _, message := range messages {
		items = append(items, controlSideChatSnapshotFromState(message))
	}

	snapshot := controlSideChatListSnapshot{
		Available: true,
		Count:     len(items),
		Items:     items,
	}
	if len(items) == 0 {
		snapshot.Message = "no side chat messages are recorded yet; backend replies are still not implemented in this slice"
	}
	return snapshot, nil
}

func listWorkersForControl(ctx context.Context, inv Invocation, request control.ListWorkersRequest) (controlWorkerListSnapshot, error) {
	if !pathExists(inv.Layout.DBPath) {
		return controlWorkerListSnapshot{
			Count:   0,
			Message: "runtime state is not initialized for worker inspection",
		}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlWorkerListSnapshot{
				Count:   0,
				Message: "runtime state is not initialized for worker inspection",
			}, nil
		}
		return controlWorkerListSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlWorkerListSnapshot{}, err
	}

	run, found, err := resolveControlRun(ctx, store, strings.TrimSpace(request.RunID))
	if err != nil {
		return controlWorkerListSnapshot{}, err
	}
	if !found {
		return controlWorkerListSnapshot{
			Count:   0,
			Message: "no run is available for worker inspection",
		}, nil
	}

	workers, err := store.ListWorkers(ctx, run.ID)
	if err != nil {
		return controlWorkerListSnapshot{}, err
	}

	counts := map[string]int{}
	for _, worker := range workers {
		counts[string(worker.WorkerStatus)]++
	}
	total := len(workers)
	if request.Limit > 0 && len(workers) > request.Limit {
		workers = workers[:request.Limit]
	}
	items := make([]controlWorkerSnapshot, 0, len(workers))
	for _, worker := range workers {
		items = append(items, controlWorkerSnapshotFromWorker(worker))
	}

	snapshot := controlWorkerListSnapshot{
		Count:          total,
		CountsByStatus: counts,
		Items:          items,
	}
	if len(items) == 0 {
		snapshot.Message = fmt.Sprintf("no workers are recorded for run %s", run.ID)
	}
	return snapshot, nil
}

func controlWorkerSnapshotFromWorker(worker state.Worker) controlWorkerSnapshot {
	return controlWorkerSnapshot{
		WorkerID:          worker.ID,
		WorkerName:        worker.WorkerName,
		Status:            string(worker.WorkerStatus),
		Scope:             strings.TrimSpace(worker.AssignedScope),
		WorktreePath:      strings.TrimSpace(worker.WorktreePath),
		ApprovalRequired:  workerApprovalRequired(worker),
		ApprovalKind:      strings.TrimSpace(worker.ExecutorApprovalKind),
		ApprovalPreview:   previewString(strings.TrimSpace(worker.ExecutorApprovalPreview), 240),
		ExecutorThreadID:  strings.TrimSpace(worker.ExecutorThreadID),
		ExecutorTurnID:    strings.TrimSpace(worker.ExecutorTurnID),
		Interruptible:     workerTurnInterruptibleState(worker),
		Steerable:         workerTurnSteerableState(worker),
		LastControlAction: strings.TrimSpace(worker.ExecutorLastControlAction),
		WorkerTaskSummary: previewString(strings.TrimSpace(worker.WorkerTaskSummary), 240),
		WorkerResult:      previewString(strings.TrimSpace(worker.WorkerResultSummary), 240),
		WorkerError:       previewString(strings.TrimSpace(worker.WorkerErrorSummary), 240),
		UpdatedAt:         formatSnapshotTime(worker.UpdatedAt),
	}
}

func controlSideChatSnapshotFromState(message state.SideChatMessage) controlSideChatMessageSnapshot {
	return controlSideChatMessageSnapshot{
		ID:              message.ID,
		RepoPath:        strings.TrimSpace(message.RepoPath),
		RunID:           strings.TrimSpace(message.RunID),
		Source:          strings.TrimSpace(message.Source),
		ContextPolicy:   strings.TrimSpace(message.ContextPolicy),
		RawText:         message.RawText,
		Status:          string(message.Status),
		BackendState:    strings.TrimSpace(message.BackendState),
		ResponseMessage: strings.TrimSpace(message.ResponseMessage),
		CreatedAt:       formatSnapshotTime(message.CreatedAt),
		UpdatedAt:       formatSnapshotTime(message.UpdatedAt),
	}
}
