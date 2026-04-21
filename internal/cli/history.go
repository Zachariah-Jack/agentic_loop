package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"orchestrator/internal/state"
)

const defaultHistoryLimit = 10

func newHistoryCommand() Command {
	return Command{
		Name:    "history",
		Summary: "Show recent durable runs in compact form.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator history [--limit N]",
			"",
			"Shows recent runs in reverse chronological order from persisted SQLite state.",
			"It stays compact and does not dump full logs.",
		),
		Run: runHistory,
	}
}

func runHistory(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	limit := fs.Int("limit", defaultHistoryLimit, "Maximum number of runs to show.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newHistoryCommand().Description)
			return nil
		}
		return err
	}

	verbosity := resolveOutputVerbosity(inv)
	if *limit <= 0 {
		return errors.New("history requires --limit >= 1")
	}

	if !pathExists(inv.Layout.DBPath) {
		fmt.Fprintf(inv.Stdout, "history.limit: %d\n", *limit)
		fmt.Fprintln(inv.Stdout, "history.count: 0")
		return nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return err
	}

	runs, err := store.ListRuns(ctx, *limit)
	if err != nil {
		return err
	}

	fmt.Fprintf(inv.Stdout, "history.limit: %d\n", *limit)
	fmt.Fprintf(inv.Stdout, "history.count: %d\n", len(runs))

	for idx, run := range runs {
		workerStats, err := store.WorkerStats(ctx, run.ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(inv.Stdout, "run.%d.run_id: %s\n", idx+1, run.ID)
		fmt.Fprintf(inv.Stdout, "run.%d.goal: %s\n", idx+1, run.Goal)
		fmt.Fprintf(inv.Stdout, "run.%d.status: %s\n", idx+1, run.Status)
		fmt.Fprintf(inv.Stdout, "run.%d.updated_at: %s\n", idx+1, run.UpdatedAt.Format(timeLayout))
		fmt.Fprintf(inv.Stdout, "run.%d.stop_reason: %s\n", idx+1, valueOrUnavailable(run.LatestStopReason))
		fmt.Fprintf(inv.Stdout, "run.%d.checkpoint_label: %s\n", idx+1, valueOrUnavailable(run.LatestCheckpoint.Label))
		fmt.Fprintf(inv.Stdout, "run.%d.resumable: %t\n", idx+1, isRunResumable(run))
		fmt.Fprintf(inv.Stdout, "run.%d.workers_used: %t\n", idx+1, workerStats.Total > 0)
		fmt.Fprintf(inv.Stdout, "run.%d.next_operator_action: %s\n", idx+1, nextOperatorActionForExistingRun(run))
		if verbosity.verboseEnabled() {
			fmt.Fprintf(inv.Stdout, "run.%d.last_error: %s\n", idx+1, valueOrUnavailable(run.RuntimeIssueMessage))
			fmt.Fprintf(inv.Stdout, "run.%d.executor_approval_state: %s\n", idx+1, valueOrUnavailable(executorApprovalStateValue(run)))
			fmt.Fprintf(inv.Stdout, "run.%d.executor_last_control_action: %s\n", idx+1, valueOrUnavailable(executorLastControlActionValue(run)))
		}
	}

	return nil
}

func isRunResumable(run state.Run) bool {
	switch run.Status {
	case state.StatusCompleted, state.StatusFailed, state.StatusCancelled:
		return false
	default:
		return true
	}
}
