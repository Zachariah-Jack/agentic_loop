package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

const autoStopFlagFileName = "auto.stop"

type foregroundLoopMode struct {
	Command         string
	RunAction       string
	EventPrefix     string
	InvocationLabel string
	StopFlagKey     string
}

func newAutoCommand() Command {
	return Command{
		Name:    "auto",
		Summary: "Run one foreground loop across repeated bounded cycles.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator auto start --goal TEXT",
			"  orchestrator auto continue",
			"",
			"Foreground only. Reuses the existing bounded-cycle core and stops only",
			"at cycle boundaries.",
			"",
			"Subcommands:",
			"  start     Create a new run, then keep advancing it automatically.",
			"  continue  Keep advancing the latest unfinished run automatically.",
			"",
			"To stop after the current bounded cycle, create the stop flag file:",
			"  .orchestrator/state/"+autoStopFlagFileName,
		),
		Run: runAuto,
	}
}

func runAuto(ctx context.Context, inv Invocation) error {
	if len(inv.Args) == 0 {
		fmt.Fprintln(inv.Stdout, newAutoCommand().Description)
		return nil
	}

	switch inv.Args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(inv.Stdout, newAutoCommand().Description)
		return nil
	case "start":
		return runAutoStart(ctx, inv, inv.Args[1:])
	case "continue":
		return runAutoContinue(ctx, inv, inv.Args[1:])
	default:
		return fmt.Errorf("auto requires subcommand start or continue")
	}
}

func runAutoStart(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("auto start", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	goal := fs.String("goal", "", "Human-entered goal for the run record.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, autoStartDescription())
			return nil
		}
		return err
	}

	if strings.TrimSpace(*goal) == "" {
		return errors.New("auto start requires --goal")
	}
	if contract := inspectTargetRepoContract(inv.RepoRoot); !contract.Ready {
		return writeMissingRepoContractReport(inv.Stdout, "auto start", inv.RepoRoot, strings.TrimSpace(*goal), contract)
	}

	store, journalWriter, run, err := createAutoRun(ctx, inv, strings.TrimSpace(*goal))
	if err != nil {
		return err
	}
	defer store.Close()

	return executeAutoLoop(ctx, inv, store, journalWriter, run, "auto start", "created_new_run")
}

func runAutoContinue(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("auto continue", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, autoContinueDescription())
			return nil
		}
		return err
	}

	if contract := inspectTargetRepoContract(inv.RepoRoot); !contract.Ready {
		return writeMissingRepoContractReport(inv.Stdout, "auto continue", inv.RepoRoot, "", contract)
	}

	if !pathExists(inv.Layout.DBPath) {
		fmt.Fprintln(inv.Stdout, "auto_continue_lookup: no unfinished run found")
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
		fmt.Fprintln(inv.Stdout, "auto_continue_lookup: no unfinished run found")
		return nil
	}

	return executeAutoLoop(ctx, inv, store, journalWriter, run, "auto continue", "continued_existing_run")
}

func executeAutoLoop(
	ctx context.Context,
	inv Invocation,
	store *state.Store,
	journalWriter *journal.Journal,
	run state.Run,
	command string,
	runAction string,
) error {
	return executeForegroundLoop(ctx, inv, store, journalWriter, run, foregroundLoopMode{
		Command:         command,
		RunAction:       runAction,
		EventPrefix:     "auto",
		InvocationLabel: "auto",
		StopFlagKey:     "auto.stop_flag_path",
	})
}

func executeForegroundLoop(
	ctx context.Context,
	inv Invocation,
	store *state.Store,
	journalWriter *journal.Journal,
	run state.Run,
	mode foregroundLoopMode,
) error {
	stopFlagPath := autoStopFlagPath(inv.Layout)
	if err := journalWriter.Append(journal.Event{
		Type:       mode.EventPrefix + ".started",
		RunID:      run.ID,
		RepoPath:   run.RepoPath,
		Goal:       run.Goal,
		Status:     string(run.Status),
		Message:    foregroundLoopMessage(mode.InvocationLabel, "started"),
		Checkpoint: journalCheckpointRef(run.LatestCheckpoint),
	}); err != nil {
		return err
	}

	fmt.Fprintf(inv.Stdout, "%s: %s\n", mode.StopFlagKey, stopFlagPath)

	currentRun := run
	for cycleNumber := 1; ; cycleNumber++ {
		if err := applyRuntimeConfigAtSafePoint(ctx, &inv, journalWriter, currentRun); err != nil {
			return err
		}

		cycleResult, cycleErr := boundedCycleRunner(ctx, inv, store, journalWriter, currentRun)
		effectiveRun := mergeCycleRun(currentRun, cycleResult)

		if cycleErr == nil && len(effectiveRun.HumanReplies) > len(currentRun.HumanReplies) {
			waitingPlannerTurn := plannerTurnThatAskedHuman(cycleResult)
			if err := journalWriter.Append(journal.Event{
				Type:               mode.EventPrefix + ".waiting_for_human",
				RunID:              effectiveRun.ID,
				RepoPath:           effectiveRun.RepoPath,
				Goal:               effectiveRun.Goal,
				Status:             string(effectiveRun.Status),
				Message:            foregroundLoopMessage(mode.InvocationLabel, "waiting_for_human"),
				CycleNumber:        cycleNumber,
				ResponseID:         plannerResultResponseID(waitingPlannerTurn),
				PreviousResponseID: effectiveRun.PreviousResponseID,
				PlannerOutcome:     plannerResultOutcome(waitingPlannerTurn),
				Checkpoint:         journalCheckpointRef(effectiveRun.LatestCheckpoint),
			}); err != nil {
				return err
			}
		}

		stopRequested, err := consumeAutoStopFlag(stopFlagPath)
		if err != nil {
			return err
		}
		if stopRequested {
			emitEngineEvent(inv, "stop_flag_detected", eventPayloadForRun(effectiveRun, map[string]any{
				"cycle_number": cycleNumber,
				"path":         stopFlagPath,
			}))
		}

		if err := applyRuntimeConfigAtSafePoint(ctx, &inv, journalWriter, effectiveRun); err != nil {
			return err
		}

		stopReason := determineForegroundStopReason(cycleResult, effectiveRun, cycleErr, stopRequested)
		if stopReason != "" && effectiveRun.LatestStopReason != stopReason {
			if err := store.SaveLatestStopReason(ctx, effectiveRun.ID, stopReason); err != nil {
				return err
			}
			effectiveRun.LatestStopReason = stopReason
		}

		emitEngineEvent(inv, "safe_point_reached", eventPayloadForRun(effectiveRun, map[string]any{
			"command":      mode.Command,
			"cycle_number": cycleNumber,
			"stop_reason":  stopReason,
			"cycle_error":  errorString(cycleErr),
		}))
		if effectiveRun.Status == state.StatusCompleted {
			emitEngineEvent(inv, "run_completed", eventPayloadForRun(effectiveRun, map[string]any{
				"command":      mode.Command,
				"cycle_number": cycleNumber,
				"stop_reason":  stopReason,
			}))
		}

		if err := writeCommandReport(inv.Stdout, resolveOutputVerbosity(inv), commandReport{
			Command:                    mode.Command,
			RunAction:                  mode.RunAction,
			CycleNumber:                cycleNumber,
			PlannerModel:               resolvePlannerModel(inv),
			Run:                        effectiveRun,
			Continuous:                 true,
			StopReason:                 stopReason,
			LatestArtifactPath:         latestArtifactPathForRun(inv.Layout, effectiveRun.ID),
			FirstPlannerResult:         cycleResult.FirstPlannerResult,
			ReconsiderationPlannerTurn: cycleResult.ReconsiderationPlannerTurn,
			SecondPlannerTurn:          cycleResult.SecondPlannerTurn,
			ExecutorDispatched:         cycleResult.ExecutorDispatched,
			CycleError:                 cycleErr,
		}); err != nil {
			return err
		}

		if cycleErr == nil {
			if err := journalWriter.Append(journal.Event{
				Type:               mode.EventPrefix + ".cycle.completed",
				RunID:              effectiveRun.ID,
				RepoPath:           effectiveRun.RepoPath,
				Goal:               effectiveRun.Goal,
				Status:             string(effectiveRun.Status),
				Message:            foregroundLoopMessage(mode.InvocationLabel, "cycle_completed"),
				CycleNumber:        cycleNumber,
				ResponseID:         latestPlannerResponseID(cycleResult),
				PreviousResponseID: effectiveRun.PreviousResponseID,
				PlannerOutcome:     latestForegroundPlannerOutcome(cycleResult),
				Checkpoint:         journalCheckpointRef(effectiveRun.LatestCheckpoint),
			}); err != nil {
				return err
			}
		}

		if stopReason != "" {
			if err := journalWriter.Append(journal.Event{
				Type:               mode.EventPrefix + ".stopped",
				RunID:              effectiveRun.ID,
				RepoPath:           effectiveRun.RepoPath,
				Goal:               effectiveRun.Goal,
				Status:             string(effectiveRun.Status),
				Message:            foregroundStopMessage(mode.InvocationLabel, stopReason, cycleErr),
				CycleNumber:        cycleNumber,
				StopReason:         stopReason,
				ResponseID:         latestPlannerResponseID(cycleResult),
				PreviousResponseID: effectiveRun.PreviousResponseID,
				PlannerOutcome:     latestForegroundPlannerOutcome(cycleResult),
				Checkpoint:         journalCheckpointRef(effectiveRun.LatestCheckpoint),
			}); err != nil {
				return err
			}

			return cycleErr
		}

		currentRun = effectiveRun
	}
}

func createAutoRun(ctx context.Context, inv Invocation, goal string) (*state.Store, *journal.Journal, state.Run, error) {
	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return nil, nil, state.Run{}, err
	}

	createdRun, err := store.CreateRun(ctx, state.CreateRunParams{
		RepoPath: inv.RepoRoot,
		Goal:     goal,
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
		},
	})
	if err != nil {
		_ = store.Close()
		return nil, nil, state.Run{}, err
	}

	if err := journalWriter.Append(journal.Event{
		Type:     "run.created",
		RunID:    createdRun.ID,
		RepoPath: createdRun.RepoPath,
		Goal:     createdRun.Goal,
		Status:   string(createdRun.Status),
		Message:  "durable run record created",
	}); err != nil {
		_ = store.Close()
		return nil, nil, state.Run{}, err
	}

	if err := journalWriter.Append(journal.Event{
		Type:     "checkpoint.persisted",
		RunID:    createdRun.ID,
		RepoPath: createdRun.RepoPath,
		Status:   string(createdRun.Status),
		Message:  "initial checkpoint persisted",
		Checkpoint: &journal.CheckpointRef{
			Sequence:  createdRun.LatestCheckpoint.Sequence,
			Stage:     createdRun.LatestCheckpoint.Stage,
			Label:     createdRun.LatestCheckpoint.Label,
			SafePause: createdRun.LatestCheckpoint.SafePause,
		},
	}); err != nil {
		_ = store.Close()
		return nil, nil, state.Run{}, err
	}

	run, found, err := store.GetRun(ctx, createdRun.ID)
	if err != nil {
		_ = store.Close()
		return nil, nil, state.Run{}, err
	}
	if !found {
		_ = store.Close()
		return nil, nil, state.Run{}, fmt.Errorf("created run %s could not be reloaded", createdRun.ID)
	}

	emitEngineEvent(inv, "run_started", eventPayloadForRun(run, nil))

	return store, journalWriter, run, nil
}

func determineForegroundStopReason(
	cycleResult orchestration.Result,
	run state.Run,
	cycleErr error,
	stopRequested bool,
) string {
	switch {
	case run.LatestStopReason == orchestration.StopReasonExecutorApprovalReq || executorApprovalStateValue(run) == orchestrationApprovalStateRequired:
		return orchestration.StopReasonExecutorApprovalReq
	case cycleErr != nil:
		switch run.RuntimeIssueReason {
		case orchestration.StopReasonPlannerValidationFailed:
			return orchestration.StopReasonPlannerValidationFailed
		case orchestration.StopReasonMissingRequiredConfig:
			return orchestration.StopReasonMissingRequiredConfig
		case orchestration.StopReasonExecutorFailed:
			return orchestration.StopReasonExecutorFailed
		default:
			return orchestration.StopReasonTransportProcessError
		}
	case run.Status == state.StatusCompleted:
		return orchestration.StopReasonPlannerComplete
	case latestForegroundPlannerOutcome(cycleResult) == string(planner.OutcomeAskHuman):
		return orchestration.StopReasonPlannerAskHuman
	case stopRequested:
		return orchestration.StopReasonOperatorStopRequested
	default:
		return ""
	}
}

func latestForegroundPlannerOutcome(cycleResult orchestration.Result) string {
	if cycleResult.SecondPlannerTurn != nil {
		return string(cycleResult.SecondPlannerTurn.Output.Outcome)
	}
	if cycleResult.ReconsiderationPlannerTurn != nil {
		return string(cycleResult.ReconsiderationPlannerTurn.Output.Outcome)
	}
	return string(cycleResult.FirstPlannerResult.Output.Outcome)
}

func plannerTurnThatAskedHuman(cycleResult orchestration.Result) *planner.Result {
	if cycleResult.ReconsiderationPlannerTurn != nil && cycleResult.ReconsiderationPlannerTurn.Output.Outcome == planner.OutcomeAskHuman {
		return cycleResult.ReconsiderationPlannerTurn
	}
	if cycleResult.FirstPlannerResult.Output.Outcome == planner.OutcomeAskHuman {
		return &cycleResult.FirstPlannerResult
	}
	return nil
}

func plannerResultResponseID(result *planner.Result) string {
	if result == nil {
		return ""
	}
	return result.ResponseID
}

func plannerResultOutcome(result *planner.Result) string {
	if result == nil {
		return ""
	}
	return string(result.Output.Outcome)
}

func foregroundLoopMessage(invocationLabel string, phase string) string {
	switch phase {
	case "started":
		return fmt.Sprintf("foreground %s invocation started", invocationLabel)
	case "cycle_completed":
		return fmt.Sprintf("bounded cycle completed during foreground %s invocation", invocationLabel)
	case "waiting_for_human":
		return fmt.Sprintf("foreground %s invocation waited for human input and resumed after one raw reply", invocationLabel)
	default:
		return fmt.Sprintf("foreground %s invocation", invocationLabel)
	}
}

func foregroundStopMessage(invocationLabel string, stopReason string, cycleErr error) string {
	switch {
	case stopReason == orchestration.StopReasonOperatorStopRequested:
		return fmt.Sprintf("foreground %s invocation stopped after operator stop flag was detected", invocationLabel)
	case stopReason == orchestration.StopReasonTransportProcessError && cycleErr != nil:
		return cycleErr.Error()
	default:
		return fmt.Sprintf("foreground %s invocation stopped: %s", invocationLabel, stopReason)
	}
}

func autoStopFlagPath(layout state.Layout) string {
	return filepath.Join(layout.StateDir, autoStopFlagFileName)
}

func consumeAutoStopFlag(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("auto stop flag path is a directory: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

func autoStartDescription() string {
	return stringsJoin(
		"Usage:",
		"  orchestrator auto start --goal TEXT",
		"",
		"Creates a durable run, then keeps advancing that same run in the foreground",
		"through repeated bounded cycles until a mechanical stop boundary is hit.",
	)
}

func autoContinueDescription() string {
	return stringsJoin(
		"Usage:",
		"  orchestrator auto continue",
		"",
		"Loads the latest unfinished run and keeps advancing it in the foreground",
		"through repeated bounded cycles until a mechanical stop boundary is hit.",
	)
}
