package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/executor/appserver"
)

func newStatusCommand() Command {
	return Command{
		Name:    "status",
		Summary: "Show current persistence, planner, and bootstrap health.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator status",
			"",
			"Prints current persistence plus live planner and primary executor transport health",
			"for the inert CLI shell. It reports only persisted runtime state.",
		),
		Run: runStatus,
	}
}

func runStatus(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newStatusCommand().Description)
			return nil
		}
		return err
	}

	cfgState := "missing"
	if _, err := config.Load(inv.ConfigPath); err == nil {
		cfgState = "present"
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	fmt.Fprintf(inv.Stdout, "binary version: %s\n", inv.Version)
	fmt.Fprintf(inv.Stdout, "repo root: %s\n", inv.RepoRoot)
	fmt.Fprintf(inv.Stdout, "config path: %s (%s)\n", inv.ConfigPath, cfgState)
	fmt.Fprintf(inv.Stdout, "repo markers: AGENTS.md=%t docs-spec=%t\n",
		pathExists(filepath.Join(inv.RepoRoot, "AGENTS.md")),
		pathExists(filepath.Join(inv.RepoRoot, "docs", "ORCHESTRATOR_CLI_UPDATED_SPEC.md")),
	)
	fmt.Fprintf(inv.Stdout, "state dir: %s (%t)\n", inv.Layout.StateDir, pathExists(inv.Layout.StateDir))
	fmt.Fprintf(inv.Stdout, "logs dir: %s (%t)\n", inv.Layout.LogsDir, pathExists(inv.Layout.LogsDir))
	fmt.Fprintf(inv.Stdout, "state db: %s (%t)\n", inv.Layout.DBPath, pathExists(inv.Layout.DBPath))
	fmt.Fprintf(inv.Stdout, "event journal: %s (%t)\n", inv.Layout.JournalPath, pathExists(inv.Layout.JournalPath))
	fmt.Fprintln(inv.Stdout, "planner.transport: responses_api")
	fmt.Fprintf(inv.Stdout, "planner.model: %s\n", resolvePlannerModel(inv))
	fmt.Fprintf(inv.Stdout, "planner.api_key: %s\n", plannerAPIKeyStatus())
	fmt.Fprintln(inv.Stdout, "executor.transport.primary: codex_app_server")
	fmt.Fprintf(inv.Stdout, "executor.app_server: %s\n", executorAppServerState())

	if pathExists(inv.Layout.DBPath) {
		store, err := openExistingStore(inv.Layout)
		if err != nil {
			return err
		}
		defer store.Close()

		if err := store.EnsureSchema(ctx); err != nil {
			return err
		}

		stats, err := store.Stats(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(inv.Stdout, "runs.total: %d\n", stats.TotalRuns)
		fmt.Fprintf(inv.Stdout, "runs.resumable: %d\n", stats.ResumableRuns)

		latest, found, err := store.LatestRun(ctx)
		if err != nil {
			return err
		}
		if found {
			fmt.Fprintf(inv.Stdout, "latest_run.id: %s\n", latest.ID)
			fmt.Fprintf(inv.Stdout, "latest_run.goal: %s\n", latest.Goal)
			fmt.Fprintf(inv.Stdout, "latest_run.status: %s\n", latest.Status)
			fmt.Fprintf(inv.Stdout, "latest_run.updated_at: %s\n", latest.UpdatedAt.Format(time.RFC3339Nano))
			fmt.Fprintf(inv.Stdout, "latest_run.previous_response_id: %s\n", latest.PreviousResponseID)
			fmt.Fprintf(inv.Stdout, "latest_run.executor_transport: %s\n", latest.ExecutorTransport)
			fmt.Fprintf(inv.Stdout, "latest_run.executor_thread_id: %s\n", latest.ExecutorThreadID)
			fmt.Fprintf(inv.Stdout, "latest_run.executor_thread_path: %s\n", latest.ExecutorThreadPath)
			fmt.Fprintf(inv.Stdout, "latest_run.executor_turn_id: %s\n", latest.ExecutorTurnID)
			fmt.Fprintf(inv.Stdout, "latest_run.executor_turn_status: %s\n", latest.ExecutorTurnStatus)
			fmt.Fprintf(inv.Stdout, "latest_run.executor_last_error: %s\n", latest.ExecutorLastError)
			fmt.Fprintf(inv.Stdout, "latest_run.executor_last_message_preview: %s\n", previewString(latest.ExecutorLastMessage, 240))
			fmt.Fprintf(inv.Stdout, "latest_run.checkpoint.sequence: %d\n", latest.LatestCheckpoint.Sequence)
			fmt.Fprintf(inv.Stdout, "latest_run.checkpoint.label: %s\n", latest.LatestCheckpoint.Label)
			fmt.Fprintf(inv.Stdout, "latest_run.checkpoint.safe_pause: %t\n", latest.LatestCheckpoint.SafePause)
		}
	} else {
		fmt.Fprintln(inv.Stdout, "runs.total: 0")
		fmt.Fprintln(inv.Stdout, "runs.resumable: 0")
	}

	fmt.Fprintf(inv.Stdout, "planner: %s\n", plannerTransportState())
	fmt.Fprintf(inv.Stdout, "executor: %s\n", executorRuntimeState())
	fmt.Fprintln(inv.Stdout, "persistence: sqlite metadata store plus jsonl journal")
	fmt.Fprintln(inv.Stdout, "ntfy: deferred")
	return nil
}

func plannerAPIKeyStatus() string {
	if plannerAPIKey() == "" {
		return "missing"
	}
	return "present"
}

func plannerTransportState() string {
	if plannerAPIKey() == "" {
		return "live planner blocked (missing OPENAI_API_KEY)"
	}
	return "live planner ready"
}

func executorAppServerState() string {
	if _, err := appserver.ResolveLaunchPlan(); err != nil {
		return "blocked (" + err.Error() + ")"
	}
	return "ready"
}

func executorRuntimeState() string {
	if _, err := appserver.ResolveLaunchPlan(); err != nil {
		return "primary executor transport blocked"
	}
	return "primary executor single-turn dispatch ready"
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
