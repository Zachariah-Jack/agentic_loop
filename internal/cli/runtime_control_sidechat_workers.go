package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"orchestrator/internal/control"
	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

const sideChatAgentMessage = "side chat answered from observable runtime context only; it did not alter the active run"

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
	var latestRun *state.Run
	if run, found, err := store.LatestRun(ctx); err != nil {
		return controlSideChatSendSnapshot{}, err
	} else if found && strings.EqualFold(strings.TrimSpace(run.RepoPath), strings.TrimSpace(repoRoot)) {
		runID = run.ID
		runCopy := run
		latestRun = &runCopy
	}
	reply := buildSideChatAgentReply(inv, latestRun, request.Message)

	recorded, err := store.RecordSideChatMessage(ctx, state.CreateSideChatMessageParams{
		RepoPath:        repoRoot,
		RunID:           runID,
		Source:          "side_chat",
		ContextPolicy:   strings.TrimSpace(request.ContextPolicy),
		RawText:         request.Message,
		BackendState:    "context_agent",
		ResponseMessage: reply,
	})
	if err != nil {
		return controlSideChatSendSnapshot{}, err
	}

	emitEngineEvent(inv, "side_chat_message_recorded", map[string]any{
		"repo_path":        repoRoot,
		"run_id":           runID,
		"side_chat_id":     recorded.ID,
		"context_policy":   strings.TrimSpace(recorded.ContextPolicy),
		"backend_state":    recorded.BackendState,
		"message_preview":  previewString(recorded.RawText, 240),
		"response_preview": previewString(recorded.ResponseMessage, 240),
	})

	entry := controlSideChatSnapshotFromState(recorded)
	return controlSideChatSendSnapshot{
		Available: true,
		Stored:    true,
		Message:   sideChatAgentMessage,
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
		snapshot.Message = "no side chat conversation is recorded yet; Side Chat answers from observable runtime context and does not affect the active run"
	}
	return snapshot, nil
}

func sideChatContextSnapshot(ctx context.Context, inv Invocation, request control.SideChatContextSnapshotRequest) (controlSideChatContextSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlSideChatContextSnapshot{}, err
	}

	status, err := buildControlStatusSnapshot(ctx, inv, strings.TrimSpace(request.RunID))
	if err != nil {
		return controlSideChatContextSnapshot{}, err
	}
	runID := strings.TrimSpace(request.RunID)
	if runID == "" && status.Run != nil {
		runID = strings.TrimSpace(status.Run.ID)
	}

	limit := request.Limit
	if limit <= 0 {
		limit = 10
	}
	messages, err := listSideChatMessages(ctx, inv, control.ListSideChatMessagesRequest{
		RepoPath: repoRoot,
		Limit:    limit,
	})
	if err != nil {
		return controlSideChatContextSnapshot{}, err
	}

	recentEvents := []controlSideChatEventSnapshot{}
	if runID != "" {
		events, err := latestRunEvents(inv.Layout, runID, limit)
		if err != nil {
			return controlSideChatContextSnapshot{}, err
		}
		recentEvents = sideChatEventSnapshots(events)
	}

	return controlSideChatContextSnapshot{
		Available: true,
		RepoPath:  repoRoot,
		RunID:     runID,
		VisibleContext: []string{
			"current repo",
			"latest run/status",
			"planner status summary",
			"executor status summary",
			"pending action and approval state",
			"worker/sub-agent status",
			"timeout settings",
			"permission mode",
			"update/model health status",
			"recent observable events",
		},
		Status:         status,
		RecentMessages: messages.Items,
		RecentEvents:   recentEvents,
		Message:        "side chat context snapshot contains only observable runtime state and persisted user-visible messages",
	}, nil
}

func requestSideChatAction(ctx context.Context, inv Invocation, request control.SideChatActionRequest) (controlSideChatActionSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlSideChatActionSnapshot{}, err
	}
	action := normalizeSideChatAction(request.Action)
	if action == "" {
		return controlSideChatActionSnapshot{}, errors.New("side chat action is required")
	}
	if !pathExists(inv.Layout.DBPath) {
		return controlSideChatActionSnapshot{
			Available: false,
			Stored:    false,
			Action:    action,
			Status:    string(state.SideChatActionUnsupported),
			Message:   "side chat actions are unavailable because runtime state has not been initialized yet",
		}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlSideChatActionSnapshot{
				Available: false,
				Stored:    false,
				Action:    action,
				Status:    string(state.SideChatActionUnsupported),
				Message:   "side chat actions are unavailable because runtime state has not been initialized yet",
			}, nil
		}
		return controlSideChatActionSnapshot{}, err
	}
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		return controlSideChatActionSnapshot{}, err
	}

	source := strings.TrimSpace(request.Source)
	if source == "" {
		source = "side_chat"
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "side_chat_action_request"
	}
	runID := strings.TrimSpace(request.RunID)
	if runID == "" {
		if run, found, err := store.LatestRun(ctx); err != nil {
			return controlSideChatActionSnapshot{}, err
		} else if found && strings.EqualFold(strings.TrimSpace(run.RepoPath), strings.TrimSpace(repoRoot)) {
			runID = strings.TrimSpace(run.ID)
		}
	}

	switch action {
	case "request_latest_status", "context_snapshot":
		snapshot, err := sideChatContextSnapshot(ctx, inv, control.SideChatContextSnapshotRequest{
			RepoPath: repoRoot,
			RunID:    runID,
			Limit:    10,
		})
		if err != nil {
			return controlSideChatActionSnapshot{}, err
		}
		recorded, err := store.RecordSideChatAction(ctx, state.CreateSideChatActionParams{
			RepoPath:      repoRoot,
			RunID:         snapshot.RunID,
			Action:        action,
			RequestText:   request.Message,
			Source:        source,
			Reason:        reason,
			Status:        state.SideChatActionCompleted,
			ResultMessage: "returned current side-chat context snapshot",
			CreatedAt:     time.Now().UTC(),
		})
		if err != nil {
			return controlSideChatActionSnapshot{}, err
		}
		entry := controlSideChatActionSnapshotFromState(recorded)
		return controlSideChatActionSnapshot{
			Available: true,
			Stored:    true,
			Action:    action,
			Status:    string(recorded.Status),
			Message:   recorded.ResultMessage,
			Entry:     &entry,
			Context:   &snapshot,
		}, nil
	case "queue_planner_note", "ask_planner_question", "ask_planner_reconsider", "inject_control_message":
		if strings.TrimSpace(request.Message) == "" {
			return controlSideChatActionSnapshot{}, errors.New("side chat planner action message is required")
		}
		cfg := currentConfig(inv)
		if cfg.Permissions.AskBeforePlannerDirection && !request.Approved {
			recorded, err := store.RecordSideChatAction(ctx, state.CreateSideChatActionParams{
				RepoPath:      repoRoot,
				RunID:         runID,
				Action:        action,
				RequestText:   request.Message,
				Source:        source,
				Reason:        reason,
				Status:        state.SideChatActionApprovalRequired,
				ResultMessage: "permission profile requires explicit approval before side chat forwards planner-direction notes",
				CreatedAt:     time.Now().UTC(),
			})
			if err != nil {
				return controlSideChatActionSnapshot{}, err
			}
			entry := controlSideChatActionSnapshotFromState(recorded)
			return controlSideChatActionSnapshot{
				Available:        true,
				Stored:           true,
				RequiresApproval: true,
				Action:           action,
				Status:           string(recorded.Status),
				Message:          recorded.ResultMessage,
				Entry:            &entry,
			}, nil
		}
		message, err := injectControlMessage(ctx, inv, control.InjectControlMessageRequest{
			RunID:   runID,
			Message: request.Message,
			Source:  source,
			Reason:  reason,
		})
		if err != nil {
			recorded, recordErr := store.RecordSideChatAction(ctx, state.CreateSideChatActionParams{
				RepoPath:      repoRoot,
				RunID:         runID,
				Action:        action,
				RequestText:   request.Message,
				Source:        source,
				Reason:        reason,
				Status:        state.SideChatActionFailed,
				ResultMessage: err.Error(),
				CreatedAt:     time.Now().UTC(),
			})
			if recordErr != nil {
				return controlSideChatActionSnapshot{}, recordErr
			}
			entry := controlSideChatActionSnapshotFromState(recorded)
			return controlSideChatActionSnapshot{
				Available: true,
				Stored:    true,
				Action:    action,
				Status:    string(recorded.Status),
				Message:   recorded.ResultMessage,
				Entry:     &entry,
			}, nil
		}
		recorded, err := store.RecordSideChatAction(ctx, state.CreateSideChatActionParams{
			RepoPath:         repoRoot,
			RunID:            message.RunID,
			Action:           action,
			RequestText:      request.Message,
			Source:           source,
			Reason:           reason,
			Status:           state.SideChatActionCompleted,
			ResultMessage:    "queued raw side-chat note for planner-visible control chat at the next safe point",
			ControlMessageID: message.ID,
			CreatedAt:        time.Now().UTC(),
		})
		if err != nil {
			return controlSideChatActionSnapshot{}, err
		}
		entry := controlSideChatActionSnapshotFromState(recorded)
		return controlSideChatActionSnapshot{
			Available:      true,
			Stored:         true,
			Action:         action,
			Status:         string(recorded.Status),
			Message:        recorded.ResultMessage,
			Entry:          &entry,
			ControlMessage: &message,
		}, nil
	case "request_safe_stop", "safe_stop":
		stopFlag, err := setControlStopFlag(inv, firstNonEmpty(strings.TrimSpace(request.Message), "side_chat_requested_safe_stop"))
		if err != nil {
			return controlSideChatActionSnapshot{}, err
		}
		recorded, err := store.RecordSideChatAction(ctx, state.CreateSideChatActionParams{
			RepoPath:      repoRoot,
			RunID:         runID,
			Action:        action,
			RequestText:   request.Message,
			Source:        source,
			Reason:        reason,
			Status:        state.SideChatActionCompleted,
			ResultMessage: "requested safe stop; the loop will stop at the next planner-safe pause point",
			CreatedAt:     time.Now().UTC(),
		})
		if err != nil {
			return controlSideChatActionSnapshot{}, err
		}
		entry := controlSideChatActionSnapshotFromState(recorded)
		return controlSideChatActionSnapshot{
			Available: true,
			Stored:    true,
			Action:    action,
			Status:    string(recorded.Status),
			Message:   recorded.ResultMessage,
			Entry:     &entry,
			StopFlag:  &stopFlag,
		}, nil
	case "clear_safe_stop":
		stopFlag, err := clearControlStopFlag(inv)
		if err != nil {
			return controlSideChatActionSnapshot{}, err
		}
		recorded, err := store.RecordSideChatAction(ctx, state.CreateSideChatActionParams{
			RepoPath:      repoRoot,
			RunID:         runID,
			Action:        action,
			RequestText:   request.Message,
			Source:        source,
			Reason:        reason,
			Status:        state.SideChatActionCompleted,
			ResultMessage: "cleared the pending safe-stop flag",
			CreatedAt:     time.Now().UTC(),
		})
		if err != nil {
			return controlSideChatActionSnapshot{}, err
		}
		entry := controlSideChatActionSnapshotFromState(recorded)
		return controlSideChatActionSnapshot{
			Available: true,
			Stored:    true,
			Action:    action,
			Status:    string(recorded.Status),
			Message:   recorded.ResultMessage,
			Entry:     &entry,
			StopFlag:  &stopFlag,
		}, nil
	default:
		recorded, err := store.RecordSideChatAction(ctx, state.CreateSideChatActionParams{
			RepoPath:      repoRoot,
			RunID:         runID,
			Action:        action,
			RequestText:   request.Message,
			Source:        source,
			Reason:        reason,
			Status:        state.SideChatActionUnsupported,
			ResultMessage: fmt.Sprintf("side chat action %q is not supported by this backend", action),
			CreatedAt:     time.Now().UTC(),
		})
		if err != nil {
			return controlSideChatActionSnapshot{}, err
		}
		entry := controlSideChatActionSnapshotFromState(recorded)
		return controlSideChatActionSnapshot{
			Available: true,
			Stored:    true,
			Action:    action,
			Status:    string(recorded.Status),
			Message:   recorded.ResultMessage,
			Entry:     &entry,
		}, nil
	}
}

func buildSideChatAgentReply(inv Invocation, run *state.Run, question string) string {
	question = strings.ToLower(strings.TrimSpace(question))
	cfg := currentConfig(inv)
	pieces := []string{
		"Side Chat can see the current repo, latest run summary, model health/config status, timeout settings, and recent recorded activity. It does not change the run unless you explicitly promote a message to Control Chat or use a control button.",
	}
	if run == nil || strings.TrimSpace(run.ID) == "" {
		pieces = append(pieces, fmt.Sprintf("Current repo: %s. No run is recorded yet for this repo.", inv.RepoRoot))
	} else {
		pieces = append(pieces, fmt.Sprintf("Current repo: %s.", inv.RepoRoot))
		pieces = append(pieces, fmt.Sprintf("Latest run: %s (%s). Goal: %s.", run.ID, run.Status, previewString(run.Goal, 220)))
		if strings.TrimSpace(run.LatestStopReason) != "" {
			pieces = append(pieces, fmt.Sprintf("Latest stop reason: %s.", run.LatestStopReason))
		}
		if run.PlannerOperatorStatus != nil && strings.TrimSpace(run.PlannerOperatorStatus.OperatorMessage) != "" {
			pieces = append(pieces, "Planner message: "+previewString(run.PlannerOperatorStatus.OperatorMessage, 360))
		}
		if strings.TrimSpace(run.ExecutorTurnStatus) != "" {
			pieces = append(pieces, fmt.Sprintf("Executor turn status: %s.", run.ExecutorTurnStatus))
		}
		if strings.TrimSpace(run.ExecutorLastError) != "" {
			pieces = append(pieces, "Latest executor error: "+previewString(run.ExecutorLastError, 360))
		}
	}
	pieces = append(pieces, fmt.Sprintf("Timeouts: executor_turn_timeout=%s, human_wait_timeout=%s.", cfg.Timeouts.ExecutorTurnTimeout, cfg.Timeouts.HumanWaitTimeout))
	pieces = append(pieces, "Permission mode: "+cfg.Permissions.Profile+".")
	if strings.Contains(question, "safe stop") || strings.Contains(question, "stop") {
		pieces = append(pieces, "If you want to stop the live loop, use Safe Stop in Control. Side Chat will not request it on its own.")
	}
	if strings.Contains(question, "planner") || strings.Contains(question, "reconsider") {
		pieces = append(pieces, "To ask the planner to reconsider, promote a note to Control Chat so it is forwarded raw at the next safe point.")
	}
	return strings.Join(pieces, "\n\n")
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

func normalizeSideChatAction(action string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(action), "-", "_"))
}

func sideChatEventSnapshots(events []journal.Event) []controlSideChatEventSnapshot {
	items := make([]controlSideChatEventSnapshot, 0, len(events))
	for _, event := range events {
		items = append(items, controlSideChatEventSnapshot{
			At:                 formatSnapshotTime(event.At),
			Type:               strings.TrimSpace(event.Type),
			Message:            previewString(strings.TrimSpace(event.Message), 240),
			PlannerOutcome:     strings.TrimSpace(event.PlannerOutcome),
			ExecutorTurnStatus: strings.TrimSpace(event.ExecutorTurnStatus),
			ArtifactPath:       strings.TrimSpace(event.ArtifactPath),
			StopReason:         strings.TrimSpace(event.StopReason),
		})
	}
	return items
}

func controlSideChatActionSnapshotFromState(action state.SideChatAction) controlSideChatActionEntrySnapshot {
	return controlSideChatActionEntrySnapshot{
		ID:               action.ID,
		RepoPath:         strings.TrimSpace(action.RepoPath),
		RunID:            strings.TrimSpace(action.RunID),
		Action:           strings.TrimSpace(action.Action),
		RequestText:      action.RequestText,
		Source:           strings.TrimSpace(action.Source),
		Reason:           strings.TrimSpace(action.Reason),
		Status:           string(action.Status),
		ResultMessage:    strings.TrimSpace(action.ResultMessage),
		ControlMessageID: strings.TrimSpace(action.ControlMessageID),
		CreatedAt:        formatSnapshotTime(action.CreatedAt),
		UpdatedAt:        formatSnapshotTime(action.UpdatedAt),
	}
}
