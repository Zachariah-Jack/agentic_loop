package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/state"
	workerctl "orchestrator/internal/workers"
)

type workerExecutor interface {
	Execute(context.Context, executor.TurnRequest) (executor.TurnResult, error)
}

var newWorkerExecutorClient = func(version string) (workerExecutor, error) {
	client, err := appserver.NewClient(version)
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func newWorkersCommand() Command {
	return Command{
		Name:    "workers",
		Summary: "Manage isolated worker worktrees mechanically.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator workers create --name TEXT --scope TEXT [--run-id ID]",
			"  orchestrator workers list [--run-id ID]",
			"  orchestrator workers remove --worker-id ID",
			"  orchestrator workers dispatch --worker-id ID --prompt TEXT",
			"  orchestrator workers approve --worker-id ID",
			"  orchestrator workers deny --worker-id ID",
			"  orchestrator workers interrupt --worker-id ID",
			"  orchestrator workers kill --worker-id ID",
			"  orchestrator workers steer --worker-id ID --message TEXT",
			"  orchestrator workers integrate --worker-ids ID,ID,...",
			"",
			"Creates isolated worker worktrees, lists the durable worker registry,",
			"removes idle/completed workers, and routes explicit executor work to a",
			"named worker workspace, controls an active worker executor turn, or",
			"builds a read-only integration preview without changing planner",
			"authority.",
		),
		Run: runWorkers,
	}
}

func runWorkers(ctx context.Context, inv Invocation) error {
	if len(inv.Args) == 0 {
		fmt.Fprintln(inv.Stdout, newWorkersCommand().Description)
		return nil
	}

	switch inv.Args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(inv.Stdout, newWorkersCommand().Description)
		return nil
	case "create":
		return runWorkersCreate(ctx, inv, inv.Args[1:])
	case "list":
		return runWorkersList(ctx, inv, inv.Args[1:])
	case "remove":
		return runWorkersRemove(ctx, inv, inv.Args[1:])
	case "dispatch":
		return runWorkersDispatch(ctx, inv, inv.Args[1:])
	case "approve":
		return runWorkersApprove(ctx, inv, inv.Args[1:])
	case "deny":
		return runWorkersDeny(ctx, inv, inv.Args[1:])
	case "interrupt":
		return runWorkersInterrupt(ctx, inv, inv.Args[1:])
	case "kill":
		return runWorkersKill(ctx, inv, inv.Args[1:])
	case "steer":
		return runWorkersSteer(ctx, inv, inv.Args[1:])
	case "integrate":
		return runWorkersIntegrate(ctx, inv, inv.Args[1:])
	default:
		return fmt.Errorf("workers requires subcommand create, list, remove, dispatch, approve, deny, interrupt, kill, steer, or integrate")
	}
}

func runWorkersCreate(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers create", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	name := fs.String("name", "", "Human-friendly worker name.")
	scope := fs.String("scope", "", "Assigned scope metadata for this worker.")
	runID := fs.String("run-id", "", "Optional run id. Defaults to the latest unfinished run.")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*name) == "" {
		return errors.New("workers create requires --name")
	}
	if strings.TrimSpace(*scope) == "" {
		return errors.New("workers create requires --scope")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	run, found, err := resolveWorkerRun(ctx, store, strings.TrimSpace(*runID))
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(inv.Stdout, "workers_create_lookup: no unfinished run found")
		return nil
	}

	manager := workerctl.NewManager(run.RepoPath, inv.Layout.WorkersDir)
	plannedPath, err := manager.PlannedPath(*name)
	if err != nil {
		appendWorkerEvent(journalWriter, "worker.create.failed", run, nil, err.Error())
		return err
	}

	worker, err := store.CreateWorker(ctx, state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    strings.TrimSpace(*name),
		WorkerStatus:  state.WorkerStatusCreating,
		AssignedScope: strings.TrimSpace(*scope),
		WorktreePath:  plannedPath,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		appendWorkerEvent(journalWriter, "worker.create.failed", run, &state.Worker{
			RunID:         run.ID,
			WorkerName:    strings.TrimSpace(*name),
			WorkerStatus:  state.WorkerStatusCreating,
			AssignedScope: strings.TrimSpace(*scope),
			WorktreePath:  plannedPath,
		}, err.Error())
		return err
	}

	if _, err := manager.Create(ctx, worker.WorkerName); err != nil {
		_ = store.DeleteWorker(ctx, worker.ID)
		appendWorkerEvent(journalWriter, "worker.create.failed", run, &worker, err.Error())
		return err
	}

	worker.WorkerStatus = state.WorkerStatusIdle
	worker.UpdatedAt = time.Now().UTC()
	if err := store.SaveWorker(ctx, worker); err != nil {
		return err
	}

	worker, found, err = store.GetWorker(ctx, worker.ID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("worker %s could not be reloaded", worker.ID)
	}

	appendWorkerEvent(journalWriter, "worker.created", run, &worker, "isolated worker worktree created")
	return writeWorkerReport(inv.Stdout, "workers create", run, worker, "")
}

func runWorkersList(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers list", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	runID := fs.String("run-id", "", "Optional run id filter.")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	workers, err := store.ListWorkers(ctx, strings.TrimSpace(*runID))
	if err != nil {
		return err
	}

	var run *state.Run
	if strings.TrimSpace(*runID) != "" {
		loadedRun, found, err := store.GetRun(ctx, strings.TrimSpace(*runID))
		if err != nil {
			return err
		}
		if found {
			run = &loadedRun
		}
	}

	appendWorkerEvent(journalWriter, "worker.listed", derefRun(run), nil, fmt.Sprintf("listed %d worker(s)", len(workers)))

	fmt.Fprintln(inv.Stdout, "command: workers list")
	fmt.Fprintf(inv.Stdout, "workers.count: %d\n", len(workers))
	if strings.TrimSpace(*runID) != "" {
		fmt.Fprintf(inv.Stdout, "workers.run_id: %s\n", strings.TrimSpace(*runID))
	}
	for idx, worker := range workers {
		fmt.Fprintf(inv.Stdout, "worker.%d.id: %s\n", idx+1, worker.ID)
		fmt.Fprintf(inv.Stdout, "worker.%d.run_id: %s\n", idx+1, worker.RunID)
		fmt.Fprintf(inv.Stdout, "worker.%d.name: %s\n", idx+1, worker.WorkerName)
		fmt.Fprintf(inv.Stdout, "worker.%d.status: %s\n", idx+1, worker.WorkerStatus)
		fmt.Fprintf(inv.Stdout, "worker.%d.scope: %s\n", idx+1, worker.AssignedScope)
		fmt.Fprintf(inv.Stdout, "worker.%d.path: %s\n", idx+1, worker.WorktreePath)
		fmt.Fprintf(inv.Stdout, "worker.%d.approval_required: %t\n", idx+1, workerApprovalRequired(worker))
		fmt.Fprintf(inv.Stdout, "worker.%d.approval_state: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorApprovalState)))
		fmt.Fprintf(inv.Stdout, "worker.%d.approval_kind: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorApprovalKind)))
		fmt.Fprintf(inv.Stdout, "worker.%d.approval_preview: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorApprovalPreview)))
		fmt.Fprintf(inv.Stdout, "worker.%d.executor_thread_id: %s\n", idx+1, valueOrUnavailable(worker.ExecutorThreadID))
		fmt.Fprintf(inv.Stdout, "worker.%d.executor_turn_id: %s\n", idx+1, valueOrUnavailable(worker.ExecutorTurnID))
		fmt.Fprintf(inv.Stdout, "worker.%d.executor_turn_status: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorTurnStatus)))
		fmt.Fprintf(inv.Stdout, "worker.%d.executor_interruptible: %t\n", idx+1, workerTurnInterruptibleState(worker))
		fmt.Fprintf(inv.Stdout, "worker.%d.executor_steerable: %t\n", idx+1, workerTurnSteerableState(worker))
		fmt.Fprintf(inv.Stdout, "worker.%d.executor_last_control_action: %s\n", idx+1, valueOrUnavailable(strings.TrimSpace(worker.ExecutorLastControlAction)))
	}

	return nil
}

func runWorkersRemove(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers remove", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	workerID := fs.String("worker-id", "", "Worker id to remove.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workerID) == "" {
		return errors.New("workers remove requires --worker-id")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	worker, found, err := store.GetWorker(ctx, strings.TrimSpace(*workerID))
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(inv.Stdout, "workers_remove_lookup: worker not found")
		return nil
	}

	run, _, err := store.GetRun(ctx, worker.RunID)
	if err != nil {
		return err
	}

	if state.IsWorkerActive(worker.WorkerStatus) {
		message := "active workers cannot be removed until they are idle or completed"
		appendWorkerEvent(journalWriter, "worker.remove.failed", run, &worker, message)
		return errors.New(message)
	}

	manager := workerctl.NewManager(run.RepoPath, inv.Layout.WorkersDir)
	if err := manager.Remove(ctx, worker.WorktreePath); err != nil {
		appendWorkerEvent(journalWriter, "worker.remove.failed", run, &worker, err.Error())
		return err
	}

	if err := store.DeleteWorker(ctx, worker.ID); err != nil {
		appendWorkerEvent(journalWriter, "worker.remove.failed", run, &worker, err.Error())
		return err
	}

	appendWorkerEvent(journalWriter, "worker.removed", run, &worker, "worker registry entry and isolated worktree removed")

	fmt.Fprintln(inv.Stdout, "command: workers remove")
	fmt.Fprintf(inv.Stdout, "worker_id: %s\n", worker.ID)
	fmt.Fprintf(inv.Stdout, "run_id: %s\n", worker.RunID)
	fmt.Fprintf(inv.Stdout, "worker_name: %s\n", worker.WorkerName)
	fmt.Fprintf(inv.Stdout, "worktree_path: %s\n", worker.WorktreePath)
	fmt.Fprintln(inv.Stdout, "worker_removed: true")
	return nil
}

func runWorkersDispatch(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers dispatch", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	workerID := fs.String("worker-id", "", "Worker id to target.")
	prompt := fs.String("prompt", "", "Raw prompt to route to the primary executor in the worker workspace.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workerID) == "" {
		return errors.New("workers dispatch requires --worker-id")
	}
	if strings.TrimSpace(*prompt) == "" {
		return errors.New("workers dispatch requires --prompt")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	worker, found, err := store.GetWorker(ctx, strings.TrimSpace(*workerID))
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(inv.Stdout, "workers_dispatch_lookup: worker not found")
		return nil
	}

	run, found, err := store.GetRun(ctx, worker.RunID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("worker run %s not found", worker.RunID)
	}
	if state.IsWorkerActive(worker.WorkerStatus) {
		return errors.New("worker already has an active executor turn")
	}
	if _, err := os.Stat(worker.WorktreePath); err != nil {
		return fmt.Errorf("worker worktree path is unavailable: %w", err)
	}

	worker.WorkerStatus = state.WorkerStatusExecutorActive
	worker.WorkerTaskSummary = previewString(strings.TrimSpace(*prompt), 240)
	worker.WorkerExecutorPromptSummary = previewString(strings.TrimSpace(*prompt), 240)
	worker.WorkerResultSummary = ""
	worker.WorkerErrorSummary = ""
	worker.ExecutorTurnStatus = string(executor.TurnStatusInProgress)
	worker.ExecutorApprovalState = ""
	worker.ExecutorApprovalKind = ""
	worker.ExecutorApprovalPreview = ""
	worker.ExecutorInterruptible = true
	worker.ExecutorSteerable = true
	worker.ExecutorFailureStage = ""
	worker.ExecutorLastControlAction = ""
	worker.ExecutorApproval = nil
	worker.ExecutorLastControl = nil
	worker.StartedAt = time.Now().UTC()
	worker.CompletedAt = time.Time{}
	worker.UpdatedAt = time.Now().UTC()
	if err := store.SaveWorker(ctx, worker); err != nil {
		return err
	}

	execClient, err := newWorkerExecutorClient(inv.Version)
	if err != nil {
		return err
	}

	executorResult, execErr := execClient.Execute(ctx, executor.TurnRequest{
		RunID:    run.ID,
		RepoPath: worker.WorktreePath,
		Prompt:   strings.TrimSpace(*prompt),
	})

	switch {
	case executorResult.ApprovalState == executor.ApprovalStateRequired || executorResult.TurnStatus == executor.TurnStatusApprovalRequired:
		worker.WorkerStatus = state.WorkerStatusApprovalRequired
		worker.WorkerResultSummary = workerExecutorApprovalMessage(executorResult)
		worker.WorkerErrorSummary = ""
	case execErr != nil || executorResult.TurnStatus == executor.TurnStatusFailed || executorResult.TurnStatus == executor.TurnStatusInterrupted:
		worker.WorkerStatus = state.WorkerStatusFailed
		worker.WorkerResultSummary = workerDispatchResultMessage(executorResult, execErr)
		worker.WorkerErrorSummary = worker.WorkerResultSummary
		worker.CompletedAt = time.Now().UTC()
	default:
		worker.WorkerStatus = state.WorkerStatusCompleted
		worker.WorkerResultSummary = workerDispatchResultMessage(executorResult, nil)
		worker.WorkerErrorSummary = ""
		if executorResult.CompletedAt.IsZero() {
			worker.CompletedAt = time.Now().UTC()
		} else {
			worker.CompletedAt = executorResult.CompletedAt.UTC()
		}
	}
	worker.ExecutorThreadID = strings.TrimSpace(executorResult.ThreadID)
	worker.ExecutorTurnID = strings.TrimSpace(executorResult.TurnID)
	worker.ExecutorTurnStatus = string(executorResult.TurnStatus)
	worker.ExecutorApprovalState = string(executorResult.ApprovalState)
	worker.ExecutorApprovalKind = workerExecutorApprovalKind(executorResult)
	worker.ExecutorApprovalPreview = workerExecutorApprovalPreview(executorResult)
	worker.ExecutorInterruptible = executorResult.Interruptible
	worker.ExecutorSteerable = executorResult.Steerable
	worker.ExecutorFailureStage = workerExecutorFailureStage(executorResult)
	worker.ExecutorApproval = workerExecutorApproval(executorResult)
	if strings.TrimSpace(worker.ExecutorTurnStatus) == "" {
		switch worker.WorkerStatus {
		case state.WorkerStatusCompleted:
			worker.ExecutorTurnStatus = string(executor.TurnStatusCompleted)
		case state.WorkerStatusApprovalRequired:
			worker.ExecutorTurnStatus = string(executor.TurnStatusApprovalRequired)
		default:
			worker.ExecutorTurnStatus = string(executor.TurnStatusFailed)
		}
	}
	worker.UpdatedAt = time.Now().UTC()
	if err := store.SaveWorker(ctx, worker); err != nil {
		return err
	}

	appendWorkerDispatchEvent(journalWriter, run, worker, executorResult, execErr)
	if err := writeWorkerDispatchReport(inv.Stdout, run, worker, executorResult, execErr); err != nil {
		return err
	}
	return execErr
}

func runWorkersApprove(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers approve", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	workerID := fs.String("worker-id", "", "Worker id to approve.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workerID) == "" {
		return errors.New("workers approve requires --worker-id")
	}

	return withControlledWorker(ctx, inv, strings.TrimSpace(*workerID), func(store *state.Store, journalWriter *journal.Journal, run state.Run, worker state.Worker) error {
		if !workerApprovalRequired(worker) || worker.ExecutorApproval == nil {
			fmt.Fprintln(inv.Stdout, "workers_approve: no persisted approval-required state found")
			return nil
		}

		client, err := newExecutorControlClient(inv.Version)
		if err != nil {
			return err
		}
		if err := client.Approve(ctx, executorRequestFromWorker(worker), executorApprovalFromWorker(worker)); err != nil {
			return err
		}

		worker.WorkerStatus = state.WorkerStatusExecutorActive
		worker.ExecutorTurnStatus = string(executor.TurnStatusInProgress)
		worker.ExecutorApprovalState = string(executor.ApprovalStateGranted)
		worker.ExecutorInterruptible = true
		worker.ExecutorSteerable = true
		if worker.ExecutorApproval != nil {
			worker.ExecutorApproval.State = string(executor.ApprovalStateGranted)
		}
		worker.ExecutorLastControlAction = string(executor.ControlActionApprove)
		worker.ExecutorLastControl = &state.ExecutorControl{
			Action: string(executor.ControlActionApprove),
			At:     time.Now().UTC(),
		}
		worker.UpdatedAt = time.Now().UTC()
		if err := store.SaveWorker(ctx, worker); err != nil {
			return err
		}

		if err := refreshWorkerRunStopReason(ctx, store, run); err != nil {
			return err
		}

		latestRun, latestWorker, found, err := reloadWorkerContext(ctx, store, worker.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("worker %s not found after approval", worker.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "worker.approval.granted",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               "worker approval response sent",
			PreviousResponseID:    latestRun.PreviousResponseID,
			WorkerID:              latestWorker.ID,
			WorkerName:            latestWorker.WorkerName,
			WorkerStatus:          string(latestWorker.WorkerStatus),
			WorkerScope:           latestWorker.AssignedScope,
			WorkerPath:            latestWorker.WorktreePath,
			ExecutorThreadID:      latestWorker.ExecutorThreadID,
			ExecutorTurnID:        latestWorker.ExecutorTurnID,
			ExecutorTurnStatus:    latestWorker.ExecutorTurnStatus,
			ExecutorApprovalState: latestWorker.ExecutorApprovalState,
			ExecutorApprovalKind:  latestWorker.ExecutorApprovalKind,
			ExecutorControlAction: string(executor.ControlActionApprove),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeWorkerControlReport(inv.Stdout, "workers approve", latestRun, latestWorker, string(executor.ControlActionApprove), "")
	})
}

func runWorkersDeny(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers deny", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	workerID := fs.String("worker-id", "", "Worker id to deny.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workerID) == "" {
		return errors.New("workers deny requires --worker-id")
	}

	return withControlledWorker(ctx, inv, strings.TrimSpace(*workerID), func(store *state.Store, journalWriter *journal.Journal, run state.Run, worker state.Worker) error {
		if !workerApprovalRequired(worker) || worker.ExecutorApproval == nil {
			fmt.Fprintln(inv.Stdout, "workers_deny: no persisted approval-required state found")
			return nil
		}

		client, err := newExecutorControlClient(inv.Version)
		if err != nil {
			return err
		}
		if err := client.Deny(ctx, executorRequestFromWorker(worker), executorApprovalFromWorker(worker)); err != nil {
			return err
		}

		worker.WorkerStatus = state.WorkerStatusExecutorActive
		worker.ExecutorTurnStatus = string(executor.TurnStatusInProgress)
		worker.ExecutorApprovalState = string(executor.ApprovalStateDenied)
		worker.ExecutorInterruptible = true
		worker.ExecutorSteerable = true
		if worker.ExecutorApproval != nil {
			worker.ExecutorApproval.State = string(executor.ApprovalStateDenied)
		}
		worker.ExecutorLastControlAction = string(executor.ControlActionDeny)
		worker.ExecutorLastControl = &state.ExecutorControl{
			Action: string(executor.ControlActionDeny),
			At:     time.Now().UTC(),
		}
		worker.UpdatedAt = time.Now().UTC()
		if err := store.SaveWorker(ctx, worker); err != nil {
			return err
		}

		if err := refreshWorkerRunStopReason(ctx, store, run); err != nil {
			return err
		}

		latestRun, latestWorker, found, err := reloadWorkerContext(ctx, store, worker.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("worker %s not found after denial", worker.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "worker.approval.denied",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               "worker denial response sent",
			PreviousResponseID:    latestRun.PreviousResponseID,
			WorkerID:              latestWorker.ID,
			WorkerName:            latestWorker.WorkerName,
			WorkerStatus:          string(latestWorker.WorkerStatus),
			WorkerScope:           latestWorker.AssignedScope,
			WorkerPath:            latestWorker.WorktreePath,
			ExecutorThreadID:      latestWorker.ExecutorThreadID,
			ExecutorTurnID:        latestWorker.ExecutorTurnID,
			ExecutorTurnStatus:    latestWorker.ExecutorTurnStatus,
			ExecutorApprovalState: latestWorker.ExecutorApprovalState,
			ExecutorApprovalKind:  latestWorker.ExecutorApprovalKind,
			ExecutorControlAction: string(executor.ControlActionDeny),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeWorkerControlReport(inv.Stdout, "workers deny", latestRun, latestWorker, string(executor.ControlActionDeny), "")
	})
}

func runWorkersInterrupt(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers interrupt", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	workerID := fs.String("worker-id", "", "Worker id to interrupt.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workerID) == "" {
		return errors.New("workers interrupt requires --worker-id")
	}

	return withControlledWorker(ctx, inv, strings.TrimSpace(*workerID), func(store *state.Store, journalWriter *journal.Journal, run state.Run, worker state.Worker) error {
		if !workerTurnInterruptibleState(worker) {
			fmt.Fprintln(inv.Stdout, "workers_interrupt: worker executor turn is not currently interruptible")
			return nil
		}

		client, err := newExecutorControlClient(inv.Version)
		if err != nil {
			return err
		}
		if err := client.InterruptTurn(ctx, executorRequestFromWorker(worker)); err != nil {
			return err
		}

		worker.ExecutorLastControlAction = string(executor.ControlActionInterrupt)
		worker.ExecutorLastControl = &state.ExecutorControl{
			Action: string(executor.ControlActionInterrupt),
			At:     time.Now().UTC(),
		}
		worker.UpdatedAt = time.Now().UTC()
		if err := store.SaveWorker(ctx, worker); err != nil {
			return err
		}

		latestRun, latestWorker, found, err := reloadWorkerContext(ctx, store, worker.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("worker %s not found after interrupt request", worker.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "worker.interrupt.requested",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               "worker interrupt request sent",
			PreviousResponseID:    latestRun.PreviousResponseID,
			WorkerID:              latestWorker.ID,
			WorkerName:            latestWorker.WorkerName,
			WorkerStatus:          string(latestWorker.WorkerStatus),
			WorkerScope:           latestWorker.AssignedScope,
			WorkerPath:            latestWorker.WorktreePath,
			ExecutorThreadID:      latestWorker.ExecutorThreadID,
			ExecutorTurnID:        latestWorker.ExecutorTurnID,
			ExecutorTurnStatus:    latestWorker.ExecutorTurnStatus,
			ExecutorApprovalState: latestWorker.ExecutorApprovalState,
			ExecutorApprovalKind:  latestWorker.ExecutorApprovalKind,
			ExecutorControlAction: string(executor.ControlActionInterrupt),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeWorkerControlReport(inv.Stdout, "workers interrupt", latestRun, latestWorker, string(executor.ControlActionInterrupt), "")
	})
}

func runWorkersKill(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers kill", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	workerID := fs.String("worker-id", "", "Worker id to force-stop.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*workerID) == "" {
		return errors.New("workers kill requires --worker-id")
	}

	return withControlledWorker(ctx, inv, strings.TrimSpace(*workerID), func(store *state.Store, journalWriter *journal.Journal, run state.Run, worker state.Worker) error {
		if strings.TrimSpace(worker.ExecutorTurnID) == "" {
			fmt.Fprintln(inv.Stdout, "workers_kill: no active worker executor turn found")
			return nil
		}

		message := "force kill is unsupported for the codex app-server primary executor transport"
		worker.ExecutorLastControlAction = string(executor.ControlActionKill)
		worker.ExecutorLastControl = &state.ExecutorControl{
			Action: string(executor.ControlActionKill),
			At:     time.Now().UTC(),
		}
		worker.UpdatedAt = time.Now().UTC()
		if err := store.SaveWorker(ctx, worker); err != nil {
			return err
		}

		latestRun, latestWorker, found, err := reloadWorkerContext(ctx, store, worker.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("worker %s not found after kill request", worker.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "worker.kill.requested",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               message,
			PreviousResponseID:    latestRun.PreviousResponseID,
			WorkerID:              latestWorker.ID,
			WorkerName:            latestWorker.WorkerName,
			WorkerStatus:          string(latestWorker.WorkerStatus),
			WorkerScope:           latestWorker.AssignedScope,
			WorkerPath:            latestWorker.WorktreePath,
			ExecutorThreadID:      latestWorker.ExecutorThreadID,
			ExecutorTurnID:        latestWorker.ExecutorTurnID,
			ExecutorTurnStatus:    latestWorker.ExecutorTurnStatus,
			ExecutorControlAction: string(executor.ControlActionKill),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeWorkerControlReport(inv.Stdout, "workers kill", latestRun, latestWorker, string(executor.ControlActionKill), message)
	})
}

func runWorkersSteer(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers steer", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	workerID := fs.String("worker-id", "", "Worker id to steer.")
	message := fs.String("message", "", "Raw note to send to the worker executor turn.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	note := strings.TrimSpace(*message)
	if note == "" {
		note = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*workerID) == "" {
		return errors.New("workers steer requires --worker-id")
	}
	if note == "" {
		return errors.New("workers steer requires --message")
	}

	return withControlledWorker(ctx, inv, strings.TrimSpace(*workerID), func(store *state.Store, journalWriter *journal.Journal, run state.Run, worker state.Worker) error {
		if !workerTurnSteerableState(worker) {
			message := "worker executor turn is not currently steerable"
			_ = journalWriter.Append(journal.Event{
				Type:                  "worker.steer.failed",
				RunID:                 run.ID,
				RepoPath:              run.RepoPath,
				Goal:                  run.Goal,
				Status:                string(run.Status),
				Message:               message,
				PreviousResponseID:    run.PreviousResponseID,
				WorkerID:              worker.ID,
				WorkerName:            worker.WorkerName,
				WorkerStatus:          string(worker.WorkerStatus),
				WorkerScope:           worker.AssignedScope,
				WorkerPath:            worker.WorktreePath,
				ExecutorThreadID:      worker.ExecutorThreadID,
				ExecutorTurnID:        worker.ExecutorTurnID,
				ExecutorTurnStatus:    worker.ExecutorTurnStatus,
				ExecutorApprovalState: worker.ExecutorApprovalState,
				ExecutorApprovalKind:  worker.ExecutorApprovalKind,
				ExecutorControlAction: string(executor.ControlActionSteer),
				Checkpoint:            journalCheckpointRef(run.LatestCheckpoint),
			})
			fmt.Fprintf(inv.Stdout, "workers_steer: %s\n", message)
			return nil
		}

		client, err := newExecutorControlClient(inv.Version)
		if err != nil {
			return err
		}
		if err := client.SteerTurn(ctx, executorRequestFromWorker(worker), note); err != nil {
			_ = journalWriter.Append(journal.Event{
				Type:                  "worker.steer.failed",
				RunID:                 run.ID,
				RepoPath:              run.RepoPath,
				Goal:                  run.Goal,
				Status:                string(run.Status),
				Message:               err.Error(),
				PreviousResponseID:    run.PreviousResponseID,
				WorkerID:              worker.ID,
				WorkerName:            worker.WorkerName,
				WorkerStatus:          string(worker.WorkerStatus),
				WorkerScope:           worker.AssignedScope,
				WorkerPath:            worker.WorktreePath,
				ExecutorThreadID:      worker.ExecutorThreadID,
				ExecutorTurnID:        worker.ExecutorTurnID,
				ExecutorTurnStatus:    worker.ExecutorTurnStatus,
				ExecutorApprovalState: worker.ExecutorApprovalState,
				ExecutorApprovalKind:  worker.ExecutorApprovalKind,
				ExecutorControlAction: string(executor.ControlActionSteer),
				Checkpoint:            journalCheckpointRef(run.LatestCheckpoint),
			})
			return err
		}

		worker.ExecutorLastControlAction = string(executor.ControlActionSteer)
		worker.ExecutorLastControl = &state.ExecutorControl{
			Action:  string(executor.ControlActionSteer),
			Payload: note,
			At:      time.Now().UTC(),
		}
		worker.UpdatedAt = time.Now().UTC()
		if err := store.SaveWorker(ctx, worker); err != nil {
			return err
		}

		latestRun, latestWorker, found, err := reloadWorkerContext(ctx, store, worker.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("worker %s not found after steer", worker.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "worker.steer.sent",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               "raw steer note sent to the active worker executor turn",
			PreviousResponseID:    latestRun.PreviousResponseID,
			WorkerID:              latestWorker.ID,
			WorkerName:            latestWorker.WorkerName,
			WorkerStatus:          string(latestWorker.WorkerStatus),
			WorkerScope:           latestWorker.AssignedScope,
			WorkerPath:            latestWorker.WorktreePath,
			ExecutorThreadID:      latestWorker.ExecutorThreadID,
			ExecutorTurnID:        latestWorker.ExecutorTurnID,
			ExecutorTurnStatus:    latestWorker.ExecutorTurnStatus,
			ExecutorApprovalState: latestWorker.ExecutorApprovalState,
			ExecutorApprovalKind:  latestWorker.ExecutorApprovalKind,
			ExecutorControlAction: string(executor.ControlActionSteer),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeWorkerControlReport(inv.Stdout, "workers steer", latestRun, latestWorker, string(executor.ControlActionSteer), "")
	})
}

func runWorkersIntegrate(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("workers integrate", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	workerIDsArg := fs.String("worker-ids", "", "Comma-separated worker ids to preview together.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	workerIDs := csvList(*workerIDsArg)
	if len(workerIDs) == 0 {
		return errors.New("workers integrate requires --worker-ids")
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	selected := make([]state.Worker, 0, len(workerIDs))
	var run state.Run
	for idx, workerID := range workerIDs {
		worker, found, err := store.GetWorker(ctx, workerID)
		if err != nil {
			return err
		}
		if !found {
			fmt.Fprintf(inv.Stdout, "workers_integrate_lookup: worker not found: %s\n", workerID)
			return nil
		}
		loadedRun, found, err := store.GetRun(ctx, worker.RunID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("worker run %s not found", worker.RunID)
		}
		if idx == 0 {
			run = loadedRun
		} else if loadedRun.ID != run.ID {
			return errors.New("workers integrate requires workers from the same run")
		}
		selected = append(selected, worker)
	}

	if journalWriter != nil {
		_ = journalWriter.Append(journal.Event{
			Type:     "integration.started",
			RunID:    run.ID,
			RepoPath: run.RepoPath,
			Goal:     run.Goal,
			Status:   string(run.Status),
			Message:  fmt.Sprintf("building integration preview from %d worker(s)", len(selected)),
		})
	}

	summary, err := workerctl.BuildIntegrationSummary(run.RepoPath, selected)
	if err != nil {
		if journalWriter != nil {
			_ = journalWriter.Append(journal.Event{
				Type:     "integration.failed",
				RunID:    run.ID,
				RepoPath: run.RepoPath,
				Goal:     run.Goal,
				Status:   string(run.Status),
				Message:  err.Error(),
			})
		}
		return err
	}

	artifactPath, artifactPreview := orchestration.WriteIntegrationArtifact(run, summary)
	if strings.TrimSpace(artifactPath) == "" {
		message := valueOrUnavailable(strings.TrimSpace(artifactPreview))
		if journalWriter != nil {
			_ = journalWriter.Append(journal.Event{
				Type:     "integration.failed",
				RunID:    run.ID,
				RepoPath: run.RepoPath,
				Goal:     run.Goal,
				Status:   string(run.Status),
				Message:  message,
			})
		}
		return errors.New(message)
	}

	if journalWriter != nil {
		_ = journalWriter.Append(journal.Event{
			Type:            "integration.completed",
			RunID:           run.ID,
			RepoPath:        run.RepoPath,
			Goal:            run.Goal,
			Status:          string(run.Status),
			Message:         summary.IntegrationPreview,
			ArtifactPath:    artifactPath,
			ArtifactPreview: artifactPreview,
		})
	}

	fmt.Fprintln(inv.Stdout, "command: workers integrate")
	fmt.Fprintf(inv.Stdout, "run_id: %s\n", run.ID)
	fmt.Fprintf(inv.Stdout, "worker_count: %d\n", len(selected))
	fmt.Fprintf(inv.Stdout, "integration_artifact_path: %s\n", artifactPath)
	fmt.Fprintf(inv.Stdout, "integration_preview: %s\n", summary.IntegrationPreview)
	return nil
}

func resolveWorkerRun(ctx context.Context, store *state.Store, runID string) (state.Run, bool, error) {
	if strings.TrimSpace(runID) != "" {
		run, found, err := store.GetRun(ctx, strings.TrimSpace(runID))
		if err != nil || !found {
			return run, found, err
		}
		if !isRunResumable(run) {
			return state.Run{}, false, nil
		}
		return run, true, nil
	}
	return store.LatestResumableRun(ctx)
}

func appendWorkerEvent(journalWriter *journal.Journal, eventType string, run state.Run, worker *state.Worker, message string) {
	if journalWriter == nil {
		return
	}

	event := journal.Event{
		Type:     eventType,
		RunID:    run.ID,
		RepoPath: run.RepoPath,
		Goal:     run.Goal,
		Status:   string(run.Status),
		Message:  strings.TrimSpace(message),
	}
	if worker != nil {
		event.WorkerID = worker.ID
		event.WorkerName = worker.WorkerName
		event.WorkerStatus = string(worker.WorkerStatus)
		event.WorkerScope = worker.AssignedScope
		event.WorkerPath = worker.WorktreePath
	}
	_ = journalWriter.Append(event)
}

func appendWorkerDispatchEvent(
	journalWriter *journal.Journal,
	run state.Run,
	worker state.Worker,
	result executor.TurnResult,
	execErr error,
) {
	if journalWriter == nil {
		return
	}

	message := "executor dispatch completed in worker workspace"
	if execErr != nil {
		message = execErr.Error()
	} else if result.TurnStatus == executor.TurnStatusApprovalRequired {
		message = "executor dispatch is waiting for approval in worker workspace"
	}

	_ = journalWriter.Append(journal.Event{
		Type:                  "worker.executor.dispatched",
		RunID:                 run.ID,
		RepoPath:              run.RepoPath,
		Goal:                  run.Goal,
		Status:                string(run.Status),
		Message:               message,
		WorkerID:              worker.ID,
		WorkerName:            worker.WorkerName,
		WorkerStatus:          string(worker.WorkerStatus),
		WorkerScope:           worker.AssignedScope,
		WorkerPath:            worker.WorktreePath,
		ExecutorTransport:     string(result.Transport),
		ExecutorThreadID:      result.ThreadID,
		ExecutorThreadPath:    result.ThreadPath,
		ExecutorTurnID:        result.TurnID,
		ExecutorTurnStatus:    string(result.TurnStatus),
		ExecutorApprovalState: string(result.ApprovalState),
		ExecutorApprovalKind:  workerExecutorApprovalKind(result),
		ExecutorFailureStage:  workerExecutorFailureStage(result),
		ExecutorControlAction: strings.TrimSpace(worker.ExecutorLastControlAction),
		ExecutorOutputPreview: previewString(result.FinalMessage, 240),
	})
	if workerApprovalRequired(worker) {
		_ = journalWriter.Append(journal.Event{
			Type:                  "worker.approval.required",
			RunID:                 run.ID,
			RepoPath:              run.RepoPath,
			Goal:                  run.Goal,
			Status:                string(run.Status),
			Message:               firstNonEmptyValue(strings.TrimSpace(worker.ExecutorApprovalPreview), "worker executor turn requires approval before it can continue"),
			WorkerID:              worker.ID,
			WorkerName:            worker.WorkerName,
			WorkerStatus:          string(worker.WorkerStatus),
			WorkerScope:           worker.AssignedScope,
			WorkerPath:            worker.WorktreePath,
			ExecutorThreadID:      worker.ExecutorThreadID,
			ExecutorTurnID:        worker.ExecutorTurnID,
			ExecutorTurnStatus:    worker.ExecutorTurnStatus,
			ExecutorApprovalState: worker.ExecutorApprovalState,
			ExecutorApprovalKind:  worker.ExecutorApprovalKind,
			StopReason:            orchestration.StopReasonExecutorApprovalReq,
		})
	}
}

func writeWorkerReport(stdout io.Writer, command string, run state.Run, worker state.Worker, message string) error {
	fmt.Fprintf(stdout, "command: %s\n", command)
	fmt.Fprintf(stdout, "run_id: %s\n", run.ID)
	fmt.Fprintf(stdout, "worker_id: %s\n", worker.ID)
	fmt.Fprintf(stdout, "worker_name: %s\n", worker.WorkerName)
	fmt.Fprintf(stdout, "worker_status: %s\n", worker.WorkerStatus)
	fmt.Fprintf(stdout, "assigned_scope: %s\n", worker.AssignedScope)
	fmt.Fprintf(stdout, "worktree_path: %s\n", worker.WorktreePath)
	fmt.Fprintf(stdout, "approval_required: %t\n", workerApprovalRequired(worker))
	fmt.Fprintf(stdout, "executor_approval_state: %s\n", valueOrUnavailable(strings.TrimSpace(worker.ExecutorApprovalState)))
	fmt.Fprintf(stdout, "executor_approval_kind: %s\n", valueOrUnavailable(strings.TrimSpace(worker.ExecutorApprovalKind)))
	fmt.Fprintf(stdout, "executor_approval_preview: %s\n", valueOrUnavailable(strings.TrimSpace(worker.ExecutorApprovalPreview)))
	fmt.Fprintf(stdout, "executor_thread_id: %s\n", valueOrUnavailable(worker.ExecutorThreadID))
	fmt.Fprintf(stdout, "executor_turn_id: %s\n", valueOrUnavailable(worker.ExecutorTurnID))
	fmt.Fprintf(stdout, "executor_turn_status: %s\n", valueOrUnavailable(strings.TrimSpace(worker.ExecutorTurnStatus)))
	fmt.Fprintf(stdout, "executor_interruptible: %t\n", workerTurnInterruptibleState(worker))
	fmt.Fprintf(stdout, "executor_steerable: %t\n", workerTurnSteerableState(worker))
	fmt.Fprintf(stdout, "executor_last_control_action: %s\n", valueOrUnavailable(strings.TrimSpace(worker.ExecutorLastControlAction)))
	if strings.TrimSpace(message) != "" {
		fmt.Fprintf(stdout, "message: %s\n", message)
	}
	return nil
}

func writeWorkerDispatchReport(
	stdout io.Writer,
	run state.Run,
	worker state.Worker,
	result executor.TurnResult,
	execErr error,
) error {
	if err := writeWorkerReport(stdout, "workers dispatch", run, worker, ""); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "executor_transport: %s\n", valueOrUnavailable(string(result.Transport)))
	fmt.Fprintf(stdout, "executor_turn_status: %s\n", valueOrUnavailable(string(result.TurnStatus)))
	fmt.Fprintf(stdout, "executor_approval_state: %s\n", valueOrUnavailable(string(result.ApprovalState)))
	fmt.Fprintf(stdout, "executor_interruptible: %t\n", result.Interruptible)
	fmt.Fprintf(stdout, "executor_steerable: %t\n", result.Steerable)
	fmt.Fprintf(stdout, "executor_message_preview: %s\n", valueOrUnavailable(previewString(result.FinalMessage, 240)))
	if execErr != nil {
		fmt.Fprintf(stdout, "error: %s\n", execErr)
	}
	return nil
}

func writeWorkerControlReport(stdout io.Writer, command string, run state.Run, worker state.Worker, action string, message string) error {
	if err := writeWorkerReport(stdout, command, run, worker, message); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "executor_control_action: %s\n", valueOrUnavailable(strings.TrimSpace(action)))
	fmt.Fprintf(stdout, "next_operator_action: %s\n", nextOperatorActionForExistingRun(run))
	return nil
}

func derefRun(run *state.Run) state.Run {
	if run == nil {
		return state.Run{}
	}
	return *run
}

func workerExecutorApprovalKind(result executor.TurnResult) string {
	if result.Approval == nil {
		return ""
	}
	return string(result.Approval.Kind)
}

func workerExecutorApproval(result executor.TurnResult) *state.ExecutorApproval {
	if result.Approval == nil && result.ApprovalState == executor.ApprovalStateNone {
		return nil
	}

	approval := &state.ExecutorApproval{
		State: string(result.ApprovalState),
	}
	if result.Approval == nil {
		return approval
	}
	approval.Kind = string(result.Approval.Kind)
	approval.RequestID = result.Approval.RequestID
	approval.ApprovalID = result.Approval.ApprovalID
	approval.ItemID = result.Approval.ItemID
	approval.Reason = result.Approval.Reason
	approval.Command = result.Approval.Command
	approval.CWD = result.Approval.CWD
	approval.GrantRoot = result.Approval.GrantRoot
	approval.RawParams = result.Approval.RawParams
	return approval
}

func workerExecutorApprovalPreview(result executor.TurnResult) string {
	if result.Approval == nil && result.ApprovalState != executor.ApprovalStateRequired {
		return ""
	}
	return previewString(workerExecutorApprovalMessage(result), 240)
}

func workerExecutorFailureStage(result executor.TurnResult) string {
	if result.Error == nil {
		return ""
	}
	return strings.TrimSpace(result.Error.Stage)
}

func workerExecutorApprovalMessage(result executor.TurnResult) string {
	if result.Approval == nil {
		return "worker executor turn requires approval before it can continue"
	}
	switch result.Approval.Kind {
	case executor.ApprovalKindCommandExecution:
		if strings.TrimSpace(result.Approval.Command) != "" {
			return "worker approval required for command: " + previewString(result.Approval.Command, 160)
		}
	case executor.ApprovalKindFileChange:
		if strings.TrimSpace(result.Approval.GrantRoot) != "" {
			return "worker approval required for file changes under: " + strings.TrimSpace(result.Approval.GrantRoot)
		}
	case executor.ApprovalKindPermissions:
		if strings.TrimSpace(result.Approval.Reason) != "" {
			return "worker approval required for permissions: " + previewString(result.Approval.Reason, 160)
		}
	}
	if strings.TrimSpace(result.Approval.Reason) != "" {
		return "worker approval required: " + previewString(result.Approval.Reason, 160)
	}
	return "worker executor turn requires approval before it can continue"
}

func workerDispatchResultMessage(result executor.TurnResult, execErr error) string {
	if preview := previewString(result.FinalMessage, 240); preview != "" {
		return preview
	}
	if result.Error != nil {
		return previewString(result.Error.Message, 240)
	}
	if execErr != nil {
		return previewString(execErr.Error(), 240)
	}
	return ""
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func workerApprovalRequired(worker state.Worker) bool {
	return strings.TrimSpace(worker.ExecutorApprovalState) == string(executor.ApprovalStateRequired) ||
		worker.WorkerStatus == state.WorkerStatusApprovalRequired
}

func workerTurnInterruptibleState(worker state.Worker) bool {
	if strings.TrimSpace(worker.ExecutorTurnID) == "" {
		return false
	}
	if worker.ExecutorInterruptible {
		return true
	}
	switch strings.TrimSpace(worker.ExecutorTurnStatus) {
	case string(executor.TurnStatusInProgress), string(executor.TurnStatusApprovalRequired):
		return true
	default:
		return false
	}
}

func workerTurnSteerableState(worker state.Worker) bool {
	if strings.TrimSpace(worker.ExecutorTurnID) == "" {
		return false
	}
	if workerApprovalRequired(worker) {
		return false
	}
	if worker.ExecutorSteerable {
		return true
	}
	return strings.TrimSpace(worker.ExecutorTurnStatus) == string(executor.TurnStatusInProgress)
}

func executorRequestFromWorker(worker state.Worker) executor.TurnRequest {
	return executor.TurnRequest{
		RunID:    worker.RunID,
		RepoPath: worker.WorktreePath,
		ThreadID: worker.ExecutorThreadID,
		TurnID:   worker.ExecutorTurnID,
		Continue: true,
	}
}

func executorApprovalFromWorker(worker state.Worker) executor.ApprovalRequest {
	if worker.ExecutorApproval == nil {
		return executor.ApprovalRequest{}
	}
	return executor.ApprovalRequest{
		RequestID:  worker.ExecutorApproval.RequestID,
		ApprovalID: worker.ExecutorApproval.ApprovalID,
		ItemID:     worker.ExecutorApproval.ItemID,
		State:      executor.ApprovalState(worker.ExecutorApproval.State),
		Kind:       executor.ApprovalKind(worker.ExecutorApproval.Kind),
		Reason:     worker.ExecutorApproval.Reason,
		Command:    worker.ExecutorApproval.Command,
		CWD:        worker.ExecutorApproval.CWD,
		GrantRoot:  worker.ExecutorApproval.GrantRoot,
		RawParams:  worker.ExecutorApproval.RawParams,
	}
}

func withControlledWorker(
	ctx context.Context,
	inv Invocation,
	workerID string,
	fn func(*state.Store, *journal.Journal, state.Run, state.Worker) error,
) error {
	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	worker, found, err := store.GetWorker(ctx, strings.TrimSpace(workerID))
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(inv.Stdout, "workers_control_lookup: worker not found")
		return nil
	}

	run, found, err := store.GetRun(ctx, worker.RunID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("worker run %s not found", worker.RunID)
	}

	return fn(store, journalWriter, run, worker)
}

func refreshWorkerRunStopReason(ctx context.Context, store *state.Store, run state.Run) error {
	workers, err := store.ListWorkers(ctx, run.ID)
	if err != nil {
		return err
	}
	for _, worker := range workers {
		if workerApprovalRequired(worker) {
			return store.SaveLatestStopReason(ctx, run.ID, orchestration.StopReasonExecutorApprovalReq)
		}
	}
	if run.ExecutorApproval != nil && strings.TrimSpace(run.ExecutorApproval.State) == string(executor.ApprovalStateRequired) {
		return store.SaveLatestStopReason(ctx, run.ID, orchestration.StopReasonExecutorApprovalReq)
	}
	return store.ClearLatestStopReason(ctx, run.ID)
}

func reloadWorkerContext(ctx context.Context, store *state.Store, workerID string) (state.Run, state.Worker, bool, error) {
	worker, found, err := store.GetWorker(ctx, workerID)
	if err != nil || !found {
		return state.Run{}, worker, found, err
	}
	run, foundRun, err := store.GetRun(ctx, worker.RunID)
	if err != nil {
		return state.Run{}, state.Worker{}, false, err
	}
	if !foundRun {
		return state.Run{}, state.Worker{}, false, nil
	}
	return run, worker, true, nil
}

func csvList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
