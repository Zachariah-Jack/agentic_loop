package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

type executorControlClient interface {
	Approve(context.Context, executor.TurnRequest, executor.ApprovalRequest) error
	Deny(context.Context, executor.TurnRequest, executor.ApprovalRequest) error
	InterruptTurn(context.Context, executor.TurnRequest) error
	SteerTurn(context.Context, executor.TurnRequest, string) error
}

var newExecutorControlClient = func(version string) (executorControlClient, error) {
	client, err := appserver.NewClient(version)
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func newExecutorCommand() Command {
	return Command{
		Name:    "executor",
		Summary: "Control the active primary executor turn mechanically.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator executor approve",
			"  orchestrator executor deny",
			"  orchestrator executor interrupt",
			"  orchestrator executor kill",
			"  orchestrator executor steer TEXT",
			"",
			"Loads the latest unfinished run and targets only the persisted active",
			"primary executor turn. These commands stay mechanical and do not add",
			"planner judgment.",
		),
		Run: runExecutor,
	}
}

func runExecutor(ctx context.Context, inv Invocation) error {
	if len(inv.Args) == 0 {
		fmt.Fprintln(inv.Stdout, newExecutorCommand().Description)
		return nil
	}

	switch inv.Args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(inv.Stdout, newExecutorCommand().Description)
		return nil
	case "approve":
		return runExecutorApprove(ctx, inv, inv.Args[1:])
	case "deny":
		return runExecutorDeny(ctx, inv, inv.Args[1:])
	case "interrupt":
		return runExecutorInterrupt(ctx, inv, inv.Args[1:])
	case "kill":
		return runExecutorKill(ctx, inv, inv.Args[1:])
	case "steer":
		return runExecutorSteer(ctx, inv, inv.Args[1:])
	default:
		return fmt.Errorf("executor requires subcommand approve, deny, interrupt, kill, or steer")
	}
}

func runExecutorApprove(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("executor approve", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	return withActiveExecutorRun(ctx, inv, "executor.approval.granted", func(store *state.Store, journalWriter *journal.Journal, run state.Run) error {
		if run.ExecutorApproval == nil || strings.TrimSpace(run.ExecutorApproval.State) != string(executor.ApprovalStateRequired) {
			fmt.Fprintln(inv.Stdout, "executor_approve: no persisted approval-required state found")
			return nil
		}

		client, err := newExecutorControlClient(inv.Version)
		if err != nil {
			return err
		}
		if err := client.Approve(ctx, executorRequestFromRun(run), executorApprovalFromRun(run)); err != nil {
			return err
		}

		updatedState := executorStateFromRun(run)
		updatedState.TurnStatus = string(executor.TurnStatusInProgress)
		updatedState.LastError = ""
		updatedState.LastFailureStage = ""
		updatedState.Approval = &state.ExecutorApproval{
			State:      string(executor.ApprovalStateGranted),
			Kind:       run.ExecutorApproval.Kind,
			RequestID:  run.ExecutorApproval.RequestID,
			ApprovalID: run.ExecutorApproval.ApprovalID,
			ItemID:     run.ExecutorApproval.ItemID,
			Reason:     run.ExecutorApproval.Reason,
			Command:    run.ExecutorApproval.Command,
			CWD:        run.ExecutorApproval.CWD,
			GrantRoot:  run.ExecutorApproval.GrantRoot,
			RawParams:  run.ExecutorApproval.RawParams,
		}
		updatedState.LastControl = &state.ExecutorControl{
			Action: string(executor.ControlActionApprove),
			At:     time.Now().UTC(),
		}
		if err := store.SaveExecutorState(ctx, run.ID, updatedState); err != nil {
			return err
		}

		latestRun, found, err := store.GetRun(ctx, run.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("run %s not found after approval", run.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "executor.approval.granted",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               "executor approval response sent",
			PreviousResponseID:    latestRun.PreviousResponseID,
			ExecutorTransport:     latestRun.ExecutorTransport,
			ExecutorThreadID:      latestRun.ExecutorThreadID,
			ExecutorThreadPath:    latestRun.ExecutorThreadPath,
			ExecutorTurnID:        latestRun.ExecutorTurnID,
			ExecutorTurnStatus:    latestRun.ExecutorTurnStatus,
			ExecutorApprovalState: executorApprovalStateValue(latestRun),
			ExecutorApprovalKind:  executorApprovalKindValue(latestRun),
			ExecutorControlAction: string(executor.ControlActionApprove),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeExecutorControlReport(inv, "executor approve", latestRun, string(executor.ControlActionApprove), "")
	})
}

func runExecutorDeny(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("executor deny", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	return withActiveExecutorRun(ctx, inv, "executor.approval.denied", func(store *state.Store, journalWriter *journal.Journal, run state.Run) error {
		if run.ExecutorApproval == nil || strings.TrimSpace(run.ExecutorApproval.State) != string(executor.ApprovalStateRequired) {
			fmt.Fprintln(inv.Stdout, "executor_deny: no persisted approval-required state found")
			return nil
		}

		client, err := newExecutorControlClient(inv.Version)
		if err != nil {
			return err
		}
		if err := client.Deny(ctx, executorRequestFromRun(run), executorApprovalFromRun(run)); err != nil {
			return err
		}

		updatedState := executorStateFromRun(run)
		updatedState.TurnStatus = string(executor.TurnStatusInProgress)
		updatedState.LastError = ""
		updatedState.LastFailureStage = ""
		updatedState.Approval = &state.ExecutorApproval{
			State:      string(executor.ApprovalStateDenied),
			Kind:       run.ExecutorApproval.Kind,
			RequestID:  run.ExecutorApproval.RequestID,
			ApprovalID: run.ExecutorApproval.ApprovalID,
			ItemID:     run.ExecutorApproval.ItemID,
			Reason:     run.ExecutorApproval.Reason,
			Command:    run.ExecutorApproval.Command,
			CWD:        run.ExecutorApproval.CWD,
			GrantRoot:  run.ExecutorApproval.GrantRoot,
			RawParams:  run.ExecutorApproval.RawParams,
		}
		updatedState.LastControl = &state.ExecutorControl{
			Action: string(executor.ControlActionDeny),
			At:     time.Now().UTC(),
		}
		if err := store.SaveExecutorState(ctx, run.ID, updatedState); err != nil {
			return err
		}

		latestRun, found, err := store.GetRun(ctx, run.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("run %s not found after denial", run.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "executor.approval.denied",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               "executor denial response sent",
			PreviousResponseID:    latestRun.PreviousResponseID,
			ExecutorTransport:     latestRun.ExecutorTransport,
			ExecutorThreadID:      latestRun.ExecutorThreadID,
			ExecutorThreadPath:    latestRun.ExecutorThreadPath,
			ExecutorTurnID:        latestRun.ExecutorTurnID,
			ExecutorTurnStatus:    latestRun.ExecutorTurnStatus,
			ExecutorApprovalState: executorApprovalStateValue(latestRun),
			ExecutorApprovalKind:  executorApprovalKindValue(latestRun),
			ExecutorControlAction: string(executor.ControlActionDeny),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeExecutorControlReport(inv, "executor deny", latestRun, string(executor.ControlActionDeny), "")
	})
}

func runExecutorInterrupt(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("executor interrupt", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	return withActiveExecutorRun(ctx, inv, "executor.interrupt.requested", func(store *state.Store, journalWriter *journal.Journal, run state.Run) error {
		client, err := newExecutorControlClient(inv.Version)
		if err != nil {
			return err
		}
		if err := client.InterruptTurn(ctx, executorRequestFromRun(run)); err != nil {
			return err
		}

		updatedState := executorStateFromRun(run)
		updatedState.LastControl = &state.ExecutorControl{
			Action: string(executor.ControlActionInterrupt),
			At:     time.Now().UTC(),
		}
		if err := store.SaveExecutorState(ctx, run.ID, updatedState); err != nil {
			return err
		}

		latestRun, found, err := store.GetRun(ctx, run.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("run %s not found after interrupt", run.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "executor.interrupt.requested",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               "executor interrupt request sent",
			PreviousResponseID:    latestRun.PreviousResponseID,
			ExecutorTransport:     latestRun.ExecutorTransport,
			ExecutorThreadID:      latestRun.ExecutorThreadID,
			ExecutorThreadPath:    latestRun.ExecutorThreadPath,
			ExecutorTurnID:        latestRun.ExecutorTurnID,
			ExecutorTurnStatus:    latestRun.ExecutorTurnStatus,
			ExecutorFailureStage:  latestRun.ExecutorLastFailureStage,
			ExecutorControlAction: string(executor.ControlActionInterrupt),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeExecutorControlReport(inv, "executor interrupt", latestRun, string(executor.ControlActionInterrupt), "")
	})
}

func runExecutorKill(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("executor kill", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	return withActiveExecutorRun(ctx, inv, "executor.kill.requested", func(store *state.Store, journalWriter *journal.Journal, run state.Run) error {
		updatedState := executorStateFromRun(run)
		updatedState.LastControl = &state.ExecutorControl{
			Action: string(executor.ControlActionKill),
			At:     time.Now().UTC(),
		}
		if err := store.SaveExecutorState(ctx, run.ID, updatedState); err != nil {
			return err
		}

		latestRun, found, err := store.GetRun(ctx, run.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("run %s not found after kill request", run.ID)
		}

		message := "force kill is unsupported for the codex app-server primary executor transport"
		if err := journalWriter.Append(journal.Event{
			Type:                  "executor.kill.requested",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               message,
			PreviousResponseID:    latestRun.PreviousResponseID,
			ExecutorTransport:     latestRun.ExecutorTransport,
			ExecutorThreadID:      latestRun.ExecutorThreadID,
			ExecutorThreadPath:    latestRun.ExecutorThreadPath,
			ExecutorTurnID:        latestRun.ExecutorTurnID,
			ExecutorTurnStatus:    latestRun.ExecutorTurnStatus,
			ExecutorControlAction: string(executor.ControlActionKill),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeExecutorControlReport(inv, "executor kill", latestRun, string(executor.ControlActionKill), message)
	})
}

func runExecutorSteer(ctx context.Context, inv Invocation, args []string) error {
	fs := flag.NewFlagSet("executor steer", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	note := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if note == "" {
		return errors.New("executor steer requires a raw note")
	}

	return withActiveExecutorRun(ctx, inv, "executor.steer.sent", func(store *state.Store, journalWriter *journal.Journal, run state.Run) error {
		if !executorTurnSteerable(run) {
			message := "active executor turn is not currently steerable"
			_ = journalWriter.Append(journal.Event{
				Type:                  "executor.steer.failed",
				RunID:                 run.ID,
				RepoPath:              run.RepoPath,
				Goal:                  run.Goal,
				Status:                string(run.Status),
				Message:               message,
				PreviousResponseID:    run.PreviousResponseID,
				ExecutorTransport:     run.ExecutorTransport,
				ExecutorThreadID:      run.ExecutorThreadID,
				ExecutorThreadPath:    run.ExecutorThreadPath,
				ExecutorTurnID:        run.ExecutorTurnID,
				ExecutorTurnStatus:    run.ExecutorTurnStatus,
				ExecutorControlAction: string(executor.ControlActionSteer),
				Checkpoint:            journalCheckpointRef(run.LatestCheckpoint),
			})
			fmt.Fprintf(inv.Stdout, "executor_steer: %s\n", message)
			return nil
		}

		client, err := newExecutorControlClient(inv.Version)
		if err != nil {
			return err
		}
		if err := client.SteerTurn(ctx, executorRequestFromRun(run), note); err != nil {
			_ = journalWriter.Append(journal.Event{
				Type:                  "executor.steer.failed",
				RunID:                 run.ID,
				RepoPath:              run.RepoPath,
				Goal:                  run.Goal,
				Status:                string(run.Status),
				Message:               err.Error(),
				PreviousResponseID:    run.PreviousResponseID,
				ExecutorTransport:     run.ExecutorTransport,
				ExecutorThreadID:      run.ExecutorThreadID,
				ExecutorThreadPath:    run.ExecutorThreadPath,
				ExecutorTurnID:        run.ExecutorTurnID,
				ExecutorTurnStatus:    run.ExecutorTurnStatus,
				ExecutorControlAction: string(executor.ControlActionSteer),
				Checkpoint:            journalCheckpointRef(run.LatestCheckpoint),
			})
			return err
		}

		updatedState := executorStateFromRun(run)
		updatedState.LastControl = &state.ExecutorControl{
			Action:  string(executor.ControlActionSteer),
			Payload: note,
			At:      time.Now().UTC(),
		}
		if err := store.SaveExecutorState(ctx, run.ID, updatedState); err != nil {
			return err
		}

		latestRun, found, err := store.GetRun(ctx, run.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("run %s not found after steer", run.ID)
		}

		if err := journalWriter.Append(journal.Event{
			Type:                  "executor.steer.sent",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Goal:                  latestRun.Goal,
			Status:                string(latestRun.Status),
			Message:               "raw steer note sent to the active executor turn",
			PreviousResponseID:    latestRun.PreviousResponseID,
			ExecutorTransport:     latestRun.ExecutorTransport,
			ExecutorThreadID:      latestRun.ExecutorThreadID,
			ExecutorThreadPath:    latestRun.ExecutorThreadPath,
			ExecutorTurnID:        latestRun.ExecutorTurnID,
			ExecutorTurnStatus:    latestRun.ExecutorTurnStatus,
			ExecutorControlAction: string(executor.ControlActionSteer),
			Checkpoint:            journalCheckpointRef(latestRun.LatestCheckpoint),
		}); err != nil {
			return err
		}

		return writeExecutorControlReport(inv, "executor steer", latestRun, string(executor.ControlActionSteer), "")
	})
}

func withActiveExecutorRun(
	ctx context.Context,
	inv Invocation,
	_ string,
	fn func(*state.Store, *journal.Journal, state.Run) error,
) error {
	if !pathExists(inv.Layout.DBPath) {
		fmt.Fprintln(inv.Stdout, "executor_lookup: no unfinished run found")
		return nil
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	run, found, err := store.LatestResumableRun(ctx)
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintln(inv.Stdout, "executor_lookup: no unfinished run found")
		return nil
	}
	if strings.TrimSpace(run.ExecutorThreadID) == "" || strings.TrimSpace(run.ExecutorTurnID) == "" {
		fmt.Fprintln(inv.Stdout, "executor_control: no active executor turn found")
		return nil
	}

	return fn(store, journalWriter, run)
}

func executorRequestFromRun(run state.Run) executor.TurnRequest {
	return executor.TurnRequest{
		RunID:      run.ID,
		RepoPath:   run.RepoPath,
		ThreadID:   run.ExecutorThreadID,
		ThreadPath: run.ExecutorThreadPath,
		TurnID:     run.ExecutorTurnID,
		Continue:   true,
	}
}

func executorApprovalFromRun(run state.Run) executor.ApprovalRequest {
	if run.ExecutorApproval == nil {
		return executor.ApprovalRequest{}
	}
	return executor.ApprovalRequest{
		RequestID:  run.ExecutorApproval.RequestID,
		ApprovalID: run.ExecutorApproval.ApprovalID,
		ItemID:     run.ExecutorApproval.ItemID,
		State:      executor.ApprovalState(run.ExecutorApproval.State),
		Kind:       executor.ApprovalKind(run.ExecutorApproval.Kind),
		Reason:     run.ExecutorApproval.Reason,
		Command:    run.ExecutorApproval.Command,
		CWD:        run.ExecutorApproval.CWD,
		GrantRoot:  run.ExecutorApproval.GrantRoot,
		RawParams:  run.ExecutorApproval.RawParams,
	}
}

func executorStateFromRun(run state.Run) state.ExecutorState {
	return state.ExecutorState{
		Transport:        run.ExecutorTransport,
		ThreadID:         run.ExecutorThreadID,
		ThreadPath:       run.ExecutorThreadPath,
		TurnID:           run.ExecutorTurnID,
		TurnStatus:       run.ExecutorTurnStatus,
		LastSuccess:      run.ExecutorLastSuccess,
		LastFailureStage: run.ExecutorLastFailureStage,
		LastError:        run.ExecutorLastError,
		LastMessage:      run.ExecutorLastMessage,
		Approval:         run.ExecutorApproval,
		LastControl:      run.ExecutorLastControl,
	}
}

func writeExecutorControlReport(inv Invocation, command string, run state.Run, action string, message string) error {
	fmt.Fprintf(inv.Stdout, "command: %s\n", command)
	fmt.Fprintf(inv.Stdout, "run_id: %s\n", run.ID)
	fmt.Fprintf(inv.Stdout, "status: %s\n", run.Status)
	fmt.Fprintf(inv.Stdout, "executor_thread_id: %s\n", valueOrUnavailable(run.ExecutorThreadID))
	fmt.Fprintf(inv.Stdout, "executor_turn_id: %s\n", valueOrUnavailable(run.ExecutorTurnID))
	fmt.Fprintf(inv.Stdout, "executor_turn_status: %s\n", valueOrUnavailable(run.ExecutorTurnStatus))
	fmt.Fprintf(inv.Stdout, "executor_approval_state: %s\n", valueOrUnavailable(executorApprovalStateValue(run)))
	fmt.Fprintf(inv.Stdout, "executor_control_action: %s\n", valueOrUnavailable(action))
	fmt.Fprintf(inv.Stdout, "executor_interruptible: %t\n", executorTurnInterruptible(run))
	fmt.Fprintf(inv.Stdout, "executor_steerable: %t\n", executorTurnSteerable(run))
	fmt.Fprintf(inv.Stdout, "next_operator_action: %s\n", nextOperatorActionForExistingRun(run))
	if strings.TrimSpace(message) != "" {
		fmt.Fprintf(inv.Stdout, "message: %s\n", message)
	}
	return nil
}
