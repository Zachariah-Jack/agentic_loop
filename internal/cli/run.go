package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"orchestrator/internal/executor"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

func newRunCommand() Command {
	return Command{
		Name:    "run",
		Summary: "Create a new run and execute one bounded cycle.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator run --goal TEXT",
			"",
			"Requires the target repo contract scaffold created by `orchestrator init`.",
			"",
			"Creates a durable run, executes one bounded planner-led cycle, persists",
			"all results durably, and stops with a mechanical stop_reason.",
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
	if contract := inspectTargetRepoContract(inv.RepoRoot); !contract.Ready {
		return writeMissingRepoContractReport(inv.Stdout, "run", inv.RepoRoot, strings.TrimSpace(*goal), contract)
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

	return executeBoundedCycle(ctx, inv, store, journalWriter, run, boundedCycleMode{
		Command:   "run",
		RunAction: "created_new_run",
	})
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

type lazyExecutorClient struct {
	version string
	mu      sync.Mutex
	client  *appserver.Client
}

func (l *lazyExecutorClient) Execute(ctx context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	l.mu.Lock()
	if l.client == nil {
		client, err := appserver.NewClient(l.version)
		if err != nil {
			l.mu.Unlock()
			return executor.TurnResult{}, err
		}
		l.client = &client
	}
	client := l.client
	l.mu.Unlock()

	return client.Execute(ctx, req)
}
