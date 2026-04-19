package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"orchestrator/internal/config"
	"orchestrator/internal/state"
)

func newDoctorCommand() Command {
	return Command{
		Name:    "doctor",
		Summary: "Check repo markers and local persistence scaffold.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator doctor",
			"",
			"Checks repo markers plus the local persistence scaffold.",
			"It verifies runtime directories, the SQLite metadata store, and the JSONL journal without adding planner or executor behavior.",
		),
		Run: runDoctor,
	}
}

func runDoctor(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newDoctorCommand().Description)
			return nil
		}
		return err
	}

	type check struct {
		label  string
		ok     bool
		detail string
	}

	required := []string{
		".git",
		"AGENTS.md",
		filepath.Join("docs", "ORCHESTRATOR_CLI_UPDATED_SPEC.md"),
	}

	checks := make([]check, 0, len(required)+8)
	for _, rel := range required {
		path := filepath.Join(inv.RepoRoot, rel)
		_, statErr := os.Stat(path)
		checks = append(checks, check{
			label:  rel,
			ok:     statErr == nil,
			detail: path,
		})
	}

	cfgState := "missing"
	if _, err := config.Load(inv.ConfigPath); err == nil {
		cfgState = "loadable"
	} else if !errors.Is(err, os.ErrNotExist) {
		checks = append(checks, check{
			label:  "config",
			ok:     false,
			detail: err.Error(),
		})
	}
	checks = append(checks, check{
		label:  "config path",
		ok:     true,
		detail: fmt.Sprintf("%s (%s)", inv.ConfigPath, cfgState),
	})

	for _, target := range []struct {
		label string
		path  string
	}{
		{label: "state dir", path: inv.Layout.StateDir},
		{label: "logs dir", path: inv.Layout.LogsDir},
		{label: "state db", path: inv.Layout.DBPath},
		{label: "event journal", path: inv.Layout.JournalPath},
	} {
		_, statErr := os.Stat(target.path)
		checks = append(checks, check{
			label:  target.label,
			ok:     statErr == nil,
			detail: target.path,
		})
	}

	if pathExists(inv.Layout.DBPath) {
		store, err := state.Open(inv.Layout.DBPath)
		if err != nil {
			checks = append(checks, check{
				label:  "sqlite open",
				ok:     false,
				detail: err.Error(),
			})
		} else {
			defer store.Close()
			if err := store.EnsureSchema(ctx); err != nil {
				checks = append(checks, check{
					label:  "sqlite schema",
					ok:     false,
					detail: err.Error(),
				})
			} else {
				checks = append(checks, check{
					label:  "sqlite schema",
					ok:     true,
					detail: "runs and checkpoints ready",
				})
			}
		}
	}

	hasFailure := false
	for _, item := range checks {
		state := "OK"
		if !item.ok {
			state = "FAIL"
			hasFailure = true
		}
		fmt.Fprintf(inv.Stdout, "[%s] %s: %s\n", state, item.label, item.detail)
	}

	if hasFailure {
		return errors.New("doctor found bootstrap issues")
	}

	return nil
}
