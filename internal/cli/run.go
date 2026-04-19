package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/journal"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func newRunCommand() Command {
	return Command{
		Name:    "run",
		Summary: "Create a run and execute one live planner turn.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator run --goal TEXT",
			"",
			"Creates a durable run, builds a planner.v1 input packet from persisted state,",
			"calls the OpenAI Responses API once, validates the planner output, persists",
			"the resulting previous_response_id and checkpoint, then stops.",
		),
		Run: runRun,
	}
}

func runRun(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	goal := fs.String("goal", "", "Human-entered goal for the run record.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newRunCommand().Description)
			return nil
		}
		return err
	}

	if strings.TrimSpace(*goal) == "" {
		return errors.New("run requires --goal")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	createdRun, err := store.CreateRun(ctx, state.CreateRunParams{
		RepoPath: inv.RepoRoot,
		Goal:     strings.TrimSpace(*goal),
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
		return err
	}

	if err := journalWriter.Append(journal.Event{
		Type:     "run.created",
		RunID:    createdRun.ID,
		RepoPath: createdRun.RepoPath,
		Goal:     createdRun.Goal,
		Status:   string(createdRun.Status),
		Message:  "durable run record created",
	}); err != nil {
		return err
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
		return err
	}

	run, found, err := store.GetRun(ctx, createdRun.ID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("created run %s could not be reloaded", createdRun.ID)
	}

	recentEvents, err := journalWriter.ReadRecent(run.ID, 5)
	if err != nil {
		return err
	}

	input := buildPlannerInput(run, recentEvents, inv.RepoRoot)
	plannerClient := planner.Client{
		APIKey: plannerAPIKey(),
		Model:  resolvePlannerModel(inv),
	}

	plannerResult, err := plannerClient.Plan(ctx, input, run.PreviousResponseID)
	if err != nil {
		_ = journalWriter.Append(journal.Event{
			Type:     "planner.turn.failed",
			RunID:    run.ID,
			RepoPath: run.RepoPath,
			Goal:     run.Goal,
			Status:   string(run.Status),
			Message:  err.Error(),
		})
		return err
	}

	plannerCheckpoint := state.Checkpoint{
		Sequence:     run.LatestCheckpoint.Sequence + 1,
		Stage:        "planner",
		Label:        "planner_turn_completed",
		SafePause:    true,
		PlannerTurn:  run.LatestCheckpoint.PlannerTurn + 1,
		ExecutorTurn: run.LatestCheckpoint.ExecutorTurn,
		CreatedAt:    time.Now().UTC(),
	}

	if err := store.SavePlannerTurn(ctx, run.ID, plannerResult.ResponseID, plannerCheckpoint); err != nil {
		return err
	}

	updatedRun, found, err := store.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("updated run %s could not be reloaded", run.ID)
	}

	if err := journalWriter.Append(journal.Event{
		Type:               "planner.turn.completed",
		RunID:              updatedRun.ID,
		RepoPath:           updatedRun.RepoPath,
		Goal:               updatedRun.Goal,
		Status:             string(updatedRun.Status),
		Message:            "planner response validated and persisted",
		ResponseID:         plannerResult.ResponseID,
		PreviousResponseID: updatedRun.PreviousResponseID,
		PlannerOutcome:     string(plannerResult.Output.Outcome),
		Checkpoint: &journal.CheckpointRef{
			Sequence:  plannerCheckpoint.Sequence,
			Stage:     plannerCheckpoint.Stage,
			Label:     plannerCheckpoint.Label,
			SafePause: plannerCheckpoint.SafePause,
		},
	}); err != nil {
		return err
	}

	if err := journalWriter.Append(journal.Event{
		Type:               "checkpoint.persisted",
		RunID:              updatedRun.ID,
		RepoPath:           updatedRun.RepoPath,
		Status:             string(updatedRun.Status),
		Message:            "planner checkpoint persisted",
		ResponseID:         plannerResult.ResponseID,
		PreviousResponseID: updatedRun.PreviousResponseID,
		PlannerOutcome:     string(plannerResult.Output.Outcome),
		Checkpoint: &journal.CheckpointRef{
			Sequence:  plannerCheckpoint.Sequence,
			Stage:     plannerCheckpoint.Stage,
			Label:     plannerCheckpoint.Label,
			SafePause: plannerCheckpoint.SafePause,
		},
	}); err != nil {
		return err
	}

	renderedOutput, err := json.MarshalIndent(plannerResult.Output, "", "  ")
	if err != nil {
		return err
	}

	fmt.Fprintf(inv.Stdout, "run_id: %s\n", updatedRun.ID)
	fmt.Fprintf(inv.Stdout, "goal: %s\n", updatedRun.Goal)
	fmt.Fprintf(inv.Stdout, "status: %s\n", updatedRun.Status)
	fmt.Fprintf(inv.Stdout, "planner_model: %s\n", resolvePlannerModel(inv))
	fmt.Fprintf(inv.Stdout, "planner_response_id: %s\n", plannerResult.ResponseID)
	fmt.Fprintf(inv.Stdout, "stored_previous_response_id: %s\n", updatedRun.PreviousResponseID)
	fmt.Fprintf(inv.Stdout, "latest_checkpoint.sequence: %d\n", updatedRun.LatestCheckpoint.Sequence)
	fmt.Fprintf(inv.Stdout, "latest_checkpoint.stage: %s\n", updatedRun.LatestCheckpoint.Stage)
	fmt.Fprintf(inv.Stdout, "latest_checkpoint.label: %s\n", updatedRun.LatestCheckpoint.Label)
	fmt.Fprintf(inv.Stdout, "latest_checkpoint.safe_pause: %t\n", updatedRun.LatestCheckpoint.SafePause)
	fmt.Fprintln(inv.Stdout, "planner_result:")
	fmt.Fprintln(inv.Stdout, string(renderedOutput))
	fmt.Fprintln(inv.Stdout, "next_step: planner outcome was persisted but not executed in this slice")
	return nil
}

func buildPlannerInput(run state.Run, events []journal.Event, repoRoot string) planner.InputEnvelope {
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
		RecentEvents: previews,
		RepoContracts: planner.RepoContractAvailability{
			HasAgentsMD:       pathExists(filepath.Join(repoRoot, "AGENTS.md")),
			HasUpdatedSpec:    pathExists(filepath.Join(repoRoot, "docs", "ORCHESTRATOR_CLI_UPDATED_SPEC.md")),
			HasNonNegotiables: pathExists(filepath.Join(repoRoot, "docs", "ORCHESTRATOR_NON_NEGOTIABLES.md")),
			HasExecPlan:       pathExists(filepath.Join(repoRoot, "docs", "CLI_ENGINE_EXECPLAN.md")),
		},
		RawHumanReplies: nil,
		Capabilities: planner.CapabilityMarkers{
			Planner:  planner.CapabilityAvailable,
			Executor: planner.CapabilityDeferred,
			NTFY:     planner.CapabilityDeferred,
		},
	}
}

func summarizeEvent(event journal.Event) string {
	if trimmed := strings.TrimSpace(event.Message); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(event.PlannerOutcome); trimmed != "" {
		return event.Type + " (" + trimmed + ")"
	}
	return event.Type
}

func resolvePlannerModel(inv Invocation) string {
	if model := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); model != "" {
		return model
	}
	if model := strings.TrimSpace(inv.Config.PlannerModel); model != "" {
		return model
	}
	return "gpt-5.1"
}

func plannerAPIKey() string {
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
}
