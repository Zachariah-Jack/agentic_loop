package main

import (
	"context"
	"fmt"
	"os"

	"orchestrator/internal/buildinfo"
	"orchestrator/internal/cli"
)

func main() {
	app := cli.NewApp(cli.Options{
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: buildinfo.Current().Version,
	})

	if err := app.Execute(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
