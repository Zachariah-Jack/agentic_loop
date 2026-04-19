package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"orchestrator/internal/journal"
)

func newResumeCommand() Command {
	return Command{
		Name:    "resume",
		Summary: "Locate the latest unfinished run mechanically.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator resume",
			"",
			"Looks up the latest unfinished run mechanically from persisted state.",
			"It does not decide whether work should continue and does not run a planner.",
		),
		Run: runResume,
	}
}

func runResume(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newResumeCommand().Description)
			return nil
		}
		return err
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(inv.Stdout, "resume_lookup: no persisted runs found")
			return nil
		}
		return err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return err
	}

	run, found, err := store.LatestResumableRun(ctx)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(inv.Stdout, "resume_lookup: no unfinished run found")
		return nil
	}

	journalWriter, err := openExistingJournal(inv.Layout)
	if err == nil {
		_ = journalWriter.Append(journal.Event{
			Type:     "run.resume_lookup",
			RunID:    run.ID,
			RepoPath: run.RepoPath,
			Goal:     run.Goal,
			Status:   string(run.Status),
			Message:  "latest unfinished run located",
			Checkpoint: &journal.CheckpointRef{
				Sequence:  run.LatestCheckpoint.Sequence,
				Stage:     run.LatestCheckpoint.Stage,
				Label:     run.LatestCheckpoint.Label,
				SafePause: run.LatestCheckpoint.SafePause,
			},
		})
	}

	fmt.Fprintf(inv.Stdout, "run_id: %s\n", run.ID)
	fmt.Fprintf(inv.Stdout, "goal: %s\n", run.Goal)
	fmt.Fprintf(inv.Stdout, "status: %s\n", run.Status)
	fmt.Fprintf(inv.Stdout, "repo_path: %s\n", run.RepoPath)
	fmt.Fprintf(inv.Stdout, "created_at: %s\n", run.CreatedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(inv.Stdout, "updated_at: %s\n", run.UpdatedAt.Format(time.RFC3339Nano))
	fmt.Fprintf(inv.Stdout, "previous_response_id: %s\n", run.PreviousResponseID)
	fmt.Fprintf(inv.Stdout, "latest_checkpoint.sequence: %d\n", run.LatestCheckpoint.Sequence)
	fmt.Fprintf(inv.Stdout, "latest_checkpoint.stage: %s\n", run.LatestCheckpoint.Stage)
	fmt.Fprintf(inv.Stdout, "latest_checkpoint.label: %s\n", run.LatestCheckpoint.Label)
	fmt.Fprintf(inv.Stdout, "latest_checkpoint.safe_pause: %t\n", run.LatestCheckpoint.SafePause)
	fmt.Fprintln(inv.Stdout, "resume_action: planner loop not implemented in this slice")
	return nil
}
