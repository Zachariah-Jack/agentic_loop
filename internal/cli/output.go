package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

type outputVerbosity string

const (
	outputVerbosityQuiet   outputVerbosity = "quiet"
	outputVerbosityNormal  outputVerbosity = "normal"
	outputVerbosityVerbose outputVerbosity = "verbose"
	outputVerbosityTrace   outputVerbosity = "trace"
)

type commandReport struct {
	Command                    string
	RunAction                  string
	CycleNumber                int
	PlannerModel               string
	Run                        state.Run
	Continuous                 bool
	StopReason                 string
	LatestArtifactPath         string
	FirstPlannerResult         planner.Result
	ReconsiderationPlannerTurn *planner.Result
	SecondPlannerTurn          *planner.Result
	ExecutorDispatched         bool
	CycleError                 error
}

type operatorStatusSnapshot struct {
	ContractVersion    string `json:"contract_version,omitempty"`
	OperatorMessage    string `json:"operator_message,omitempty"`
	CurrentFocus       string `json:"current_focus,omitempty"`
	NextIntendedStep   string `json:"next_intended_step,omitempty"`
	WhyThisStep        string `json:"why_this_step,omitempty"`
	ProgressPercent    int    `json:"progress_percent"`
	ProgressConfidence string `json:"progress_confidence,omitempty"`
	ProgressBasis      string `json:"progress_basis,omitempty"`
}

func resolveOutputVerbosity(inv Invocation) outputVerbosity {
	switch currentConfig(inv).Verbosity {
	case config.VerbosityQuiet:
		return outputVerbosityQuiet
	case config.VerbosityVerbose:
		return outputVerbosityVerbose
	case config.VerbosityTrace:
		return outputVerbosityTrace
	default:
		return outputVerbosityNormal
	}
}

func (v outputVerbosity) verboseEnabled() bool {
	return v == outputVerbosityVerbose || v == outputVerbosityTrace
}

func (v outputVerbosity) traceEnabled() bool {
	return v == outputVerbosityTrace
}

func writeCommandReport(stdout io.Writer, verbosity outputVerbosity, report commandReport) error {
	fmt.Fprintf(stdout, "command: %s\n", report.Command)
	fmt.Fprintf(stdout, "run_id: %s\n", report.Run.ID)
	fmt.Fprintf(stdout, "goal: %s\n", report.Run.Goal)
	fmt.Fprintf(stdout, "run_action: %s\n", report.RunAction)
	if report.CycleNumber > 0 {
		fmt.Fprintf(stdout, "cycle_number: %d\n", report.CycleNumber)
	}
	fmt.Fprintf(stdout, "status: %s\n", report.Run.Status)
	fmt.Fprintf(stdout, "elapsed: %s\n", runElapsedLabel(report.Run, time.Now().UTC()))
	fmt.Fprintf(stdout, "first_planner_outcome: %s\n", valueOrUnavailable(string(report.FirstPlannerResult.Output.Outcome)))
	fmt.Fprintf(stdout, "second_planner_outcome: %s\n", valueOrUnavailable(secondPlannerOutcome(report.ReconsiderationPlannerTurn, report.SecondPlannerTurn)))
	fmt.Fprintf(stdout, "executor_dispatched: %t\n", report.ExecutorDispatched)
	fmt.Fprintf(stdout, "latest_checkpoint.sequence: %d\n", report.Run.LatestCheckpoint.Sequence)
	fmt.Fprintf(stdout, "latest_checkpoint.stage: %s\n", valueOrUnavailable(report.Run.LatestCheckpoint.Stage))
	fmt.Fprintf(stdout, "latest_checkpoint.label: %s\n", valueOrUnavailable(report.Run.LatestCheckpoint.Label))
	fmt.Fprintf(stdout, "latest_checkpoint.safe_pause: %t\n", report.Run.LatestCheckpoint.SafePause)
	fmt.Fprintf(stdout, "stop_reason: %s\n", valueOrUnavailable(report.StopReason))
	fmt.Fprintf(stdout, "latest_artifact_path: %s\n", valueOrUnavailable(report.LatestArtifactPath))
	if hasExecutorMetadata(report.Run) {
		fmt.Fprintf(stdout, "executor_thread_id: %s\n", valueOrUnavailable(report.Run.ExecutorThreadID))
		fmt.Fprintf(stdout, "executor_turn_id: %s\n", valueOrUnavailable(report.Run.ExecutorTurnID))
		fmt.Fprintf(stdout, "executor_turn_status: %s\n", valueOrUnavailable(report.Run.ExecutorTurnStatus))
		fmt.Fprintf(stdout, "executor_failure_stage: %s\n", valueOrUnavailable(report.Run.ExecutorLastFailureStage))
		fmt.Fprintf(stdout, "executor_last_error: %s\n", valueOrUnavailable(report.Run.ExecutorLastError))
		fmt.Fprintf(stdout, "executor_approval_state: %s\n", valueOrUnavailable(executorApprovalStateValue(report.Run)))
		fmt.Fprintf(stdout, "executor_approval_kind: %s\n", valueOrUnavailable(executorApprovalKindValue(report.Run)))
		fmt.Fprintf(stdout, "executor_interruptible: %t\n", executorTurnInterruptible(report.Run))
		fmt.Fprintf(stdout, "executor_steerable: %t\n", executorTurnSteerable(report.Run))
		fmt.Fprintf(stdout, "executor_last_control_action: %s\n", valueOrUnavailable(executorLastControlActionValue(report.Run)))
	}
	fmt.Fprintf(stdout, "next_operator_action: %s\n", nextOperatorAction(report.Command, report.Run, report.StopReason, report.CycleError, report.Continuous))
	if report.CycleError != nil {
		fmt.Fprintf(stdout, "cycle_error: %s\n", report.CycleError)
	}
	writeOperatorStatus(stdout, verbosity, "planner.", latestOperatorStatusForReport(report))

	if !verbosity.verboseEnabled() {
		return nil
	}

	fmt.Fprintf(stdout, "planner_model: %s\n", valueOrUnavailable(report.PlannerModel))
	fmt.Fprintf(stdout, "first_planner_response_id: %s\n", valueOrUnavailable(report.FirstPlannerResult.ResponseID))
	fmt.Fprintf(stdout, "stored_previous_response_id: %s\n", valueOrUnavailable(report.Run.PreviousResponseID))
	fmt.Fprintf(stdout, "second_planner_response_id: %s\n", valueOrUnavailable(secondPlannerResponseID(report.ReconsiderationPlannerTurn, report.SecondPlannerTurn)))
	fmt.Fprintf(stdout, "runtime_issue.reason: %s\n", valueOrUnavailable(report.Run.RuntimeIssueReason))
	fmt.Fprintf(stdout, "runtime_issue.message: %s\n", valueOrUnavailable(report.Run.RuntimeIssueMessage))

	if report.ExecutorDispatched || strings.TrimSpace(report.Run.ExecutorTurnStatus) != "" {
		fmt.Fprintf(stdout, "executor_transport: %s\n", valueOrUnavailable(report.Run.ExecutorTransport))
		fmt.Fprintf(stdout, "executor_thread_id: %s\n", valueOrUnavailable(report.Run.ExecutorThreadID))
		fmt.Fprintf(stdout, "executor_thread_path: %s\n", valueOrUnavailable(report.Run.ExecutorThreadPath))
		fmt.Fprintf(stdout, "executor_turn_id: %s\n", valueOrUnavailable(report.Run.ExecutorTurnID))
		fmt.Fprintf(stdout, "executor_turn_status: %s\n", valueOrUnavailable(report.Run.ExecutorTurnStatus))
		fmt.Fprintf(stdout, "executor_failure_stage: %s\n", valueOrUnavailable(report.Run.ExecutorLastFailureStage))
		fmt.Fprintf(stdout, "executor_last_error: %s\n", valueOrUnavailable(report.Run.ExecutorLastError))
		fmt.Fprintf(stdout, "executor_last_message_preview: %s\n", valueOrUnavailable(previewString(report.Run.ExecutorLastMessage, 240)))
	}

	if !verbosity.traceEnabled() {
		return nil
	}

	if firstPlannerOutput, err := plannerOutputJSON(report.FirstPlannerResult); err == nil && len(firstPlannerOutput) > 0 {
		fmt.Fprintln(stdout, "first_planner_result:")
		fmt.Fprintln(stdout, string(firstPlannerOutput))
	} else if err != nil {
		return err
	}

	if secondPlannerOutput, err := plannerOutputJSONPtr(preferredSecondPlannerTurn(report.ReconsiderationPlannerTurn, report.SecondPlannerTurn)); err == nil && len(secondPlannerOutput) > 0 {
		fmt.Fprintln(stdout, "second_planner_result:")
		fmt.Fprintln(stdout, string(secondPlannerOutput))
	} else if err != nil {
		return err
	}

	return nil
}

func latestOperatorStatusForReport(report commandReport) *operatorStatusSnapshot {
	for _, result := range []*planner.Result{
		preferredSecondPlannerTurn(report.ReconsiderationPlannerTurn, report.SecondPlannerTurn),
		&report.FirstPlannerResult,
	} {
		status := operatorStatusFromPlannerResult(result)
		if status != nil {
			return status
		}
	}
	return operatorStatusFromState(report.Run.PlannerOperatorStatus)
}

func operatorStatusFromPlannerResult(result *planner.Result) *operatorStatusSnapshot {
	if result == nil {
		return nil
	}
	return operatorStatusFromPlanner(result.Output.ContractVersion, result.Output.OperatorStatus)
}

func operatorStatusFromPlanner(contractVersion string, status *planner.OperatorStatus) *operatorStatusSnapshot {
	if status == nil {
		return nil
	}

	return &operatorStatusSnapshot{
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

func operatorStatusFromState(status *state.PlannerOperatorStatus) *operatorStatusSnapshot {
	if status == nil {
		return nil
	}

	return &operatorStatusSnapshot{
		ContractVersion:    strings.TrimSpace(status.ContractVersion),
		OperatorMessage:    strings.TrimSpace(status.OperatorMessage),
		CurrentFocus:       strings.TrimSpace(status.CurrentFocus),
		NextIntendedStep:   strings.TrimSpace(status.NextIntendedStep),
		WhyThisStep:        strings.TrimSpace(status.WhyThisStep),
		ProgressPercent:    status.ProgressPercent,
		ProgressConfidence: strings.TrimSpace(status.ProgressConfidence),
		ProgressBasis:      strings.TrimSpace(status.ProgressBasis),
	}
}

func writeOperatorStatus(stdout io.Writer, verbosity outputVerbosity, prefix string, status *operatorStatusSnapshot) {
	if stdout == nil || verbosity == outputVerbosityQuiet || status == nil {
		return
	}

	fmt.Fprintf(stdout, "%soperator_message: %s\n", prefix, valueOrUnavailable(status.OperatorMessage))
	fmt.Fprintf(stdout, "%sprogress_percent: %d\n", prefix, status.ProgressPercent)
	if verbosity.verboseEnabled() {
		fmt.Fprintf(stdout, "%scurrent_focus: %s\n", prefix, valueOrUnavailable(status.CurrentFocus))
		fmt.Fprintf(stdout, "%snext_intended_step: %s\n", prefix, valueOrUnavailable(status.NextIntendedStep))
		fmt.Fprintf(stdout, "%swhy_this_step: %s\n", prefix, valueOrUnavailable(status.WhyThisStep))
	}
	if verbosity.traceEnabled() {
		fmt.Fprintf(stdout, "%scontract_version: %s\n", prefix, valueOrUnavailable(status.ContractVersion))
		fmt.Fprintf(stdout, "%sprogress_confidence: %s\n", prefix, valueOrUnavailable(status.ProgressConfidence))
		fmt.Fprintf(stdout, "%sprogress_basis: %s\n", prefix, valueOrUnavailable(status.ProgressBasis))
		if encoded, err := json.MarshalIndent(status, "", "  "); err == nil {
			fmt.Fprintf(stdout, "%sstatus_json: %s\n", prefix, string(encoded))
		}
	}
}

func nextOperatorAction(command string, run state.Run, stopReason string, cycleErr error, continuous bool) string {
	switch {
	case run.Status == state.StatusCompleted:
		return "no_action_required_run_completed"
	case executorApprovalStateValue(run) == orchestrationApprovalStateRequired:
		return "approve_or_deny_executor_request"
	case stopReason == orchestration.StopReasonExecutorApprovalReq:
		return "inspect_status"
	case stopReason == orchestration.StopReasonPlannerAskHuman && len(run.HumanReplies) == 0:
		return "answer_human_question"
	case cycleErr != nil || strings.TrimSpace(run.RuntimeIssueReason) != "":
		return "inspect_status"
	case !isRunResumable(run):
		return "run_new_goal"
	case command == "run" && continuous:
		return "continue_existing_run"
	case command == "run":
		return "resume_existing_run"
	case command == "resume" || command == "continue" || strings.HasPrefix(command, "auto"):
		return "continue_existing_run"
	default:
		return "inspect_history"
	}
}

func nextOperatorActionForExistingRun(run state.Run) string {
	switch {
	case run.Status == state.StatusCompleted:
		return "no_action_required_run_completed"
	case executorApprovalStateValue(run) == orchestrationApprovalStateRequired:
		return "approve_or_deny_executor_request"
	case run.LatestStopReason == orchestration.StopReasonExecutorApprovalReq:
		return "inspect_status"
	case run.LatestStopReason == orchestration.StopReasonPlannerAskHuman && len(run.HumanReplies) == 0:
		return "answer_human_question"
	case strings.TrimSpace(run.RuntimeIssueReason) != "":
		return "inspect_status"
	case !isRunResumable(run):
		return "run_new_goal"
	default:
		return "continue_existing_run"
	}
}

func latestArtifactPathForRun(layout state.Layout, runID string) string {
	if strings.TrimSpace(runID) == "" {
		return ""
	}

	events, err := latestRunEvents(layout, runID, 64)
	if err != nil {
		return ""
	}
	return latestArtifactPathFromEvents(events)
}

func preferredSecondPlannerTurn(reconsideration *planner.Result, second *planner.Result) *planner.Result {
	if second != nil {
		return second
	}
	return reconsideration
}

func secondPlannerOutcome(reconsideration *planner.Result, second *planner.Result) string {
	result := preferredSecondPlannerTurn(reconsideration, second)
	if result == nil {
		return ""
	}
	return string(result.Output.Outcome)
}

func secondPlannerResponseID(reconsideration *planner.Result, second *planner.Result) string {
	result := preferredSecondPlannerTurn(reconsideration, second)
	if result == nil {
		return ""
	}
	return result.ResponseID
}

func plannerOutputJSON(result planner.Result) ([]byte, error) {
	if strings.TrimSpace(result.ResponseID) == "" && strings.TrimSpace(string(result.Output.Outcome)) == "" {
		return nil, nil
	}
	return json.MarshalIndent(result.Output, "", "  ")
}

func plannerOutputJSONPtr(result *planner.Result) ([]byte, error) {
	if result == nil {
		return nil, nil
	}
	return plannerOutputJSON(*result)
}

func checkpointSummary(checkpoint state.Checkpoint) string {
	return fmt.Sprintf(
		"sequence=%d stage=%s label=%s safe_pause=%t",
		checkpoint.Sequence,
		valueOrUnavailable(checkpoint.Stage),
		valueOrUnavailable(checkpoint.Label),
		checkpoint.SafePause,
	)
}

const orchestrationApprovalStateRequired = "required"

func hasExecutorMetadata(run state.Run) bool {
	return strings.TrimSpace(run.ExecutorThreadID) != "" ||
		strings.TrimSpace(run.ExecutorTurnID) != "" ||
		strings.TrimSpace(run.ExecutorTurnStatus) != "" ||
		run.ExecutorApproval != nil ||
		run.ExecutorLastControl != nil
}

func executorApprovalStateValue(run state.Run) string {
	if run.ExecutorApproval == nil {
		return ""
	}
	return strings.TrimSpace(run.ExecutorApproval.State)
}

func executorApprovalKindValue(run state.Run) string {
	if run.ExecutorApproval == nil {
		return ""
	}
	return strings.TrimSpace(run.ExecutorApproval.Kind)
}

func executorLastControlActionValue(run state.Run) string {
	if run.ExecutorLastControl == nil {
		return ""
	}
	return strings.TrimSpace(run.ExecutorLastControl.Action)
}

func executorTurnInterruptible(run state.Run) bool {
	if strings.TrimSpace(run.ExecutorTurnID) == "" {
		return false
	}

	switch strings.TrimSpace(run.ExecutorTurnStatus) {
	case "in_progress", "approval_required":
		return true
	default:
		return false
	}
}

func executorTurnSteerable(run state.Run) bool {
	if strings.TrimSpace(run.ExecutorTurnID) == "" {
		return false
	}
	if executorApprovalStateValue(run) == orchestrationApprovalStateRequired {
		return false
	}
	return strings.TrimSpace(run.ExecutorTurnStatus) == "in_progress"
}
