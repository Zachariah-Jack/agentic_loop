package orchestration

import (
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

const (
	StopReasonPlannerComplete         = "planner_complete"
	StopReasonPlannerAskHuman         = "planner_ask_human"
	StopReasonPlannerPause            = "planner_pause"
	StopReasonPlannerCollectContext   = "planner_collect_context"
	StopReasonPlannerExecute          = "planner_execute"
	StopReasonOperatorStopRequested   = "operator_stop_requested"
	StopReasonMaxCyclesReached        = "max_cycles_reached"
	StopReasonTransportProcessError   = "transport_or_process_error"
	StopReasonPlannerValidationFailed = "planner_validation_failed"
	StopReasonMissingRequiredConfig   = "missing_required_config"
	StopReasonExecutorFailed          = "executor_failed"
	StopReasonExecutorApprovalReq     = "executor_approval_required"
	StopReasonNTFYFallbackUsed        = "ntfy_failed_terminal_fallback_used"
)

func StopReasonForError(err error) string {
	switch {
	case err == nil:
		return ""
	case planner.IsValidationError(err):
		return StopReasonPlannerValidationFailed
	case planner.IsMissingRequiredConfig(err):
		return StopReasonMissingRequiredConfig
	default:
		return StopReasonTransportProcessError
	}
}

func StopReasonForPlannerOutcome(outcome planner.OutcomeKind) string {
	switch outcome {
	case planner.OutcomeComplete:
		return StopReasonPlannerComplete
	case planner.OutcomeAskHuman:
		return StopReasonPlannerAskHuman
	case planner.OutcomePause:
		return StopReasonPlannerPause
	case planner.OutcomeCollectContext:
		return StopReasonPlannerCollectContext
	case planner.OutcomeExecute:
		return StopReasonPlannerExecute
	default:
		return ""
	}
}

func StopReasonForBoundedCycle(result Result, cycleErr error) string {
	if result.Run.LatestStopReason != "" {
		return result.Run.LatestStopReason
	}
	if reason := StopReasonForError(cycleErr); reason != "" {
		return reason
	}
	if result.Run.Status == state.StatusCompleted {
		return StopReasonPlannerComplete
	}
	if result.FirstPlannerResult.Output.Outcome == planner.OutcomeAskHuman {
		return StopReasonPlannerAskHuman
	}
	if result.ReconsiderationPlannerTurn != nil {
		if result.ReconsiderationPlannerTurn.Output.Outcome == planner.OutcomeAskHuman {
			return StopReasonPlannerAskHuman
		}
		if reason := StopReasonForPlannerOutcome(result.ReconsiderationPlannerTurn.Output.Outcome); reason != "" {
			return reason
		}
	}
	if result.SecondPlannerTurn != nil {
		if result.SecondPlannerTurn.Output.Outcome == planner.OutcomeAskHuman {
			return StopReasonPlannerAskHuman
		}
		if reason := StopReasonForPlannerOutcome(result.SecondPlannerTurn.Output.Outcome); reason != "" {
			return reason
		}
	}
	if reason := StopReasonForPlannerOutcome(result.FirstPlannerResult.Output.Outcome); reason != "" {
		return reason
	}
	return ""
}
