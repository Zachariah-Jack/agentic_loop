package main

import (
	"context"
	"fmt"
	"os"

	"orchestrator/internal/cli"
)

var version = "dev"

func main() {
	app := cli.NewApp(cli.Options{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: version,
	})

	if err := app.Execute(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
