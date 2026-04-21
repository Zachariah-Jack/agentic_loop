package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"orchestrator/internal/config"
	"orchestrator/internal/journal"
	ntfybridge "orchestrator/internal/ntfy"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/plugins"
	"orchestrator/internal/state"
)

type boundedCycleMode struct {
	Command   string
	RunAction string
}

type boundedCycleRunnerFunc func(context.Context, Invocation, *state.Store, *journal.Journal, state.Run) (orchestration.Result, error)

var boundedCycleRunner boundedCycleRunnerFunc = runBoundedCycle

func executeBoundedCycle(
	ctx context.Context,
	inv Invocation,
	store *state.Store,
	journalWriter *journal.Journal,
	run state.Run,
	mode boundedCycleMode,
) error {
	cycleResult, cycleErr := boundedCycleRunner(ctx, inv, store, journalWriter, run)
	if cycleResult.Run.ID == "" {
		cycleResult.Run = run
	}
	stopReason := orchestration.StopReasonForBoundedCycle(cycleResult, cycleErr)
	if stopReason != "" && cycleResult.Run.LatestStopReason != stopReason {
		if err := store.SaveLatestStopReason(ctx, cycleResult.Run.ID, stopReason); err != nil {
			return err
		}
		cycleResult.Run.LatestStopReason = stopReason
	}

	if err := writeBoundedCycleReport(inv.Stdout, inv, cycleResult, cycleErr, mode); err != nil {
		return err
	}

	return cycleErr
}

func runBoundedCycle(
	ctx context.Context,
	inv Invocation,
	store *state.Store,
	journalWriter *journal.Journal,
	run state.Run,
) (orchestration.Result, error) {
	plannerClient := planner.Client{
		APIKey: plannerAPIKey(),
		Model:  resolvePlannerModel(inv),
	}
	pluginManager := loadRuntimePlugins(journalWriter, run)

	cycle := orchestration.Cycle{
		Store:                      store,
		Journal:                    journalWriter,
		Planner:                    &plannerClient,
		Executor:                   &lazyExecutorClient{version: inv.Version},
		HumanInteractor:            newHumanInteractor(inv, journalWriter),
		DriftWatcher:               orchestration.NewDeterministicDriftWatcher(),
		DriftReviewOn:              inv.Config.DriftWatcherEnabled,
		WorkerPlanConcurrencyLimit: inv.Config.WorkerConcurrencyLimit,
		Plugins:                    pluginManager,
	}

	return cycle.RunOnce(ctx, run)
}

func loadRuntimePlugins(journalWriter *journal.Journal, run state.Run) *plugins.Manager {
	pluginDir := filepath.Join(run.RepoPath, plugins.DefaultDirectory)
	if journalWriter != nil && strings.TrimSpace(run.ID) != "" {
		_ = journalWriter.Append(journal.Event{
			Type:     "plugin.load.started",
			RunID:    run.ID,
			RepoPath: run.RepoPath,
			Goal:     run.Goal,
			Status:   string(run.Status),
			Message:  "loading local plugins from " + pluginDir,
		})
	}

	manager, summary := plugins.Load(run.RepoPath)
	if journalWriter != nil && strings.TrimSpace(run.ID) != "" {
		for _, failure := range summary.Failures {
			pluginName := strings.TrimSpace(failure.Plugin)
			if pluginName == "" {
				pluginName = "plugin_loader"
			}
			_ = journalWriter.Append(journal.Event{
				Type:         "plugin.load.failed",
				RunID:        run.ID,
				RepoPath:     run.RepoPath,
				Goal:         run.Goal,
				Status:       string(run.Status),
				Message:      failure.Message,
				PluginName:   pluginName,
				ArtifactPath: strings.TrimSpace(failure.Path),
			})
		}

		_ = journalWriter.Append(journal.Event{
			Type:     "plugin.load.completed",
			RunID:    run.ID,
			RepoPath: run.RepoPath,
			Goal:     run.Goal,
			Status:   string(run.Status),
			Message:  fmt.Sprintf("loaded %d plugin(s) from %s", summary.Loaded, summary.Directory),
		})
	}

	return manager
}

func newHumanInteractor(inv Invocation, journalWriter *journal.Journal) orchestration.HumanInteractor {
	terminal := terminalHumanInteractor{input: inv.Stdin, output: inv.Stdout}

	if !ntfybridge.IsConfigured(inv.Config.NTFY) {
		return terminal
	}

	client, err := ntfybridge.NewClient(inv.Config.NTFY)
	if err != nil {
		return terminal
	}

	return ntfyHumanInteractor{
		client:   client,
		fallback: terminal,
		output:   inv.Stdout,
		journal:  journalWriter,
	}
}

func writeBoundedCycleReport(stdout io.Writer, inv Invocation, cycleResult orchestration.Result, cycleErr error, mode boundedCycleMode) error {
	return writeCommandReport(stdout, resolveOutputVerbosity(inv), commandReport{
		Command:                    mode.Command,
		RunAction:                  mode.RunAction,
		CycleNumber:                1,
		PlannerModel:               resolvePlannerModel(inv),
		Run:                        cycleResult.Run,
		StopReason:                 orchestration.StopReasonForBoundedCycle(cycleResult, cycleErr),
		LatestArtifactPath:         latestArtifactPathForRun(inv.Layout, cycleResult.Run.ID),
		FirstPlannerResult:         cycleResult.FirstPlannerResult,
		ReconsiderationPlannerTurn: cycleResult.ReconsiderationPlannerTurn,
		SecondPlannerTurn:          cycleResult.SecondPlannerTurn,
		ExecutorDispatched:         cycleResult.ExecutorDispatched,
		CycleError:                 cycleErr,
	})
}

type terminalHumanInteractor struct {
	input  io.Reader
	output io.Writer
}

func (t terminalHumanInteractor) Ask(_ context.Context, _ state.Run, outcome planner.AskHumanOutcome) (orchestration.HumanInput, error) {
	if t.input == nil {
		return orchestration.HumanInput{}, errors.New("stdin is required when planner outcome is ask_human")
	}

	output := t.output
	if output == nil {
		output = io.Discard
	}

	fmt.Fprintln(output, "planner_question:")
	fmt.Fprintln(output, outcome.Question)
	if strings.TrimSpace(outcome.Context) != "" {
		fmt.Fprintln(output, "planner_question_context:")
		fmt.Fprintln(output, outcome.Context)
	}
	fmt.Fprint(output, "human_reply> ")

	reply, err := bufio.NewReader(t.input).ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && reply != "" {
			return orchestration.HumanInput{Source: "terminal", Payload: reply}, nil
		}
		if errors.Is(err, io.EOF) {
			return orchestration.HumanInput{}, errors.New("terminal input closed before human reply was received")
		}
		return orchestration.HumanInput{}, err
	}

	return orchestration.HumanInput{Source: "terminal", Payload: reply}, nil
}

type ntfyHumanInteractor struct {
	client   *ntfybridge.Client
	fallback terminalHumanInteractor
	output   io.Writer
	journal  *journal.Journal
}

func (n ntfyHumanInteractor) Ask(ctx context.Context, run state.Run, outcome planner.AskHumanOutcome) (orchestration.HumanInput, error) {
	if n.client == nil {
		return n.fallback.Ask(ctx, run, outcome)
	}

	published, err := n.client.PublishQuestion(ctx, ntfybridge.Question{
		Question: outcome.Question,
		Context:  outcome.Context,
	})
	if err != nil {
		n.appendNTFYEvent("ntfy.question.publish_failed", run, "ntfy publish failed; falling back to terminal: "+err.Error(), "", "")
		n.printFallback(err)
		return n.fallback.Ask(ctx, run, outcome)
	}

	n.appendNTFYEvent("ntfy.question.published", run, "planner question published to ntfy", published.ID, "")
	n.appendNTFYEvent("ntfy.wait.started", run, "waiting for one ntfy reply", published.ID, "")
	n.printWait()

	reply, err := n.client.WaitForReply(ctx, published.ID)
	if err != nil {
		n.appendNTFYEvent("ntfy.wait.failed", run, "ntfy wait failed; falling back to terminal: "+err.Error(), published.ID, "")
		n.printFallback(err)
		return n.fallback.Ask(ctx, run, outcome)
	}

	n.appendNTFYEvent("ntfy.reply.received", run, "raw ntfy reply received", reply.ID, reply.Payload)
	return orchestration.HumanInput{
		Source:  "ntfy",
		Payload: reply.Payload,
	}, nil
}

func (n ntfyHumanInteractor) appendNTFYEvent(eventType string, run state.Run, message string, messageID string, payload string) {
	if n.journal == nil {
		return
	}

	event := journal.Event{
		Type:              eventType,
		RunID:             run.ID,
		RepoPath:          run.RepoPath,
		Goal:              run.Goal,
		Status:            string(run.Status),
		Message:           message,
		NTFYServerURL:     n.client.ServerURL(),
		NTFYTopic:         n.client.Topic(),
		NTFYMessageID:     strings.TrimSpace(messageID),
		HumanReplySource:  "ntfy",
		HumanReplyPayload: payload,
	}
	if eventType == "ntfy.question.publish_failed" || eventType == "ntfy.wait.failed" {
		event.StopReason = orchestration.StopReasonNTFYFallbackUsed
	}

	_ = n.journal.Append(event)
}

func (n ntfyHumanInteractor) printWait() {
	output := n.output
	if output == nil {
		return
	}

	fmt.Fprintln(output, "planner_question_delivery: ntfy")
	fmt.Fprintf(output, "ntfy.server: %s\n", n.client.ServerURL())
	fmt.Fprintf(output, "ntfy.topic: %s\n", n.client.Topic())
	fmt.Fprintln(output, "ntfy.wait: waiting for one raw reply message")
}

func (n ntfyHumanInteractor) printFallback(err error) {
	output := n.output
	if output == nil {
		return
	}

	fmt.Fprintln(output, "planner_question_delivery: terminal_fallback")
	fmt.Fprintf(output, "ntfy.stop_reason: %s\n", orchestration.StopReasonNTFYFallbackUsed)
	fmt.Fprintf(output, "ntfy.error: %s\n", err)
}

func ntfyConfigured(cfg config.Config) bool {
	return ntfybridge.IsConfigured(cfg.NTFY)
}
