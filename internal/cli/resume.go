package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"orchestrator/internal/journal"
)

func newResumeCommand() Command {
	return Command{
		Name:    "resume",
		Summary: "Continue the latest unfinished run for one bounded cycle.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator resume",
			"",
			"Loads the latest unfinished run from persisted state, performs one bounded",
			"planner-led continuation cycle on that existing run, persists the result,",
			"and stops again at the next safe pause point.",
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

	if !pathExists(inv.Layout.DBPath) {
		fmt.Fprintln(inv.Stdout, "resume_lookup: no unfinished run found")
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
		fmt.Fprintln(inv.Stdout, "resume_lookup: no unfinished run found")
		return nil
	}

	if err := journalWriter.Append(journal.Event{
		Type:     "run.resumed",
		RunID:    run.ID,
		RepoPath: run.RepoPath,
		Goal:     run.Goal,
		Status:   string(run.Status),
		Message:  "bounded continuation started from latest unfinished run",
		Checkpoint: &journal.CheckpointRef{
			Sequence:  run.LatestCheckpoint.Sequence,
			Stage:     run.LatestCheckpoint.Stage,
			Label:     run.LatestCheckpoint.Label,
			SafePause: run.LatestCheckpoint.SafePause,
		},
	}); err != nil {
		return err
	}

	return executeBoundedCycle(ctx, inv, store, journalWriter, run, boundedCycleMode{
		Command:   "resume",
		RunAction: "resumed_existing_run",
	})
}
