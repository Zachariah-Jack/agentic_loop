package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"orchestrator/internal/config"
)

func newSetupCommand() Command {
	return Command{
		Name:    "setup",
		Summary: "Create or inspect the user config file.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator setup",
			"",
			"Creates the user config file if it does not exist yet.",
			"The config lives under the user config directory unless --config overrides it.",
		),
		Run: runSetup,
	}
}

func runSetup(_ context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newSetupCommand().Description)
			return nil
		}
		return err
	}

	cfg, err := config.Load(inv.ConfigPath)
	if err == nil {
		fmt.Fprintf(inv.Stdout, "config already exists at %s\n", inv.ConfigPath)
		fmt.Fprintf(inv.Stdout, "log level: %s\n", cfg.LogLevel)
		fmt.Fprintf(inv.Stdout, "verbosity: %s\n", cfg.Verbosity)
		fmt.Fprintf(inv.Stdout, "planner model: %s\n", cfg.PlannerModel)
		return nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	cfg = config.Default()
	if err := config.Save(inv.ConfigPath, cfg); err != nil {
		return err
	}

	fmt.Fprintf(inv.Stdout, "created config at %s\n", inv.ConfigPath)
	fmt.Fprintf(inv.Stdout, "log level: %s\n", cfg.LogLevel)
	fmt.Fprintf(inv.Stdout, "verbosity: %s\n", cfg.Verbosity)
	fmt.Fprintf(inv.Stdout, "planner model: %s\n", cfg.PlannerModel)
	return nil
}
