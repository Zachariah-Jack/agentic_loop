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
		Summary: "Advance the latest unfinished run until a real stop boundary.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator continue [--max-cycles N]",
			"",
			"Requires the target repo contract scaffold created by `orchestrator init`.",
			"",
			"Loads the latest unfinished run from persisted state and, by default,",
			"keeps advancing it in the foreground through repeated bounded cycles",
			"until a mechanical stop boundary is hit.",
			"",
			fmt.Sprintf("Use --max-cycles N to keep this invocation explicitly bounded (recommended safe small number: %d).", defaultContinueMaxCycles),
		),
		Run: runContinue,
	}
}

func runContinue(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("continue", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	maxCycles := fs.Int("max-cycles", 0, "Maximum bounded cycles to execute in this foreground invocation.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newContinueCommand().Description)
			return nil
		}
		return err
	}

	maxCyclesExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "max-cycles" {
			maxCyclesExplicit = true
		}
	})
	if maxCyclesExplicit && *maxCycles <= 0 {
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

	if maxCyclesExplicit {
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

	return executeForegroundLoop(ctx, inv, store, journalWriter, run, foregroundLoopMode{
		Command:         "continue",
		RunAction:       "continued_existing_run",
		EventPrefix:     "continue",
		InvocationLabel: "continue",
		StopFlagKey:     "continue.stop_flag_path",
	})
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
	executeHandoffGraceUsed := false

	for cycleNumber := 1; ; cycleNumber++ {
		if err := applyRuntimeConfigAtSafePoint(ctx, &inv, journalWriter, currentRun); err != nil {
			return err
		}

		cycleResult, cycleErr := boundedCycleRunner(ctx, inv, store, journalWriter, currentRun)
		effectiveRun := mergeCycleRun(currentRun, cycleResult)
		if err := applyRuntimeConfigAtSafePoint(ctx, &inv, journalWriter, effectiveRun); err != nil {
			return err
		}
		readyExecuteHandoff := continueReadyExecuteHandoff(cycleResult)
		allowExecuteHandoffGrace := cycleNumber >= maxCycles && readyExecuteHandoff && !executeHandoffGraceUsed
		stopReason := determineContinueStopReason(cycleResult, effectiveRun, cycleErr, cycleNumber, maxCycles, allowExecuteHandoffGrace)

		emitEngineEvent(inv, "safe_point_reached", eventPayloadForRun(effectiveRun, map[string]any{
			"command":      "continue",
			"cycle_number": cycleNumber,
			"stop_reason":  string(stopReason),
			"cycle_error":  errorString(cycleErr),
		}))
		if effectiveRun.Status == state.StatusCompleted {
			emitEngineEvent(inv, "run_completed", eventPayloadForRun(effectiveRun, map[string]any{
				"command":      "continue",
				"cycle_number": cycleNumber,
				"stop_reason":  string(stopReason),
			}))
		}

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
				Message:            continueStopMessage(stopReason, cycleResult, cycleErr),
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

		if allowExecuteHandoffGrace {
			executeHandoffGraceUsed = true
		}

		currentRun = effectiveRun
	}
}

func determineContinueStopReason(
	cycleResult orchestration.Result,
	run state.Run,
	cycleErr error,
	cycleNumber int,
	maxCycles int,
	allowExecuteHandoffGrace bool,
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
	case cycleNumber >= maxCycles && !allowExecuteHandoffGrace:
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

func continueStopMessage(stopReason continueStopReason, cycleResult orchestration.Result, cycleErr error) string {
	if stopReason == continueStopReasonTransportProcessError && cycleErr != nil {
		return cycleErr.Error()
	}
	if stopReason == continueStopReasonMaxCyclesReached {
		switch {
		case cycleResult.ExecutorDispatched:
			return "continue invocation stopped: max_cycles_reached after executor dispatch"
		case continueReadyExecuteHandoff(cycleResult):
			return "continue invocation stopped: max_cycles_reached with execute ready but not yet dispatched"
		default:
			return "continue invocation stopped: max_cycles_reached before executor dispatch"
		}
	}
	return "continue invocation stopped: " + string(stopReason)
}

func continueReadyExecuteHandoff(cycleResult orchestration.Result) bool {
	if cycleResult.ExecutorDispatched {
		return false
	}

	nextPlannerTurn := preferredSecondPlannerTurn(cycleResult.ReconsiderationPlannerTurn, cycleResult.SecondPlannerTurn)
	if nextPlannerTurn == nil {
		return false
	}

	return orchestration.ShouldDispatchExecutor(nextPlannerTurn.Output)
}

func journalCheckpointRef(checkpoint state.Checkpoint) *journal.CheckpointRef {
	return &journal.CheckpointRef{
		Sequence:  checkpoint.Sequence,
		Stage:     checkpoint.Stage,
		Label:     checkpoint.Label,
		SafePause: checkpoint.SafePause,
	}
}
