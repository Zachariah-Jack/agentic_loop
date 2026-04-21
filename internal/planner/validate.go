package planner

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateInput(input InputEnvelope) error {
	var issues []string

	if input.ContractVersion != ContractVersionV1 {
		issues = append(issues, fmt.Sprintf("contract_version must be %q", ContractVersionV1))
	}
	if strings.TrimSpace(input.RunID) == "" {
		issues = append(issues, "run_id is required")
	}
	if strings.TrimSpace(input.RepoPath) == "" {
		issues = append(issues, "repo_path is required")
	}
	if strings.TrimSpace(input.Goal) == "" {
		issues = append(issues, "goal is required")
	}
	if strings.TrimSpace(input.RunStatus) == "" {
		issues = append(issues, "run_status is required")
	}

	issues = append(issues, validateCheckpoint("latest_checkpoint", input.LatestCheckpoint)...)
	issues = append(issues, validateCapabilities(input.Capabilities)...)
	issues = append(issues, validateRepoContracts(input.RepoContracts)...)

	for i, event := range input.RecentEvents {
		prefix := fmt.Sprintf("recent_events[%d]", i)
		if event.At.IsZero() {
			issues = append(issues, prefix+".at is required")
		}
		if strings.TrimSpace(event.Type) == "" {
			issues = append(issues, prefix+".type is required")
		}
		if strings.TrimSpace(event.Summary) == "" {
			issues = append(issues, prefix+".summary is required")
		}
	}

	for i, reply := range input.RawHumanReplies {
		prefix := fmt.Sprintf("raw_human_replies[%d]", i)
		if strings.TrimSpace(reply.ID) == "" {
			issues = append(issues, prefix+".id is required")
		}
		if strings.TrimSpace(reply.Source) == "" {
			issues = append(issues, prefix+".source is required")
		}
		if reply.ReceivedAt.IsZero() {
			issues = append(issues, prefix+".received_at is required")
		}
		if reply.Payload == "" {
			issues = append(issues, prefix+".payload is required")
		}
	}

	if input.CollectedContext != nil {
		if strings.TrimSpace(input.CollectedContext.Focus) == "" {
			issues = append(issues, "collected_context.focus is required")
		}

		for i, result := range input.CollectedContext.Results {
			prefix := fmt.Sprintf("collected_context.results[%d]", i)
			if strings.TrimSpace(result.RequestedPath) == "" {
				issues = append(issues, prefix+".requested_path is required")
			}
			switch strings.TrimSpace(result.Kind) {
			case "file", "dir", "missing":
			default:
				issues = append(issues, prefix+".kind must be file, dir, or missing")
			}
		}
	}

	if input.DriftReview != nil {
		if strings.TrimSpace(input.DriftReview.Reviewer) == "" {
			issues = append(issues, "drift_review.reviewer is required")
		}
		for i, concern := range input.DriftReview.Concerns {
			if strings.TrimSpace(concern) == "" {
				issues = append(issues, fmt.Sprintf("drift_review.concerns[%d] must be non-empty when set", i))
			}
		}
		for i, item := range input.DriftReview.MissingContext {
			if strings.TrimSpace(item) == "" {
				issues = append(issues, fmt.Sprintf("drift_review.missing_context[%d] must be non-empty when set", i))
			}
		}
		for i, item := range input.DriftReview.RecommendedPlannerAdjustments {
			if strings.TrimSpace(item) == "" {
				issues = append(issues, fmt.Sprintf("drift_review.recommended_planner_adjustments[%d] must be non-empty when set", i))
			}
		}
		for i, path := range input.DriftReview.EvidencePaths {
			if strings.TrimSpace(path) == "" {
				issues = append(issues, fmt.Sprintf("drift_review.evidence_paths[%d] must be non-empty when set", i))
			}
		}
	}

	for i, tool := range input.PluginTools {
		prefix := fmt.Sprintf("plugin_tools[%d]", i)
		if strings.TrimSpace(tool.Name) == "" {
			issues = append(issues, prefix+".name is required")
		}
		if strings.TrimSpace(tool.Description) == "" {
			issues = append(issues, prefix+".description is required")
		}
	}

	if input.CollectedContext != nil {
		for i, result := range input.CollectedContext.ToolResults {
			prefix := fmt.Sprintf("collected_context.tool_results[%d]", i)
			if strings.TrimSpace(result.Tool) == "" {
				issues = append(issues, prefix+".tool is required")
			}
		}
		for i, result := range input.CollectedContext.WorkerResults {
			prefix := fmt.Sprintf("collected_context.worker_results[%d]", i)
			if !isKnownWorkerActionKind(result.Action) {
				issues = append(issues, prefix+".action must be create, dispatch, list, remove, integrate, or apply")
			}
			if result.Worker != nil {
				if strings.TrimSpace(result.Worker.WorkerStatus) == "" {
					issues = append(issues, prefix+".worker.worker_status is required when worker is set")
				}
				if strings.TrimSpace(result.Worker.WorktreePath) == "" {
					issues = append(issues, prefix+".worker.worktree_path is required when worker is set")
				}
			}
			for listedIdx, worker := range result.ListedWorkers {
				workerPrefix := fmt.Sprintf("%s.listed_workers[%d]", prefix, listedIdx)
				if strings.TrimSpace(worker.WorkerStatus) == "" {
					issues = append(issues, workerPrefix+".worker_status is required")
				}
				if strings.TrimSpace(worker.WorktreePath) == "" {
					issues = append(issues, workerPrefix+".worktree_path is required")
				}
			}
			if result.Integration != nil {
				for workerIdx, worker := range result.Integration.Workers {
					workerPrefix := fmt.Sprintf("%s.integration.workers[%d]", prefix, workerIdx)
					if strings.TrimSpace(worker.WorkerID) == "" {
						issues = append(issues, workerPrefix+".worker_id is required")
					}
					if strings.TrimSpace(worker.WorktreePath) == "" {
						issues = append(issues, workerPrefix+".worktree_path is required")
					}
				}
				for conflictIdx, candidate := range result.Integration.ConflictCandidates {
					conflictPrefix := fmt.Sprintf("%s.integration.conflict_candidates[%d]", prefix, conflictIdx)
					if strings.TrimSpace(candidate.Path) == "" {
						issues = append(issues, conflictPrefix+".path is required")
					}
					if strings.TrimSpace(candidate.Reason) == "" {
						issues = append(issues, conflictPrefix+".reason is required")
					}
				}
			}
			if result.Apply != nil {
				if strings.TrimSpace(result.Apply.Status) == "" {
					issues = append(issues, prefix+".apply.status is required")
				}
				if strings.TrimSpace(result.Apply.SourceArtifactPath) == "" {
					issues = append(issues, prefix+".apply.source_artifact_path is required")
				}
				if !isKnownWorkerApplyMode(result.Apply.ApplyMode) {
					issues = append(issues, prefix+".apply.apply_mode must be abort_if_conflicts or apply_non_conflicting")
				}
				for appliedIdx, item := range result.Apply.FilesApplied {
					appliedPrefix := fmt.Sprintf("%s.apply.files_applied[%d]", prefix, appliedIdx)
					if strings.TrimSpace(item.Path) == "" {
						issues = append(issues, appliedPrefix+".path is required")
					}
					if strings.TrimSpace(item.ChangeKind) == "" {
						issues = append(issues, appliedPrefix+".change_kind is required")
					}
				}
				for skippedIdx, item := range result.Apply.FilesSkipped {
					skippedPrefix := fmt.Sprintf("%s.apply.files_skipped[%d]", prefix, skippedIdx)
					if strings.TrimSpace(item.Path) == "" {
						issues = append(issues, skippedPrefix+".path is required")
					}
					if strings.TrimSpace(item.ChangeKind) == "" {
						issues = append(issues, skippedPrefix+".change_kind is required")
					}
					if strings.TrimSpace(item.Reason) == "" {
						issues = append(issues, skippedPrefix+".reason is required")
					}
				}
			}
		}
		if input.CollectedContext.WorkerPlan != nil {
			if strings.TrimSpace(input.CollectedContext.WorkerPlan.Status) == "" {
				issues = append(issues, "collected_context.worker_plan.status is required")
			}
			if !isKnownWorkerPlanApplyMode(input.CollectedContext.WorkerPlan.ApplyMode) {
				issues = append(issues, "collected_context.worker_plan.apply_mode must be abort_if_conflicts, apply_non_conflicting, or unavailable")
			}
			if input.CollectedContext.WorkerPlan.Apply != nil && strings.TrimSpace(input.CollectedContext.WorkerPlan.ApplyArtifactPath) == "" {
				issues = append(issues, "collected_context.worker_plan.apply_artifact_path is required when worker_plan.apply is set")
			}
			for i, worker := range input.CollectedContext.WorkerPlan.Workers {
				prefix := fmt.Sprintf("collected_context.worker_plan.workers[%d]", i)
				if strings.TrimSpace(worker.WorkerID) == "" {
					issues = append(issues, prefix+".worker_id is required")
				}
				if strings.TrimSpace(worker.WorkerStatus) == "" {
					issues = append(issues, prefix+".worker_status is required")
				}
				if strings.TrimSpace(worker.WorktreePath) == "" {
					issues = append(issues, prefix+".worktree_path is required")
				}
			}
		}
	}

	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}

	return nil
}

func ValidateOutput(output OutputEnvelope) error {
	var issues []string

	if output.ContractVersion != ContractVersionV1 {
		issues = append(issues, fmt.Sprintf("contract_version must be %q", ContractVersionV1))
	}

	payloadCount := 0
	if output.Execute != nil {
		payloadCount++
	}
	if output.AskHuman != nil {
		payloadCount++
	}
	if output.CollectContext != nil {
		payloadCount++
	}
	if output.Pause != nil {
		payloadCount++
	}
	if output.Complete != nil {
		payloadCount++
	}
	if payloadCount != 1 {
		issues = append(issues, "exactly one outcome payload must be set")
	}

	switch output.Outcome {
	case OutcomeExecute:
		if output.Execute == nil {
			issues = append(issues, "execute payload is required when outcome=execute")
			break
		}
		if strings.TrimSpace(output.Execute.Task) == "" {
			issues = append(issues, "execute.task is required")
		}
		if len(nonEmpty(output.Execute.AcceptanceCriteria)) == 0 {
			issues = append(issues, "execute.acceptance_criteria must contain at least one non-empty item")
		}
		if output.AskHuman != nil || output.CollectContext != nil || output.Pause != nil || output.Complete != nil {
			issues = append(issues, "only execute payload may be set when outcome=execute")
		}
	case OutcomeAskHuman:
		if output.AskHuman == nil {
			issues = append(issues, "ask_human payload is required when outcome=ask_human")
			break
		}
		if strings.TrimSpace(output.AskHuman.Question) == "" {
			issues = append(issues, "ask_human.question is required")
		}
		if output.Execute != nil || output.CollectContext != nil || output.Pause != nil || output.Complete != nil {
			issues = append(issues, "only ask_human payload may be set when outcome=ask_human")
		}
	case OutcomeCollectContext:
		if output.CollectContext == nil {
			issues = append(issues, "collect_context payload is required when outcome=collect_context")
			break
		}
		if strings.TrimSpace(output.CollectContext.Focus) == "" {
			issues = append(issues, "collect_context.focus is required")
		}
		if len(nonEmpty(output.CollectContext.Questions)) == 0 &&
			len(nonEmpty(output.CollectContext.Paths)) == 0 &&
			len(nonEmptyToolCalls(output.CollectContext.ToolCalls)) == 0 &&
			len(nonEmptyWorkerActions(output.CollectContext.WorkerActions)) == 0 &&
			output.CollectContext.WorkerPlan == nil {
			issues = append(issues, "collect_context must include at least one non-empty question, path, tool_call, worker_action, or worker_plan")
		}
		for i, call := range output.CollectContext.ToolCalls {
			if strings.TrimSpace(call.Tool) == "" {
				issues = append(issues, fmt.Sprintf("collect_context.tool_calls[%d].tool is required", i))
			}
		}
		for i, action := range output.CollectContext.WorkerActions {
			prefix := fmt.Sprintf("collect_context.worker_actions[%d]", i)
			if !isKnownWorkerActionKind(action.Action) {
				issues = append(issues, prefix+".action must be create, dispatch, list, remove, integrate, or apply")
				continue
			}
			switch action.Action {
			case WorkerActionCreate:
				if strings.TrimSpace(action.WorkerName) == "" {
					issues = append(issues, prefix+".worker_name is required for create")
				}
				if strings.TrimSpace(action.Scope) == "" {
					issues = append(issues, prefix+".scope is required for create")
				}
			case WorkerActionDispatch:
				if strings.TrimSpace(action.WorkerID) == "" && strings.TrimSpace(action.WorkerName) == "" {
					issues = append(issues, prefix+".worker_id or worker_name is required for dispatch")
				}
				if strings.TrimSpace(action.TaskSummary) == "" {
					issues = append(issues, prefix+".task_summary is required for dispatch")
				}
				if strings.TrimSpace(action.ExecutorPrompt) == "" {
					issues = append(issues, prefix+".executor_prompt is required for dispatch")
				}
			case WorkerActionRemove:
				if strings.TrimSpace(action.WorkerID) == "" && strings.TrimSpace(action.WorkerName) == "" {
					issues = append(issues, prefix+".worker_id or worker_name is required for remove")
				}
			case WorkerActionList:
			case WorkerActionIntegrate:
				if len(nonEmpty(action.WorkerIDs)) == 0 {
					issues = append(issues, prefix+".worker_ids must contain at least one worker id for integrate")
				}
			case WorkerActionApply:
				if !isKnownWorkerApplyMode(action.ApplyMode) {
					issues = append(issues, prefix+".apply_mode must be abort_if_conflicts or apply_non_conflicting for apply")
				}
				if strings.TrimSpace(action.ArtifactPath) == "" && len(nonEmpty(action.WorkerIDs)) == 0 {
					issues = append(issues, prefix+".artifact_path or worker_ids is required for apply")
				}
			}
		}
		if output.CollectContext.WorkerPlan != nil {
			if len(output.CollectContext.WorkerPlan.Workers) == 0 {
				issues = append(issues, "collect_context.worker_plan.workers must contain at least one worker")
			}
			if !isKnownWorkerPlanApplyMode(output.CollectContext.WorkerPlan.ApplyMode) {
				issues = append(issues, "collect_context.worker_plan.apply_mode must be abort_if_conflicts, apply_non_conflicting, or unavailable")
			}
			if strings.TrimSpace(output.CollectContext.WorkerPlan.ApplyMode) != string(WorkerApplyModeUnavailable) &&
				!output.CollectContext.WorkerPlan.IntegrationRequested {
				issues = append(issues, "collect_context.worker_plan.integration_requested must be true when apply_mode is not unavailable")
			}
			for i, worker := range output.CollectContext.WorkerPlan.Workers {
				prefix := fmt.Sprintf("collect_context.worker_plan.workers[%d]", i)
				if strings.TrimSpace(worker.Name) == "" {
					issues = append(issues, prefix+".name is required")
				}
				if strings.TrimSpace(worker.Scope) == "" {
					issues = append(issues, prefix+".scope is required")
				}
				if strings.TrimSpace(worker.TaskSummary) == "" {
					issues = append(issues, prefix+".task_summary is required")
				}
				if strings.TrimSpace(worker.ExecutorPrompt) == "" {
					issues = append(issues, prefix+".executor_prompt is required")
				}
			}
		}
		if output.Execute != nil || output.AskHuman != nil || output.Pause != nil || output.Complete != nil {
			issues = append(issues, "only collect_context payload may be set when outcome=collect_context")
		}
	case OutcomePause:
		if output.Pause == nil {
			issues = append(issues, "pause payload is required when outcome=pause")
			break
		}
		if strings.TrimSpace(output.Pause.Reason) == "" {
			issues = append(issues, "pause.reason is required")
		}
		if output.Execute != nil || output.AskHuman != nil || output.CollectContext != nil || output.Complete != nil {
			issues = append(issues, "only pause payload may be set when outcome=pause")
		}
	case OutcomeComplete:
		if output.Complete == nil {
			issues = append(issues, "complete payload is required when outcome=complete")
			break
		}
		if strings.TrimSpace(output.Complete.Summary) == "" {
			issues = append(issues, "complete.summary is required")
		}
		if output.Execute != nil || output.AskHuman != nil || output.CollectContext != nil || output.Pause != nil {
			issues = append(issues, "only complete payload may be set when outcome=complete")
		}
	default:
		issues = append(issues, "outcome must be one of execute, ask_human, collect_context, pause, complete")
	}

	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}

	return nil
}

func validateCheckpoint(prefix string, checkpoint Checkpoint) []string {
	var issues []string

	if checkpoint.Sequence <= 0 {
		issues = append(issues, prefix+".sequence must be greater than zero")
	}
	if strings.TrimSpace(checkpoint.Stage) == "" {
		issues = append(issues, prefix+".stage is required")
	}
	if strings.TrimSpace(checkpoint.Label) == "" {
		issues = append(issues, prefix+".label is required")
	}
	if checkpoint.CreatedAt.IsZero() {
		issues = append(issues, prefix+".created_at is required")
	}

	return issues
}

func validateCapabilities(markers CapabilityMarkers) []string {
	var issues []string

	for _, item := range []struct {
		name  string
		value CapabilityStatus
	}{
		{name: "capabilities.planner", value: markers.Planner},
		{name: "capabilities.executor", value: markers.Executor},
		{name: "capabilities.ntfy", value: markers.NTFY},
	} {
		if !isKnownCapabilityStatus(item.value) {
			issues = append(issues, item.name+" must be a known capability status")
		}
	}

	return issues
}

func validateRepoContracts(contracts RepoContractAvailability) []string {
	var issues []string

	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "repo_contracts.agents_md_path", value: contracts.AgentsMDPath},
		{name: "repo_contracts.updated_spec_path", value: contracts.UpdatedSpecPath},
		{name: "repo_contracts.non_negotiables_path", value: contracts.NonNegotiablesPath},
		{name: "repo_contracts.exec_plan_path", value: contracts.ExecPlanPath},
		{name: "repo_contracts.orchestrator_dir_path", value: contracts.OrchestratorDirPath},
		{name: "repo_contracts.roadmap_path", value: contracts.RoadmapPath},
		{name: "repo_contracts.decisions_path", value: contracts.DecisionsPath},
	} {
		if strings.TrimSpace(item.value) == "" {
			issues = append(issues, item.name+" is required")
		}
	}

	return issues
}

func isKnownCapabilityStatus(status CapabilityStatus) bool {
	switch status {
	case CapabilityContractOnly, CapabilityDeferred, CapabilityAvailable, CapabilityUnavailable:
		return true
	default:
		return false
	}
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

func nonEmptyToolCalls(calls []PluginToolCall) []PluginToolCall {
	out := make([]PluginToolCall, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.Tool) == "" {
			continue
		}
		out = append(out, call)
	}
	return out
}

func nonEmptyWorkerActions(actions []WorkerAction) []WorkerAction {
	out := make([]WorkerAction, 0, len(actions))
	for _, action := range actions {
		if !isKnownWorkerActionKind(action.Action) {
			continue
		}
		out = append(out, action)
	}
	return out
}

func isKnownWorkerActionKind(kind WorkerActionKind) bool {
	switch kind {
	case WorkerActionCreate, WorkerActionDispatch, WorkerActionList, WorkerActionRemove:
		return true
	case WorkerActionIntegrate, WorkerActionApply:
		return true
	default:
		return false
	}
}

func isKnownWorkerApplyMode(mode string) bool {
	switch WorkerApplyMode(strings.TrimSpace(mode)) {
	case WorkerApplyModeAbortIfConflicts, WorkerApplyModeNonConflicting:
		return true
	default:
		return false
	}
}

func isKnownWorkerPlanApplyMode(mode string) bool {
	switch WorkerApplyMode(strings.TrimSpace(mode)) {
	case WorkerApplyModeAbortIfConflicts, WorkerApplyModeNonConflicting, WorkerApplyModeUnavailable:
		return true
	default:
		return false
	}
}
