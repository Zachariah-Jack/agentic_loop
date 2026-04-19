package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
)

func newVersionCommand() Command {
	return Command{
		Name:    "version",
		Summary: "Print the orchestrator binary version.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator version",
			"",
			"Prints the binary version for this CLI shell.",
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

	fmt.Fprintln(inv.Stdout, inv.Version)
	return nil
}
