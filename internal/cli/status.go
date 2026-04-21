package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/journal"
	ntfybridge "orchestrator/internal/ntfy"
	"orchestrator/internal/plugins"
	"orchestrator/internal/state"
	workerctl "orchestrator/internal/workers"
)

const timeLayout = time.RFC3339Nano

func newStatusCommand() Command {
	return Command{
		Name:    "status",
		Summary: "Show the latest durable run snapshot and operator readiness.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator status",
			"",
			"Shows the latest durable run summary, stable checkpoint, stop reason,",
			"next operator action, and current runtime readiness.",
		),
		Run: runStatus,
	}
}

func runStatus(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newStatusCommand().Description)
			return nil
		}
		return err
	}

	verbosity := resolveOutputVerbosity(inv)
	_, pluginSummary := plugins.Load(inv.RepoRoot)
	cfgState := "missing"
	if _, err := config.Load(inv.ConfigPath); err == nil {
		cfgState = "present"
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	fmt.Fprintln(inv.Stdout, "runtime:")
	fmt.Fprintf(inv.Stdout, "  binary.version: %s\n", inv.Version)
	fmt.Fprintf(inv.Stdout, "  repo.root: %s\n", inv.RepoRoot)
	fmt.Fprintf(inv.Stdout, "  config.path: %s (%s)\n", inv.ConfigPath, cfgState)
	fmt.Fprintf(inv.Stdout, "  repo.markers.agents_md: %t\n", pathExists(filepath.Join(inv.RepoRoot, "AGENTS.md")))
	fmt.Fprintf(inv.Stdout, "  repo.markers.updated_spec: %t\n", pathExists(filepath.Join(inv.RepoRoot, "docs", "ORCHESTRATOR_CLI_UPDATED_SPEC.md")))
	fmt.Fprintln(inv.Stdout, "  planner.transport: responses_api")
	fmt.Fprintf(inv.Stdout, "  planner.model: %s\n", resolvePlannerModel(inv))
	fmt.Fprintf(inv.Stdout, "  planner.api_key: %s\n", plannerAPIKeyStatus())
	fmt.Fprintf(inv.Stdout, "  workers.concurrency_limit: %d\n", inv.Config.WorkerConcurrencyLimit)
	fmt.Fprintf(inv.Stdout, "  review.drift_watcher_enabled: %t\n", inv.Config.DriftWatcherEnabled)
	fmt.Fprintf(inv.Stdout, "  plugins.enabled: %t\n", pluginSummary.Enabled)
	fmt.Fprintf(inv.Stdout, "  plugins.loaded: %d\n", pluginSummary.Loaded)
	fmt.Fprintf(inv.Stdout, "  plugins.load_failures: %d\n", len(pluginSummary.Failures))
	workerSupport := workerctl.DetectSupport(ctx, inv.RepoRoot)
	fmt.Fprintf(inv.Stdout, "  workers.support: %s\n", workerSupportLabel(workerSupport))
	fmt.Fprintln(inv.Stdout, "  executor.transport.primary: codex_app_server")
	fmt.Fprintf(inv.Stdout, "  executor.app_server: %s\n", executorAppServerState())
	fmt.Fprintf(inv.Stdout, "  ntfy.configured: %t\n", ntfyConfigured(inv.Config))
	if verbosity != outputVerbosityQuiet {
		fmt.Fprintf(inv.Stdout, "  plugins.directory: %s\n", valueOrUnavailable(strings.TrimSpace(pluginSummary.Directory)))
		fmt.Fprintf(inv.Stdout, "  plugins.last_load_failure: %s\n", valueOrUnavailable(pluginFailureSummary(pluginSummary)))
		fmt.Fprintf(inv.Stdout, "  workers.directory: %s\n", inv.Layout.WorkersDir)
		fmt.Fprintf(inv.Stdout, "  ntfy.server_url: %s\n", valueOrUnavailable(strings.TrimSpace(inv.Config.NTFY.ServerURL)))
		fmt.Fprintf(inv.Stdout, "  ntfy.topic: %s\n", valueOrUnavailable(strings.TrimSpace(inv.Config.NTFY.Topic)))
		fmt.Fprintf(inv.Stdout, "  ntfy.auth_token: %s\n", ntfyAuthTokenState(inv.Config))
	}
	fmt.Fprintf(inv.Stdout, "  ntfy.bridge: %s\n", ntfyBridgeState(inv.Config))

	fmt.Fprintln(inv.Stdout, "")
	fmt.Fprintln(inv.Stdout, "persistence:")
	fmt.Fprintf(inv.Stdout, "  state.dir: %s (%t)\n", inv.Layout.StateDir, pathExists(inv.Layout.StateDir))
	fmt.Fprintf(inv.Stdout, "  logs.dir: %s (%t)\n", inv.Layout.LogsDir, pathExists(inv.Layout.LogsDir))
	fmt.Fprintf(inv.Stdout, "  state.db: %s (%t)\n", inv.Layout.DBPath, pathExists(inv.Layout.DBPath))
	fmt.Fprintf(inv.Stdout, "  event.journal: %s (%t)\n", inv.Layout.JournalPath, pathExists(inv.Layout.JournalPath))

	if pathExists(inv.Layout.DBPath) {
		store, err := openExistingStore(inv.Layout)
		if err != nil {
			return err
		}
		defer store.Close()

		if err := store.EnsureSchema(ctx); err != nil {
			return err
		}

		var latestEvents []journal.Event
		var latestPlannerOutcome string
		var latestRun state.Run
		var latestFound bool
		var workerStats state.WorkerStats
		var latestRunWorkers []state.Worker

		stats, err := store.Stats(ctx)
		if err != nil {
			return err
		}
		workerStats, err = store.WorkerStats(ctx, "")
		if err != nil {
			return err
		}
		fmt.Fprintf(inv.Stdout, "  runs.total: %d\n", stats.TotalRuns)
		fmt.Fprintf(inv.Stdout, "  runs.resumable: %d\n", stats.ResumableRuns)

		latestRun, latestFound, err = store.LatestRun(ctx)
		if err != nil {
			return err
		}

		if latestFound {
			latestRunWorkers, err = store.ListWorkers(ctx, latestRun.ID)
			if err != nil {
				return err
			}
			if latestEvents, err = latestRunEvents(inv.Layout, latestRun.ID, 64); err != nil {
				return err
			}
			latestPlannerOutcome = latestPlannerOutcomeFromEvents(latestEvents)

			fmt.Fprintln(inv.Stdout, "")
			fmt.Fprintln(inv.Stdout, "latest_run:")
			fmt.Fprintln(inv.Stdout, "  present: true")
			fmt.Fprintf(inv.Stdout, "  summary.id: %s\n", latestRun.ID)
			fmt.Fprintf(inv.Stdout, "  summary.goal: %s\n", latestRun.Goal)
			fmt.Fprintf(inv.Stdout, "  summary.status: %s\n", latestRun.Status)
			fmt.Fprintf(inv.Stdout, "  summary.updated_at: %s\n", latestRun.UpdatedAt.Format(timeLayout))
			fmt.Fprintf(inv.Stdout, "  summary.resumable: %t\n", isRunResumable(latestRun))
			fmt.Fprintf(inv.Stdout, "  summary.completed: %t\n", latestRun.Status == state.StatusCompleted)
			fmt.Fprintf(inv.Stdout, "  summary.next_operator_action: %s\n", nextOperatorActionForExistingRun(latestRun))
			fmt.Fprintf(inv.Stdout, "  checkpoint.sequence: %d\n", latestRun.LatestCheckpoint.Sequence)
			fmt.Fprintf(inv.Stdout, "  checkpoint.stage: %s\n", valueOrUnavailable(latestRun.LatestCheckpoint.Stage))
			fmt.Fprintf(inv.Stdout, "  checkpoint.label: %s\n", valueOrUnavailable(latestRun.LatestCheckpoint.Label))
			fmt.Fprintf(inv.Stdout, "  checkpoint.safe_pause: %t\n", latestRun.LatestCheckpoint.SafePause)
			fmt.Fprintf(inv.Stdout, "  planner.outcome: %s\n", valueOrUnavailable(latestPlannerOutcome))
			fmt.Fprintf(inv.Stdout, "  executor.turn_status: %s\n", valueOrUnavailable(latestRun.ExecutorTurnStatus))
			fmt.Fprintf(inv.Stdout, "  executor.preview: %s\n", valueOrUnavailable(previewString(latestRun.ExecutorLastMessage, 240)))
			fmt.Fprintf(inv.Stdout, "  executor.approval_state: %s\n", valueOrUnavailable(executorApprovalStateValue(latestRun)))
			fmt.Fprintf(inv.Stdout, "  executor.approval_kind: %s\n", valueOrUnavailable(executorApprovalKindValue(latestRun)))
			fmt.Fprintf(inv.Stdout, "  executor.thread_id: %s\n", valueOrUnavailable(latestRun.ExecutorThreadID))
			fmt.Fprintf(inv.Stdout, "  executor.turn_id: %s\n", valueOrUnavailable(latestRun.ExecutorTurnID))
			fmt.Fprintf(inv.Stdout, "  executor.interruptible: %t\n", executorTurnInterruptible(latestRun))
			fmt.Fprintf(inv.Stdout, "  executor.steerable: %t\n", executorTurnSteerable(latestRun))
			fmt.Fprintf(inv.Stdout, "  executor.last_control_action: %s\n", valueOrUnavailable(executorLastControlActionValue(latestRun)))
			fmt.Fprintf(inv.Stdout, "  human_reply.source: %s\n", valueOrUnavailable(latestHumanReplySource(latestRun)))
			fmt.Fprintf(inv.Stdout, "  human_reply.count: %d\n", len(latestRun.HumanReplies))
			fmt.Fprintf(inv.Stdout, "  stop.reason: %s\n", valueOrUnavailable(latestRun.LatestStopReason))
			fmt.Fprintf(inv.Stdout, "  stop.stable_checkpoint: %s\n", checkpointSummary(latestRun.LatestCheckpoint))
			fmt.Fprintf(inv.Stdout, "  artifact.path: %s\n", valueOrUnavailable(latestArtifactPathFromEvents(latestEvents)))
			integrationArtifactPath := latestIntegrationArtifactPathFromEvents(latestEvents)
			integrationApplyStatus, integrationApplyArtifactPath := latestIntegrationApplySummaryFromEvents(latestEvents)
			fmt.Fprintf(inv.Stdout, "  integration.present: %t\n", strings.TrimSpace(integrationArtifactPath) != "")
			fmt.Fprintf(inv.Stdout, "  integration.artifact_path: %s\n", valueOrUnavailable(integrationArtifactPath))
			fmt.Fprintf(inv.Stdout, "  integration.apply_status: %s\n", valueOrUnavailable(integrationApplyStatus))
			fmt.Fprintf(inv.Stdout, "  integration.apply_artifact_path: %s\n", valueOrUnavailable(integrationApplyArtifactPath))
			workerPlan := latestWorkerPlan(latestRun)
			fmt.Fprintf(inv.Stdout, "  worker_plan.present: %t\n", workerPlan != nil)
			if workerPlan != nil {
				workerPlanApplyStatus := ""
				if workerPlan.Apply != nil {
					workerPlanApplyStatus = strings.TrimSpace(workerPlan.Apply.Status)
				}
				fmt.Fprintf(inv.Stdout, "  worker_plan.status: %s\n", valueOrUnavailable(strings.TrimSpace(workerPlan.Status)))
				fmt.Fprintf(inv.Stdout, "  worker_plan.worker_count: %d\n", len(workerPlan.Workers))
				fmt.Fprintf(inv.Stdout, "  worker_plan.concurrency_limit: %d\n", workerPlan.ConcurrencyLimit)
				fmt.Fprintf(inv.Stdout, "  worker_plan.active: %t\n", workerPlanActive(latestRunWorkers))
				fmt.Fprintf(inv.Stdout, "  worker_plan.integration_requested: %t\n", workerPlan.IntegrationRequested)
				fmt.Fprintf(inv.Stdout, "  worker_plan.integration_artifact_path: %s\n", valueOrUnavailable(strings.TrimSpace(workerPlan.IntegrationArtifactPath)))
				fmt.Fprintf(inv.Stdout, "  worker_plan.apply_mode: %s\n", valueOrUnavailable(strings.TrimSpace(workerPlan.ApplyMode)))
				fmt.Fprintf(inv.Stdout, "  worker_plan.apply_status: %s\n", valueOrUnavailable(workerPlanApplyStatus))
				fmt.Fprintf(inv.Stdout, "  worker_plan.apply_artifact_path: %s\n", valueOrUnavailable(strings.TrimSpace(workerPlan.ApplyArtifactPath)))
			}
			if verbosity.verboseEnabled() {
				fmt.Fprintf(inv.Stdout, "  summary.previous_response_id: %s\n", valueOrUnavailable(latestRun.PreviousResponseID))
				fmt.Fprintf(inv.Stdout, "  last_error.code: %s\n", valueOrUnavailable(latestRun.RuntimeIssueReason))
				fmt.Fprintf(inv.Stdout, "  last_error.message: %s\n", valueOrUnavailable(latestRun.RuntimeIssueMessage))
			}
			if verbosity.verboseEnabled() && latestRun.ExecutorTurnStatus != "" {
				fmt.Fprintf(inv.Stdout, "  executor.transport: %s\n", valueOrUnavailable(latestRun.ExecutorTransport))
				fmt.Fprintf(inv.Stdout, "  executor.turn_id: %s\n", valueOrUnavailable(latestRun.ExecutorTurnID))
				fmt.Fprintf(inv.Stdout, "  executor.last_error: %s\n", valueOrUnavailable(latestRun.ExecutorLastError))
			}
		} else {
			fmt.Fprintln(inv.Stdout, "")
			fmt.Fprintln(inv.Stdout, "latest_run:")
			fmt.Fprintln(inv.Stdout, "  present: false")
		}

		fmt.Fprintln(inv.Stdout, "")
		fmt.Fprintln(inv.Stdout, "workers:")
		fmt.Fprintf(inv.Stdout, "  total: %d\n", workerStats.Total)
		fmt.Fprintf(inv.Stdout, "  active: %d\n", workerStats.Active)
		fmt.Fprintf(inv.Stdout, "  latest_run_count: %d\n", len(latestRunWorkers))
		if len(latestRunWorkers) == 0 {
			fmt.Fprintln(inv.Stdout, "  present: false")
		} else {
			fmt.Fprintln(inv.Stdout, "  present: true")
			fmt.Fprintf(inv.Stdout, "  latest_run_status_counts.pending: %d\n", workerStatusCount(latestRunWorkers, state.WorkerStatusPending))
			fmt.Fprintf(inv.Stdout, "  latest_run_status_counts.active: %d\n", activeWorkerStatusCount(latestRunWorkers))
			fmt.Fprintf(inv.Stdout, "  latest_run_status_counts.approval_required: %d\n", workerStatusCount(latestRunWorkers, state.WorkerStatusApprovalRequired))
			fmt.Fprintf(inv.Stdout, "  latest_run_status_counts.completed: %d\n", workerStatusCount(latestRunWorkers, state.WorkerStatusCompleted))
			fmt.Fprintf(inv.Stdout, "  latest_run_status_counts.failed: %d\n", workerStatusCount(latestRunWorkers, state.WorkerStatusFailed))
			for idx, worker := range workerSummary(latestRunWorkers, 3) {
				fmt.Fprintf(inv.Stdout, "  worker.%d.id: %s\n", idx+1, worker.ID)
				fmt.Fprintf(inv.Stdout, "  worker.%d.name: %s\n", idx+1, worker.WorkerName)
				fmt.Fprintf(inv.Stdout, "  worker.%d.status: %s\n", idx+1, worker.WorkerStatus)
				fmt.Fprintf(inv.Stdout, "  worker.%d.scope: %s\n", idx+1, worker.AssignedScope)
				fmt.Fprintf(inv.Stdout, "  worker.%d.path: %s\n", idx+1, worker.WorktreePath)
				fmt.Fprintf(inv.Stdout, "  worker.%d.approval_required: %t\n", idx+1, workerApprovalRequired(worker))
				fmt.Fprintf(inv.Stdout, "  worker.%d.approval_kind: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorApprovalKind)))
				fmt.Fprintf(inv.Stdout, "  worker.%d.executor_thread_id: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorThreadID)))
				fmt.Fprintf(inv.Stdout, "  worker.%d.executor_turn_id: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorTurnID)))
				fmt.Fprintf(inv.Stdout, "  worker.%d.executor_interruptible: %t\n", idx+1, workerTurnInterruptibleState(worker))
				fmt.Fprintf(inv.Stdout, "  worker.%d.executor_steerable: %t\n", idx+1, workerTurnSteerableState(worker))
				fmt.Fprintf(inv.Stdout, "  worker.%d.executor_last_control_action: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorLastControlAction)))
			}
		}
	} else {
		fmt.Fprintln(inv.Stdout, "  runs.total: 0")
		fmt.Fprintln(inv.Stdout, "  runs.resumable: 0")
		fmt.Fprintln(inv.Stdout, "")
		fmt.Fprintln(inv.Stdout, "latest_run:")
		fmt.Fprintln(inv.Stdout, "  present: false")
		fmt.Fprintln(inv.Stdout, "")
		fmt.Fprintln(inv.Stdout, "workers:")
		fmt.Fprintln(inv.Stdout, "  total: 0")
		fmt.Fprintln(inv.Stdout, "  active: 0")
		fmt.Fprintln(inv.Stdout, "  latest_run_count: 0")
		fmt.Fprintln(inv.Stdout, "  present: false")
	}

	fmt.Fprintln(inv.Stdout, "")
	fmt.Fprintln(inv.Stdout, "health:")
	fmt.Fprintf(inv.Stdout, "  planner: %s\n", plannerTransportState())
	fmt.Fprintf(inv.Stdout, "  executor: %s\n", executorRuntimeState())
	fmt.Fprintln(inv.Stdout, "  persistence: sqlite metadata store plus jsonl journal")
	return nil
}

func plannerAPIKeyStatus() string {
	if plannerAPIKey() == "" {
		return "missing"
	}
	return "present"
}

func plannerTransportState() string {
	if plannerAPIKey() == "" {
		return "live planner blocked (missing OPENAI_API_KEY)"
	}
	return "live planner ready"
}

func executorAppServerState() string {
	if _, err := appserver.ResolveLaunchPlan(); err != nil {
		return "blocked (" + err.Error() + ")"
	}
	return "ready"
}

func executorRuntimeState() string {
	if _, err := appserver.ResolveLaunchPlan(); err != nil {
		return "primary executor transport blocked"
	}
	return "primary executor single-turn dispatch ready"
}

func ntfyAuthTokenState(cfg config.Config) string {
	if strings.TrimSpace(cfg.NTFY.AuthToken) == "" {
		return "missing"
	}
	return "present"
}

func ntfyBridgeState(cfg config.Config) string {
	if !ntfyConfigured(cfg) {
		return "terminal fallback only"
	}
	if _, err := ntfybridge.NewClient(cfg.NTFY); err != nil {
		return "blocked (" + err.Error() + ")"
	}
	return "ready"
}

func latestRunEvents(layout state.Layout, runID string, limit int) ([]journal.Event, error) {
	if !pathExists(layout.JournalPath) {
		return nil, nil
	}

	journalWriter, err := openExistingJournal(layout)
	if err != nil {
		return nil, err
	}

	return journalWriter.ReadRecent(runID, limit)
}

func latestPlannerOutcomeFromEvents(events []journal.Event) string {
	for idx := len(events) - 1; idx >= 0; idx-- {
		if strings.TrimSpace(events[idx].Type) == "planner.turn.completed" {
			return strings.TrimSpace(events[idx].PlannerOutcome)
		}
	}
	return ""
}

func latestHumanReplySource(run state.Run) string {
	if len(run.HumanReplies) == 0 {
		return ""
	}
	return strings.TrimSpace(run.HumanReplies[len(run.HumanReplies)-1].Source)
}

func latestArtifactPathFromEvents(events []journal.Event) string {
	for idx := len(events) - 1; idx >= 0; idx-- {
		if path := strings.TrimSpace(events[idx].ArtifactPath); path != "" {
			return path
		}
	}
	return ""
}

func latestIntegrationArtifactPathFromEvents(events []journal.Event) string {
	for idx := len(events) - 1; idx >= 0; idx-- {
		if strings.TrimSpace(events[idx].Type) != "integration.completed" {
			continue
		}
		if path := strings.TrimSpace(events[idx].ArtifactPath); path != "" {
			return path
		}
	}
	return ""
}

func latestIntegrationApplySummaryFromEvents(events []journal.Event) (string, string) {
	for idx := len(events) - 1; idx >= 0; idx-- {
		eventType := strings.TrimSpace(events[idx].Type)
		switch eventType {
		case "integration.apply.completed":
			return "completed", strings.TrimSpace(events[idx].ArtifactPath)
		case "integration.apply.failed":
			return "failed", strings.TrimSpace(events[idx].ArtifactPath)
		}
	}
	return "", ""
}

func latestWorkerPlan(run state.Run) *state.WorkerPlanResult {
	if run.CollectedContext == nil {
		return nil
	}
	return run.CollectedContext.WorkerPlan
}

func pluginFailureSummary(summary plugins.Summary) string {
	if len(summary.Failures) == 0 {
		return ""
	}

	failure := summary.Failures[0]
	if strings.TrimSpace(failure.Plugin) == "" {
		return strings.TrimSpace(failure.Message)
	}
	return strings.TrimSpace(failure.Plugin) + ": " + strings.TrimSpace(failure.Message)
}

func valueOrUnavailable(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unavailable"
	}
	return value
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func workerSupportLabel(support workerctl.Support) string {
	if support.Available {
		return "available"
	}
	return "blocked (" + strings.TrimSpace(support.Detail) + ")"
}

func workerSummary(workers []state.Worker, limit int) []state.Worker {
	if limit <= 0 || len(workers) <= limit {
		return workers
	}
	return workers[:limit]
}

func workerStatusCount(workers []state.Worker, status state.WorkerStatus) int {
	count := 0
	for _, worker := range workers {
		if worker.WorkerStatus == status {
			count++
		}
	}
	return count
}

func activeWorkerStatusCount(workers []state.Worker) int {
	count := 0
	for _, worker := range workers {
		switch worker.WorkerStatus {
		case state.WorkerStatusCreating, state.WorkerStatusAssigned, state.WorkerStatusExecutorActive:
			count++
		}
	}
	return count
}

func workerPlanActive(workers []state.Worker) bool {
	return workerStatusCount(workers, state.WorkerStatusPending) > 0 || activeWorkerStatusCount(workers) > 0
}
