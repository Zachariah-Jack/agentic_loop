package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func newControlCommand() Command {
	return Command{
		Name:    "control",
		Summary: "Serve or demo the local V2 control protocol and event stream.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator control serve [--addr HOST:PORT]",
			"  orchestrator control demo <status|pending|inject|set-verbosity|stop-safe|clear-stop|events> [flags]",
			"",
			"Starts the local loopback V2 control server for future console integration.",
			"It exposes the control action endpoint and the NDJSON event stream, and",
			"also includes a small protocol demo client for local operator testing.",
			"",
			"This slice supports status, runtime config, safe-stop flag, pending-action",
			"inspection, control-message queueing, live operator-status rendering, and",
			"a truthful side-chat stub.",
		),
		Run: runControl,
	}
}

func runControl(ctx context.Context, inv Invocation) error {
	if len(inv.Args) == 0 {
		fmt.Fprintln(inv.Stdout, newControlCommand().Description)
		return nil
	}

	switch inv.Args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(inv.Stdout, newControlCommand().Description)
		return nil
	case "serve":
		return runControlServe(ctx, inv, inv.Args[1:])
	case "demo":
		return runControlDemo(ctx, inv, inv.Args[1:])
	default:
		return fmt.Errorf("control requires subcommand serve or demo")
	}
}

func runControlServe(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("control serve", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	addr := fs.String("addr", "127.0.0.1:44777", "Loopback listen address.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, controlServeDescription())
			return nil
		}
		return err
	}

	return serveControlServer(ctx, inv, strings.TrimSpace(*addr), nil)
}

func serveControlServer(ctx context.Context, inv Invocation, addr string, ready func(string)) error {
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:44777"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &http.Server{
		Handler: newLocalControlServer(inv).Handler(),
	}

	baseURL := "http://" + listener.Addr().String()
	_ = os.Setenv("ORCHESTRATOR_CONTROL_ADDR", baseURL)
	if ready != nil {
		ready(baseURL)
	}

	fmt.Fprintf(inv.Stdout, "control.listen: %s\n", baseURL)
	fmt.Fprintf(inv.Stdout, "control.action_endpoint: %s/v2/control\n", baseURL)
	fmt.Fprintf(inv.Stdout, "control.events_endpoint: %s/v2/events\n", baseURL)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err = server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func controlServeDescription() string {
	return stringsJoin(
		"Usage:",
		"  orchestrator control serve [--addr HOST:PORT]",
		"",
		"Serves the local V2 control protocol and NDJSON event stream on a loopback",
		"address for future console attachment. This command is optional and does not",
		"change headless CLI run semantics.",
	)
}
