package orchestration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"orchestrator/internal/activity"
	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/planner"
	"orchestrator/internal/plugins"
	"orchestrator/internal/state"
	workerctl "orchestrator/internal/workers"
)

type Planner interface {
	Plan(context.Context, planner.InputEnvelope, string) (planner.Result, error)
}

type Executor interface {
	Execute(context.Context, executor.TurnRequest) (executor.TurnResult, error)
}

type HumanInput struct {
	Source  string
	Payload string
}

type HumanInteractor interface {
	Ask(context.Context, state.Run, planner.AskHumanOutcome) (HumanInput, error)
}

type Cycle struct {
	Store                      *state.Store
	Journal                    *journal.Journal
	Planner                    Planner
	Executor                   Executor
	HumanInteractor            HumanInteractor
	DriftWatcher               DriftWatcher
	DriftReviewOn              bool
	WorkerPlanConcurrencyLimit int
	Plugins                    *plugins.Manager
	Events                     activity.Publisher
}

type Result struct {
	Run                        state.Run
	FirstPlannerResult         planner.Result
	ReconsiderationPlannerTurn *planner.Result
	SecondPlannerTurn          *planner.Result
	PostExecutorPlannerTurn    *planner.Result
	ExecutorResult             *executor.TurnResult
	ExecutorDispatched         bool
}

func (c Cycle) emitEvent(name string, run state.Run, extra map[string]any) {
	if c.Events == nil {
		return
	}

	payload := map[string]any{}
	if strings.TrimSpace(run.ID) != "" {
		payload["run_id"] = run.ID
	}
	if strings.TrimSpace(run.RepoPath) != "" {
		payload["repo_path"] = run.RepoPath
	}
	if strings.TrimSpace(string(run.Status)) != "" {
		payload["status"] = string(run.Status)
	}
	if run.LatestCheckpoint.Sequence > 0 {
		payload["checkpoint"] = map[string]any{
			"sequence":   run.LatestCheckpoint.Sequence,
			"stage":      run.LatestCheckpoint.Stage,
			"label":      run.LatestCheckpoint.Label,
			"safe_pause": run.LatestCheckpoint.SafePause,
		}
	}
	for key, value := range extra {
		payload[key] = value
	}

	c.Events.Publish(name, payload)
}

const (
	maxCollectedFilePreviewBytes = 2048
	maxCollectedDirEntries       = 20
)

func (c Cycle) RunOnce(ctx context.Context, run state.Run) (result Result, err error) {
	if c.Store == nil {
		return Result{}, errors.New("store is required")
	}
	if c.Journal == nil {
		return Result{}, errors.New("journal is required")
	}
	if c.Planner == nil {
		return Result{}, errors.New("planner is required")
	}
	defer func() {
		finalRun := result.Run
		if finalRun.ID == "" {
			finalRun = run
		}
		c.runPluginHooks(ctx, plugins.HookRunEnd, finalRun, preferredPlannerResult(result), result.ExecutorResult, StopReasonForBoundedCycle(result, err), err)
	}()
	c.runPluginHooks(ctx, plugins.HookRunStart, run, nil, nil, "", nil)
	if hasActiveExecutorTurn(run) {
		return c.continueExecutorTurn(ctx, run)
	}
	activeWorkerTurns, err := c.activeWorkerTurns(ctx, run.ID)
	if err != nil {
		return Result{}, err
	}
	if len(activeWorkerTurns) > 0 {
		continuedRun, stopNow, err := c.continueWorkerPlan(ctx, run, activeWorkerTurns)
		if err != nil {
			return Result{Run: continuedRun}, err
		}
		run = continuedRun
		if stopNow {
			return Result{Run: run}, nil
		}
	}

	initialPendingAction, initialPendingPresent, err := c.Store.GetPendingAction(ctx, run.ID)
	if err != nil {
		return Result{}, err
	}
	initialControlMessage, hasInitialIntervention, err := c.Store.NextQueuedControlMessage(ctx, run.ID)
	if err != nil {
		return Result{}, err
	}

	recentEvents, err := c.Journal.ReadRecent(run.ID, 5)
	if err != nil {
		return Result{}, err
	}

	firstInput := BuildPlannerInput(run, recentEvents, nil, nil, nil, c.pluginToolDescriptors())
	if hasInitialIntervention {
		firstInput.PendingAction = mapPendingActionToPlanner(initialPendingAction, initialPendingPresent)
		firstInput.ControlIntervention = mapControlMessageToPlanner(initialControlMessage, "queued_control_message_before_planner_turn")
		c.emitEvent("safe_point_intervention_pending", run, map[string]any{
			"control_message_id": initialControlMessage.ID,
			"source":             initialControlMessage.Source,
			"reason":             initialControlMessage.Reason,
			"message_preview":    previewString(initialControlMessage.RawText, 240),
		})
		c.emitEvent("planner_intervention_turn_started", run, map[string]any{
			"phase":              "initial",
			"control_message_id": initialControlMessage.ID,
			"source":             initialControlMessage.Source,
		})
	}
	c.emitEvent("planner_turn_started", run, map[string]any{
		"phase":                "initial",
		"control_intervention": hasInitialIntervention,
	})
	firstPlannerResult, err := c.Planner.Plan(ctx, firstInput, run.PreviousResponseID)
	if err != nil {
		stopReason := StopReasonForError(err)
		artifactPath, artifactPreview := c.persistPlannerValidationArtifact(run, err)

		failedRun, issueErr := c.persistRuntimeIssue(ctx, run, stopReason, err.Error())
		if issueErr != nil {
			return Result{Run: run}, errors.Join(err, issueErr)
		}

		_ = c.Journal.Append(journal.Event{
			Type:            "planner.turn.failed",
			RunID:           failedRun.ID,
			RepoPath:        failedRun.RepoPath,
			Goal:            failedRun.Goal,
			Status:          string(failedRun.Status),
			Message:         err.Error(),
			StopReason:      stopReason,
			ArtifactPath:    artifactPath,
			ArtifactPreview: artifactPreview,
			Checkpoint:      checkpointRef(&failedRun.LatestCheckpoint),
		})
		return Result{Run: failedRun}, err
	}

	firstPlannerCheckpoint := state.Checkpoint{
		Sequence:     run.LatestCheckpoint.Sequence + 1,
		Stage:        "planner",
		Label:        plannerCheckpointLabel(firstPlannerResult.Output, "planner_turn_completed"),
		SafePause:    true,
		PlannerTurn:  run.LatestCheckpoint.PlannerTurn + 1,
		ExecutorTurn: run.LatestCheckpoint.ExecutorTurn,
		CreatedAt:    time.Now().UTC(),
	}

	if err := c.savePlannerTurn(ctx, run.ID, firstPlannerResult, firstPlannerCheckpoint); err != nil {
		return Result{}, err
	}

	updatedRun, found, err := c.Store.GetRun(ctx, run.ID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Result{}, fmt.Errorf("updated run %s could not be reloaded", run.ID)
	}

	firstPlannerMessage := "planner response validated and persisted"
	firstCheckpointMessage := "planner checkpoint persisted"
	if ShouldMarkRunCompleted(firstPlannerResult.Output) {
		firstPlannerMessage = "planner declared completion and run marked completed"
		firstCheckpointMessage = "completion checkpoint persisted"
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "planner.turn.completed",
		RunID:              updatedRun.ID,
		RepoPath:           updatedRun.RepoPath,
		Goal:               updatedRun.Goal,
		Status:             string(updatedRun.Status),
		Message:            firstPlannerMessage,
		ResponseID:         firstPlannerResult.ResponseID,
		PreviousResponseID: updatedRun.PreviousResponseID,
		PlannerOutcome:     string(firstPlannerResult.Output.Outcome),
		Checkpoint:         checkpointRef(&firstPlannerCheckpoint),
	}); err != nil {
		return Result{}, err
	}
	c.emitEvent("planner_turn_completed", updatedRun, map[string]any{
		"phase":           "initial",
		"planner_outcome": string(firstPlannerResult.Output.Outcome),
		"response_id":     firstPlannerResult.ResponseID,
		"operator_status": plannerOperatorStatusEventPayload(firstPlannerResult.Output.OperatorStatus),
	})
	c.emitPlannerOperatorMessage(updatedRun, "initial", firstPlannerResult)

	if err := c.Journal.Append(journal.Event{
		Type:               "checkpoint.persisted",
		RunID:              updatedRun.ID,
		RepoPath:           updatedRun.RepoPath,
		Status:             string(updatedRun.Status),
		Message:            firstCheckpointMessage,
		ResponseID:         firstPlannerResult.ResponseID,
		PreviousResponseID: updatedRun.PreviousResponseID,
		PlannerOutcome:     string(firstPlannerResult.Output.Outcome),
		Checkpoint:         checkpointRef(&firstPlannerCheckpoint),
	}); err != nil {
		return Result{}, err
	}

	c.runPluginHooks(ctx, plugins.HookPlannerAfter, updatedRun, &firstPlannerResult, nil, "", nil)

	result = Result{
		Run:                updatedRun,
		FirstPlannerResult: firstPlannerResult,
	}

	if hasInitialIntervention {
		if err := c.Store.ConsumeControlMessage(ctx, initialControlMessage.ID, time.Now().UTC()); err != nil {
			return result, err
		}
		c.emitEvent("control_message_consumed", updatedRun, map[string]any{
			"control_message_id": initialControlMessage.ID,
			"source":             initialControlMessage.Source,
			"reason":             initialControlMessage.Reason,
		})
		c.emitEvent("planner_intervention_turn_completed", updatedRun, map[string]any{
			"phase":              "initial",
			"control_message_id": initialControlMessage.ID,
			"planner_outcome":    string(firstPlannerResult.Output.Outcome),
			"response_id":        firstPlannerResult.ResponseID,
		})
	}

	if err := c.persistPendingAction(ctx, updatedRun, firstPlannerResult, false, ""); err != nil {
		return result, err
	}

	if ShouldMarkRunCompleted(firstPlannerResult.Output) {
		if err := c.appendRunCompletedEvent(updatedRun, firstPlannerResult, &firstPlannerCheckpoint); err != nil {
			return result, err
		}
		return result, nil
	}

	activeRun := updatedRun
	activePlannerResult := firstPlannerResult

	activeRun, activePlannerResult, result, err = c.maybeRunDriftReview(ctx, updatedRun, result, firstPlannerResult)
	if err != nil {
		return result, err
	}
	if activeRun.ID != "" {
		result.Run = activeRun
	}
	if activeRun.Status == state.StatusCompleted || ShouldMarkRunCompleted(activePlannerResult.Output) {
		return result, nil
	}

	activeRun, activePlannerResult, result, err = c.maybeRunControlIntervention(ctx, activeRun, activePlannerResult, result)
	if err != nil {
		return result, err
	}
	if activeRun.ID != "" {
		result.Run = activeRun
	}
	if activeRun.Status == state.StatusCompleted || ShouldMarkRunCompleted(activePlannerResult.Output) {
		return result, nil
	}

	if ShouldAskHuman(activePlannerResult.Output) {
		return c.handleAskHuman(ctx, result, activeRun, activePlannerResult)
	}

	if ShouldCollectContext(activePlannerResult.Output) {
		return c.handleCollectContext(ctx, result, activeRun, activePlannerResult)
	}

	if !ShouldDispatchExecutor(activePlannerResult.Output) {
		return result, nil
	}

	return c.handleExecute(ctx, result, activeRun, activePlannerResult)
}

func BuildPlannerInput(
	run state.Run,
	events []journal.Event,
	executorResult *planner.ExecutorResultSummary,
	collectedContext *planner.CollectedContextSummary,
	driftReview *planner.DriftReviewSummary,
	pluginTools []planner.PluginToolDescriptor,
) planner.InputEnvelope {
	previews := make([]planner.EventPreview, 0, len(events))
	for _, event := range events {
		previews = append(previews, planner.EventPreview{
			At:      event.At,
			Type:    event.Type,
			Summary: summarizeEvent(event),
		})
	}

	return planner.InputEnvelope{
		ContractVersion: planner.ContractVersionV1,
		RunID:           run.ID,
		RepoPath:        run.RepoPath,
		Goal:            run.Goal,
		RunStatus:       string(run.Status),
		LatestCheckpoint: planner.Checkpoint{
			Sequence:     run.LatestCheckpoint.Sequence,
			Stage:        run.LatestCheckpoint.Stage,
			Label:        run.LatestCheckpoint.Label,
			SafePause:    run.LatestCheckpoint.SafePause,
			PlannerTurn:  run.LatestCheckpoint.PlannerTurn,
			ExecutorTurn: run.LatestCheckpoint.ExecutorTurn,
			CreatedAt:    run.LatestCheckpoint.CreatedAt,
		},
		RecentEvents:     previews,
		ExecutorResult:   executorResult,
		CollectedContext: collectedContext,
		DriftReview:      driftReview,
		PluginTools:      pluginTools,
		RepoContracts: planner.RepoContractAvailability{
			HasAgentsMD:         pathExists(filepath.Join(run.RepoPath, "AGENTS.md")),
			AgentsMDPath:        "AGENTS.md",
			HasUpdatedSpec:      pathExists(filepath.Join(run.RepoPath, "docs", "ORCHESTRATOR_CLI_UPDATED_SPEC.md")),
			UpdatedSpecPath:     "docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md",
			HasNonNegotiables:   pathExists(filepath.Join(run.RepoPath, "docs", "ORCHESTRATOR_NON_NEGOTIABLES.md")),
			NonNegotiablesPath:  "docs/ORCHESTRATOR_NON_NEGOTIABLES.md",
			HasExecPlan:         pathExists(filepath.Join(run.RepoPath, "docs", "CLI_ENGINE_EXECPLAN.md")),
			ExecPlanPath:        "docs/CLI_ENGINE_EXECPLAN.md",
			OrchestratorDirPath: ".orchestrator",
			RoadmapPath:         ".orchestrator/roadmap.md",
			DecisionsPath:       ".orchestrator/decisions.md",
		},
		RawHumanReplies: buildRawHumanReplies(run),
		Capabilities: planner.CapabilityMarkers{
			Planner:  planner.CapabilityAvailable,
			Executor: planner.CapabilityAvailable,
			NTFY:     planner.CapabilityDeferred,
		},
	}
}

func BuildExecutorResultInput(run state.Run) *planner.ExecutorResultSummary {
	if run.ExecutorLastSuccess == nil {
		return nil
	}

	return &planner.ExecutorResultSummary{
		FinalMessage: strings.TrimSpace(run.ExecutorLastMessage),
		Success:      *run.ExecutorLastSuccess,
		ThreadID:     strings.TrimSpace(run.ExecutorThreadID),
	}
}

func BuildCollectedContextInput(run state.Run) *planner.CollectedContextSummary {
	if run.CollectedContext == nil {
		return nil
	}

	results := make([]planner.CollectedContextResult, 0, len(run.CollectedContext.Results))
	for _, item := range run.CollectedContext.Results {
		results = append(results, planner.CollectedContextResult{
			RequestedPath: item.RequestedPath,
			ResolvedPath:  item.ResolvedPath,
			Kind:          item.Kind,
			Detail:        item.Detail,
			Preview:       item.Preview,
			Entries:       append([]string(nil), item.Entries...),
			Truncated:     item.Truncated,
		})
	}

	toolResults := make([]planner.PluginToolResult, 0, len(run.CollectedContext.ToolResults))
	for _, item := range run.CollectedContext.ToolResults {
		toolResults = append(toolResults, planner.PluginToolResult{
			Tool:            item.Tool,
			Success:         item.Success,
			Message:         item.Message,
			Data:            item.Data,
			ArtifactPath:    item.ArtifactPath,
			ArtifactPreview: item.ArtifactPreview,
		})
	}

	workerResults := make([]planner.WorkerActionResult, 0, len(run.CollectedContext.WorkerResults))
	for _, item := range run.CollectedContext.WorkerResults {
		workerResults = append(workerResults, planner.WorkerActionResult{
			Action:          planner.WorkerActionKind(item.Action),
			Success:         item.Success,
			Message:         item.Message,
			Worker:          mapWorkerResultSummaryToPlanner(item.Worker),
			ListedWorkers:   mapWorkerResultSummariesToPlanner(item.ListedWorkers),
			Removed:         item.Removed,
			ArtifactPath:    item.ArtifactPath,
			ArtifactPreview: item.ArtifactPreview,
			Integration:     mapIntegrationSummaryToPlanner(item.Integration),
			Apply:           mapIntegrationApplySummaryToPlanner(item.Apply),
		})
	}

	return &planner.CollectedContextSummary{
		Focus:         strings.TrimSpace(run.CollectedContext.Focus),
		Questions:     append([]string(nil), run.CollectedContext.Questions...),
		Results:       results,
		ToolResults:   toolResults,
		WorkerResults: workerResults,
		WorkerPlan:    mapWorkerPlanResultToPlanner(run.CollectedContext.WorkerPlan),
	}
}

func mapWorkerResultSummaryToPlanner(item *state.WorkerResultSummary) *planner.WorkerResultSummary {
	if item == nil {
		return nil
	}

	return &planner.WorkerResultSummary{
		WorkerID:                   item.WorkerID,
		WorkerName:                 item.WorkerName,
		WorkerStatus:               item.WorkerStatus,
		AssignedScope:              item.AssignedScope,
		WorktreePath:               item.WorktreePath,
		WorkerTaskSummary:          item.WorkerTaskSummary,
		ExecutorPromptSummary:      item.ExecutorPromptSummary,
		WorkerResultSummary:        item.WorkerResultSummary,
		WorkerErrorSummary:         item.WorkerErrorSummary,
		ExecutorThreadID:           item.ExecutorThreadID,
		ExecutorTurnID:             item.ExecutorTurnID,
		ExecutorTurnStatus:         item.ExecutorTurnStatus,
		ExecutorApprovalState:      item.ExecutorApprovalState,
		ExecutorApprovalKind:       item.ExecutorApprovalKind,
		ExecutorApprovalPreview:    item.ExecutorApprovalPreview,
		ExecutorInterruptible:      item.ExecutorInterruptible,
		ExecutorSteerable:          item.ExecutorSteerable,
		ExecutorFailureStage:       item.ExecutorFailureStage,
		ExecutorLastControlAction:  item.ExecutorLastControlAction,
		ExecutorLastControlPayload: item.ExecutorLastControlPayload,
		StartedAt:                  item.StartedAt,
		CompletedAt:                item.CompletedAt,
	}
}

func mapWorkerResultSummariesToPlanner(items []state.WorkerResultSummary) []planner.WorkerResultSummary {
	out := make([]planner.WorkerResultSummary, 0, len(items))
	for _, item := range items {
		itemCopy := item
		out = append(out, *mapWorkerResultSummaryToPlanner(&itemCopy))
	}
	return out
}

func mapIntegrationSummaryToPlanner(item *state.IntegrationSummary) *planner.IntegrationSummary {
	if item == nil {
		return nil
	}

	workers := make([]planner.IntegrationWorkerSummary, 0, len(item.Workers))
	for _, worker := range item.Workers {
		workers = append(workers, planner.IntegrationWorkerSummary{
			WorkerID:            worker.WorkerID,
			WorkerName:          worker.WorkerName,
			WorktreePath:        worker.WorktreePath,
			WorkerResultSummary: worker.WorkerResultSummary,
			FileList:            append([]string(nil), worker.FileList...),
			DiffSummary:         append([]string(nil), worker.DiffSummary...),
		})
	}

	conflicts := make([]planner.ConflictCandidate, 0, len(item.ConflictCandidates))
	conflicts = mapConflictCandidatesToPlanner(item.ConflictCandidates)

	return &planner.IntegrationSummary{
		WorkerIDs:          append([]string(nil), item.WorkerIDs...),
		Workers:            workers,
		ConflictCandidates: conflicts,
		IntegrationPreview: item.IntegrationPreview,
	}
}

func mapIntegrationApplySummaryToPlanner(item *state.IntegrationApplySummary) *planner.IntegrationApplySummary {
	if item == nil {
		return nil
	}

	applied := make([]planner.IntegrationAppliedFile, 0, len(item.FilesApplied))
	for _, file := range item.FilesApplied {
		applied = append(applied, planner.IntegrationAppliedFile{
			WorkerID:   file.WorkerID,
			WorkerName: file.WorkerName,
			Path:       file.Path,
			ChangeKind: file.ChangeKind,
		})
	}

	skipped := make([]planner.IntegrationSkippedFile, 0, len(item.FilesSkipped))
	for _, file := range item.FilesSkipped {
		skipped = append(skipped, planner.IntegrationSkippedFile{
			WorkerID:   file.WorkerID,
			WorkerName: file.WorkerName,
			Path:       file.Path,
			ChangeKind: file.ChangeKind,
			Reason:     file.Reason,
		})
	}

	return &planner.IntegrationApplySummary{
		Status:             item.Status,
		SourceArtifactPath: item.SourceArtifactPath,
		ApplyMode:          item.ApplyMode,
		FilesApplied:       applied,
		FilesSkipped:       skipped,
		ConflictCandidates: mapConflictCandidatesToPlanner(item.ConflictCandidates),
		BeforeSummary:      item.BeforeSummary,
		AfterSummary:       item.AfterSummary,
	}
}

func mapWorkerPlanResultToPlanner(item *state.WorkerPlanResult) *planner.WorkerPlanResult {
	if item == nil {
		return nil
	}

	return &planner.WorkerPlanResult{
		Status:                  item.Status,
		WorkerIDs:               append([]string(nil), item.WorkerIDs...),
		Workers:                 mapWorkerResultSummariesToPlanner(item.Workers),
		ConcurrencyLimit:        item.ConcurrencyLimit,
		IntegrationRequested:    item.IntegrationRequested,
		IntegrationArtifactPath: item.IntegrationArtifactPath,
		IntegrationPreview:      item.IntegrationPreview,
		ApplyMode:               item.ApplyMode,
		ApplyArtifactPath:       item.ApplyArtifactPath,
		Apply:                   mapIntegrationApplySummaryToPlanner(item.Apply),
		Message:                 item.Message,
	}
}

func mapConflictCandidatesToPlanner(items []state.ConflictCandidate) []planner.ConflictCandidate {
	conflicts := make([]planner.ConflictCandidate, 0, len(items))
	for _, candidate := range items {
		conflicts = append(conflicts, planner.ConflictCandidate{
			Path:        candidate.Path,
			Reason:      candidate.Reason,
			WorkerIDs:   append([]string(nil), candidate.WorkerIDs...),
			WorkerNames: append([]string(nil), candidate.WorkerNames...),
		})
	}
	return conflicts
}

func buildRawHumanReplies(run state.Run) []planner.RawHumanReply {
	if len(run.HumanReplies) == 0 {
		return nil
	}

	replies := make([]planner.RawHumanReply, 0, len(run.HumanReplies))
	for _, reply := range run.HumanReplies {
		replies = append(replies, planner.RawHumanReply{
			ID:         reply.ID,
			Source:     reply.Source,
			ReceivedAt: reply.ReceivedAt,
			Payload:    reply.Payload,
		})
	}
	return replies
}

func mapPendingActionToPlanner(pending state.PendingAction, found bool) *planner.PendingActionInput {
	if !found {
		return &planner.PendingActionInput{Present: false}
	}

	mapped := &planner.PendingActionInput{
		Present:                true,
		TurnType:               strings.TrimSpace(pending.TurnType),
		PlannerOutcome:         strings.TrimSpace(pending.PlannerOutcome),
		PlannerResponseID:      strings.TrimSpace(pending.PlannerResponseID),
		PendingActionSummary:   strings.TrimSpace(pending.PendingActionSummary),
		PendingExecutorPrompt:  strings.TrimSpace(pending.PendingExecutorPrompt),
		PendingExecutorSummary: strings.TrimSpace(pending.PendingExecutorSummary),
		PendingReason:          strings.TrimSpace(pending.PendingReason),
		Held:                   pending.Held,
		HoldReason:             strings.TrimSpace(pending.HoldReason),
		UpdatedAt:              pending.UpdatedAt,
	}
	if pending.PendingDispatchTarget != nil {
		mapped.PendingDispatchTarget = &planner.PendingDispatchTarget{
			Kind:         strings.TrimSpace(pending.PendingDispatchTarget.Kind),
			WorkerID:     strings.TrimSpace(pending.PendingDispatchTarget.WorkerID),
			WorkerName:   strings.TrimSpace(pending.PendingDispatchTarget.WorkerName),
			WorktreePath: strings.TrimSpace(pending.PendingDispatchTarget.WorktreePath),
		}
	}
	return mapped
}

func mapControlMessageToPlanner(message state.ControlMessage, pauseReason string) *planner.ControlInterventionInput {
	return &planner.ControlInterventionInput{
		Present:        true,
		InterventionID: strings.TrimSpace(message.ID),
		RawMessage:     message.RawText,
		Source:         strings.TrimSpace(message.Source),
		Reason:         firstNonEmpty(strings.TrimSpace(message.Reason), "operator_intervention"),
		PauseReason:    firstNonEmpty(strings.TrimSpace(pauseReason), "operator_intervention_at_safe_point"),
		QueuedAt:       message.CreatedAt,
	}
}

func (c Cycle) persistPendingAction(
	ctx context.Context,
	run state.Run,
	plannerTurn planner.Result,
	held bool,
	holdReason string,
) error {
	pendingAction, err := BuildPendingAction(run, plannerTurn)
	if err != nil {
		return err
	}
	if pendingAction != nil {
		pendingAction.Held = held
		pendingAction.HoldReason = strings.TrimSpace(holdReason)
	}
	if err := c.Store.SavePendingAction(ctx, run.ID, pendingAction); err != nil {
		return err
	}

	if pendingAction == nil {
		c.emitEvent("pending_action_cleared", run, map[string]any{
			"response_id": plannerTurn.ResponseID,
		})
		return nil
	}

	c.emitEvent("pending_action_updated", run, map[string]any{
		"turn_type":       pendingAction.TurnType,
		"planner_outcome": pendingAction.PlannerOutcome,
		"response_id":     pendingAction.PlannerResponseID,
		"held":            pendingAction.Held,
		"hold_reason":     pendingAction.HoldReason,
		"dispatch_target": pendingActionDispatchPayload(pendingAction.PendingDispatchTarget),
		"action_summary":  pendingAction.PendingActionSummary,
	})
	return nil
}

func (c Cycle) clearPendingAction(ctx context.Context, run state.Run) error {
	if strings.TrimSpace(run.ID) == "" {
		return nil
	}
	pendingAction, found, err := c.Store.GetPendingAction(ctx, run.ID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := c.Store.SavePendingAction(ctx, run.ID, nil); err != nil {
		return err
	}
	c.emitEvent("pending_action_cleared", run, map[string]any{
		"turn_type":       pendingAction.TurnType,
		"planner_outcome": pendingAction.PlannerOutcome,
		"response_id":     pendingAction.PlannerResponseID,
	})
	return nil
}

func BuildPendingAction(run state.Run, plannerTurn planner.Result) (*state.PendingAction, error) {
	switch plannerTurn.Output.Outcome {
	case planner.OutcomeExecute:
		prompt, err := RenderExecutorPrompt(run.Goal, plannerTurn.Output)
		if err != nil {
			return nil, err
		}
		return &state.PendingAction{
			TurnType:               "executor_dispatch",
			PlannerOutcome:         string(plannerTurn.Output.Outcome),
			PlannerResponseID:      strings.TrimSpace(plannerTurn.ResponseID),
			PendingActionSummary:   ExecutorTask(plannerTurn.Output),
			PendingExecutorPrompt:  prompt,
			PendingExecutorSummary: previewString(prompt, 240),
			PendingDispatchTarget: &state.PendingDispatchTarget{
				Kind: "primary_executor",
			},
			PendingReason: "planner_selected_execute",
			UpdatedAt:     time.Now().UTC(),
		}, nil
	case planner.OutcomeCollectContext:
		focus := "collect additional repo context"
		if plannerTurn.Output.CollectContext != nil {
			focus = firstNonEmpty(strings.TrimSpace(plannerTurn.Output.CollectContext.Focus), focus)
		}
		return &state.PendingAction{
			TurnType:             "collect_context",
			PlannerOutcome:       string(plannerTurn.Output.Outcome),
			PlannerResponseID:    strings.TrimSpace(plannerTurn.ResponseID),
			PendingActionSummary: focus,
			PendingReason:        "planner_selected_collect_context",
			UpdatedAt:            time.Now().UTC(),
		}, nil
	case planner.OutcomeAskHuman:
		question := "ask the human a question"
		if plannerTurn.Output.AskHuman != nil {
			question = firstNonEmpty(strings.TrimSpace(plannerTurn.Output.AskHuman.Question), question)
		}
		return &state.PendingAction{
			TurnType:             "ask_human",
			PlannerOutcome:       string(plannerTurn.Output.Outcome),
			PlannerResponseID:    strings.TrimSpace(plannerTurn.ResponseID),
			PendingActionSummary: question,
			PendingReason:        "planner_selected_ask_human",
			UpdatedAt:            time.Now().UTC(),
		}, nil
	default:
		return nil, nil
	}
}

func pendingActionDispatchPayload(target *state.PendingDispatchTarget) map[string]any {
	if target == nil {
		return nil
	}
	return map[string]any{
		"kind":          strings.TrimSpace(target.Kind),
		"worker_id":     strings.TrimSpace(target.WorkerID),
		"worker_name":   strings.TrimSpace(target.WorkerName),
		"worktree_path": strings.TrimSpace(target.WorktreePath),
	}
}

func ShouldAskHuman(output planner.OutputEnvelope) bool {
	return output.Outcome == planner.OutcomeAskHuman && output.AskHuman != nil
}

func ShouldCollectContext(output planner.OutputEnvelope) bool {
	return output.Outcome == planner.OutcomeCollectContext && output.CollectContext != nil
}

func ShouldDispatchExecutor(output planner.OutputEnvelope) bool {
	return output.Outcome == planner.OutcomeExecute && output.Execute != nil
}

func RenderExecutorPrompt(goal string, output planner.OutputEnvelope) (string, error) {
	if !ShouldDispatchExecutor(output) {
		return "", errors.New("planner outcome is not execute")
	}

	execute := output.Execute
	lines := []string{
		"Planner-selected executor task.",
		"",
		"Run goal:",
		strings.TrimSpace(goal),
		"",
		"Task:",
		strings.TrimSpace(execute.Task),
		"",
		"Acceptance criteria:",
	}

	for _, criterion := range nonEmpty(execute.AcceptanceCriteria) {
		lines = append(lines, "- "+criterion)
	}

	writeScope := nonEmpty(execute.WriteScope)
	if len(writeScope) > 0 {
		lines = append(lines, "", "Write scope:")
		for _, path := range writeScope {
			lines = append(lines, "- "+path)
		}
	}

	lines = append(lines,
		"",
		"Artifact placement:",
		"- Do not write repo-analysis files, orchestration summaries, or run reports into the repo root by default.",
		"- Prefer .orchestrator/artifacts/reports/ for orchestration-only reports and .orchestrator/artifacts/executor/ for large executor-only summaries.",
		"- Only write an orchestration-only file outside .orchestrator/artifacts/ when the planner explicitly requested that path and the write scope clearly allows it.",
		"",
		"Perform only this bounded task.",
		"Do not choose a new task.",
		"Do not decide whether the overall run is complete.",
		"In your final answer, report the concrete work completed and any blockers.",
	)

	return strings.Join(lines, "\n"), nil
}

func BuildExecutorCheckpoint(previous state.Checkpoint, label string, at time.Time) state.Checkpoint {
	timestamp := at.UTC()
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	return state.Checkpoint{
		Sequence:     previous.Sequence + 1,
		Stage:        "executor",
		Label:        strings.TrimSpace(label),
		SafePause:    true,
		PlannerTurn:  previous.PlannerTurn,
		ExecutorTurn: previous.ExecutorTurn + 1,
		CreatedAt:    timestamp,
	}
}

func ExecutorTask(output planner.OutputEnvelope) string {
	if output.Execute == nil {
		return ""
	}
	return strings.TrimSpace(output.Execute.Task)
}

func ShouldMarkRunCompleted(output planner.OutputEnvelope) bool {
	return output.Outcome == planner.OutcomeComplete && output.Complete != nil
}

func plannerCheckpointLabel(output planner.OutputEnvelope, fallback string) string {
	if ShouldMarkRunCompleted(output) {
		return "planner_declared_complete"
	}
	return fallback
}

func plannerDeclaredCompletionMessage(output planner.OutputEnvelope) string {
	if output.Complete == nil {
		return "planner declared run complete"
	}

	summary := previewString(output.Complete.Summary, 240)
	if summary == "" {
		return "planner declared run complete"
	}
	return "planner declared run complete: " + summary
}

func (c Cycle) savePlannerTurn(ctx context.Context, runID string, plannerTurn planner.Result, checkpoint state.Checkpoint) error {
	if ShouldMarkRunCompleted(plannerTurn.Output) {
		if err := c.Store.SavePlannerCompletion(ctx, runID, plannerTurn.ResponseID, checkpoint); err != nil {
			return err
		}
		return c.Store.SavePlannerOperatorStatus(ctx, runID, mapPlannerOperatorStatusToState(plannerTurn.Output.ContractVersion, plannerTurn.Output.OperatorStatus))
	}
	if err := c.Store.SavePlannerTurn(ctx, runID, plannerTurn.ResponseID, checkpoint); err != nil {
		return err
	}
	return c.Store.SavePlannerOperatorStatus(ctx, runID, mapPlannerOperatorStatusToState(plannerTurn.Output.ContractVersion, plannerTurn.Output.OperatorStatus))
}

func (c Cycle) appendRunCompletedEvent(run state.Run, plannerTurn planner.Result, checkpoint *state.Checkpoint) error {
	return c.Journal.Append(journal.Event{
		Type:               "run.completed",
		RunID:              run.ID,
		RepoPath:           run.RepoPath,
		Goal:               run.Goal,
		Status:             string(run.Status),
		Message:            plannerDeclaredCompletionMessage(plannerTurn.Output),
		ResponseID:         plannerTurn.ResponseID,
		PreviousResponseID: run.PreviousResponseID,
		PlannerOutcome:     string(plannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(checkpoint),
	})
}

func (c Cycle) runSecondPlannerTurn(
	ctx context.Context,
	currentRun state.Run,
	result Result,
	input planner.InputEnvelope,
	triggeringOutcome string,
	checkpointLabel string,
	completedMessage string,
	checkpointMessage string,
	failurePrefix string,
) (Result, error) {
	secondPlannerTurn, err := c.Planner.Plan(ctx, input, currentRun.PreviousResponseID)
	if err != nil {
		stopReason := StopReasonForError(err)
		artifactPath, artifactPreview := c.persistPlannerValidationArtifact(currentRun, err)

		failedRun, issueErr := c.persistRuntimeIssue(ctx, currentRun, stopReason, err.Error())
		if issueErr != nil {
			return result, errors.Join(err, issueErr)
		}
		failureEvent := journal.Event{
			Type:               "planner.turn.failed",
			RunID:              failedRun.ID,
			RepoPath:           failedRun.RepoPath,
			Goal:               failedRun.Goal,
			Status:             string(failedRun.Status),
			Message:            failurePrefix + ": " + err.Error(),
			StopReason:         stopReason,
			PreviousResponseID: failedRun.PreviousResponseID,
			PlannerOutcome:     triggeringOutcome,
			ArtifactPath:       artifactPath,
			ArtifactPreview:    artifactPreview,
			Checkpoint:         checkpointRef(&failedRun.LatestCheckpoint),
		}
		populateExecutorEventFields(&failureEvent, failedRun)
		_ = c.Journal.Append(failureEvent)
		result.Run = failedRun
		return result, err
	}

	originCheckpointLabel := checkpointLabel
	if ShouldMarkRunCompleted(secondPlannerTurn.Output) {
		checkpointLabel = plannerCheckpointLabel(secondPlannerTurn.Output, checkpointLabel)
		completedMessage = "planner declared completion and run marked completed"
		checkpointMessage = "completion checkpoint persisted"
	}

	checkpoint := state.Checkpoint{
		Sequence:     currentRun.LatestCheckpoint.Sequence + 1,
		Stage:        "planner",
		Label:        checkpointLabel,
		SafePause:    true,
		PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn + 1,
		ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
		CreatedAt:    time.Now().UTC(),
	}

	if err := c.savePlannerTurn(ctx, currentRun.ID, secondPlannerTurn, checkpoint); err != nil {
		return result, err
	}

	finalRun, found, err := c.Store.GetRun(ctx, currentRun.ID)
	if err != nil {
		return result, err
	}
	if !found {
		return result, fmt.Errorf("updated run %s could not be reloaded after second planner turn", currentRun.ID)
	}

	completedEvent := journal.Event{
		Type:               "planner.turn.completed",
		RunID:              finalRun.ID,
		RepoPath:           finalRun.RepoPath,
		Goal:               finalRun.Goal,
		Status:             string(finalRun.Status),
		Message:            completedMessage,
		ResponseID:         secondPlannerTurn.ResponseID,
		PreviousResponseID: finalRun.PreviousResponseID,
		PlannerOutcome:     string(secondPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&checkpoint),
	}
	populateExecutorEventFields(&completedEvent, finalRun)
	if err := c.Journal.Append(completedEvent); err != nil {
		return result, err
	}

	checkpointEvent := journal.Event{
		Type:               "checkpoint.persisted",
		RunID:              finalRun.ID,
		RepoPath:           finalRun.RepoPath,
		Status:             string(finalRun.Status),
		Message:            checkpointMessage,
		ResponseID:         secondPlannerTurn.ResponseID,
		PreviousResponseID: finalRun.PreviousResponseID,
		PlannerOutcome:     string(secondPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&checkpoint),
	}
	populateExecutorEventFields(&checkpointEvent, finalRun)
	if err := c.Journal.Append(checkpointEvent); err != nil {
		return result, err
	}
	c.emitEvent("planner_turn_completed", finalRun, map[string]any{
		"phase":           checkpointLabel,
		"planner_outcome": string(secondPlannerTurn.Output.Outcome),
		"response_id":     secondPlannerTurn.ResponseID,
		"operator_status": plannerOperatorStatusEventPayload(secondPlannerTurn.Output.OperatorStatus),
	})
	c.emitPlannerOperatorMessage(finalRun, checkpointLabel, secondPlannerTurn)

	c.runPluginHooks(ctx, plugins.HookPlannerAfter, finalRun, &secondPlannerTurn, nil, "", nil)

	if ShouldMarkRunCompleted(secondPlannerTurn.Output) {
		if err := c.appendRunCompletedEvent(finalRun, secondPlannerTurn, &checkpoint); err != nil {
			return result, err
		}
	}

	result.Run = finalRun
	if originCheckpointLabel == "planner_turn_post_executor" {
		result.PostExecutorPlannerTurn = &secondPlannerTurn
	}
	if originCheckpointLabel == "planner_turn_post_drift_review" {
		result.ReconsiderationPlannerTurn = &secondPlannerTurn
		if err := c.persistPendingAction(ctx, finalRun, secondPlannerTurn, false, ""); err != nil {
			return result, err
		}
		return result, nil
	}
	result.SecondPlannerTurn = &secondPlannerTurn
	if err := c.persistPendingAction(ctx, finalRun, secondPlannerTurn, false, ""); err != nil {
		return result, err
	}
	return result, nil
}

func mapPlannerOperatorStatusToState(contractVersion string, status *planner.OperatorStatus) *state.PlannerOperatorStatus {
	if status == nil {
		return nil
	}

	return &state.PlannerOperatorStatus{
		ContractVersion:    strings.TrimSpace(contractVersion),
		OperatorMessage:    strings.TrimSpace(status.OperatorMessage),
		CurrentFocus:       strings.TrimSpace(status.CurrentFocus),
		NextIntendedStep:   strings.TrimSpace(status.NextIntendedStep),
		WhyThisStep:        strings.TrimSpace(status.WhyThisStep),
		ProgressPercent:    status.ProgressPercent,
		ProgressConfidence: strings.TrimSpace(string(status.ProgressConfidence)),
		ProgressBasis:      strings.TrimSpace(status.ProgressBasis),
	}
}

func plannerOperatorStatusEventPayload(status *planner.OperatorStatus) map[string]any {
	if status == nil {
		return nil
	}

	return map[string]any{
		"operator_message":    strings.TrimSpace(status.OperatorMessage),
		"current_focus":       strings.TrimSpace(status.CurrentFocus),
		"next_intended_step":  strings.TrimSpace(status.NextIntendedStep),
		"why_this_step":       strings.TrimSpace(status.WhyThisStep),
		"progress_percent":    status.ProgressPercent,
		"progress_confidence": strings.TrimSpace(string(status.ProgressConfidence)),
		"progress_basis":      strings.TrimSpace(status.ProgressBasis),
	}
}

func (c Cycle) emitPlannerOperatorMessage(run state.Run, phase string, plannerTurn planner.Result) {
	if plannerTurn.Output.OperatorStatus == nil {
		return
	}

	c.emitEvent("planner_operator_message", run, map[string]any{
		"phase":               strings.TrimSpace(phase),
		"planner_outcome":     string(plannerTurn.Output.Outcome),
		"response_id":         plannerTurn.ResponseID,
		"operator_message":    strings.TrimSpace(plannerTurn.Output.OperatorStatus.OperatorMessage),
		"current_focus":       strings.TrimSpace(plannerTurn.Output.OperatorStatus.CurrentFocus),
		"next_intended_step":  strings.TrimSpace(plannerTurn.Output.OperatorStatus.NextIntendedStep),
		"why_this_step":       strings.TrimSpace(plannerTurn.Output.OperatorStatus.WhyThisStep),
		"progress_percent":    plannerTurn.Output.OperatorStatus.ProgressPercent,
		"progress_confidence": strings.TrimSpace(string(plannerTurn.Output.OperatorStatus.ProgressConfidence)),
		"progress_basis":      strings.TrimSpace(plannerTurn.Output.OperatorStatus.ProgressBasis),
		"contract_version":    strings.TrimSpace(plannerTurn.Output.ContractVersion),
	})
}

func (c Cycle) maybeRunDriftReview(
	ctx context.Context,
	currentRun state.Run,
	result Result,
	firstPlannerTurn planner.Result,
) (state.Run, planner.Result, Result, error) {
	if !c.DriftReviewOn || c.DriftWatcher == nil {
		return currentRun, firstPlannerTurn, result, nil
	}
	if !ShouldDispatchExecutor(firstPlannerTurn.Output) && !ShouldCollectContext(firstPlannerTurn.Output) {
		return currentRun, firstPlannerTurn, result, nil
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "review.drift.started",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "drift watcher started",
		ResponseID:         firstPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(firstPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return result.Run, firstPlannerTurn, result, err
	}

	recentEvents, err := c.Journal.ReadRecent(currentRun.ID, 8)
	if err != nil {
		return result.Run, firstPlannerTurn, result, err
	}

	reviewResult, err := c.DriftWatcher.Review(ctx, DriftReviewRequest{
		Run:           currentRun,
		PlannerResult: firstPlannerTurn,
		RecentEvents:  recentEvents,
	})
	if err != nil {
		if appendErr := c.Journal.Append(journal.Event{
			Type:               "review.drift.failed",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            "drift watcher failed; continuing with the original planner outcome: " + err.Error(),
			ResponseID:         firstPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(firstPlannerTurn.Output.Outcome),
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}); appendErr != nil {
			return result.Run, firstPlannerTurn, result, appendErr
		}
		return currentRun, firstPlannerTurn, result, nil
	}

	artifactPath, artifactPreview := persistDriftReviewArtifact(currentRun, firstPlannerTurn, reviewResult)
	if artifactPath == "" && artifactPreview != "" && strings.HasPrefix(artifactPreview, "artifact_write_failed:") {
		if appendErr := c.Journal.Append(journal.Event{
			Type:               "review.drift.failed",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            "drift watcher artifact persistence failed; continuing with the original planner outcome",
			ResponseID:         firstPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(firstPlannerTurn.Output.Outcome),
			ArtifactPreview:    artifactPreview,
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}); appendErr != nil {
			return result.Run, firstPlannerTurn, result, appendErr
		}
		return currentRun, firstPlannerTurn, result, nil
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "review.drift.completed",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            driftReviewMessage(reviewResult.Summary),
		ResponseID:         firstPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(firstPlannerTurn.Output.Outcome),
		ArtifactPath:       artifactPath,
		ArtifactPreview:    artifactPreview,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return result.Run, firstPlannerTurn, result, err
	}

	postReviewEvents, err := c.Journal.ReadRecent(currentRun.ID, 8)
	if err != nil {
		return result.Run, firstPlannerTurn, result, err
	}

	reconsiderationInput := BuildPlannerInput(currentRun, postReviewEvents, nil, nil, &reviewResult.Summary, c.pluginToolDescriptors())
	result, err = c.runSecondPlannerTurn(
		ctx,
		currentRun,
		result,
		reconsiderationInput,
		string(firstPlannerTurn.Output.Outcome),
		"planner_turn_post_drift_review",
		"planner reconsideration response validated and persisted",
		"planner reconsideration checkpoint persisted",
		"planner reconsideration turn failed",
	)
	if err != nil {
		return result.Run, firstPlannerTurn, result, err
	}
	if result.ReconsiderationPlannerTurn == nil || result.Run.ID == "" {
		return currentRun, firstPlannerTurn, result, nil
	}

	return result.Run, *result.ReconsiderationPlannerTurn, result, nil
}

func (c Cycle) maybeRunControlIntervention(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	result Result,
) (state.Run, planner.Result, Result, error) {
	controlMessage, found, err := c.Store.NextQueuedControlMessage(ctx, currentRun.ID)
	if err != nil {
		return currentRun, currentPlannerTurn, result, err
	}
	if !found {
		return currentRun, currentPlannerTurn, result, nil
	}

	pendingAction, pendingFound, err := c.Store.GetPendingAction(ctx, currentRun.ID)
	if err != nil {
		return currentRun, currentPlannerTurn, result, err
	}
	if pendingFound && !pendingAction.Held {
		if err := c.persistPendingAction(ctx, currentRun, currentPlannerTurn, true, "control_message_queued"); err != nil {
			return currentRun, currentPlannerTurn, result, err
		}
		pendingAction, pendingFound, err = c.Store.GetPendingAction(ctx, currentRun.ID)
		if err != nil {
			return currentRun, currentPlannerTurn, result, err
		}
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "control.intervention.pending",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "queued control message is holding the pending planner-selected action at a safe point",
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		HumanReplyID:       controlMessage.ID,
		HumanReplySource:   controlMessage.Source,
		HumanReplyPayload:  controlMessage.RawText,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return currentRun, currentPlannerTurn, result, err
	}

	c.emitEvent("safe_point_intervention_pending", currentRun, map[string]any{
		"control_message_id": controlMessage.ID,
		"source":             controlMessage.Source,
		"reason":             controlMessage.Reason,
		"planner_outcome":    string(currentPlannerTurn.Output.Outcome),
		"message_preview":    previewString(controlMessage.RawText, 240),
	})

	postInterventionEvents, err := c.Journal.ReadRecent(currentRun.ID, 8)
	if err != nil {
		return currentRun, currentPlannerTurn, result, err
	}

	interventionInput := BuildPlannerInput(currentRun, postInterventionEvents, nil, nil, nil, c.pluginToolDescriptors())
	interventionInput.PendingAction = mapPendingActionToPlanner(pendingAction, pendingFound)
	interventionInput.ControlIntervention = mapControlMessageToPlanner(controlMessage, "operator_intervention_at_safe_point")

	c.emitEvent("planner_intervention_turn_started", currentRun, map[string]any{
		"control_message_id": controlMessage.ID,
		"source":             controlMessage.Source,
		"reason":             controlMessage.Reason,
		"planner_outcome":    string(currentPlannerTurn.Output.Outcome),
		"response_id":        currentPlannerTurn.ResponseID,
	})

	result, err = c.runSecondPlannerTurn(
		ctx,
		currentRun,
		result,
		interventionInput,
		string(currentPlannerTurn.Output.Outcome),
		"planner_turn_post_control_intervention",
		"planner intervention response validated and persisted",
		"planner intervention checkpoint persisted",
		"planner intervention turn failed",
	)
	if err != nil {
		return result.Run, currentPlannerTurn, result, err
	}

	if err := c.Store.ConsumeControlMessage(ctx, controlMessage.ID, time.Now().UTC()); err != nil {
		return result.Run, currentPlannerTurn, result, err
	}
	if result.Run.ID != "" {
		c.emitEvent("control_message_consumed", result.Run, map[string]any{
			"control_message_id": controlMessage.ID,
			"source":             controlMessage.Source,
			"reason":             controlMessage.Reason,
		})
	}

	var updatedPlannerTurn *planner.Result
	switch {
	case result.SecondPlannerTurn != nil:
		updatedPlannerTurn = result.SecondPlannerTurn
	case result.ReconsiderationPlannerTurn != nil:
		updatedPlannerTurn = result.ReconsiderationPlannerTurn
	}
	if updatedPlannerTurn == nil || result.Run.ID == "" {
		return currentRun, currentPlannerTurn, result, nil
	}

	c.emitEvent("planner_intervention_turn_completed", result.Run, map[string]any{
		"control_message_id": controlMessage.ID,
		"planner_outcome":    string(updatedPlannerTurn.Output.Outcome),
		"response_id":        updatedPlannerTurn.ResponseID,
	})
	return result.Run, *updatedPlannerTurn, result, nil
}

func (c Cycle) handleAskHuman(
	ctx context.Context,
	result Result,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
) (Result, error) {
	if err := c.clearPendingAction(ctx, currentRun); err != nil {
		return result, err
	}
	if c.HumanInteractor == nil {
		err := errors.New("human interactor is required when planner outcome is ask_human")
		failedRun, issueErr := c.persistRuntimeIssue(ctx, currentRun, StopReasonForError(err), err.Error())
		if issueErr != nil {
			return result, errors.Join(err, issueErr)
		}
		result.Run = failedRun
		return result, err
	}

	askHuman := currentPlannerTurn.Output.AskHuman
	if err := c.Journal.Append(journal.Event{
		Type:               "human.question.presented",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "planner question presented to human input bridge",
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		HumanQuestion:      askHuman.Question,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return result, err
	}

	humanInput, err := c.HumanInteractor.Ask(ctx, currentRun, *askHuman)
	if err != nil {
		failedRun, issueErr := c.persistRuntimeIssue(ctx, currentRun, StopReasonForError(err), err.Error())
		if issueErr != nil {
			return result, errors.Join(err, issueErr)
		}
		_ = c.Journal.Append(journal.Event{
			Type:               "human.reply.failed",
			RunID:              failedRun.ID,
			RepoPath:           failedRun.RepoPath,
			Goal:               failedRun.Goal,
			Status:             string(failedRun.Status),
			Message:            err.Error(),
			StopReason:         StopReasonForError(err),
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: failedRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			HumanQuestion:      askHuman.Question,
			Checkpoint:         checkpointRef(&failedRun.LatestCheckpoint),
		})
		result.Run = failedRun
		return result, err
	}

	replySource := strings.TrimSpace(humanInput.Source)
	if replySource == "" {
		replySource = "terminal"
	}

	recordedReply, err := c.Store.RecordHumanReply(ctx, currentRun.ID, replySource, humanInput.Payload, time.Now().UTC())
	if err != nil {
		return result, err
	}

	latestRun, found, err := c.Store.GetRun(ctx, currentRun.ID)
	if err != nil {
		return result, err
	}
	if !found {
		return result, fmt.Errorf("updated run %s could not be reloaded after human reply", currentRun.ID)
	}
	result.Run = latestRun
	humanReplyArtifactPath, humanReplyArtifactPreview := persistHumanReplyArtifact(latestRun, recordedReply)
	humanReplyPayload := recordedReply.Payload
	if humanReplyArtifactPath != "" {
		humanReplyPayload = ""
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "human.reply.recorded",
		RunID:              latestRun.ID,
		RepoPath:           latestRun.RepoPath,
		Goal:               latestRun.Goal,
		Status:             string(latestRun.Status),
		Message:            "raw human reply recorded",
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: latestRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		HumanQuestion:      askHuman.Question,
		HumanReplyID:       recordedReply.ID,
		HumanReplySource:   recordedReply.Source,
		HumanReplyPayload:  humanReplyPayload,
		ArtifactPath:       humanReplyArtifactPath,
		ArtifactPreview:    humanReplyArtifactPreview,
		Checkpoint:         checkpointRef(&latestRun.LatestCheckpoint),
	}); err != nil {
		return result, err
	}

	postHumanEvents, err := c.Journal.ReadRecent(latestRun.ID, 8)
	if err != nil {
		return result, err
	}

	postHumanInput := BuildPlannerInput(latestRun, postHumanEvents, nil, nil, nil, c.pluginToolDescriptors())
	return c.runSecondPlannerTurn(
		ctx,
		latestRun,
		result,
		postHumanInput,
		string(currentPlannerTurn.Output.Outcome),
		"planner_turn_post_human_reply",
		"post-human-reply planner response validated and persisted",
		"post-human-reply planner checkpoint persisted",
		"post-human-reply planner turn failed",
	)
}

func (c Cycle) handleCollectContext(
	ctx context.Context,
	result Result,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
) (Result, error) {
	if err := c.clearPendingAction(ctx, currentRun); err != nil {
		return result, err
	}
	collectedContext, err := BuildCollectedContextState(currentRun.RepoPath, currentPlannerTurn.Output)
	if err != nil {
		return result, err
	}
	if len(currentPlannerTurn.Output.CollectContext.ToolCalls) > 0 {
		toolResults, err := c.executePluginToolCalls(ctx, currentRun, currentPlannerTurn, currentRun.LatestCheckpoint, currentPlannerTurn.Output.CollectContext.ToolCalls)
		if err != nil {
			return result, err
		}
		collectedContext.ToolResults = toolResults
	}
	if len(currentPlannerTurn.Output.CollectContext.WorkerActions) > 0 {
		workerResults, err := c.executeWorkerActions(ctx, currentRun, currentPlannerTurn, currentPlannerTurn.Output.CollectContext.WorkerActions)
		if err != nil {
			return result, err
		}
		collectedContext.WorkerResults = workerResults
	}
	if currentPlannerTurn.Output.CollectContext.WorkerPlan != nil {
		planResult, planWorkerResults, err := c.executeWorkerPlan(ctx, currentRun, currentPlannerTurn, *currentPlannerTurn.Output.CollectContext.WorkerPlan)
		if err != nil {
			return result, err
		}
		collectedContext.WorkerResults = append(collectedContext.WorkerResults, planWorkerResults...)
		collectedContext.WorkerPlan = planResult
	}
	collectedContext.ArtifactPath, collectedContext.ArtifactPreview = persistCollectedContextStateArtifact(currentRun, collectedContext)
	if err := c.Store.SaveCollectedContext(ctx, currentRun.ID, collectedContext); err != nil {
		return result, err
	}

	latestRun, found, err := c.Store.GetRun(ctx, currentRun.ID)
	if err != nil {
		return result, err
	}
	if !found {
		return result, fmt.Errorf("updated run %s could not be reloaded after context collection", currentRun.ID)
	}
	result.Run = latestRun
	contextArtifactPath := ""
	contextArtifactPreview := ""
	if latestRun.CollectedContext != nil {
		contextArtifactPath = strings.TrimSpace(latestRun.CollectedContext.ArtifactPath)
		contextArtifactPreview = strings.TrimSpace(latestRun.CollectedContext.ArtifactPreview)
	}

	for _, item := range collectedContext.Results {
		contextArtifactPath, contextArtifactPreview := persistCollectedContextArtifact(latestRun, item)
		if err := c.Journal.Append(journal.Event{
			Type:                 "context.collection.recorded",
			RunID:                latestRun.ID,
			RepoPath:             latestRun.RepoPath,
			Goal:                 latestRun.Goal,
			Status:               string(latestRun.Status),
			Message:              contextCollectionMessage(item),
			ResponseID:           currentPlannerTurn.ResponseID,
			PreviousResponseID:   latestRun.PreviousResponseID,
			PlannerOutcome:       string(currentPlannerTurn.Output.Outcome),
			ContextRequestedPath: item.RequestedPath,
			ContextResolvedPath:  item.ResolvedPath,
			ContextKind:          item.Kind,
			ContextDetail:        item.Detail,
			ContextPreview:       contextCollectionPreview(item),
			ArtifactPath:         contextArtifactPath,
			ArtifactPreview:      contextArtifactPreview,
			Checkpoint:           checkpointRef(&latestRun.LatestCheckpoint),
		}); err != nil {
			return result, err
		}
		if item.Detail == "read_failed" || item.Detail == "stat_failed" {
			if err := c.Journal.Append(journal.Event{
				Type:                 "context.collection.read_failed",
				RunID:                latestRun.ID,
				RepoPath:             latestRun.RepoPath,
				Goal:                 latestRun.Goal,
				Status:               string(latestRun.Status),
				Message:              contextCollectionMessage(item),
				ResponseID:           currentPlannerTurn.ResponseID,
				PreviousResponseID:   latestRun.PreviousResponseID,
				PlannerOutcome:       string(currentPlannerTurn.Output.Outcome),
				ContextRequestedPath: item.RequestedPath,
				ContextResolvedPath:  item.ResolvedPath,
				ContextKind:          item.Kind,
				ContextDetail:        item.Detail,
				ContextPreview:       contextCollectionPreview(item),
				ArtifactPath:         contextArtifactPath,
				ArtifactPreview:      contextArtifactPreview,
				Checkpoint:           checkpointRef(&latestRun.LatestCheckpoint),
			}); err != nil {
				return result, err
			}
		}
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "context.collection.completed",
		RunID:              latestRun.ID,
		RepoPath:           latestRun.RepoPath,
		Goal:               latestRun.Goal,
		Status:             string(latestRun.Status),
		Message:            fmt.Sprintf("collected %d path result(s), %d plugin tool result(s), %d worker action result(s), and worker plan present=%t", len(collectedContext.Results), len(collectedContext.ToolResults), len(collectedContext.WorkerResults), collectedContext.WorkerPlan != nil),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: latestRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		ArtifactPath:       contextArtifactPath,
		ArtifactPreview:    contextArtifactPreview,
		Checkpoint:         checkpointRef(&latestRun.LatestCheckpoint),
	}); err != nil {
		return result, err
	}

	postCollectionEvents, err := c.Journal.ReadRecent(latestRun.ID, 8)
	if err != nil {
		return result, err
	}

	postCollectionInput := BuildPlannerInput(latestRun, postCollectionEvents, nil, BuildCollectedContextInput(latestRun), nil, c.pluginToolDescriptors())
	result, err = c.runSecondPlannerTurn(
		ctx,
		latestRun,
		result,
		postCollectionInput,
		string(currentPlannerTurn.Output.Outcome),
		"planner_turn_post_collect_context",
		"post-collect-context planner response validated and persisted",
		"post-collect-context planner checkpoint persisted",
		"post-collect-context planner turn failed",
	)
	if err != nil {
		return result, err
	}

	if workerPlanStopReason(collectedContext.WorkerPlan) != "" && result.Run.ID != "" {
		if err := c.Store.SaveLatestStopReason(ctx, result.Run.ID, workerPlanStopReason(collectedContext.WorkerPlan)); err != nil {
			return result, err
		}
		result.Run.LatestStopReason = workerPlanStopReason(collectedContext.WorkerPlan)
	}
	return result, nil
}

func (c Cycle) handleExecute(
	ctx context.Context,
	result Result,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
) (Result, error) {
	if err := c.clearPendingAction(ctx, currentRun); err != nil {
		return result, err
	}
	if c.Executor == nil {
		err := errors.New("executor is required when planner outcome is execute")
		failedRun, issueErr := c.persistRuntimeIssue(ctx, currentRun, StopReasonTransportProcessError, err.Error())
		if issueErr != nil {
			return result, errors.Join(err, issueErr)
		}
		result.Run = failedRun
		return result, err
	}

	prompt, err := RenderExecutorPrompt(currentRun.Goal, currentPlannerTurn.Output)
	if err != nil {
		failedRun, issueErr := c.persistRuntimeIssue(ctx, currentRun, StopReasonTransportProcessError, err.Error())
		if issueErr != nil {
			return result, errors.Join(err, issueErr)
		}
		result.Run = failedRun
		return result, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "executor.turn.dispatched",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "planner execute outcome dispatched to primary executor: " + ExecutorTask(currentPlannerTurn.Output),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
	}); err != nil {
		return result, err
	}
	c.emitEvent("executor_turn_started", currentRun, map[string]any{
		"task":        ExecutorTask(currentPlannerTurn.Output),
		"response_id": currentPlannerTurn.ResponseID,
	})

	executorResult, execErr := c.Executor.Execute(ctx, executor.TurnRequest{
		RunID:      currentRun.ID,
		RepoPath:   currentRun.RepoPath,
		Prompt:     prompt,
		ThreadID:   currentRun.ExecutorThreadID,
		ThreadPath: currentRun.ExecutorThreadPath,
	})

	executorErrorMessage := executorFailureMessage(executorResult)
	if executorErrorMessage == "" && execErr != nil {
		executorErrorMessage = execErr.Error()
	}
	approvalRequired := executorResult.TurnStatus == executor.TurnStatusApprovalRequired || executorResult.ApprovalState == executor.ApprovalStateRequired
	executorFailed := execErr != nil || executorResult.TurnStatus != executor.TurnStatusCompleted || executorResult.CompletedAt.IsZero()
	if approvalRequired {
		executorFailed = false
	}
	if executorFailed && executorErrorMessage == "" {
		executorErrorMessage = executorFailureFallbackMessage(executorResult)
	}

	executorState := state.ExecutorState{
		Transport:        string(executorResult.Transport),
		ThreadID:         executorResult.ThreadID,
		ThreadPath:       executorResult.ThreadPath,
		TurnID:           executorResult.TurnID,
		TurnStatus:       string(executorResult.TurnStatus),
		LastSuccess:      executorSuccess(executorResult),
		LastFailureStage: executorFailureStage(executorResult),
		LastError:        executorErrorMessage,
		LastMessage:      executorResult.FinalMessage,
		Approval:         executorApprovalState(executorResult),
	}

	var executorCheckpoint *state.Checkpoint
	if approvalRequired || executorFailed {
		if err := c.Store.SaveExecutorState(ctx, currentRun.ID, executorState); err != nil {
			return result, err
		}
	} else {
		checkpoint := BuildExecutorCheckpoint(currentRun.LatestCheckpoint, executorCheckpointLabel(executorResult.TurnStatus), executorResult.CompletedAt)
		executorCheckpoint = &checkpoint
		if err := c.Store.SaveExecutorTurn(ctx, currentRun.ID, executorState, checkpoint); err != nil {
			return result, err
		}
	}

	executorFailureErr := execErr
	if executorFailed && executorFailureErr == nil {
		executorFailureErr = errors.New(executorErrorMessage)
	}

	if executorFailed {
		if _, issueErr := c.persistRuntimeIssue(ctx, currentRun, StopReasonExecutorFailed, executorErrorMessage); issueErr != nil {
			return result, errors.Join(executorFailureErr, issueErr)
		}
	}

	latestRun, found, err := c.Store.GetRun(ctx, currentRun.ID)
	if err != nil {
		return result, err
	}
	if !found {
		return result, fmt.Errorf("updated run %s could not be reloaded after executor dispatch", currentRun.ID)
	}
	executorArtifactPath, executorArtifactPreview := persistExecutorOutputArtifact(latestRun, executorResult)

	eventType := "executor.turn.completed"
	eventMessage := "executor turn completed"
	eventAt := executorResult.CompletedAt
	stopReason := ""
	if approvalRequired {
		eventType = "executor.turn.paused"
		eventMessage = executorApprovalMessage(executorResult)
		stopReason = StopReasonExecutorApprovalReq
		if eventAt.IsZero() {
			eventAt = time.Now().UTC()
		}
	} else if executorFailed {
		eventType = "executor.turn.failed"
		eventMessage = executorErrorMessage
		stopReason = StopReasonExecutorFailed
		if eventAt.IsZero() {
			eventAt = time.Now().UTC()
		}
	}

	if err := c.Journal.Append(journal.Event{
		At:                    eventAt,
		Type:                  eventType,
		RunID:                 latestRun.ID,
		RepoPath:              latestRun.RepoPath,
		Goal:                  latestRun.Goal,
		Status:                string(latestRun.Status),
		Message:               eventMessage,
		ResponseID:            currentPlannerTurn.ResponseID,
		PreviousResponseID:    latestRun.PreviousResponseID,
		PlannerOutcome:        string(currentPlannerTurn.Output.Outcome),
		StopReason:            stopReason,
		ExecutorTransport:     string(executorResult.Transport),
		ExecutorThreadID:      executorResult.ThreadID,
		ExecutorThreadPath:    executorResult.ThreadPath,
		ExecutorTurnID:        executorResult.TurnID,
		ExecutorTurnStatus:    string(executorResult.TurnStatus),
		ExecutorApprovalState: string(executorResult.ApprovalState),
		ExecutorApprovalKind:  executorApprovalKind(executorResult),
		ExecutorFailureStage:  executorFailureStage(executorResult),
		ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
		ArtifactPath:          executorArtifactPath,
		ArtifactPreview:       executorArtifactPreview,
		Checkpoint:            checkpointRef(executorCheckpoint),
	}); err != nil {
		return result, err
	}
	c.emitEvent(executorEngineEventName(approvalRequired, executorFailed), latestRun, map[string]any{
		"turn_status":       string(executorResult.TurnStatus),
		"thread_id":         executorResult.ThreadID,
		"turn_id":           executorResult.TurnID,
		"stop_reason":       stopReason,
		"failure_stage":     executorFailureStage(executorResult),
		"error_message":     executorErrorMessage,
		"model":             strings.TrimSpace(executorResult.Model),
		"model_provider":    strings.TrimSpace(executorResult.ModelProvider),
		"model_unavailable": modelUnavailableFromExecutorError(executorErrorMessage),
		"output_preview":    previewString(executorResult.FinalMessage, 240),
	})

	if executorCheckpoint != nil {
		if err := c.Journal.Append(journal.Event{
			At:                    executorCheckpoint.CreatedAt,
			Type:                  "checkpoint.persisted",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Status:                string(latestRun.Status),
			Message:               "executor checkpoint persisted",
			ResponseID:            currentPlannerTurn.ResponseID,
			PreviousResponseID:    latestRun.PreviousResponseID,
			PlannerOutcome:        string(currentPlannerTurn.Output.Outcome),
			ExecutorTransport:     string(executorResult.Transport),
			ExecutorThreadID:      executorResult.ThreadID,
			ExecutorThreadPath:    executorResult.ThreadPath,
			ExecutorTurnID:        executorResult.TurnID,
			ExecutorTurnStatus:    string(executorResult.TurnStatus),
			ExecutorFailureStage:  executorFailureStage(executorResult),
			ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
			Checkpoint:            checkpointRef(executorCheckpoint),
		}); err != nil {
			return result, err
		}
	}

	result.Run = latestRun
	result.ExecutorDispatched = true
	result.ExecutorResult = &executorResult
	c.runPluginHooks(ctx, plugins.HookExecutorAfter, latestRun, &currentPlannerTurn, &executorResult, stopReason, executorFailureErr)

	if approvalRequired {
		if err := c.Store.SaveLatestStopReason(ctx, latestRun.ID, StopReasonExecutorApprovalReq); err != nil {
			return result, err
		}
		latestRun.LatestStopReason = StopReasonExecutorApprovalReq
		result.Run = latestRun

		approvalEvent := journal.Event{
			Type:                  "executor.approval.required",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               executorApprovalMessage(executorResult),
			ResponseID:            currentPlannerTurn.ResponseID,
			PreviousResponseID:    latestRun.PreviousResponseID,
			PlannerOutcome:        string(currentPlannerTurn.Output.Outcome),
			StopReason:            StopReasonExecutorApprovalReq,
			ExecutorTransport:     string(executorResult.Transport),
			ExecutorThreadID:      executorResult.ThreadID,
			ExecutorThreadPath:    executorResult.ThreadPath,
			ExecutorTurnID:        executorResult.TurnID,
			ExecutorTurnStatus:    string(executorResult.TurnStatus),
			ExecutorApprovalState: string(executorResult.ApprovalState),
			ExecutorApprovalKind:  executorApprovalKind(executorResult),
			ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
			Checkpoint:            checkpointRef(&latestRun.LatestCheckpoint),
		}
		if err := c.Journal.Append(approvalEvent); err != nil {
			return result, err
		}
		return result, nil
	}

	reportArtifactPath, reportArtifactPreview := relocateKnownReportArtifact(latestRun, currentPlannerTurn.Output)
	if reportArtifactPath != "" {
		reportEvent := journal.Event{
			Type:               "report.artifact.recorded",
			RunID:              latestRun.ID,
			RepoPath:           latestRun.RepoPath,
			Goal:               latestRun.Goal,
			Status:             string(latestRun.Status),
			Message:            "orchestration report moved under .orchestrator/artifacts/reports",
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: latestRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			ArtifactPath:       reportArtifactPath,
			ArtifactPreview:    reportArtifactPreview,
			Checkpoint:         checkpointRef(executorCheckpoint),
		}
		populateExecutorEventFields(&reportEvent, latestRun)
		if err := c.Journal.Append(reportEvent); err != nil {
			return result, err
		}
	}

	if executorFailed {
		return result, executorFailureErr
	}

	postExecutorEvents, err := c.Journal.ReadRecent(latestRun.ID, 8)
	if err != nil {
		return result, err
	}

	postExecutorInput := BuildPlannerInput(latestRun, postExecutorEvents, BuildExecutorResultInput(latestRun), nil, nil, c.pluginToolDescriptors())
	result, err = c.runSecondPlannerTurn(
		ctx,
		latestRun,
		result,
		postExecutorInput,
		string(currentPlannerTurn.Output.Outcome),
		"planner_turn_post_executor",
		"post-executor planner response validated and persisted",
		"post-executor planner checkpoint persisted",
		"post-executor planner turn failed",
	)
	return result, joinErrors(execErr, err)
}

func (c Cycle) continueExecutorTurn(ctx context.Context, currentRun state.Run) (Result, error) {
	result := Result{Run: currentRun}

	if err := c.clearPendingAction(ctx, currentRun); err != nil {
		return result, err
	}

	if c.Executor == nil {
		err := errors.New("executor is required when continuing an active executor turn")
		failedRun, issueErr := c.persistRuntimeIssue(ctx, currentRun, StopReasonTransportProcessError, err.Error())
		if issueErr != nil {
			return result, errors.Join(err, issueErr)
		}
		result.Run = failedRun
		return result, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "executor.turn.resumed",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "continuing persisted active executor turn",
		PreviousResponseID: currentRun.PreviousResponseID,
		ExecutorTransport:  currentRun.ExecutorTransport,
		ExecutorThreadID:   currentRun.ExecutorThreadID,
		ExecutorThreadPath: currentRun.ExecutorThreadPath,
		ExecutorTurnID:     currentRun.ExecutorTurnID,
		ExecutorTurnStatus: currentRun.ExecutorTurnStatus,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return result, err
	}

	c.emitEvent("executor_turn_started", currentRun, map[string]any{
		"phase":     "continue",
		"thread_id": currentRun.ExecutorThreadID,
		"turn_id":   currentRun.ExecutorTurnID,
	})

	executorResult, execErr := c.Executor.Execute(ctx, executor.TurnRequest{
		RunID:      currentRun.ID,
		RepoPath:   currentRun.RepoPath,
		ThreadID:   currentRun.ExecutorThreadID,
		ThreadPath: currentRun.ExecutorThreadPath,
		TurnID:     currentRun.ExecutorTurnID,
		Continue:   true,
	})

	executorErrorMessage := executorFailureMessage(executorResult)
	if executorErrorMessage == "" && execErr != nil {
		executorErrorMessage = execErr.Error()
	}
	approvalRequired := executorResult.TurnStatus == executor.TurnStatusApprovalRequired || executorResult.ApprovalState == executor.ApprovalStateRequired
	executorFailed := execErr != nil || executorResult.TurnStatus != executor.TurnStatusCompleted || executorResult.CompletedAt.IsZero()
	if approvalRequired {
		executorFailed = false
	}
	if executorFailed && executorErrorMessage == "" {
		executorErrorMessage = executorFailureFallbackMessage(executorResult)
	}

	executorState := state.ExecutorState{
		Transport:        string(executorResult.Transport),
		ThreadID:         executorResult.ThreadID,
		ThreadPath:       executorResult.ThreadPath,
		TurnID:           executorResult.TurnID,
		TurnStatus:       string(executorResult.TurnStatus),
		LastSuccess:      executorSuccess(executorResult),
		LastFailureStage: executorFailureStage(executorResult),
		LastError:        executorErrorMessage,
		LastMessage:      executorResult.FinalMessage,
		Approval:         executorApprovalState(executorResult),
	}

	var executorCheckpoint *state.Checkpoint
	if approvalRequired || executorFailed {
		if err := c.Store.SaveExecutorState(ctx, currentRun.ID, executorState); err != nil {
			return result, err
		}
	} else {
		checkpoint := BuildExecutorCheckpoint(currentRun.LatestCheckpoint, executorCheckpointLabel(executorResult.TurnStatus), executorResult.CompletedAt)
		executorCheckpoint = &checkpoint
		if err := c.Store.SaveExecutorTurn(ctx, currentRun.ID, executorState, checkpoint); err != nil {
			return result, err
		}
	}

	executorFailureErr := execErr
	if executorFailed && executorFailureErr == nil {
		executorFailureErr = errors.New(executorErrorMessage)
	}
	if executorFailed {
		if _, issueErr := c.persistRuntimeIssue(ctx, currentRun, StopReasonExecutorFailed, executorErrorMessage); issueErr != nil {
			return result, errors.Join(executorFailureErr, issueErr)
		}
	}

	latestRun, found, err := c.Store.GetRun(ctx, currentRun.ID)
	if err != nil {
		return result, err
	}
	if !found {
		return result, fmt.Errorf("updated run %s could not be reloaded after executor continuation", currentRun.ID)
	}

	eventType := "executor.turn.completed"
	eventMessage := "executor turn completed"
	eventAt := executorResult.CompletedAt
	stopReason := ""
	if approvalRequired {
		eventType = "executor.turn.paused"
		eventMessage = executorApprovalMessage(executorResult)
		stopReason = StopReasonExecutorApprovalReq
		if eventAt.IsZero() {
			eventAt = time.Now().UTC()
		}
	} else if executorFailed {
		eventType = "executor.turn.failed"
		eventMessage = executorErrorMessage
		stopReason = StopReasonExecutorFailed
		if eventAt.IsZero() {
			eventAt = time.Now().UTC()
		}
	}

	if err := c.Journal.Append(journal.Event{
		At:                    eventAt,
		Type:                  eventType,
		RunID:                 latestRun.ID,
		RepoPath:              latestRun.RepoPath,
		Goal:                  latestRun.Goal,
		Status:                string(latestRun.Status),
		Message:               eventMessage,
		PreviousResponseID:    latestRun.PreviousResponseID,
		StopReason:            stopReason,
		ExecutorTransport:     string(executorResult.Transport),
		ExecutorThreadID:      executorResult.ThreadID,
		ExecutorThreadPath:    executorResult.ThreadPath,
		ExecutorTurnID:        executorResult.TurnID,
		ExecutorTurnStatus:    string(executorResult.TurnStatus),
		ExecutorApprovalState: string(executorResult.ApprovalState),
		ExecutorApprovalKind:  executorApprovalKind(executorResult),
		ExecutorFailureStage:  executorFailureStage(executorResult),
		ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
		Checkpoint:            checkpointRef(executorCheckpoint),
	}); err != nil {
		return result, err
	}
	c.emitEvent(executorEngineEventName(approvalRequired, executorFailed), latestRun, map[string]any{
		"phase":             "continue",
		"turn_status":       string(executorResult.TurnStatus),
		"thread_id":         executorResult.ThreadID,
		"turn_id":           executorResult.TurnID,
		"stop_reason":       stopReason,
		"failure_stage":     executorFailureStage(executorResult),
		"error_message":     executorErrorMessage,
		"model":             strings.TrimSpace(executorResult.Model),
		"model_provider":    strings.TrimSpace(executorResult.ModelProvider),
		"model_unavailable": modelUnavailableFromExecutorError(executorErrorMessage),
		"output_preview":    previewString(executorResult.FinalMessage, 240),
	})

	if executorCheckpoint != nil {
		if err := c.Journal.Append(journal.Event{
			At:                    executorCheckpoint.CreatedAt,
			Type:                  "checkpoint.persisted",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Status:                string(latestRun.Status),
			Message:               "executor checkpoint persisted",
			PreviousResponseID:    latestRun.PreviousResponseID,
			ExecutorTransport:     string(executorResult.Transport),
			ExecutorThreadID:      executorResult.ThreadID,
			ExecutorThreadPath:    executorResult.ThreadPath,
			ExecutorTurnID:        executorResult.TurnID,
			ExecutorTurnStatus:    string(executorResult.TurnStatus),
			ExecutorApprovalState: string(executorResult.ApprovalState),
			ExecutorApprovalKind:  executorApprovalKind(executorResult),
			ExecutorFailureStage:  executorFailureStage(executorResult),
			ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
			Checkpoint:            checkpointRef(executorCheckpoint),
		}); err != nil {
			return result, err
		}
	}

	result.Run = latestRun
	result.ExecutorDispatched = true
	result.ExecutorResult = &executorResult
	c.runPluginHooks(ctx, plugins.HookExecutorAfter, latestRun, nil, &executorResult, stopReason, executorFailureErr)

	if approvalRequired {
		if err := c.Store.SaveLatestStopReason(ctx, latestRun.ID, StopReasonExecutorApprovalReq); err != nil {
			return result, err
		}
		latestRun.LatestStopReason = StopReasonExecutorApprovalReq
		result.Run = latestRun
		if err := c.Journal.Append(journal.Event{
			Type:                  "executor.approval.required",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               executorApprovalMessage(executorResult),
			PreviousResponseID:    latestRun.PreviousResponseID,
			StopReason:            StopReasonExecutorApprovalReq,
			ExecutorTransport:     string(executorResult.Transport),
			ExecutorThreadID:      executorResult.ThreadID,
			ExecutorThreadPath:    executorResult.ThreadPath,
			ExecutorTurnID:        executorResult.TurnID,
			ExecutorTurnStatus:    string(executorResult.TurnStatus),
			ExecutorApprovalState: string(executorResult.ApprovalState),
			ExecutorApprovalKind:  executorApprovalKind(executorResult),
			ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
			Checkpoint:            checkpointRef(&latestRun.LatestCheckpoint),
		}); err != nil {
			return result, err
		}
		return result, nil
	}

	if executorFailed {
		return result, executorFailureErr
	}

	postExecutorEvents, err := c.Journal.ReadRecent(latestRun.ID, 8)
	if err != nil {
		return result, err
	}

	postExecutorInput := BuildPlannerInput(latestRun, postExecutorEvents, BuildExecutorResultInput(latestRun), nil, nil, c.pluginToolDescriptors())
	result, err = c.runSecondPlannerTurn(
		ctx,
		latestRun,
		result,
		postExecutorInput,
		"",
		"planner_turn_post_executor",
		"post-executor planner response validated and persisted",
		"post-executor planner checkpoint persisted",
		"post-executor planner turn failed",
	)
	return result, joinErrors(execErr, err)
}

func (c Cycle) activeWorkerTurns(ctx context.Context, runID string) ([]state.Worker, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, nil
	}

	workers, err := c.Store.ListWorkers(ctx, runID)
	if err != nil {
		return nil, err
	}

	active := make([]state.Worker, 0, len(workers))
	for _, worker := range workers {
		if hasContinuableWorkerTurn(worker) {
			active = append(active, worker)
		}
	}
	return active, nil
}

func hasContinuableWorkerTurn(worker state.Worker) bool {
	if strings.TrimSpace(worker.WorktreePath) == "" ||
		strings.TrimSpace(worker.ExecutorThreadID) == "" ||
		strings.TrimSpace(worker.ExecutorTurnID) == "" {
		return false
	}

	switch worker.WorkerStatus {
	case state.WorkerStatusExecutorActive, state.WorkerStatusApprovalRequired:
		return true
	default:
		return false
	}
}

func (c Cycle) continueWorkerPlan(ctx context.Context, currentRun state.Run, workers []state.Worker) (state.Run, bool, error) {
	if len(workers) == 0 {
		return currentRun, false, nil
	}
	if err := c.clearPendingAction(ctx, currentRun); err != nil {
		return currentRun, true, err
	}
	if c.Executor == nil {
		err := errors.New("executor is required when continuing active worker executor turns")
		failedRun, issueErr := c.persistRuntimeIssue(ctx, currentRun, StopReasonTransportProcessError, err.Error())
		if issueErr != nil {
			return currentRun, true, errors.Join(err, issueErr)
		}
		return failedRun, true, err
	}

	concurrencyLimit := c.workerPlanConcurrencyLimit()
	if err := c.Journal.Append(journal.Event{
		Type:               "worker.plan.dispatch.started",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            fmt.Sprintf("continuing %d active worker executor turn(s) concurrently with limit=%d", len(workers), concurrencyLimit),
		PreviousResponseID: currentRun.PreviousResponseID,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return currentRun, true, err
	}

	if err := c.executeConcurrentWorkerContinuations(ctx, currentRun, workers, concurrencyLimit); err != nil {
		return currentRun, true, err
	}

	updatedRun, err := c.syncRunWorkerPlanState(ctx, currentRun.ID)
	if err != nil {
		return currentRun, true, err
	}

	updatedWorkers, err := c.Store.ListWorkers(ctx, currentRun.ID)
	if err != nil {
		return updatedRun, true, err
	}
	approvalCount := workerStatusCount(updatedWorkers, state.WorkerStatusApprovalRequired)
	if approvalCount > 0 {
		if err := c.Store.SaveLatestStopReason(ctx, updatedRun.ID, StopReasonExecutorApprovalReq); err != nil {
			return updatedRun, true, err
		}
		reloadedRun, found, err := c.Store.GetRun(ctx, updatedRun.ID)
		if err != nil {
			return updatedRun, true, err
		}
		if found {
			updatedRun = reloadedRun
		}
		if err := c.Journal.Append(journal.Event{
			Type:               "worker.plan.waiting_on_approval",
			RunID:              updatedRun.ID,
			RepoPath:           updatedRun.RepoPath,
			Goal:               updatedRun.Goal,
			Status:             string(updatedRun.Status),
			Message:            fmt.Sprintf("worker plan is waiting on approval for %d worker executor turn(s)", approvalCount),
			PreviousResponseID: updatedRun.PreviousResponseID,
			StopReason:         StopReasonExecutorApprovalReq,
			Checkpoint:         checkpointRef(&updatedRun.LatestCheckpoint),
		}); err != nil {
			return updatedRun, true, err
		}
		return updatedRun, true, nil
	}

	if strings.TrimSpace(updatedRun.LatestStopReason) == StopReasonExecutorApprovalReq && executorApprovalStateValueFromRun(updatedRun) == "" {
		if err := c.Store.ClearLatestStopReason(ctx, updatedRun.ID); err != nil {
			return updatedRun, true, err
		}
		reloadedRun, found, err := c.Store.GetRun(ctx, updatedRun.ID)
		if err != nil {
			return updatedRun, true, err
		}
		if found {
			updatedRun = reloadedRun
		}
	}

	return updatedRun, false, nil
}

func (c Cycle) executeConcurrentWorkerContinuations(
	ctx context.Context,
	currentRun state.Run,
	workers []state.Worker,
	concurrencyLimit int,
) error {
	type continuationOutcome struct {
		err error
	}

	if len(workers) == 0 {
		return nil
	}

	sem := make(chan struct{}, concurrencyLimit)
	outcomes := make(chan continuationOutcome, len(workers))
	var wg sync.WaitGroup

	for _, worker := range workers {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			outcomes <- continuationOutcome{
				err: c.continueWorkerTurn(ctx, currentRun, worker),
			}
		}()
	}

	wg.Wait()
	close(outcomes)

	var joinedErr error
	for outcome := range outcomes {
		if outcome.err != nil {
			joinedErr = errors.Join(joinedErr, outcome.err)
		}
	}
	return joinedErr
}

func (c Cycle) continueWorkerTurn(ctx context.Context, currentRun state.Run, worker state.Worker) error {
	if !hasContinuableWorkerTurn(worker) {
		return nil
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.executor.started",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "continuing persisted worker executor turn",
		PreviousResponseID: currentRun.PreviousResponseID,
		WorkerID:           worker.ID,
		WorkerName:         worker.WorkerName,
		WorkerStatus:       string(worker.WorkerStatus),
		WorkerScope:        worker.AssignedScope,
		WorkerPath:         worker.WorktreePath,
		ExecutorThreadID:   worker.ExecutorThreadID,
		ExecutorTurnID:     worker.ExecutorTurnID,
		ExecutorTurnStatus: worker.ExecutorTurnStatus,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return err
	}

	executorResult, execErr := c.Executor.Execute(ctx, executor.TurnRequest{
		RunID:    currentRun.ID,
		RepoPath: worker.WorktreePath,
		ThreadID: worker.ExecutorThreadID,
		TurnID:   worker.ExecutorTurnID,
		Continue: true,
	})

	worker.ExecutorThreadID = strings.TrimSpace(executorResult.ThreadID)
	worker.ExecutorTurnID = strings.TrimSpace(executorResult.TurnID)
	worker.ExecutorTurnStatus = string(executorResult.TurnStatus)
	worker.ExecutorApprovalState = string(executorResult.ApprovalState)
	worker.ExecutorApprovalKind = executorApprovalKind(executorResult)
	worker.ExecutorApprovalPreview = workerApprovalPreview(executorResult)
	worker.ExecutorInterruptible = executorResult.Interruptible
	worker.ExecutorSteerable = executorResult.Steerable
	worker.ExecutorFailureStage = executorFailureStage(executorResult)
	worker.ExecutorApproval = executorApprovalState(executorResult)

	resultMessage := previewString(executorResult.FinalMessage, 240)
	if resultMessage == "" && executorResult.Error != nil {
		resultMessage = previewString(executorFailureMessage(executorResult), 240)
	}
	if resultMessage == "" && execErr != nil {
		resultMessage = previewString(execErr.Error(), 240)
	}

	eventType := "worker.executor.completed"
	switch {
	case executorResult.TurnStatus == executor.TurnStatusApprovalRequired || executorResult.ApprovalState == executor.ApprovalStateRequired:
		worker.WorkerStatus = state.WorkerStatusApprovalRequired
		worker.WorkerResultSummary = executorApprovalMessage(executorResult)
		worker.WorkerErrorSummary = ""
		worker.CompletedAt = time.Time{}
		eventType = "worker.executor.approval_required"
	case execErr != nil || executorResult.TurnStatus == executor.TurnStatusFailed || executorResult.TurnStatus == executor.TurnStatusInterrupted || executorResult.CompletedAt.IsZero():
		worker.WorkerStatus = state.WorkerStatusFailed
		if resultMessage == "" {
			resultMessage = previewString(executorFailureFallbackMessage(executorResult), 240)
		}
		worker.WorkerResultSummary = resultMessage
		worker.WorkerErrorSummary = resultMessage
		worker.CompletedAt = time.Now().UTC()
		eventType = "worker.executor.failed"
	default:
		worker.WorkerStatus = state.WorkerStatusCompleted
		if resultMessage == "" {
			resultMessage = "worker executor turn completed"
		}
		worker.WorkerResultSummary = resultMessage
		worker.WorkerErrorSummary = ""
		if executorResult.CompletedAt.IsZero() {
			worker.CompletedAt = time.Now().UTC()
		} else {
			worker.CompletedAt = executorResult.CompletedAt.UTC()
		}
	}

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

	if err := c.Store.SaveWorker(ctx, worker); err != nil {
		return err
	}

	if err := c.Journal.Append(journal.Event{
		Type:                  eventType,
		RunID:                 currentRun.ID,
		RepoPath:              currentRun.RepoPath,
		Goal:                  currentRun.Goal,
		Status:                string(currentRun.Status),
		Message:               fallbackString(strings.TrimSpace(worker.WorkerResultSummary), "worker executor continuation settled"),
		PreviousResponseID:    currentRun.PreviousResponseID,
		WorkerID:              worker.ID,
		WorkerName:            worker.WorkerName,
		WorkerStatus:          string(worker.WorkerStatus),
		WorkerScope:           worker.AssignedScope,
		WorkerPath:            worker.WorktreePath,
		ExecutorTransport:     string(executorResult.Transport),
		ExecutorThreadID:      worker.ExecutorThreadID,
		ExecutorTurnID:        worker.ExecutorTurnID,
		ExecutorTurnStatus:    worker.ExecutorTurnStatus,
		ExecutorApprovalState: worker.ExecutorApprovalState,
		ExecutorApprovalKind:  worker.ExecutorApprovalKind,
		ExecutorFailureStage:  worker.ExecutorFailureStage,
		ExecutorControlAction: strings.TrimSpace(worker.ExecutorLastControlAction),
		ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
		Checkpoint:            checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return err
	}

	if worker.WorkerStatus == state.WorkerStatusApprovalRequired {
		if err := c.Journal.Append(journal.Event{
			Type:                  "worker.approval.required",
			RunID:                 currentRun.ID,
			RepoPath:              currentRun.RepoPath,
			Goal:                  currentRun.Goal,
			Status:                string(currentRun.Status),
			Message:               fallbackString(strings.TrimSpace(worker.ExecutorApprovalPreview), executorApprovalMessage(executorResult)),
			PreviousResponseID:    currentRun.PreviousResponseID,
			WorkerID:              worker.ID,
			WorkerName:            worker.WorkerName,
			WorkerStatus:          string(worker.WorkerStatus),
			WorkerScope:           worker.AssignedScope,
			WorkerPath:            worker.WorktreePath,
			ExecutorThreadID:      worker.ExecutorThreadID,
			ExecutorTurnID:        worker.ExecutorTurnID,
			ExecutorTurnStatus:    worker.ExecutorTurnStatus,
			ExecutorApprovalState: worker.ExecutorApprovalState,
			ExecutorApprovalKind:  worker.ExecutorApprovalKind,
			StopReason:            StopReasonExecutorApprovalReq,
			Checkpoint:            checkpointRef(&currentRun.LatestCheckpoint),
		}); err != nil {
			return err
		}
	}

	return c.Journal.Append(journal.Event{
		Type:               "worker.result.recorded",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            fallbackString(strings.TrimSpace(worker.WorkerResultSummary), "worker result recorded"),
		PreviousResponseID: currentRun.PreviousResponseID,
		WorkerID:           worker.ID,
		WorkerName:         worker.WorkerName,
		WorkerStatus:       string(worker.WorkerStatus),
		WorkerScope:        worker.AssignedScope,
		WorkerPath:         worker.WorktreePath,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	})
}

func (c Cycle) syncRunWorkerPlanState(ctx context.Context, runID string) (state.Run, error) {
	run, found, err := c.Store.GetRun(ctx, runID)
	if err != nil {
		return state.Run{}, err
	}
	if !found {
		return state.Run{}, fmt.Errorf("run %s not found", runID)
	}
	if run.CollectedContext == nil || run.CollectedContext.WorkerPlan == nil {
		return run, nil
	}

	workers, err := c.Store.ListWorkers(ctx, runID)
	if err != nil {
		return state.Run{}, err
	}
	summaries := make([]state.WorkerResultSummary, 0, len(workers))
	workerIDs := make([]string, 0, len(workers))
	for _, worker := range workers {
		summaries = append(summaries, summarizeWorker(worker))
		workerIDs = append(workerIDs, worker.ID)
	}

	collectedContext := *run.CollectedContext
	workerPlan := *collectedContext.WorkerPlan
	workerPlan.Workers = summaries
	workerPlan.WorkerIDs = workerIDs
	workerPlan.Status = workerPlanStatusFromWorkers(summaries)
	workerPlan.Message = workerPlanMessageFromWorkers(summaries, workerPlan.ConcurrencyLimit)
	collectedContext.WorkerPlan = &workerPlan
	if err := c.Store.SaveCollectedContext(ctx, runID, &collectedContext); err != nil {
		return state.Run{}, err
	}

	updatedRun, found, err := c.Store.GetRun(ctx, runID)
	if err != nil {
		return state.Run{}, err
	}
	if !found {
		return state.Run{}, fmt.Errorf("updated run %s not found after worker plan sync", runID)
	}
	return updatedRun, nil
}

func workerPlanStatusFromWorkers(workers []state.WorkerResultSummary) string {
	if len(workers) == 0 {
		return ""
	}
	approvalCount := 0
	activeCount := 0
	failedCount := 0
	for _, worker := range workers {
		switch strings.TrimSpace(worker.WorkerStatus) {
		case string(state.WorkerStatusApprovalRequired):
			approvalCount++
		case string(state.WorkerStatusExecutorActive), string(state.WorkerStatusAssigned), string(state.WorkerStatusPending), string(state.WorkerStatusCreating):
			activeCount++
		case string(state.WorkerStatusFailed):
			failedCount++
		}
	}
	switch {
	case approvalCount > 0:
		return "approval_required"
	case activeCount > 0:
		return "in_progress"
	case failedCount > 0:
		return "failed"
	default:
		return "completed"
	}
}

func workerPlanMessageFromWorkers(workers []state.WorkerResultSummary, concurrencyLimit int) string {
	if len(workers) == 0 {
		return ""
	}
	completedCount := 0
	failedCount := 0
	approvalCount := 0
	activeCount := 0
	for _, worker := range workers {
		switch strings.TrimSpace(worker.WorkerStatus) {
		case string(state.WorkerStatusCompleted):
			completedCount++
		case string(state.WorkerStatusFailed):
			failedCount++
		case string(state.WorkerStatusApprovalRequired):
			approvalCount++
		case string(state.WorkerStatusExecutorActive), string(state.WorkerStatusAssigned), string(state.WorkerStatusPending), string(state.WorkerStatusCreating):
			activeCount++
		}
	}
	return fmt.Sprintf("worker plan settled with completed=%d failed=%d approval_required=%d active=%d (limit=%d)", completedCount, failedCount, approvalCount, activeCount, concurrencyLimit)
}

func workerStatusCount(workers []state.Worker, status state.WorkerStatus) int {
	count := 0
	for _, worker := range workers {
		if worker.WorkerStatus == status {
			count++
		}
	}
	return count
}

func executorApprovalStateValueFromRun(run state.Run) string {
	if run.ExecutorApproval == nil {
		return ""
	}
	return strings.TrimSpace(run.ExecutorApproval.State)
}

func driftReviewMessage(summary planner.DriftReviewSummary) string {
	if summary.Aligned {
		return "drift watcher found no roadmap-alignment concerns"
	}
	if len(summary.Concerns) > 0 {
		return "drift watcher recorded concerns: " + previewString(strings.Join(summary.Concerns, "; "), 240)
	}
	if len(summary.MissingContext) > 0 {
		return "drift watcher recorded missing context: " + previewString(strings.Join(summary.MissingContext, "; "), 240)
	}
	return "drift watcher completed"
}

func BuildCollectedContextState(repoPath string, output planner.OutputEnvelope) (*state.CollectedContextState, error) {
	if !ShouldCollectContext(output) {
		return nil, errors.New("planner outcome is not collect_context")
	}

	collected := &state.CollectedContextState{
		Focus:         strings.TrimSpace(output.CollectContext.Focus),
		Questions:     nonEmpty(output.CollectContext.Questions),
		Results:       make([]state.CollectedContextResult, 0, len(nonEmpty(output.CollectContext.Paths))),
		ToolResults:   nil,
		WorkerResults: nil,
		WorkerPlan:    nil,
	}

	for _, requestedPath := range nonEmpty(output.CollectContext.Paths) {
		collected.Results = append(collected.Results, inspectCollectedContextPath(repoPath, requestedPath))
	}

	return collected, nil
}

func (c Cycle) executeWorkerActions(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	actions []planner.WorkerAction,
) ([]state.WorkerActionResult, error) {
	results := make([]state.WorkerActionResult, 0, len(actions))
	for _, action := range actions {
		actionResult, err := c.executeWorkerAction(ctx, currentRun, currentPlannerTurn, action)
		if err != nil {
			return nil, err
		}
		results = append(results, actionResult)
	}
	return results, nil
}

func (c Cycle) executeWorkerPlan(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	plan planner.WorkerPlan,
) (*state.WorkerPlanResult, []state.WorkerActionResult, error) {
	concurrencyLimit := c.workerPlanConcurrencyLimit()

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.plan.received",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            fmt.Sprintf("planner submitted worker plan with %d worker(s)", len(plan.Workers)),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return nil, nil, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.plan.started",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            fmt.Sprintf("executing planner-owned worker plan concurrently in isolated workspaces (limit=%d)", concurrencyLimit),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return nil, nil, err
	}

	actionResults := make([]state.WorkerActionResult, 0, len(plan.Workers)*2+2)
	createdWorkerIDs := make([]string, 0, len(plan.Workers))
	queuedDispatches := make([]planner.WorkerAction, 0, len(plan.Workers))
	workerFailures := 0
	workerApprovals := 0

	for _, requestedWorker := range plan.Workers {
		createResult, err := c.executeWorkerCreate(ctx, currentRun, currentPlannerTurn, planner.WorkerAction{
			Action:     planner.WorkerActionCreate,
			WorkerName: requestedWorker.Name,
			Scope:      requestedWorker.Scope,
		})
		if err != nil {
			if appendErr := c.appendWorkerPlanFailureEvent(currentRun, currentPlannerTurn, err.Error()); appendErr != nil {
				return nil, nil, appendErr
			}
			return nil, nil, err
		}
		actionResults = append(actionResults, createResult)

		var workerID string
		if createResult.Worker != nil {
			workerID = strings.TrimSpace(createResult.Worker.WorkerID)
		}
		if !createResult.Success || workerID == "" {
			workerFailures++
			continue
		}
		createdWorkerIDs = append(createdWorkerIDs, workerID)

		queueResult, err := c.queueWorkerPlanDispatch(ctx, currentRun, currentPlannerTurn, planner.WorkerAction{
			Action:         planner.WorkerActionDispatch,
			WorkerID:       workerID,
			TaskSummary:    requestedWorker.TaskSummary,
			ExecutorPrompt: requestedWorker.ExecutorPrompt,
		})
		if err != nil {
			if appendErr := c.appendWorkerPlanFailureEvent(currentRun, currentPlannerTurn, err.Error()); appendErr != nil {
				return nil, nil, appendErr
			}
			return nil, nil, err
		}
		actionResults = append(actionResults, queueResult)
		if !queueResult.Success {
			workerFailures++
			continue
		}
		queuedDispatches = append(queuedDispatches, planner.WorkerAction{
			Action:         planner.WorkerActionDispatch,
			WorkerID:       workerID,
			TaskSummary:    requestedWorker.TaskSummary,
			ExecutorPrompt: requestedWorker.ExecutorPrompt,
		})
	}

	planResult := &state.WorkerPlanResult{
		Status:               "completed",
		WorkerIDs:            append([]string(nil), createdWorkerIDs...),
		ConcurrencyLimit:     concurrencyLimit,
		IntegrationRequested: plan.IntegrationRequested,
		ApplyMode:            strings.TrimSpace(plan.ApplyMode),
	}

	if planResult.ApplyMode == "" {
		planResult.ApplyMode = string(planner.WorkerApplyModeUnavailable)
	}

	if len(queuedDispatches) > 0 {
		if err := c.Journal.Append(journal.Event{
			Type:               "worker.plan.dispatch.started",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            fmt.Sprintf("dispatching %d worker executor turn(s) concurrently with limit=%d", len(queuedDispatches), concurrencyLimit),
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}); err != nil {
			return nil, nil, err
		}

		dispatchResults, err := c.executeConcurrentWorkerPlanDispatches(ctx, currentRun, currentPlannerTurn, queuedDispatches, concurrencyLimit)
		if err != nil {
			if appendErr := c.appendWorkerPlanFailureEvent(currentRun, currentPlannerTurn, err.Error()); appendErr != nil {
				return nil, nil, appendErr
			}
			return nil, nil, err
		}
		actionResults = append(actionResults, dispatchResults...)

		completedDispatches := 0
		for _, dispatchResult := range dispatchResults {
			if dispatchResult.Worker == nil {
				if !dispatchResult.Success {
					workerFailures++
				}
				continue
			}
			switch strings.TrimSpace(dispatchResult.Worker.WorkerStatus) {
			case string(state.WorkerStatusCompleted):
				completedDispatches++
			case string(state.WorkerStatusApprovalRequired):
				workerApprovals++
			default:
				if !dispatchResult.Success {
					workerFailures++
				}
			}
		}

		if err := c.Journal.Append(journal.Event{
			Type:               "worker.plan.dispatch.completed",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            fmt.Sprintf("worker executor dispatch settled: completed=%d failed=%d approval_required=%d", completedDispatches, workerFailures, workerApprovals),
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}); err != nil {
			return nil, nil, err
		}

		if workerApprovals > 0 {
			if err := c.Journal.Append(journal.Event{
				Type:               "worker.plan.waiting_on_approval",
				RunID:              currentRun.ID,
				RepoPath:           currentRun.RepoPath,
				Goal:               currentRun.Goal,
				Status:             string(currentRun.Status),
				Message:            fmt.Sprintf("worker plan is waiting on approval for %d worker executor turn(s)", workerApprovals),
				ResponseID:         currentPlannerTurn.ResponseID,
				PreviousResponseID: currentRun.PreviousResponseID,
				PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
				StopReason:         StopReasonExecutorApprovalReq,
				Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
			}); err != nil {
				return nil, nil, err
			}
		}
	}

	if len(createdWorkerIDs) > 0 {
		workers := make([]state.WorkerResultSummary, 0, len(createdWorkerIDs))
		for _, workerID := range createdWorkerIDs {
			worker, found, err := c.Store.GetWorker(ctx, workerID)
			if err != nil {
				if appendErr := c.appendWorkerPlanFailureEvent(currentRun, currentPlannerTurn, err.Error()); appendErr != nil {
					return nil, nil, appendErr
				}
				return nil, nil, err
			}
			if !found {
				continue
			}
			workers = append(workers, summarizeWorker(worker))
		}
		planResult.Workers = workers
	}

	var integrationResult state.WorkerActionResult
	if plan.IntegrationRequested && len(createdWorkerIDs) > 0 {
		result, err := c.executeWorkerIntegrate(ctx, currentRun, currentPlannerTurn, planner.WorkerAction{
			Action:    planner.WorkerActionIntegrate,
			WorkerIDs: append([]string(nil), createdWorkerIDs...),
		})
		if err != nil {
			if appendErr := c.appendWorkerPlanFailureEvent(currentRun, currentPlannerTurn, err.Error()); appendErr != nil {
				return nil, nil, appendErr
			}
			return nil, nil, err
		}
		integrationResult = result
		actionResults = append(actionResults, integrationResult)
		if integrationResult.Integration != nil {
			planResult.IntegrationArtifactPath = strings.TrimSpace(integrationResult.ArtifactPath)
			planResult.IntegrationPreview = strings.TrimSpace(integrationResult.Integration.IntegrationPreview)
		}
		if !integrationResult.Success {
			workerFailures++
		}
	}

	if planResult.ApplyMode != string(planner.WorkerApplyModeUnavailable) {
		if strings.TrimSpace(planResult.IntegrationArtifactPath) != "" && allWorkerPlanWorkersCompleted(planResult.Workers) {
			applyResult, err := c.executeWorkerApply(ctx, currentRun, currentPlannerTurn, planner.WorkerAction{
				Action:       planner.WorkerActionApply,
				ArtifactPath: planResult.IntegrationArtifactPath,
				ApplyMode:    planResult.ApplyMode,
			})
			if err != nil {
				if appendErr := c.appendWorkerPlanFailureEvent(currentRun, currentPlannerTurn, err.Error()); appendErr != nil {
					return nil, nil, appendErr
				}
				return nil, nil, err
			}
			actionResults = append(actionResults, applyResult)
			planResult.ApplyArtifactPath = strings.TrimSpace(applyResult.ArtifactPath)
			planResult.Apply = applyResult.Apply
			if !applyResult.Success {
				workerFailures++
			}
		} else {
			workerFailures++
			planResult.Message = "worker plan apply requested but skipped because integration output was unavailable or one or more worker turns did not complete successfully"
		}
	}

	switch {
	case workerApprovals > 0:
		planResult.Status = "approval_required"
		if strings.TrimSpace(planResult.Message) == "" {
			planResult.Message = fmt.Sprintf("worker plan is waiting on approval for %d isolated worker(s)", workerApprovals)
		}
	case workerFailures > 0:
		planResult.Status = "failed"
		if strings.TrimSpace(planResult.Message) == "" {
			planResult.Message = fmt.Sprintf("worker plan recorded %d mechanical failure(s) or skipped apply step(s)", workerFailures)
		}
		if err := c.Journal.Append(journal.Event{
			Type:               "worker.plan.failed",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            planResult.Message,
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			ArtifactPath:       firstNonEmpty(planResult.ApplyArtifactPath, planResult.IntegrationArtifactPath, applyArtifactPath(planResult.Apply)),
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}); err != nil {
			return nil, nil, err
		}
	}

	if strings.TrimSpace(planResult.Message) == "" {
		planResult.Message = fmt.Sprintf("worker plan completed across %d isolated worker(s) with concurrency limit=%d", len(planResult.Workers), concurrencyLimit)
	}

	if planResult.Status == "completed" {
		if err := c.Journal.Append(journal.Event{
			Type:               "worker.plan.completed",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            planResult.Message,
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			ArtifactPath:       firstNonEmpty(planResult.ApplyArtifactPath, planResult.IntegrationArtifactPath, applyArtifactPath(planResult.Apply)),
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}); err != nil {
			return nil, nil, err
		}
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.plan.finished",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            planResult.Message,
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		ArtifactPath:       firstNonEmpty(planResult.ApplyArtifactPath, planResult.IntegrationArtifactPath, applyArtifactPath(planResult.Apply)),
		StopReason:         workerPlanStopReason(planResult),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return nil, nil, err
	}

	return planResult, actionResults, nil
}

func (c Cycle) executeConcurrentWorkerPlanDispatches(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	actions []planner.WorkerAction,
	concurrencyLimit int,
) ([]state.WorkerActionResult, error) {
	type dispatchOutcome struct {
		index  int
		result state.WorkerActionResult
		err    error
	}

	results := make([]state.WorkerActionResult, len(actions))
	if len(actions) == 0 {
		return results, nil
	}

	sem := make(chan struct{}, concurrencyLimit)
	outcomes := make(chan dispatchOutcome, len(actions))
	var wg sync.WaitGroup

	for index, action := range actions {
		wg.Add(1)
		go func(index int, action planner.WorkerAction) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			result, err := c.executeWorkerPlanDispatch(ctx, currentRun, currentPlannerTurn, action)
			outcomes <- dispatchOutcome{
				index:  index,
				result: result,
				err:    err,
			}
		}(index, action)
	}

	wg.Wait()
	close(outcomes)

	var joinedErr error
	for outcome := range outcomes {
		results[outcome.index] = outcome.result
		if outcome.err != nil {
			joinedErr = errors.Join(joinedErr, outcome.err)
		}
	}

	return results, joinedErr
}

func workerPlanStopReason(planResult *state.WorkerPlanResult) string {
	if planResult == nil {
		return ""
	}
	if strings.TrimSpace(planResult.Status) == "approval_required" {
		return StopReasonExecutorApprovalReq
	}
	return ""
}

func (c Cycle) workerPlanConcurrencyLimit() int {
	if c.WorkerPlanConcurrencyLimit > 0 {
		return c.WorkerPlanConcurrencyLimit
	}
	return 2
}

func (c Cycle) executeWorkerAction(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	switch action.Action {
	case planner.WorkerActionCreate:
		return c.executeWorkerCreate(ctx, currentRun, currentPlannerTurn, action)
	case planner.WorkerActionDispatch:
		return c.executeWorkerDispatch(ctx, currentRun, currentPlannerTurn, action)
	case planner.WorkerActionList:
		return c.executeWorkerList(ctx, currentRun, currentPlannerTurn, action)
	case planner.WorkerActionRemove:
		return c.executeWorkerRemove(ctx, currentRun, currentPlannerTurn, action)
	case planner.WorkerActionIntegrate:
		return c.executeWorkerIntegrate(ctx, currentRun, currentPlannerTurn, action)
	case planner.WorkerActionApply:
		return c.executeWorkerApply(ctx, currentRun, currentPlannerTurn, action)
	default:
		return state.WorkerActionResult{
			Action:  string(action.Action),
			Success: false,
			Message: "unknown worker action",
		}, nil
	}
}

func (c Cycle) executeWorkerCreate(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	manager := workerctl.NewManager(currentRun.RepoPath, state.ResolveLayout(currentRun.RepoPath).WorkersDir)
	plannedPath, err := manager.PlannedPath(action.WorkerName)
	if err != nil {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, err.Error(), false)
	}

	if existingWorker, found, err := c.Store.GetWorkerByPath(ctx, plannedPath); err != nil {
		return state.WorkerActionResult{}, err
	} else if found {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &existingWorker, "worker already exists for planned isolated path", false)
	}

	worker, err := c.Store.CreateWorker(ctx, state.CreateWorkerParams{
		RunID:         currentRun.ID,
		WorkerName:    strings.TrimSpace(action.WorkerName),
		WorkerStatus:  state.WorkerStatusCreating,
		AssignedScope: strings.TrimSpace(action.Scope),
		WorktreePath:  plannedPath,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &state.Worker{
			RunID:         currentRun.ID,
			WorkerName:    strings.TrimSpace(action.WorkerName),
			WorkerStatus:  state.WorkerStatusCreating,
			AssignedScope: strings.TrimSpace(action.Scope),
			WorktreePath:  plannedPath,
		}, err.Error(), false)
	}

	if _, err := manager.Create(ctx, worker.WorkerName); err != nil {
		_ = c.Store.DeleteWorker(ctx, worker.ID)
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &worker, err.Error(), false)
	}

	worker.WorkerStatus = state.WorkerStatusIdle
	worker.UpdatedAt = time.Now().UTC()
	worker.WorkerResultSummary = "isolated worker created; awaiting explicit dispatch"
	if err := c.Store.SaveWorker(ctx, worker); err != nil {
		return state.WorkerActionResult{}, err
	}

	reloaded, found, err := c.Store.GetWorker(ctx, worker.ID)
	if err != nil {
		return state.WorkerActionResult{}, err
	}
	if !found {
		return state.WorkerActionResult{}, fmt.Errorf("worker %s could not be reloaded after creation", worker.ID)
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.result.recorded",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "planner worker create completed: " + reloaded.WorkerName,
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		WorkerID:           reloaded.ID,
		WorkerName:         reloaded.WorkerName,
		WorkerStatus:       string(reloaded.WorkerStatus),
		WorkerScope:        reloaded.AssignedScope,
		WorkerPath:         reloaded.WorktreePath,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	return state.WorkerActionResult{
		Action:  string(action.Action),
		Success: true,
		Message: "worker created; awaiting explicit dispatch",
		Worker:  func() *state.WorkerResultSummary { summary := summarizeWorker(reloaded); return &summary }(),
	}, nil
}

func (c Cycle) executeWorkerDispatch(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	worker, found, err := c.resolveWorkerForAction(ctx, currentRun.ID, action)
	if err != nil {
		return state.WorkerActionResult{}, err
	}
	if !found {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, "worker not found for dispatch", false)
	}
	if err := c.validateWorkerDispatchTarget(currentRun, worker); err != nil {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &worker, err.Error(), false)
	}
	if err := c.prepareWorkerDispatchState(ctx, currentRun, currentPlannerTurn, &worker, action, state.WorkerStatusAssigned, "planner assigned worker task"); err != nil {
		return state.WorkerActionResult{}, err
	}
	return c.runWorkerExecutorTurn(ctx, currentRun, currentPlannerTurn, action, worker)
}

func (c Cycle) queueWorkerPlanDispatch(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	worker, found, err := c.resolveWorkerForAction(ctx, currentRun.ID, action)
	if err != nil {
		return state.WorkerActionResult{}, err
	}
	if !found {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, "worker not found for plan dispatch", false)
	}
	if err := c.validateWorkerDispatchTarget(currentRun, worker); err != nil {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &worker, err.Error(), false)
	}
	if err := c.prepareWorkerDispatchState(ctx, currentRun, currentPlannerTurn, &worker, action, state.WorkerStatusPending, "planner queued worker task"); err != nil {
		return state.WorkerActionResult{}, err
	}

	workerSummary := summarizeWorker(worker)
	return state.WorkerActionResult{
		Action:  string(action.Action),
		Success: true,
		Message: "worker queued for executor dispatch",
		Worker:  &workerSummary,
	}, nil
}

func (c Cycle) executeWorkerPlanDispatch(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	worker, found, err := c.resolveWorkerForAction(ctx, currentRun.ID, action)
	if err != nil {
		return state.WorkerActionResult{}, err
	}
	if !found {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, "worker not found for plan dispatch", false)
	}
	if err := c.validateWorkerDispatchTarget(currentRun, worker); err != nil {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &worker, err.Error(), false)
	}
	return c.runWorkerExecutorTurn(ctx, currentRun, currentPlannerTurn, action, worker)
}

func (c Cycle) validateWorkerDispatchTarget(currentRun state.Run, worker state.Worker) error {
	if state.IsWorkerActive(worker.WorkerStatus) && worker.WorkerStatus != state.WorkerStatusPending {
		return errors.New("worker already has active work")
	}
	if workerUsesMainTree(currentRun.RepoPath, worker.WorktreePath) {
		return errors.New("worker path may not reuse the main repo working tree")
	}
	if _, err := os.Stat(worker.WorktreePath); err != nil {
		return fmt.Errorf("worker worktree path is unavailable: %w", err)
	}
	return nil
}

func (c Cycle) prepareWorkerDispatchState(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	worker *state.Worker,
	action planner.WorkerAction,
	status state.WorkerStatus,
	messagePrefix string,
) error {
	now := time.Now().UTC()
	worker.WorkerStatus = status
	worker.WorkerTaskSummary = previewString(action.TaskSummary, 240)
	worker.WorkerExecutorPromptSummary = previewString(action.ExecutorPrompt, 240)
	worker.WorkerResultSummary = ""
	worker.WorkerErrorSummary = ""
	worker.ExecutorTurnStatus = ""
	worker.ExecutorApprovalState = ""
	worker.ExecutorApprovalKind = ""
	worker.ExecutorApprovalPreview = ""
	worker.ExecutorInterruptible = false
	worker.ExecutorSteerable = false
	worker.ExecutorFailureStage = ""
	worker.ExecutorLastControlAction = ""
	worker.ExecutorApproval = nil
	worker.ExecutorLastControl = nil
	worker.AssignedAt = now
	worker.StartedAt = time.Time{}
	worker.CompletedAt = time.Time{}
	worker.UpdatedAt = now
	if err := c.Store.SaveWorker(ctx, *worker); err != nil {
		return err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.task.assigned",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            messagePrefix + ": " + previewString(action.TaskSummary, 160),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		WorkerID:           worker.ID,
		WorkerName:         worker.WorkerName,
		WorkerStatus:       string(worker.WorkerStatus),
		WorkerScope:        worker.AssignedScope,
		WorkerPath:         worker.WorktreePath,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return err
	}

	return nil
}

func (c Cycle) runWorkerExecutorTurn(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
	worker state.Worker,
) (state.WorkerActionResult, error) {
	if c.Executor == nil {
		now := time.Now().UTC()
		worker.WorkerStatus = state.WorkerStatusFailed
		worker.WorkerResultSummary = "executor unavailable"
		worker.WorkerErrorSummary = "executor unavailable"
		worker.ExecutorFailureStage = "start"
		worker.CompletedAt = now
		worker.UpdatedAt = now
		if err := c.Store.SaveWorker(ctx, worker); err != nil {
			return state.WorkerActionResult{}, err
		}
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &worker, "executor is required when planner dispatches worker work", false)
	}

	startedAt := time.Now().UTC()
	worker.WorkerStatus = state.WorkerStatusExecutorActive
	worker.StartedAt = startedAt
	worker.CompletedAt = time.Time{}
	worker.ExecutorTurnStatus = string(executor.TurnStatusInProgress)
	worker.ExecutorApprovalState = ""
	worker.ExecutorApprovalKind = ""
	worker.ExecutorFailureStage = ""
	worker.WorkerResultSummary = ""
	worker.WorkerErrorSummary = ""
	worker.UpdatedAt = startedAt
	if err := c.Store.SaveWorker(ctx, worker); err != nil {
		return state.WorkerActionResult{}, err
	}

	startMessage := "planner dispatched one executor turn to worker: " + previewString(action.TaskSummary, 160)
	for _, eventType := range []string{"worker.executor.started", "worker.executor.dispatched"} {
		if err := c.Journal.Append(journal.Event{
			Type:               eventType,
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            startMessage,
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			WorkerID:           worker.ID,
			WorkerName:         worker.WorkerName,
			WorkerStatus:       string(worker.WorkerStatus),
			WorkerScope:        worker.AssignedScope,
			WorkerPath:         worker.WorktreePath,
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}); err != nil {
			return state.WorkerActionResult{}, err
		}
	}

	executorResult, execErr := c.Executor.Execute(ctx, executor.TurnRequest{
		RunID:    currentRun.ID,
		RepoPath: worker.WorktreePath,
		Prompt:   strings.TrimSpace(action.ExecutorPrompt),
		ThreadID: worker.ExecutorThreadID,
	})

	worker.ExecutorThreadID = strings.TrimSpace(executorResult.ThreadID)
	worker.ExecutorTurnID = strings.TrimSpace(executorResult.TurnID)
	worker.ExecutorTurnStatus = string(executorResult.TurnStatus)
	worker.ExecutorApprovalState = string(executorResult.ApprovalState)
	worker.ExecutorApprovalKind = executorApprovalKind(executorResult)
	worker.ExecutorApprovalPreview = workerApprovalPreview(executorResult)
	worker.ExecutorInterruptible = executorResult.Interruptible
	worker.ExecutorSteerable = executorResult.Steerable
	worker.ExecutorFailureStage = executorFailureStage(executorResult)
	worker.ExecutorApproval = executorApprovalState(executorResult)

	resultMessage := previewString(executorResult.FinalMessage, 240)
	if resultMessage == "" && executorResult.Error != nil {
		resultMessage = previewString(executorFailureMessage(executorResult), 240)
	}
	if resultMessage == "" && execErr != nil {
		resultMessage = previewString(execErr.Error(), 240)
	}

	eventType := "worker.executor.completed"
	success := true
	switch {
	case executorResult.TurnStatus == executor.TurnStatusApprovalRequired || executorResult.ApprovalState == executor.ApprovalStateRequired:
		worker.WorkerStatus = state.WorkerStatusApprovalRequired
		worker.WorkerResultSummary = executorApprovalMessage(executorResult)
		worker.WorkerErrorSummary = ""
		worker.CompletedAt = time.Time{}
		success = false
		eventType = "worker.executor.approval_required"
	case execErr != nil || executorResult.TurnStatus == executor.TurnStatusFailed || executorResult.TurnStatus == executor.TurnStatusInterrupted || executorResult.CompletedAt.IsZero():
		worker.WorkerStatus = state.WorkerStatusFailed
		if resultMessage == "" {
			resultMessage = previewString(executorFailureFallbackMessage(executorResult), 240)
		}
		worker.WorkerResultSummary = resultMessage
		worker.WorkerErrorSummary = resultMessage
		worker.CompletedAt = time.Now().UTC()
		success = false
		eventType = "worker.executor.failed"
	default:
		worker.WorkerStatus = state.WorkerStatusCompleted
		if resultMessage == "" {
			resultMessage = "worker executor turn completed"
		}
		worker.WorkerResultSummary = resultMessage
		worker.WorkerErrorSummary = ""
		if executorResult.CompletedAt.IsZero() {
			worker.CompletedAt = time.Now().UTC()
		} else {
			worker.CompletedAt = executorResult.CompletedAt.UTC()
		}
	}
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

	if err := c.Store.SaveWorker(ctx, worker); err != nil {
		return state.WorkerActionResult{}, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:                  eventType,
		RunID:                 currentRun.ID,
		RepoPath:              currentRun.RepoPath,
		Goal:                  currentRun.Goal,
		Status:                string(currentRun.Status),
		Message:               fallbackString(strings.TrimSpace(worker.WorkerResultSummary), "worker executor dispatch finished"),
		ResponseID:            currentPlannerTurn.ResponseID,
		PreviousResponseID:    currentRun.PreviousResponseID,
		PlannerOutcome:        string(currentPlannerTurn.Output.Outcome),
		WorkerID:              worker.ID,
		WorkerName:            worker.WorkerName,
		WorkerStatus:          string(worker.WorkerStatus),
		WorkerScope:           worker.AssignedScope,
		WorkerPath:            worker.WorktreePath,
		ExecutorTransport:     string(executorResult.Transport),
		ExecutorThreadID:      executorResult.ThreadID,
		ExecutorTurnID:        executorResult.TurnID,
		ExecutorTurnStatus:    string(executorResult.TurnStatus),
		ExecutorApprovalState: string(executorResult.ApprovalState),
		ExecutorApprovalKind:  executorApprovalKind(executorResult),
		ExecutorFailureStage:  executorFailureStage(executorResult),
		ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
		Checkpoint:            checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	if worker.WorkerStatus == state.WorkerStatusApprovalRequired {
		if err := c.Journal.Append(journal.Event{
			Type:                  "worker.approval.required",
			RunID:                 currentRun.ID,
			RepoPath:              currentRun.RepoPath,
			Goal:                  currentRun.Goal,
			Status:                string(currentRun.Status),
			Message:               fallbackString(strings.TrimSpace(worker.ExecutorApprovalPreview), executorApprovalMessage(executorResult)),
			ResponseID:            currentPlannerTurn.ResponseID,
			PreviousResponseID:    currentRun.PreviousResponseID,
			PlannerOutcome:        string(currentPlannerTurn.Output.Outcome),
			WorkerID:              worker.ID,
			WorkerName:            worker.WorkerName,
			WorkerStatus:          string(worker.WorkerStatus),
			WorkerScope:           worker.AssignedScope,
			WorkerPath:            worker.WorktreePath,
			ExecutorThreadID:      worker.ExecutorThreadID,
			ExecutorTurnID:        worker.ExecutorTurnID,
			ExecutorTurnStatus:    worker.ExecutorTurnStatus,
			ExecutorApprovalState: worker.ExecutorApprovalState,
			ExecutorApprovalKind:  worker.ExecutorApprovalKind,
			StopReason:            StopReasonExecutorApprovalReq,
			Checkpoint:            checkpointRef(&currentRun.LatestCheckpoint),
		}); err != nil {
			return state.WorkerActionResult{}, err
		}
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.result.recorded",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            fallbackString(strings.TrimSpace(worker.WorkerResultSummary), "worker result recorded"),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		WorkerID:           worker.ID,
		WorkerName:         worker.WorkerName,
		WorkerStatus:       string(worker.WorkerStatus),
		WorkerScope:        worker.AssignedScope,
		WorkerPath:         worker.WorktreePath,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	workerSummary := summarizeWorker(worker)
	return state.WorkerActionResult{
		Action:  string(action.Action),
		Success: success,
		Message: worker.WorkerResultSummary,
		Worker:  &workerSummary,
	}, nil
}

func (c Cycle) executeWorkerList(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	workers, err := c.Store.ListWorkers(ctx, currentRun.ID)
	if err != nil {
		return state.WorkerActionResult{}, err
	}

	listed := make([]state.WorkerResultSummary, 0, len(workers))
	for _, worker := range workers {
		listed = append(listed, summarizeWorker(worker))
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.result.recorded",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            fmt.Sprintf("listed %d worker(s) for planner inspection", len(listed)),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	return state.WorkerActionResult{
		Action:        string(action.Action),
		Success:       true,
		Message:       fmt.Sprintf("listed %d worker(s)", len(listed)),
		ListedWorkers: listed,
	}, nil
}

func (c Cycle) executeWorkerRemove(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	worker, found, err := c.resolveWorkerForAction(ctx, currentRun.ID, action)
	if err != nil {
		return state.WorkerActionResult{}, err
	}
	if !found {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, "worker not found for remove", false)
	}
	if state.IsWorkerActive(worker.WorkerStatus) {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &worker, "active workers cannot be removed until they are idle or completed", false)
	}

	manager := workerctl.NewManager(currentRun.RepoPath, state.ResolveLayout(currentRun.RepoPath).WorkersDir)
	if err := manager.Remove(ctx, worker.WorktreePath); err != nil {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, &worker, err.Error(), false)
	}
	if err := c.Store.DeleteWorker(ctx, worker.ID); err != nil {
		return state.WorkerActionResult{}, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.result.recorded",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "removed worker from isolated workspace registry: " + worker.WorkerName,
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		WorkerID:           worker.ID,
		WorkerName:         worker.WorkerName,
		WorkerStatus:       string(worker.WorkerStatus),
		WorkerScope:        worker.AssignedScope,
		WorkerPath:         worker.WorktreePath,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	workerSummary := summarizeWorker(worker)
	return state.WorkerActionResult{
		Action:  string(action.Action),
		Success: true,
		Message: "worker removed",
		Worker:  &workerSummary,
		Removed: true,
	}, nil
}

func (c Cycle) executeWorkerIntegrate(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	integrationSummary, artifactPath, artifactPreview, err := c.buildIntegrationArtifactForWorkerIDs(ctx, currentRun, currentPlannerTurn, action.WorkerIDs)
	if err != nil {
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, err.Error(), false)
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "integration.completed",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            integrationSummary.IntegrationPreview,
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		ArtifactPath:       artifactPath,
		ArtifactPreview:    artifactPreview,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.result.recorded",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            integrationSummary.IntegrationPreview,
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		ArtifactPath:       artifactPath,
		ArtifactPreview:    artifactPreview,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	return state.WorkerActionResult{
		Action:          string(action.Action),
		Success:         true,
		Message:         integrationSummary.IntegrationPreview,
		ArtifactPath:    artifactPath,
		ArtifactPreview: artifactPreview,
		Integration:     &integrationSummary,
	}, nil
}

func (c Cycle) executeWorkerApply(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
) (state.WorkerActionResult, error) {
	if err := c.Journal.Append(journal.Event{
		Type:               "integration.apply.started",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            "applying worker integration output using " + strings.TrimSpace(action.ApplyMode),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	sourceArtifactPath := filepath.ToSlash(strings.TrimSpace(action.ArtifactPath))
	var integrationSummary state.IntegrationSummary

	switch {
	case sourceArtifactPath != "":
		loadedSummary, resolvedArtifactPath, err := workerctl.LoadIntegrationArtifact(currentRun.RepoPath, sourceArtifactPath)
		if err != nil {
			if appendErr := c.appendIntegrationApplyFailureEvent(currentRun, currentPlannerTurn, err.Error(), "", ""); appendErr != nil {
				return state.WorkerActionResult{}, appendErr
			}
			return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, err.Error(), false)
		}
		integrationSummary = loadedSummary
		sourceArtifactPath = resolvedArtifactPath
	case len(nonEmpty(action.WorkerIDs)) > 0:
		loadedSummary, artifactPath, artifactPreview, err := c.buildIntegrationArtifactForWorkerIDs(ctx, currentRun, currentPlannerTurn, action.WorkerIDs)
		if err != nil {
			if appendErr := c.appendIntegrationApplyFailureEvent(currentRun, currentPlannerTurn, err.Error(), "", ""); appendErr != nil {
				return state.WorkerActionResult{}, appendErr
			}
			return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, err.Error(), false)
		}
		if err := c.Journal.Append(journal.Event{
			Type:               "integration.completed",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            loadedSummary.IntegrationPreview,
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			ArtifactPath:       artifactPath,
			ArtifactPreview:    artifactPreview,
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}); err != nil {
			return state.WorkerActionResult{}, err
		}
		integrationSummary = loadedSummary
		sourceArtifactPath = artifactPath
	default:
		message := "integration apply requires an integration artifact path or worker ids"
		if appendErr := c.appendIntegrationApplyFailureEvent(currentRun, currentPlannerTurn, message, "", ""); appendErr != nil {
			return state.WorkerActionResult{}, appendErr
		}
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, message, false)
	}

	applySummary, err := workerctl.ApplyIntegration(currentRun.RepoPath, integrationSummary, sourceArtifactPath, action.ApplyMode)
	if err != nil {
		if appendErr := c.appendIntegrationApplyFailureEvent(currentRun, currentPlannerTurn, err.Error(), sourceArtifactPath, ""); appendErr != nil {
			return state.WorkerActionResult{}, appendErr
		}
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, err.Error(), false)
	}

	artifactPath, artifactPreview := persistIntegrationApplyArtifact(currentRun, applySummary)
	if strings.TrimSpace(artifactPath) == "" {
		message := fallbackString(strings.TrimSpace(artifactPreview), "integration apply artifact write failed")
		if appendErr := c.appendIntegrationApplyFailureEvent(currentRun, currentPlannerTurn, message, sourceArtifactPath, ""); appendErr != nil {
			return state.WorkerActionResult{}, appendErr
		}
		return c.recordWorkerActionFailure(currentRun, currentPlannerTurn, action, nil, message, false)
	}

	eventType := "integration.apply.completed"
	if strings.TrimSpace(applySummary.Status) != "completed" {
		eventType = "integration.apply.failed"
	}
	message := fallbackString(strings.TrimSpace(applySummary.AfterSummary), "integration apply result recorded")

	if err := c.Journal.Append(journal.Event{
		Type:               eventType,
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            message,
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		ArtifactPath:       artifactPath,
		ArtifactPreview:    artifactPreview,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "worker.result.recorded",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            message,
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		ArtifactPath:       artifactPath,
		ArtifactPreview:    artifactPreview,
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.WorkerActionResult{}, err
	}

	return state.WorkerActionResult{
		Action:          string(action.Action),
		Success:         strings.TrimSpace(applySummary.Status) == "completed",
		Message:         message,
		ArtifactPath:    artifactPath,
		ArtifactPreview: artifactPreview,
		Apply:           &applySummary,
	}, nil
}

func (c Cycle) buildIntegrationArtifactForWorkerIDs(
	ctx context.Context,
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	workerIDs []string,
) (state.IntegrationSummary, string, string, error) {
	if err := c.Journal.Append(journal.Event{
		Type:               "integration.started",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            fmt.Sprintf("building integration preview from %d worker(s)", len(workerIDs)),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	}); err != nil {
		return state.IntegrationSummary{}, "", "", err
	}

	selectedWorkers := make([]state.Worker, 0, len(workerIDs))
	for _, workerID := range nonEmpty(workerIDs) {
		worker, found, err := c.Store.GetWorker(ctx, workerID)
		if err != nil {
			return state.IntegrationSummary{}, "", "", err
		}
		if !found {
			message := "worker not found for integration: " + workerID
			if appendErr := c.appendIntegrationFailureEvent(currentRun, currentPlannerTurn, message); appendErr != nil {
				return state.IntegrationSummary{}, "", "", appendErr
			}
			return state.IntegrationSummary{}, "", "", errors.New(message)
		}
		selectedWorkers = append(selectedWorkers, worker)
	}

	integrationSummary, err := workerctl.BuildIntegrationSummary(currentRun.RepoPath, selectedWorkers)
	if err != nil {
		if appendErr := c.appendIntegrationFailureEvent(currentRun, currentPlannerTurn, err.Error()); appendErr != nil {
			return state.IntegrationSummary{}, "", "", appendErr
		}
		return state.IntegrationSummary{}, "", "", err
	}

	artifactPath, artifactPreview := persistIntegrationArtifact(currentRun, integrationSummary)
	if strings.TrimSpace(artifactPath) == "" {
		message := fallbackString(strings.TrimSpace(artifactPreview), "integration artifact write failed")
		if appendErr := c.appendIntegrationFailureEvent(currentRun, currentPlannerTurn, message); appendErr != nil {
			return state.IntegrationSummary{}, "", "", appendErr
		}
		return state.IntegrationSummary{}, "", "", errors.New(message)
	}

	return integrationSummary, artifactPath, artifactPreview, nil
}

func (c Cycle) appendIntegrationFailureEvent(currentRun state.Run, currentPlannerTurn planner.Result, message string) error {
	return c.Journal.Append(journal.Event{
		Type:               "integration.failed",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            strings.TrimSpace(message),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	})
}

func (c Cycle) appendIntegrationApplyFailureEvent(currentRun state.Run, currentPlannerTurn planner.Result, message string, sourceArtifactPath string, applyArtifactPath string) error {
	return c.Journal.Append(journal.Event{
		Type:               "integration.apply.failed",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            strings.TrimSpace(message),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		ArtifactPath:       fallbackString(strings.TrimSpace(applyArtifactPath), strings.TrimSpace(sourceArtifactPath)),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	})
}

func (c Cycle) appendWorkerPlanFailureEvent(currentRun state.Run, currentPlannerTurn planner.Result, message string) error {
	return c.Journal.Append(journal.Event{
		Type:               "worker.plan.failed",
		RunID:              currentRun.ID,
		RepoPath:           currentRun.RepoPath,
		Goal:               currentRun.Goal,
		Status:             string(currentRun.Status),
		Message:            strings.TrimSpace(message),
		ResponseID:         currentPlannerTurn.ResponseID,
		PreviousResponseID: currentRun.PreviousResponseID,
		PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
	})
}

func (c Cycle) resolveWorkerForAction(ctx context.Context, runID string, action planner.WorkerAction) (state.Worker, bool, error) {
	if strings.TrimSpace(action.WorkerID) != "" {
		return c.Store.GetWorker(ctx, action.WorkerID)
	}

	workers, err := c.Store.ListWorkers(ctx, runID)
	if err != nil {
		return state.Worker{}, false, err
	}
	for _, worker := range workers {
		if strings.TrimSpace(worker.WorkerName) == strings.TrimSpace(action.WorkerName) {
			return worker, true, nil
		}
	}
	return state.Worker{}, false, nil
}

func summarizeWorker(worker state.Worker) state.WorkerResultSummary {
	return state.WorkerResultSummary{
		WorkerID:                   worker.ID,
		WorkerName:                 worker.WorkerName,
		WorkerStatus:               string(worker.WorkerStatus),
		AssignedScope:              worker.AssignedScope,
		WorktreePath:               worker.WorktreePath,
		WorkerTaskSummary:          strings.TrimSpace(worker.WorkerTaskSummary),
		ExecutorPromptSummary:      strings.TrimSpace(worker.WorkerExecutorPromptSummary),
		WorkerResultSummary:        strings.TrimSpace(worker.WorkerResultSummary),
		WorkerErrorSummary:         strings.TrimSpace(worker.WorkerErrorSummary),
		ExecutorThreadID:           strings.TrimSpace(worker.ExecutorThreadID),
		ExecutorTurnID:             strings.TrimSpace(worker.ExecutorTurnID),
		ExecutorTurnStatus:         strings.TrimSpace(worker.ExecutorTurnStatus),
		ExecutorApprovalState:      strings.TrimSpace(worker.ExecutorApprovalState),
		ExecutorApprovalKind:       strings.TrimSpace(worker.ExecutorApprovalKind),
		ExecutorApprovalPreview:    strings.TrimSpace(worker.ExecutorApprovalPreview),
		ExecutorInterruptible:      workerTurnInterruptible(worker),
		ExecutorSteerable:          workerTurnSteerable(worker),
		ExecutorFailureStage:       strings.TrimSpace(worker.ExecutorFailureStage),
		ExecutorLastControlAction:  strings.TrimSpace(worker.ExecutorLastControlAction),
		ExecutorLastControlPayload: workerLastControlPayload(worker),
		StartedAt:                  worker.StartedAt,
		CompletedAt:                worker.CompletedAt,
	}
}

func (c Cycle) recordWorkerActionFailure(
	currentRun state.Run,
	currentPlannerTurn planner.Result,
	action planner.WorkerAction,
	worker *state.Worker,
	message string,
	removed bool,
) (state.WorkerActionResult, error) {
	if c.Journal != nil {
		event := journal.Event{
			Type:               "worker.result.recorded",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            strings.TrimSpace(message),
			ResponseID:         currentPlannerTurn.ResponseID,
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(currentPlannerTurn.Output.Outcome),
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}
		if worker != nil {
			event.WorkerID = worker.ID
			event.WorkerName = worker.WorkerName
			event.WorkerStatus = string(worker.WorkerStatus)
			event.WorkerScope = worker.AssignedScope
			event.WorkerPath = worker.WorktreePath
		}
		if err := c.Journal.Append(event); err != nil {
			return state.WorkerActionResult{}, err
		}
	}

	var summary *state.WorkerResultSummary
	if worker != nil {
		workerCopy := summarizeWorker(*worker)
		summary = &workerCopy
	}

	return state.WorkerActionResult{
		Action:  string(action.Action),
		Success: false,
		Message: strings.TrimSpace(message),
		Worker:  summary,
		Removed: removed,
	}, nil
}

func workerUsesMainTree(repoRoot string, workerPath string) bool {
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	workerPath = filepath.Clean(strings.TrimSpace(workerPath))
	if repoRoot == "" || workerPath == "" {
		return false
	}
	if repoRoot == workerPath {
		return true
	}
	relative, err := filepath.Rel(repoRoot, workerPath)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func fallbackString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func applyArtifactPath(apply *state.IntegrationApplySummary) string {
	if apply == nil {
		return ""
	}
	return strings.TrimSpace(apply.SourceArtifactPath)
}

func allWorkerPlanWorkersCompleted(workers []state.WorkerResultSummary) bool {
	if len(workers) == 0 {
		return false
	}
	for _, worker := range workers {
		if strings.TrimSpace(worker.WorkerStatus) != string(state.WorkerStatusCompleted) {
			return false
		}
	}
	return true
}

func inspectCollectedContextPath(repoPath string, requestedPath string) state.CollectedContextResult {
	result := state.CollectedContextResult{
		RequestedPath: strings.TrimSpace(requestedPath),
		Kind:          "missing",
	}

	resolvedPath, detail := resolveCollectContextPath(repoPath, result.RequestedPath)
	result.ResolvedPath = resolvedPath
	if detail != "" {
		result.Detail = detail
		return result
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			result.Detail = "path_not_found"
			return result
		}
		result.Detail = "stat_failed"
		result.Preview = err.Error()
		return result
	}

	if info.IsDir() {
		entries, truncated, readErr := readDirectoryPreview(resolvedPath, maxCollectedDirEntries)
		result.Kind = "dir"
		result.Entries = entries
		result.Truncated = truncated
		if readErr != nil {
			result.Detail = "read_failed"
			result.Preview = readErr.Error()
		}
		return result
	}

	preview, truncated, readErr := readFilePreview(resolvedPath, maxCollectedFilePreviewBytes)
	result.Kind = "file"
	result.Preview = preview
	result.Truncated = truncated
	if readErr != nil {
		result.Detail = "read_failed"
		result.Preview = readErr.Error()
	}
	return result
}

func resolveCollectContextPath(repoPath string, requestedPath string) (string, string) {
	trimmed := strings.TrimSpace(requestedPath)
	if trimmed == "" {
		return "", "empty_path"
	}
	if filepath.IsAbs(trimmed) {
		return "", "absolute_path_not_allowed"
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", "path_outside_repo_root"
	}

	resolvedPath := filepath.Join(repoPath, cleaned)
	relativePath, err := filepath.Rel(repoPath, resolvedPath)
	if err != nil {
		return "", "path_outside_repo_root"
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", "path_outside_repo_root"
	}

	return resolvedPath, ""
}

func readFilePreview(path string, limit int) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	if limit <= 0 {
		limit = maxCollectedFilePreviewBytes
	}

	data, err := io.ReadAll(io.LimitReader(file, int64(limit+1)))
	if err != nil {
		return "", false, err
	}

	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
	}

	return string(data), truncated, nil
}

func readDirectoryPreview(path string, limit int) ([]string, bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, err
	}

	if limit <= 0 {
		limit = maxCollectedDirEntries
	}

	truncated := len(entries) > limit
	if truncated {
		entries = entries[:limit]
	}

	listing := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += string(filepath.Separator)
		}
		listing = append(listing, name)
	}

	return listing, truncated, nil
}

func contextCollectionMessage(item state.CollectedContextResult) string {
	switch item.Kind {
	case "file":
		return "collected file context for " + item.RequestedPath
	case "dir":
		return "collected directory context for " + item.RequestedPath
	default:
		if strings.TrimSpace(item.Detail) != "" {
			return "context path unresolved for " + item.RequestedPath + " (" + item.Detail + ")"
		}
		return "context path unresolved for " + item.RequestedPath
	}
}

func contextCollectionPreview(item state.CollectedContextResult) string {
	switch item.Kind {
	case "file":
		return previewString(item.Preview, 240)
	case "dir":
		preview := strings.Join(item.Entries, ", ")
		if item.Truncated {
			preview += ", ..."
		}
		return preview
	default:
		return strings.TrimSpace(item.Detail)
	}
}

func (c Cycle) persistRuntimeIssue(ctx context.Context, run state.Run, reason string, message string) (state.Run, error) {
	if strings.TrimSpace(reason) == "" {
		reason = StopReasonTransportProcessError
	}
	if err := c.Store.SaveRuntimeIssue(ctx, run.ID, reason, message); err != nil {
		return state.Run{}, err
	}

	updatedRun, found, err := c.Store.GetRun(ctx, run.ID)
	if err != nil {
		return state.Run{}, err
	}
	if !found {
		return state.Run{}, fmt.Errorf("updated run %s could not be reloaded after runtime issue persistence", run.ID)
	}

	c.runPluginHooks(ctx, plugins.HookFaultRecorded, updatedRun, nil, nil, reason, errors.New(strings.TrimSpace(message)))
	c.emitEvent("fault_recorded", updatedRun, map[string]any{
		"reason":  strings.TrimSpace(reason),
		"message": strings.TrimSpace(message),
	})

	return updatedRun, nil
}

func (c Cycle) persistPlannerValidationArtifact(run state.Run, err error) (string, string) {
	if !planner.IsValidationError(err) {
		return "", ""
	}

	rawResponse, rawOutput, responseID, ok := planner.ValidationErrorData(err)
	if !ok {
		return "", ""
	}

	content := strings.TrimSpace(rawOutput)
	if content == "" {
		content = strings.TrimSpace(rawResponse)
	}
	if content == "" {
		return "", ""
	}

	fileName := fmt.Sprintf("planner_validation_%s.txt", time.Now().UTC().Format("20060102T150405.000000000Z"))
	if strings.TrimSpace(responseID) != "" {
		fileName = fmt.Sprintf("planner_validation_%s_%s.txt", responseID, time.Now().UTC().Format("20060102T150405.000000000Z"))
	}

	relativePath, writeErr := writeRunArtifact(run, "planner", fileName, []byte(content))
	if writeErr != nil {
		return "", previewString("artifact_write_failed: "+writeErr.Error(), 240)
	}

	return relativePath, previewString(content, 240)
}

func populateExecutorEventFields(event *journal.Event, run state.Run) {
	if event == nil {
		return
	}

	event.ExecutorTransport = run.ExecutorTransport
	event.ExecutorThreadID = run.ExecutorThreadID
	event.ExecutorThreadPath = run.ExecutorThreadPath
	event.ExecutorTurnID = run.ExecutorTurnID
	event.ExecutorTurnStatus = run.ExecutorTurnStatus
	if run.ExecutorApproval != nil {
		event.ExecutorApprovalState = strings.TrimSpace(run.ExecutorApproval.State)
		event.ExecutorApprovalKind = strings.TrimSpace(run.ExecutorApproval.Kind)
	}
	if run.ExecutorLastControl != nil {
		event.ExecutorControlAction = strings.TrimSpace(run.ExecutorLastControl.Action)
	}
	event.ExecutorFailureStage = run.ExecutorLastFailureStage
	event.ExecutorOutputPreview = previewString(run.ExecutorLastMessage, 240)
}

func hasActiveExecutorTurn(run state.Run) bool {
	if strings.TrimSpace(run.ExecutorThreadID) == "" || strings.TrimSpace(run.ExecutorTurnID) == "" {
		return false
	}

	switch strings.TrimSpace(run.ExecutorTurnStatus) {
	case string(executor.TurnStatusInProgress), string(executor.TurnStatusApprovalRequired):
		return true
	default:
		return false
	}
}

func executorApprovalState(result executor.TurnResult) *state.ExecutorApproval {
	if result.Approval == nil && result.ApprovalState == executor.ApprovalStateNone {
		return nil
	}

	approval := &state.ExecutorApproval{
		State: string(result.ApprovalState),
	}
	if result.Approval == nil {
		return approval
	}

	approval.Kind = string(result.Approval.Kind)
	approval.RequestID = result.Approval.RequestID
	approval.ApprovalID = result.Approval.ApprovalID
	approval.ItemID = result.Approval.ItemID
	approval.Reason = result.Approval.Reason
	approval.Command = result.Approval.Command
	approval.CWD = result.Approval.CWD
	approval.GrantRoot = result.Approval.GrantRoot
	approval.RawParams = result.Approval.RawParams
	return approval
}

func executorApprovalKind(result executor.TurnResult) string {
	if result.Approval == nil {
		return ""
	}
	return string(result.Approval.Kind)
}

func executorApprovalMessage(result executor.TurnResult) string {
	if result.Approval == nil {
		return "executor turn requires approval before it can continue"
	}

	switch result.Approval.Kind {
	case executor.ApprovalKindCommandExecution:
		if strings.TrimSpace(result.Approval.Command) != "" {
			return "executor approval required for command: " + previewString(result.Approval.Command, 160)
		}
	case executor.ApprovalKindFileChange:
		if strings.TrimSpace(result.Approval.GrantRoot) != "" {
			return "executor approval required for file changes under: " + strings.TrimSpace(result.Approval.GrantRoot)
		}
	case executor.ApprovalKindPermissions:
		if strings.TrimSpace(result.Approval.Reason) != "" {
			return "executor approval required for permissions: " + previewString(result.Approval.Reason, 160)
		}
	}

	if strings.TrimSpace(result.Approval.Reason) != "" {
		return "executor approval required: " + previewString(result.Approval.Reason, 160)
	}
	return "executor turn requires approval before it can continue"
}

func workerApprovalPreview(result executor.TurnResult) string {
	if result.Approval == nil && result.ApprovalState != executor.ApprovalStateRequired {
		return ""
	}
	return previewString(executorApprovalMessage(result), 240)
}

func workerApprovalRequired(worker state.Worker) bool {
	return strings.TrimSpace(worker.ExecutorApprovalState) == string(executor.ApprovalStateRequired) ||
		worker.WorkerStatus == state.WorkerStatusApprovalRequired
}

func workerTurnInterruptible(worker state.Worker) bool {
	if strings.TrimSpace(worker.ExecutorTurnID) == "" {
		return false
	}
	if worker.ExecutorInterruptible {
		return true
	}
	switch strings.TrimSpace(worker.ExecutorTurnStatus) {
	case string(executor.TurnStatusInProgress), string(executor.TurnStatusApprovalRequired):
		return true
	default:
		return false
	}
}

func workerTurnSteerable(worker state.Worker) bool {
	if strings.TrimSpace(worker.ExecutorTurnID) == "" {
		return false
	}
	if workerApprovalRequired(worker) {
		return false
	}
	if worker.ExecutorSteerable {
		return true
	}
	return strings.TrimSpace(worker.ExecutorTurnStatus) == string(executor.TurnStatusInProgress)
}

func workerLastControlPayload(worker state.Worker) string {
	if worker.ExecutorLastControl == nil {
		return ""
	}
	return strings.TrimSpace(worker.ExecutorLastControl.Payload)
}

func summarizeEvent(event journal.Event) string {
	message := strings.TrimSpace(event.Message)
	humanQuestion := strings.TrimSpace(event.HumanQuestion)
	humanReplyPreview := previewString(event.HumanReplyPayload, 240)
	outputPreview := strings.TrimSpace(event.ExecutorOutputPreview)
	contextPreview := strings.TrimSpace(event.ContextPreview)
	artifactPath := strings.TrimSpace(event.ArtifactPath)

	var summary string
	switch {
	case message != "" && humanReplyPreview != "":
		summary = message + " | human reply: " + humanReplyPreview
	case message != "" && humanQuestion != "":
		summary = message + " | question: " + humanQuestion
	case message != "" && outputPreview != "":
		summary = message + " | executor output: " + outputPreview
	case message != "" && contextPreview != "":
		summary = message + " | context: " + contextPreview
	case message != "":
		summary = message
	case humanReplyPreview != "":
		summary = "human reply: " + humanReplyPreview
	case humanQuestion != "":
		summary = "human question: " + humanQuestion
	case outputPreview != "":
		summary = "executor output: " + outputPreview
	case contextPreview != "":
		summary = "context: " + contextPreview
	case strings.TrimSpace(event.ContextRequestedPath) != "":
		summary = event.Type + " (" + strings.TrimSpace(event.ContextRequestedPath) + ")"
	case strings.TrimSpace(event.PlannerOutcome) != "":
		summary = event.Type + " (" + strings.TrimSpace(event.PlannerOutcome) + ")"
	default:
		summary = event.Type
	}

	if artifactPath == "" {
		return summary
	}
	if summary == "" {
		return "artifact: " + artifactPath
	}
	return summary + " | artifact: " + artifactPath
}

func checkpointRef(checkpoint *state.Checkpoint) *journal.CheckpointRef {
	if checkpoint == nil {
		return nil
	}

	return &journal.CheckpointRef{
		Sequence:  checkpoint.Sequence,
		Stage:     checkpoint.Stage,
		Label:     checkpoint.Label,
		SafePause: checkpoint.SafePause,
	}
}

func executorCheckpointLabel(status executor.TurnStatus) string {
	if status == executor.TurnStatusCompleted {
		return "executor_turn_completed"
	}
	return "executor_turn_failed"
}

func executorEngineEventName(approvalRequired bool, executorFailed bool) string {
	if approvalRequired {
		return "executor_approval_required"
	}
	if executorFailed {
		return "executor_turn_failed"
	}
	return "executor_turn_completed"
}

func modelUnavailableFromExecutorError(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" || !strings.Contains(normalized, "model") {
		return false
	}
	return strings.Contains(normalized, "does not exist") ||
		strings.Contains(normalized, "do not have access") ||
		strings.Contains(normalized, "don't have access") ||
		strings.Contains(normalized, "lack access") ||
		strings.Contains(normalized, "lacks access")
}

func executorFailureMessage(result executor.TurnResult) string {
	if result.Error == nil {
		return ""
	}
	if strings.TrimSpace(result.Error.Detail) == "" {
		return result.Error.Message
	}
	return result.Error.Message + " (" + strings.TrimSpace(result.Error.Detail) + ")"
}

func executorFailureStage(result executor.TurnResult) string {
	if result.Error == nil {
		return ""
	}
	return strings.TrimSpace(result.Error.Stage)
}

func executorFailureFallbackMessage(result executor.TurnResult) string {
	switch result.TurnStatus {
	case executor.TurnStatusInterrupted:
		return "executor turn ended with interrupted status"
	case executor.TurnStatusFailed:
		return "executor turn ended with failed status"
	default:
		return "executor turn did not complete successfully"
	}
}

func executorSuccess(result executor.TurnResult) *bool {
	if result.CompletedAt.IsZero() {
		return nil
	}

	success := result.TurnStatus == executor.TurnStatusCompleted
	return &success
}

func joinErrors(left error, right error) error {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return errors.Join(left, right)
}

func previewString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "..."
}

func nonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
