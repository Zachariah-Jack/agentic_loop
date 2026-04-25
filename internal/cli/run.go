package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/executor"
	"orchestrator/internal/executor/appserver"
)

func newRunCommand() Command {
	return Command{
		Name:    "run",
		Summary: "Create a new run and keep advancing it until a real stop boundary.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator run --goal TEXT [--bounded]",
			"",
			"Requires the target repo contract scaffold created by `orchestrator init`.",
			"",
			"Creates a durable run and, by default, keeps advancing it in the",
			"foreground through repeated bounded cycles until a mechanical stop",
			"boundary is hit.",
			"",
			"Use --bounded to execute exactly one bounded cycle instead.",
		),
		Run: runRun,
	}
}

func runRun(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	goal := fs.String("goal", "", "Human-entered goal for the run record.")
	bounded := fs.Bool("bounded", false, "Execute exactly one bounded cycle instead of foreground unattended progress.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newRunCommand().Description)
			return nil
		}
		return err
	}

	if strings.TrimSpace(*goal) == "" {
		return errors.New("run requires --goal")
	}
	if contract := inspectTargetRepoContract(inv.RepoRoot); !contract.Ready {
		return writeMissingRepoContractReport(inv.Stdout, "run", inv.RepoRoot, strings.TrimSpace(*goal), contract)
	}

	store, journalWriter, run, err := createAutoRun(ctx, inv, strings.TrimSpace(*goal))
	if err != nil {
		return err
	}
	defer store.Close()

	if *bounded {
		return executeBoundedCycle(ctx, inv, store, journalWriter, run, boundedCycleMode{
			Command:   "run",
			RunAction: "created_new_run",
		})
	}

	return executeForegroundLoop(ctx, inv, store, journalWriter, run, foregroundLoopMode{
		Command:         "run",
		RunAction:       "created_new_run",
		EventPrefix:     "run",
		InvocationLabel: "run",
		StopFlagKey:     "run.stop_flag_path",
	})
}

type lazyExecutorClient struct {
	version        string
	executeTimeout time.Duration
	mu             sync.Mutex
	client         *appserver.Client
}

func (l *lazyExecutorClient) Execute(ctx context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	l.mu.Lock()
	if l.client == nil {
		client, err := appserver.NewClient(l.version)
		if err != nil {
			l.mu.Unlock()
			return executor.TurnResult{}, err
		}
		client.Timeout = l.executeTimeout
		l.client = &client
	}
	client := l.client
	l.mu.Unlock()

	return client.Execute(ctx, req)
}

func newLazyExecutorClient(version string, cfg config.Config) lazyExecutorClient {
	timeout, unlimited, err := config.ExecutorTurnTimeoutDuration(cfg)
	if err != nil || unlimited {
		timeout = 0
	}
	return lazyExecutorClient{version: version, executeTimeout: timeout}
}

func ptrLazyExecutorClient(client lazyExecutorClient) *lazyExecutorClient {
	return &client
}
