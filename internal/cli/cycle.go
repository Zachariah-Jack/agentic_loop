package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

type boundedCycleMode struct {
	Command   string
	RunAction string
}

func executeBoundedCycle(
	ctx context.Context,
	inv Invocation,
	store *state.Store,
	journalWriter *journal.Journal,
	run state.Run,
	mode boundedCycleMode,
) error {
	plannerClient := planner.Client{
		APIKey: plannerAPIKey(),
		Model:  resolvePlannerModel(inv),
	}

	cycle := orchestration.Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  &plannerClient,
		Executor: &lazyExecutorClient{version: inv.Version},
	}

	cycleResult, cycleErr := cycle.RunOnce(ctx, run)
	if cycleErr != nil && cycleResult.Run.ID == "" {
		return cycleErr
	}

	if err := writeBoundedCycleReport(inv.Stdout, inv, cycleResult, mode); err != nil {
		return err
	}

	return cycleErr
}

func writeBoundedCycleReport(stdout io.Writer, inv Invocation, cycleResult orchestration.Result, mode boundedCycleMode) error {
	firstPlannerOutput, err := json.MarshalIndent(cycleResult.FirstPlannerResult.Output, "", "  ")
	if err != nil {
		return err
	}

	var secondPlannerOutput []byte
	if cycleResult.SecondPlannerTurn != nil {
		secondPlannerOutput, err = json.MarshalIndent(cycleResult.SecondPlannerTurn.Output, "", "  ")
		if err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "command: %s\n", mode.Command)
	fmt.Fprintf(stdout, "run_action: %s\n", mode.RunAction)
	fmt.Fprintf(stdout, "run_id: %s\n", cycleResult.Run.ID)
	fmt.Fprintf(stdout, "goal: %s\n", cycleResult.Run.Goal)
	fmt.Fprintf(stdout, "status: %s\n", cycleResult.Run.Status)
	fmt.Fprintf(stdout, "planner_model: %s\n", resolvePlannerModel(inv))
	fmt.Fprintf(stdout, "first_planner_response_id: %s\n", cycleResult.FirstPlannerResult.ResponseID)
	fmt.Fprintf(stdout, "stored_previous_response_id: %s\n", cycleResult.Run.PreviousResponseID)
	fmt.Fprintf(stdout, "first_planner_outcome: %s\n", cycleResult.FirstPlannerResult.Output.Outcome)
	fmt.Fprintf(stdout, "executor_dispatched: %t\n", cycleResult.ExecutorDispatched)
	if cycleResult.SecondPlannerTurn != nil {
		fmt.Fprintf(stdout, "second_planner_response_id: %s\n", cycleResult.SecondPlannerTurn.ResponseID)
		fmt.Fprintf(stdout, "second_planner_outcome: %s\n", cycleResult.SecondPlannerTurn.Output.Outcome)
	}
	fmt.Fprintf(stdout, "latest_checkpoint.sequence: %d\n", cycleResult.Run.LatestCheckpoint.Sequence)
	fmt.Fprintf(stdout, "latest_checkpoint.stage: %s\n", cycleResult.Run.LatestCheckpoint.Stage)
	fmt.Fprintf(stdout, "latest_checkpoint.label: %s\n", cycleResult.Run.LatestCheckpoint.Label)
	fmt.Fprintf(stdout, "latest_checkpoint.safe_pause: %t\n", cycleResult.Run.LatestCheckpoint.SafePause)
	if cycleResult.ExecutorResult != nil {
		fmt.Fprintf(stdout, "executor_transport: %s\n", cycleResult.Run.ExecutorTransport)
		fmt.Fprintf(stdout, "executor_thread_id: %s\n", cycleResult.Run.ExecutorThreadID)
		fmt.Fprintf(stdout, "executor_thread_path: %s\n", cycleResult.Run.ExecutorThreadPath)
		fmt.Fprintf(stdout, "executor_turn_id: %s\n", cycleResult.Run.ExecutorTurnID)
		fmt.Fprintf(stdout, "executor_turn_status: %s\n", cycleResult.Run.ExecutorTurnStatus)
		fmt.Fprintf(stdout, "executor_failure_stage: %s\n", cycleResult.Run.ExecutorLastFailureStage)
		fmt.Fprintf(stdout, "executor_last_error: %s\n", cycleResult.Run.ExecutorLastError)
		fmt.Fprintf(stdout, "executor_last_message_preview: %s\n", previewString(cycleResult.Run.ExecutorLastMessage, 240))
	}
	fmt.Fprintln(stdout, "first_planner_result:")
	fmt.Fprintln(stdout, string(firstPlannerOutput))
	if cycleResult.SecondPlannerTurn != nil {
		fmt.Fprintln(stdout, "second_planner_result:")
		fmt.Fprintln(stdout, string(secondPlannerOutput))
	}

	switch {
	case cycleResult.SecondPlannerTurn != nil && cycleResult.Run.LatestCheckpoint.Label == "planner_turn_post_collect_context":
		fmt.Fprintln(stdout, "next_step: bounded run stopped after the post-collect-context planner turn; that outcome was persisted and not executed")
	case cycleResult.SecondPlannerTurn != nil:
		fmt.Fprintln(stdout, "next_step: bounded run stopped after the post-executor planner turn; that outcome was persisted and not executed")
	case cycleResult.ExecutorDispatched && cycleResult.Run.ExecutorTurnStatus == string(executor.TurnStatusFailed):
		fmt.Fprintln(stdout, "next_step: bounded run stopped after executor failure; no post-executor planner turn ran")
	case cycleResult.ExecutorDispatched && cycleResult.Run.ExecutorTurnStatus == string(executor.TurnStatusInterrupted):
		fmt.Fprintln(stdout, "next_step: bounded run stopped after executor interruption; no post-executor planner turn ran")
	case cycleResult.ExecutorDispatched:
		fmt.Fprintln(stdout, "next_step: bounded run stopped after one executor turn because the post-executor planner turn did not complete")
	default:
		fmt.Fprintln(stdout, "next_step: non-execute planner outcome was persisted and surfaced; no executor turn was dispatched")
	}

	return nil
}
