package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

func newExecutorProbeCommand() Command {
	return Command{
		Name:    "executor-probe",
		Summary: "Perform one safe read-only executor turn through codex app-server.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator executor-probe --prompt TEXT",
			"",
			"Performs one real executor probe turn through codex app-server using a read-only",
			"transport setup, persists executor transport metadata durably, records journal",
			"events, prints a structured summary, then stops.",
		),
		Run: runExecutorProbe,
	}
}

func runExecutorProbe(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("executor-probe", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	prompt := fs.String("prompt", "", "Prompt for a single read-only executor probe turn.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newExecutorProbeCommand().Description)
			return nil
		}
		return err
	}

	if strings.TrimSpace(*prompt) == "" {
		return errors.New("executor-probe requires --prompt")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	run, err := resolveExecutorProbeRun(ctx, store, journalWriter, inv.RepoRoot)
	if err != nil {
		return err
	}

	client, err := appserver.NewClient(inv.Version)
	if err != nil {
		return err
	}

	result, probeErr := client.Probe(ctx, executor.ProbeRequest{
		RunID:      run.ID,
		RepoPath:   inv.RepoRoot,
		Prompt:     strings.TrimSpace(*prompt),
		ThreadID:   run.ExecutorThreadID,
		ThreadPath: run.ExecutorThreadPath,
	})

	executorState := state.ExecutorState{
		Transport:        string(result.Transport),
		ThreadID:         result.ThreadID,
		ThreadPath:       result.ThreadPath,
		TurnID:           result.TurnID,
		TurnStatus:       string(result.TurnStatus),
		LastSuccess:      executorProbeSuccess(result),
		LastFailureStage: executorProbeFailureStage(result),
		LastError:        executorFailureMessage(result),
		LastMessage:      result.FinalMessage,
	}

	var checkpoint *state.Checkpoint
	if result.CompletedAt.IsZero() {
		if err := store.SaveExecutorState(ctx, run.ID, executorState); err != nil {
			return err
		}
	} else {
		label := "executor_probe_completed"
		if result.TurnStatus != executor.TurnStatusCompleted {
			label = "executor_probe_failed"
		}

		cp := state.Checkpoint{
			Sequence:     run.LatestCheckpoint.Sequence + 1,
			Stage:        "executor",
			Label:        label,
			SafePause:    true,
			PlannerTurn:  run.LatestCheckpoint.PlannerTurn,
			ExecutorTurn: run.LatestCheckpoint.ExecutorTurn + 1,
			CreatedAt:    result.CompletedAt.UTC(),
		}
		checkpoint = &cp

		if err := store.SaveExecutorTurn(ctx, run.ID, executorState, cp); err != nil {
			return err
		}
	}

	if !result.StartedAt.IsZero() {
		if err := journalWriter.Append(journal.Event{
			At:                 result.StartedAt,
			Type:               "executor.turn.started",
			RunID:              run.ID,
			RepoPath:           run.RepoPath,
			Goal:               run.Goal,
			Status:             string(run.Status),
			Message:            "executor probe turn started",
			ExecutorTransport:  string(result.Transport),
			ExecutorThreadID:   result.ThreadID,
			ExecutorThreadPath: result.ThreadPath,
			ExecutorTurnID:     result.TurnID,
			ExecutorTurnStatus: string(executor.TurnStatusInProgress),
		}); err != nil {
			return err
		}
	}

	if probeErr == nil {
		if err := journalWriter.Append(journal.Event{
			At:                    result.CompletedAt,
			Type:                  "executor.turn.completed",
			RunID:                 run.ID,
			RepoPath:              run.RepoPath,
			Goal:                  run.Goal,
			Status:                string(run.Status),
			Message:               "executor probe turn completed",
			ExecutorTransport:     string(result.Transport),
			ExecutorThreadID:      result.ThreadID,
			ExecutorThreadPath:    result.ThreadPath,
			ExecutorTurnID:        result.TurnID,
			ExecutorTurnStatus:    string(result.TurnStatus),
			ExecutorOutputPreview: previewString(result.FinalMessage, 240),
			Checkpoint:            checkpointRef(checkpoint),
		}); err != nil {
			return err
		}

		if checkpoint != nil {
			if err := journalWriter.Append(journal.Event{
				At:                    checkpoint.CreatedAt,
				Type:                  "checkpoint.persisted",
				RunID:                 run.ID,
				RepoPath:              run.RepoPath,
				Status:                string(run.Status),
				Message:               "executor checkpoint persisted",
				ExecutorTransport:     string(result.Transport),
				ExecutorThreadID:      result.ThreadID,
				ExecutorThreadPath:    result.ThreadPath,
				ExecutorTurnID:        result.TurnID,
				ExecutorTurnStatus:    string(result.TurnStatus),
				ExecutorOutputPreview: previewString(result.FinalMessage, 240),
				Checkpoint:            checkpointRef(checkpoint),
			}); err != nil {
				return err
			}
		}
	} else {
		failureAt := time.Now().UTC()
		if !result.CompletedAt.IsZero() {
			failureAt = result.CompletedAt
		}
		if err := journalWriter.Append(journal.Event{
			At:                    failureAt,
			Type:                  "executor.turn.failed",
			RunID:                 run.ID,
			RepoPath:              run.RepoPath,
			Goal:                  run.Goal,
			Status:                string(run.Status),
			Message:               executorFailureMessage(result),
			ExecutorTransport:     string(result.Transport),
			ExecutorThreadID:      result.ThreadID,
			ExecutorThreadPath:    result.ThreadPath,
			ExecutorTurnID:        result.TurnID,
			ExecutorTurnStatus:    string(result.TurnStatus),
			ExecutorOutputPreview: previewString(result.FinalMessage, 240),
			Checkpoint:            checkpointRef(checkpoint),
		}); err != nil {
			return err
		}

		if checkpoint != nil {
			if err := journalWriter.Append(journal.Event{
				At:                 checkpoint.CreatedAt,
				Type:               "checkpoint.persisted",
				RunID:              run.ID,
				RepoPath:           run.RepoPath,
				Status:             string(run.Status),
				Message:            "executor failure checkpoint persisted",
				ExecutorTransport:  string(result.Transport),
				ExecutorThreadID:   result.ThreadID,
				ExecutorThreadPath: result.ThreadPath,
				ExecutorTurnID:     result.TurnID,
				ExecutorTurnStatus: string(result.TurnStatus),
				Checkpoint:         checkpointRef(checkpoint),
			}); err != nil {
				return err
			}
		}
	}

	summary, err := json.MarshalIndent(map[string]any{
		"run_id":           run.ID,
		"goal":             run.Goal,
		"checkpoint_label": checkpointLabel(checkpoint),
		"result":           result,
	}, "", "  ")
	if err != nil {
		return err
	}

	fmt.Fprintln(inv.Stdout, string(summary))
	return probeErr
}

func resolveExecutorProbeRun(ctx context.Context, store *state.Store, journalWriter *journal.Journal, repoRoot string) (state.Run, error) {
	latest, found, err := store.LatestRun(ctx)
	if err != nil {
		return state.Run{}, err
	}
	if found {
		return latest, nil
	}

	run, err := store.CreateRun(ctx, state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "executor probe",
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
		return state.Run{}, err
	}

	if err := journalWriter.Append(journal.Event{
		Type:     "run.created",
		RunID:    run.ID,
		RepoPath: run.RepoPath,
		Goal:     run.Goal,
		Status:   string(run.Status),
		Message:  "durable run record created for executor probe",
	}); err != nil {
		return state.Run{}, err
	}

	if err := journalWriter.Append(journal.Event{
		Type:       "checkpoint.persisted",
		RunID:      run.ID,
		RepoPath:   run.RepoPath,
		Status:     string(run.Status),
		Message:    "initial checkpoint persisted",
		Checkpoint: checkpointRef(&run.LatestCheckpoint),
	}); err != nil {
		return state.Run{}, err
	}

	return run, nil
}

func executorFailureMessage(result executor.ProbeResult) string {
	if result.Error == nil {
		return ""
	}
	if strings.TrimSpace(result.Error.Detail) == "" {
		return result.Error.Message
	}
	return result.Error.Message + " (" + strings.TrimSpace(result.Error.Detail) + ")"
}

func previewString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "..."
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

func checkpointLabel(checkpoint *state.Checkpoint) string {
	if checkpoint == nil {
		return ""
	}
	return checkpoint.Label
}

func executorProbeSuccess(result executor.ProbeResult) *bool {
	if result.CompletedAt.IsZero() {
		return nil
	}

	success := result.TurnStatus == executor.TurnStatusCompleted
	return &success
}

func executorProbeFailureStage(result executor.ProbeResult) string {
	if result.Error == nil {
		return ""
	}
	return strings.TrimSpace(result.Error.Stage)
}
