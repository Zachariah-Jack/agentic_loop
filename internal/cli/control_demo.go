package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"orchestrator/internal/activity"
	"orchestrator/internal/control"
)

func runControlDemo(ctx context.Context, inv Invocation, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(inv.Stdout, controlDemoDescription())
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(inv.Stdout, controlDemoDescription())
		return nil
	case "status":
		return runControlDemoStatus(ctx, inv, args[1:])
	case "pending":
		return runControlDemoPending(ctx, inv, args[1:])
	case "inject":
		return runControlDemoInject(ctx, inv, args[1:])
	case "set-verbosity":
		return runControlDemoSetVerbosity(ctx, inv, args[1:])
	case "stop-safe":
		return runControlDemoStopSafe(ctx, inv, args[1:])
	case "clear-stop":
		return runControlDemoClearStop(ctx, inv, args[1:])
	case "events":
		return runControlDemoEvents(ctx, inv, args[1:])
	default:
		return fmt.Errorf("unknown control demo subcommand %q", args[0])
	}
}

func runControlDemoStatus(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("control demo status", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	addr := fs.String("addr", "127.0.0.1:44777", "Control server listen address.")
	runID := fs.String("run-id", "", "Optional run id.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, controlDemoDescription())
			return nil
		}
		return err
	}

	envelope, err := newControlDemoClient(*addr).Call(ctx, "get_status_snapshot", control.StatusSnapshotRequest{RunID: strings.TrimSpace(*runID)})
	if err != nil {
		return err
	}
	return writePrettyJSON(inv.Stdout, envelope.Payload)
}

func runControlDemoPending(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("control demo pending", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	addr := fs.String("addr", "127.0.0.1:44777", "Control server listen address.")
	runID := fs.String("run-id", "", "Optional run id.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, controlDemoDescription())
			return nil
		}
		return err
	}

	envelope, err := newControlDemoClient(*addr).Call(ctx, "get_pending_action", control.PendingActionRequest{RunID: strings.TrimSpace(*runID)})
	if err != nil {
		return err
	}
	return writePrettyJSON(inv.Stdout, envelope.Payload)
}

func runControlDemoInject(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("control demo inject", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	addr := fs.String("addr", "127.0.0.1:44777", "Control server listen address.")
	runID := fs.String("run-id", "", "Optional run id.")
	source := fs.String("source", "control_chat", "Control message source.")
	reason := fs.String("reason", "operator_intervention", "Control message reason.")
	message := fs.String("message", "", "Raw control message text.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, controlDemoDescription())
			return nil
		}
		return err
	}
	if strings.TrimSpace(*message) == "" {
		return errors.New("control demo inject requires --message")
	}

	envelope, err := newControlDemoClient(*addr).Call(ctx, "inject_control_message", control.InjectControlMessageRequest{
		RunID:   strings.TrimSpace(*runID),
		Message: *message,
		Source:  strings.TrimSpace(*source),
		Reason:  strings.TrimSpace(*reason),
	})
	if err != nil {
		return err
	}
	return writePrettyJSON(inv.Stdout, envelope.Payload)
}

func runControlDemoSetVerbosity(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("control demo set-verbosity", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	addr := fs.String("addr", "127.0.0.1:44777", "Control server listen address.")
	verbosity := fs.String("verbosity", "", "Verbosity level: quiet, normal, verbose, or trace.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, controlDemoDescription())
			return nil
		}
		return err
	}
	if strings.TrimSpace(*verbosity) == "" {
		return errors.New("control demo set-verbosity requires --verbosity")
	}

	envelope, err := newControlDemoClient(*addr).Call(ctx, "set_verbosity", control.SetVerbosityRequest{
		Scope:     "runtime",
		Verbosity: strings.TrimSpace(*verbosity),
	})
	if err != nil {
		return err
	}
	return writePrettyJSON(inv.Stdout, envelope.Payload)
}

func runControlDemoStopSafe(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("control demo stop-safe", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	addr := fs.String("addr", "127.0.0.1:44777", "Control server listen address.")
	runID := fs.String("run-id", "", "Optional run id.")
	reason := fs.String("reason", "operator_requested_safe_stop", "Stop reason written to the stop flag.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, controlDemoDescription())
			return nil
		}
		return err
	}

	envelope, err := newControlDemoClient(*addr).Call(ctx, "stop_safe", control.StopFlagRequest{
		RunID:  strings.TrimSpace(*runID),
		Reason: strings.TrimSpace(*reason),
	})
	if err != nil {
		return err
	}
	return writePrettyJSON(inv.Stdout, envelope.Payload)
}

func runControlDemoClearStop(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("control demo clear-stop", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	addr := fs.String("addr", "127.0.0.1:44777", "Control server listen address.")
	runID := fs.String("run-id", "", "Optional run id.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, controlDemoDescription())
			return nil
		}
		return err
	}

	envelope, err := newControlDemoClient(*addr).Call(ctx, "clear_stop_flag", control.StopFlagRequest{RunID: strings.TrimSpace(*runID)})
	if err != nil {
		return err
	}
	return writePrettyJSON(inv.Stdout, envelope.Payload)
}

func runControlDemoEvents(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("control demo events", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	addr := fs.String("addr", "127.0.0.1:44777", "Control server listen address.")
	runID := fs.String("run-id", "", "Optional run id.")
	fromSequence := fs.Int64("from-sequence", 0, "Starting event sequence.")
	maxEvents := fs.Int("max-events", 0, "Optional event count limit.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, controlDemoDescription())
			return nil
		}
		return err
	}

	count := 0
	return newControlDemoClient(*addr).StreamEvents(ctx, control.StreamEventsParams{
		FromSequence: *fromSequence,
		RunID:        strings.TrimSpace(*runID),
	}, func(event activity.Event) error {
		payloadJSON, err := json.Marshal(event.Payload)
		if err != nil {
			return err
		}
		fmt.Fprintf(inv.Stdout, "sequence=%d event=%s at=%s payload=%s\n", event.Sequence, event.Event, event.At.UTC().Format(timeLayout), string(payloadJSON))
		count++
		if *maxEvents > 0 && count >= *maxEvents {
			return control.ErrStopStream
		}
		return nil
	})
}

func newControlDemoClient(addr string) control.Client {
	return control.Client{BaseURL: strings.TrimSpace(addr)}
}

func writePrettyJSON(stdout io.Writer, payload any) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(encoded))
	return err
}

func controlDemoDescription() string {
	return stringsJoin(
		"Usage:",
		"  orchestrator control serve [--addr HOST:PORT]",
		"  orchestrator control demo status [--addr HOST:PORT] [--run-id ID]",
		"  orchestrator control demo pending [--addr HOST:PORT] [--run-id ID]",
		"  orchestrator control demo inject --message TEXT [--addr HOST:PORT] [--run-id ID] [--source SOURCE] [--reason REASON]",
		"  orchestrator control demo set-verbosity --verbosity LEVEL [--addr HOST:PORT]",
		"  orchestrator control demo stop-safe [--addr HOST:PORT] [--run-id ID] [--reason TEXT]",
		"  orchestrator control demo clear-stop [--addr HOST:PORT] [--run-id ID]",
		"  orchestrator control demo events [--addr HOST:PORT] [--run-id ID] [--from-sequence N] [--max-events N]",
		"",
		"Uses the real local V2 control protocol for a minimal operator-facing demo.",
		"Start 'orchestrator control serve' first, then use these demo subcommands",
		"from another terminal to inspect status, watch events, inject a control",
		"message, change verbosity, or set and clear the safe-stop flag.",
	)
}
