package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"orchestrator/internal/journal"
)

func newInitCommand() Command {
	return Command{
		Name:    "init",
		Summary: "Ensure the repo-local persistence scaffold exists.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator init",
			"",
			"Creates the repo-local persistence scaffold under .orchestrator.",
			"It ensures runtime directories, the SQLite schema, and the append-only JSONL journal.",
		),
		Run: runInit,
	}
}

func runInit(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newInitCommand().Description)
			return nil
		}
		return err
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := journalWriter.Append(journal.Event{
		Type:     "runtime.initialized",
		RepoPath: inv.RepoRoot,
		Message:  "runtime scaffold ready",
	}); err != nil {
		return err
	}

	fmt.Fprintf(inv.Stdout, "repo root: %s\n", inv.RepoRoot)
	fmt.Fprintf(inv.Stdout, "state dir: %s\n", inv.Layout.StateDir)
	fmt.Fprintf(inv.Stdout, "logs dir: %s\n", inv.Layout.LogsDir)
	fmt.Fprintf(inv.Stdout, "state db: %s\n", inv.Layout.DBPath)
	fmt.Fprintf(inv.Stdout, "event journal: %s\n", inv.Layout.JournalPath)
	fmt.Fprintln(inv.Stdout, "schema: runs, checkpoints")
	return nil
}
