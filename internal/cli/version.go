package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"orchestrator/internal/buildinfo"
)

func newVersionCommand() Command {
	return Command{
		Name:    "version",
		Summary: "Print the orchestrator build version and release metadata.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator version",
			"",
			"Prints the binary version, revision, and build time for this CLI shell.",
		),
		Run: runVersion,
	}
}

func runVersion(_ context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newVersionCommand().Description)
			return nil
		}
		return err
	}

	info := buildinfo.Current()
	version := strings.TrimSpace(inv.Version)
	if version == "" {
		version = info.Version
	}

	fmt.Fprintf(inv.Stdout, "version: %s\n", version)
	fmt.Fprintf(inv.Stdout, "revision: %s\n", info.Revision)
	fmt.Fprintf(inv.Stdout, "build_time: %s\n", info.BuildTime)
	return nil
}
