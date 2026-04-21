package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

const defaultContinueMaxCycles = 3

type continueStopReason string

const (
	continueStopReasonNone                  continueStopReason = ""
	continueStopReasonPlannerComplete       continueStopReason = orchestration.StopReasonPlannerComplete
	continueStopReasonPlannerAskHuman       continueStopReason = orchestration.StopReasonPlannerAskHuman
	continueStopReasonMaxCyclesReached      continueStopReason = orchestration.StopReasonMaxCyclesReached
	continueStopReasonTransportProcessError continueStopReason = orchestration.StopReasonTransportProcessError
	continueStopReasonPlannerValidationFail continueStopReason = orchestration.StopReasonPlannerValidationFailed
	continueStopReasonMissingRequiredConfig continueStopReason = orchestration.StopReasonMissingRequiredConfig
	continueStopReasonExecutorFailed        continueStopReason = orchestration.StopReasonExecutorFailed
	continueStopReasonExecutorApprovalReq   continueStopReason = orchestration.StopReasonExecutorApprovalReq
)

func newContinueCommand() Command {
	return Command{
		Name:    "continue",
		Summary: "Advance the latest unfinished run through bounded cycles.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator continue [--max-cycles N]",
			"",
			"Requires the target repo contract scaffold created by `orchestrator init`.",
			"",
			"Loads the latest unfinished run from persisted state and executes repeated",
			"bounded cycles on that existing run until a mechanical stop boundary is hit.",
		),
		Run: runContinue,
	}
}

func runContinue(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("continue", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	maxCycles := fs.Int("max-cycles", defaultContinueMaxCycles, "Maximum bounded cycles to execute in this foreground invocation.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newContinueCommand().Description)
			return nil
		}
		return err
	}

	if *maxCycles <= 0 {
		return errors.New("continue requires --max-cycles >= 1")
	}
	if contract := inspectTargetRepoContract(inv.RepoRoot); !contract.Ready {
		return writeMissingRepoContractReport(inv.Stdout, "continue", inv.RepoRoot, "", contract)
	}

	if !pathExists(inv.Layout.DBPath) {
		fmt.Fprintln(inv.Stdout, "continue_lookup: no unfinished run found")
		return nil
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	run, found, err := store.LatestResumableRun(ctx)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(inv.Stdout, "continue_lookup: no unfinished run found")
		return nil
	}

	if err := journalWriter.Append(journal.Event{
		Type:        "continue.started",
		RunID:       run.ID,
		RepoPath:    run.RepoPath,
		Goal:        run.Goal,
		Status:      string(run.Status),
		Message:     fmt.Sprintf("foreground continue invocation started (max_cycles=%d)", *maxCycles),
		CycleNumber: 0,
		Checkpoint:  journalCheckpointRef(run.LatestCheckpoint),
	}); err != nil {
		return err
	}

	return executeContinueCycles(ctx, inv, store, journalWriter, run, *maxCycles)
}

func executeContinueCycles(
	ctx context.Context,
	inv Invocation,
	store *state.Store,
	journalWriter *journal.Journal,
	run state.Run,
	maxCycles int,
) error {
	currentRun := run

	for cycleNumber := 1; cycleNumber <= maxCycles; cycleNumber++ {
		cycleResult, cycleErr := boundedCycleRunner(ctx, inv, store, journalWriter, currentRun)
		effectiveRun := mergeCycleRun(currentRun, cycleResult)
		stopReason := determineContinueStopReason(cycleResult, effectiveRun, cycleErr, cycleNumber, maxCycles)

		if err := writeContinueCycleReport(inv.Stdout, inv, cycleNumber, effectiveRun, cycleResult, cycleErr, stopReason); err != nil {
			return err
		}

		if cycleErr == nil {
			if err := journalWriter.Append(journal.Event{
				Type:               "continue.cycle.completed",
				RunID:              effectiveRun.ID,
				RepoPath:           effectiveRun.RepoPath,
				Goal:               effectiveRun.Goal,
				Status:             string(effectiveRun.Status),
				Message:            "bounded cycle completed during continue invocation",
				CycleNumber:        cycleNumber,
				ResponseID:         latestPlannerResponseID(cycleResult),
				PreviousResponseID: effectiveRun.PreviousResponseID,
				PlannerOutcome:     firstPlannerOutcome(cycleResult),
				Checkpoint:         journalCheckpointRef(effectiveRun.LatestCheckpoint),
			}); err != nil {
				return err
			}
		}

		if stopReason != continueStopReasonNone {
			if effectiveRun.LatestStopReason != string(stopReason) {
				if err := store.SaveLatestStopReason(ctx, effectiveRun.ID, string(stopReason)); err != nil {
					return err
				}
				effectiveRun.LatestStopReason = string(stopReason)
			}

			if err := journalWriter.Append(journal.Event{
				Type:               "continue.stopped",
				RunID:              effectiveRun.ID,
				RepoPath:           effectiveRun.RepoPath,
				Goal:               effectiveRun.Goal,
				Status:             string(effectiveRun.Status),
				Message:            continueStopMessage(stopReason, cycleErr),
				CycleNumber:        cycleNumber,
				StopReason:         string(stopReason),
				ResponseID:         latestPlannerResponseID(cycleResult),
				PreviousResponseID: effectiveRun.PreviousResponseID,
				PlannerOutcome:     firstPlannerOutcome(cycleResult),
				Checkpoint:         journalCheckpointRef(effectiveRun.LatestCheckpoint),
			}); err != nil {
				return err
			}

			return cycleErr
		}

		currentRun = effectiveRun
	}

	return nil
}

func determineContinueStopReason(
	cycleResult orchestration.Result,
	run state.Run,
	cycleErr error,
	cycleNumber int,
	maxCycles int,
) continueStopReason {
	switch {
	case run.LatestStopReason == orchestration.StopReasonExecutorApprovalReq || executorApprovalStateValue(run) == orchestrationApprovalStateRequired:
		return continueStopReasonExecutorApprovalReq
	case cycleErr != nil:
		switch run.RuntimeIssueReason {
		case orchestration.StopReasonPlannerValidationFailed:
			return continueStopReasonPlannerValidationFail
		case orchestration.StopReasonMissingRequiredConfig:
			return continueStopReasonMissingRequiredConfig
		case orchestration.StopReasonExecutorFailed:
			return continueStopReasonExecutorFailed
		default:
			return continueStopReasonTransportProcessError
		}
	case run.Status == state.StatusCompleted:
		return continueStopReasonPlannerComplete
	case cycleResult.FirstPlannerResult.Output.Outcome == planner.OutcomeAskHuman:
		return continueStopReasonPlannerAskHuman
	case cycleNumber >= maxCycles:
		return continueStopReasonMaxCyclesReached
	default:
		return continueStopReasonNone
	}
}

func writeContinueCycleReport(
	stdout io.Writer,
	inv Invocation,
	cycleNumber int,
	run state.Run,
	cycleResult orchestration.Result,
	cycleErr error,
	stopReason continueStopReason,
) error {
	return writeCommandReport(stdout, resolveOutputVerbosity(inv), commandReport{
		Command:                    "continue",
		RunAction:                  "continued_existing_run",
		CycleNumber:                cycleNumber,
		PlannerModel:               resolvePlannerModel(inv),
		Run:                        run,
		StopReason:                 string(stopReason),
		LatestArtifactPath:         latestArtifactPathForRun(inv.Layout, run.ID),
		FirstPlannerResult:         cycleResult.FirstPlannerResult,
		ReconsiderationPlannerTurn: cycleResult.ReconsiderationPlannerTurn,
		SecondPlannerTurn:          cycleResult.SecondPlannerTurn,
		ExecutorDispatched:         cycleResult.ExecutorDispatched,
		CycleError:                 cycleErr,
	})
}

func mergeCycleRun(currentRun state.Run, cycleResult orchestration.Result) state.Run {
	if cycleResult.Run.ID != "" {
		return cycleResult.Run
	}
	return currentRun
}

func latestPlannerResponseID(cycleResult orchestration.Result) string {
	if cycleResult.SecondPlannerTurn != nil {
		return cycleResult.SecondPlannerTurn.ResponseID
	}
	if cycleResult.ReconsiderationPlannerTurn != nil {
		return cycleResult.ReconsiderationPlannerTurn.ResponseID
	}
	return cycleResult.FirstPlannerResult.ResponseID
}

func firstPlannerOutcome(cycleResult orchestration.Result) string {
	return string(cycleResult.FirstPlannerResult.Output.Outcome)
}

func continueStopMessage(stopReason continueStopReason, cycleErr error) string {
	if stopReason == continueStopReasonTransportProcessError && cycleErr != nil {
		return cycleErr.Error()
	}
	return "continue invocation stopped: " + string(stopReason)
}

func journalCheckpointRef(checkpoint state.Checkpoint) *journal.CheckpointRef {
	return &journal.CheckpointRef{
		Sequence:  checkpoint.Sequence,
		Stage:     checkpoint.Stage,
		Label:     checkpoint.Label,
		SafePause: checkpoint.SafePause,
	}
}
