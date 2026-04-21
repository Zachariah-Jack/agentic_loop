package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/state"
)

func TestRunWorkersCreatePersistsIsolatedWorker(t *testing.T) {
	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)
	installFakeGitTooling(t)

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "create a worker",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:  1,
			Stage:     "bootstrap",
			Label:     "run_initialized",
			CreatedAt: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runWorkers(context.Background(), Invocation{
		Args:   []string{"create", "--name", "Frontend Worker", "--scope", "ui shell"},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		Layout: layout,
	}); err != nil {
		t.Fatalf("runWorkers(create) error = %v", err)
	}

	store, err = openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()

	workers, err := store.ListWorkers(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkers() error = %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("len(ListWorkers()) = %d, want 1", len(workers))
	}
	worker := workers[0]
	if worker.WorkerStatus != state.WorkerStatusIdle {
		t.Fatalf("worker.WorkerStatus = %q, want %q", worker.WorkerStatus, state.WorkerStatusIdle)
	}
	if !strings.HasPrefix(strings.ToLower(worker.WorktreePath), strings.ToLower(layout.WorkersDir+string(filepath.Separator))) {
		t.Fatalf("worker.WorktreePath = %q, want within %q", worker.WorktreePath, layout.WorkersDir)
	}
	if strings.HasPrefix(strings.ToLower(worker.WorktreePath), strings.ToLower(repoRoot+string(filepath.Separator))) {
		t.Fatalf("worker.WorktreePath = %q unexpectedly under repo root %q", worker.WorktreePath, repoRoot)
	}
	if _, err := os.Stat(worker.WorktreePath); err != nil {
		t.Fatalf("expected worker path %s: %v", worker.WorktreePath, err)
	}

	journalWriter, err := openExistingJournal(layout)
	if err != nil {
		t.Fatalf("openExistingJournal() error = %v", err)
	}
	events, err := journalWriter.ReadRecent(run.ID, 16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "worker.created") {
		t.Fatalf("expected worker.created event, got %#v", events)
	}

	for _, want := range []string{
		"command: workers create",
		"run_id: " + run.ID,
		"worker_name: Frontend Worker",
		"worker_status: idle",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workers create output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkersCreateRejectsDuplicateWorkerPath(t *testing.T) {
	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)
	installFakeGitTooling(t)

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	if _, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "duplicate worker path",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence: 1,
			Stage:    "bootstrap",
			Label:    "run_initialized",
		},
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := runWorkers(context.Background(), Invocation{
		Args:   []string{"create", "--name", "api-worker", "--scope", "api"},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Layout: layout,
	}); err != nil {
		t.Fatalf("first runWorkers(create) error = %v", err)
	}

	err = runWorkers(context.Background(), Invocation{
		Args:   []string{"create", "--name", "api-worker", "--scope", "api"},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Layout: layout,
	})
	if err == nil {
		t.Fatal("second runWorkers(create) unexpectedly succeeded")
	}

	store, err = openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()
	workers, err := store.ListWorkers(context.Background(), "")
	if err != nil {
		t.Fatalf("ListWorkers() error = %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("len(ListWorkers()) = %d, want 1 after duplicate rejection", len(workers))
	}
}

func TestRunWorkersRemoveRejectsActiveWorker(t *testing.T) {
	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "reject active worker removal",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence: 1,
			Stage:    "bootstrap",
			Label:    "run_initialized",
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	workerPath := filepath.Join(layout.WorkersDir, "active-worker")
	if err := os.MkdirAll(workerPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", workerPath, err)
	}
	worker, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "active-worker",
		WorkerStatus:  state.WorkerStatusExecutorActive,
		AssignedScope: "active scope",
		WorktreePath:  workerPath,
	})
	if err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err = runWorkers(context.Background(), Invocation{
		Args:   []string{"remove", "--worker-id", worker.ID},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Layout: layout,
	})
	if err == nil {
		t.Fatal("runWorkers(remove) unexpectedly succeeded for active worker")
	}

	store, err = openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()

	_, found, err := store.GetWorker(context.Background(), worker.ID)
	if err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	}
	if !found {
		t.Fatal("active worker was removed despite rejection")
	}
}

func TestRunWorkersListOutputsRegistry(t *testing.T) {
	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "list workers",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence: 1,
			Stage:    "bootstrap",
			Label:    "run_initialized",
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	for _, item := range []struct {
		name   string
		scope  string
		status state.WorkerStatus
	}{
		{name: "api-worker", scope: "api", status: state.WorkerStatusIdle},
		{name: "ui-worker", scope: "ui", status: state.WorkerStatusCompleted},
	} {
		_, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
			RunID:         run.ID,
			WorkerName:    item.name,
			WorkerStatus:  item.status,
			AssignedScope: item.scope,
			WorktreePath:  filepath.Join(layout.WorkersDir, item.name),
		})
		if err != nil {
			t.Fatalf("CreateWorker(%s) error = %v", item.name, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runWorkers(context.Background(), Invocation{
		Args:   []string{"list", "--run-id", run.ID},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		Layout: layout,
	}); err != nil {
		t.Fatalf("runWorkers(list) error = %v", err)
	}

	for _, want := range []string{
		"command: workers list",
		"workers.count: 2",
		"workers.run_id: " + run.ID,
		"worker.1.run_id: " + run.ID,
		"worker.1.name:",
		"worker.1.path:",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workers list output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkersDispatchTargetsWorkerPath(t *testing.T) {
	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "dispatch worker executor",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence: 1,
			Stage:    "bootstrap",
			Label:    "run_initialized",
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	workerPath := filepath.Join(layout.WorkersDir, "dispatch-worker")
	if err := os.MkdirAll(workerPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", workerPath, err)
	}
	worker, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "dispatch-worker",
		WorkerStatus:  state.WorkerStatusIdle,
		AssignedScope: "worker dispatch seam",
		WorktreePath:  workerPath,
	})
	if err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	originalFactory := newWorkerExecutorClient
	defer func() {
		newWorkerExecutorClient = originalFactory
	}()

	var captured executor.TurnRequest
	newWorkerExecutorClient = func(version string) (workerExecutor, error) {
		return workerExecutorStub(func(ctx context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
			captured = req
			return executor.TurnResult{
				Transport:    executor.TransportAppServer,
				RunID:        req.RunID,
				ThreadID:     "thread_worker",
				TurnID:       "turn_worker",
				TurnStatus:   executor.TurnStatusCompleted,
				StartedAt:    time.Now().UTC(),
				CompletedAt:  time.Now().UTC(),
				FinalMessage: "worker task completed",
			}, nil
		}), nil
	}

	var stdout bytes.Buffer
	if err := runWorkers(context.Background(), Invocation{
		Args:   []string{"dispatch", "--worker-id", worker.ID, "--prompt", "Apply the assigned worker task."},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		Layout: layout,
	}); err != nil {
		t.Fatalf("runWorkers(dispatch) error = %v", err)
	}

	if captured.RepoPath != workerPath {
		t.Fatalf("captured.RepoPath = %q, want %q", captured.RepoPath, workerPath)
	}
	if captured.RepoPath == repoRoot {
		t.Fatalf("captured.RepoPath unexpectedly used main repo root %q", repoRoot)
	}

	store, err = openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()
	loaded, found, err := store.GetWorker(context.Background(), worker.ID)
	if err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	}
	if !found {
		t.Fatal("worker missing after dispatch")
	}
	if loaded.WorkerStatus != state.WorkerStatusCompleted {
		t.Fatalf("loaded.WorkerStatus = %q, want %q", loaded.WorkerStatus, state.WorkerStatusCompleted)
	}
	if loaded.ExecutorThreadID != "thread_worker" || loaded.ExecutorTurnID != "turn_worker" {
		t.Fatalf("loaded executor ids = (%q, %q), want thread_worker/turn_worker", loaded.ExecutorThreadID, loaded.ExecutorTurnID)
	}

	for _, want := range []string{
		"command: workers dispatch",
		"worker_status: completed",
		"executor_turn_status: completed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workers dispatch output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunWorkersIntegrateWritesReadOnlyArtifact(t *testing.T) {
	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "integrate worker outputs",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence: 1,
			Stage:    "bootstrap",
			Label:    "run_initialized",
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := os.MkdirAll(filepath.Join(repoRoot, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot/src) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "src", "shared.txt"), []byte("base shared\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(shared) error = %v", err)
	}

	workerOnePath := filepath.Join(layout.WorkersDir, "worker-one")
	workerTwoPath := filepath.Join(layout.WorkersDir, "worker-two")
	for _, item := range []struct {
		path     string
		contents string
	}{
		{path: filepath.Join(workerOnePath, "src", "shared.txt"), contents: "worker one shared\n"},
		{path: filepath.Join(workerOnePath, "src", "ui.txt"), contents: "worker one ui\n"},
		{path: filepath.Join(workerTwoPath, "src", "shared.txt"), contents: "worker two shared\n"},
		{path: filepath.Join(workerTwoPath, "src", "api.txt"), contents: "worker two api\n"},
	} {
		if err := os.MkdirAll(filepath.Dir(item.path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", item.path, err)
		}
		if err := os.WriteFile(item.path, []byte(item.contents), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", item.path, err)
		}
	}

	workerOne, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "worker-one",
		WorkerStatus:  state.WorkerStatusCompleted,
		AssignedScope: "ui shell",
		WorktreePath:  workerOnePath,
	})
	if err != nil {
		t.Fatalf("CreateWorker(worker one) error = %v", err)
	}
	workerTwo, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "worker-two",
		WorkerStatus:  state.WorkerStatusCompleted,
		AssignedScope: "api shell",
		WorktreePath:  workerTwoPath,
	})
	if err != nil {
		t.Fatalf("CreateWorker(worker two) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runWorkers(context.Background(), Invocation{
		Args:   []string{"integrate", "--worker-ids", workerOne.ID + "," + workerTwo.ID},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		Layout: layout,
	}); err != nil {
		t.Fatalf("runWorkers(integrate) error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "src", "ui.txt")); !os.IsNotExist(err) {
		t.Fatalf("main repo unexpectedly changed during workers integrate: %v", err)
	}
	if !strings.Contains(stdout.String(), "command: workers integrate") {
		t.Fatalf("workers integrate output missing command line\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), ".orchestrator/artifacts/integration/"+run.ID+"/") {
		t.Fatalf("workers integrate output missing integration artifact path\n%s", stdout.String())
	}
}

func TestRunWorkersApproveRecordsDurableApprovalAction(t *testing.T) {
	layout, run, worker := newWorkerControlRuntime(t, state.Worker{
		WorkerName:              "approval-worker",
		WorkerStatus:            state.WorkerStatusApprovalRequired,
		AssignedScope:           "api approvals",
		ExecutorThreadID:        "worker_thread_approve",
		ExecutorTurnID:          "worker_turn_approve",
		ExecutorTurnStatus:      string(executor.TurnStatusApprovalRequired),
		ExecutorApprovalState:   string(executor.ApprovalStateRequired),
		ExecutorApprovalKind:    string(executor.ApprovalKindCommandExecution),
		ExecutorApprovalPreview: "worker approval required for command: go test ./...",
		ExecutorApproval: &state.ExecutorApproval{
			State:      string(executor.ApprovalStateRequired),
			Kind:       string(executor.ApprovalKindCommandExecution),
			RequestID:  "req_worker_approve",
			ApprovalID: "approval_worker_approve",
			ItemID:     "item_worker_approve",
			Command:    "go test ./...",
			CWD:        ".",
			RawParams:  `{"approvalId":"approval_worker_approve"}`,
		},
	})

	fakeClient := &fakeExecutorControlClient{}
	restoreClient := stubExecutorControlClient(fakeClient)
	defer restoreClient()

	var stdout bytes.Buffer
	err := runWorkers(context.Background(), Invocation{
		Args:    []string{"approve", "--worker-id", worker.ID},
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Layout:  layout,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("runWorkers(approve) error = %v", err)
	}

	if fakeClient.lastAction != "approve" {
		t.Fatalf("lastAction = %q, want approve", fakeClient.lastAction)
	}
	if fakeClient.lastRequest.RepoPath != worker.WorktreePath {
		t.Fatalf("lastRequest.RepoPath = %q, want %q", fakeClient.lastRequest.RepoPath, worker.WorktreePath)
	}
	if fakeClient.lastRequest.RepoPath == layout.RepoRoot {
		t.Fatalf("lastRequest.RepoPath unexpectedly used main repo root %q", layout.RepoRoot)
	}
	if fakeClient.lastApproval.ApprovalID != "approval_worker_approve" {
		t.Fatalf("lastApproval.ApprovalID = %q, want approval_worker_approve", fakeClient.lastApproval.ApprovalID)
	}

	updatedRun := latestRunForLayout(t, layout)
	if updatedRun.LatestStopReason != "" {
		t.Fatalf("LatestStopReason = %q, want cleared", updatedRun.LatestStopReason)
	}

	updatedWorker := latestWorkerForLayout(t, layout, worker.ID)
	if updatedWorker.WorkerStatus != state.WorkerStatusExecutorActive {
		t.Fatalf("WorkerStatus = %q, want %q", updatedWorker.WorkerStatus, state.WorkerStatusExecutorActive)
	}
	if updatedWorker.ExecutorApproval == nil || updatedWorker.ExecutorApproval.State != string(executor.ApprovalStateGranted) {
		t.Fatalf("ExecutorApproval = %#v, want granted", updatedWorker.ExecutorApproval)
	}
	if updatedWorker.ExecutorLastControl == nil || updatedWorker.ExecutorLastControl.Action != string(executor.ControlActionApprove) {
		t.Fatalf("ExecutorLastControl = %#v, want approved action", updatedWorker.ExecutorLastControl)
	}

	for _, want := range []string{
		"command: workers approve",
		"run_id: " + run.ID,
		"worker_id: " + worker.ID,
		"executor_approval_state: granted",
		"executor_control_action: approved",
		"next_operator_action: continue_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workers approve output missing %q\n%s", want, stdout.String())
		}
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 16), "worker.approval.granted")
	if event.Type != "worker.approval.granted" {
		t.Fatal("journal missing worker.approval.granted event")
	}
	if event.WorkerID != worker.ID {
		t.Fatalf("event.WorkerID = %q, want %q", event.WorkerID, worker.ID)
	}
	if event.ExecutorControlAction != string(executor.ControlActionApprove) {
		t.Fatalf("event.ExecutorControlAction = %q, want approved", event.ExecutorControlAction)
	}
}

func TestRunWorkersDenyPersistsDataWithoutSemanticFailure(t *testing.T) {
	layout, run, worker := newWorkerControlRuntime(t, state.Worker{
		WorkerName:              "deny-worker",
		WorkerStatus:            state.WorkerStatusApprovalRequired,
		AssignedScope:           "deny path",
		ExecutorThreadID:        "worker_thread_deny",
		ExecutorTurnID:          "worker_turn_deny",
		ExecutorTurnStatus:      string(executor.TurnStatusApprovalRequired),
		ExecutorApprovalState:   string(executor.ApprovalStateRequired),
		ExecutorApprovalKind:    string(executor.ApprovalKindFileChange),
		ExecutorApprovalPreview: "worker approval required for file changes",
		ExecutorApproval: &state.ExecutorApproval{
			State:      string(executor.ApprovalStateRequired),
			Kind:       string(executor.ApprovalKindFileChange),
			RequestID:  "req_worker_deny",
			ApprovalID: "approval_worker_deny",
			ItemID:     "item_worker_deny",
			GrantRoot:  ".",
			RawParams:  `{"itemId":"item_worker_deny"}`,
		},
	})

	fakeClient := &fakeExecutorControlClient{}
	restoreClient := stubExecutorControlClient(fakeClient)
	defer restoreClient()

	var stdout bytes.Buffer
	err := runWorkers(context.Background(), Invocation{
		Args:    []string{"deny", "--worker-id", worker.ID},
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Layout:  layout,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("runWorkers(deny) error = %v", err)
	}

	if fakeClient.lastAction != "deny" {
		t.Fatalf("lastAction = %q, want deny", fakeClient.lastAction)
	}
	if fakeClient.lastRequest.RepoPath != worker.WorktreePath {
		t.Fatalf("lastRequest.RepoPath = %q, want %q", fakeClient.lastRequest.RepoPath, worker.WorktreePath)
	}

	updatedRun := latestRunForLayout(t, layout)
	if updatedRun.LatestStopReason != "" {
		t.Fatalf("LatestStopReason = %q, want cleared", updatedRun.LatestStopReason)
	}
	if updatedRun.RuntimeIssueReason != "" {
		t.Fatalf("RuntimeIssueReason = %q, want empty", updatedRun.RuntimeIssueReason)
	}

	updatedWorker := latestWorkerForLayout(t, layout, worker.ID)
	if updatedWorker.ExecutorApproval == nil || updatedWorker.ExecutorApproval.State != string(executor.ApprovalStateDenied) {
		t.Fatalf("ExecutorApproval = %#v, want denied", updatedWorker.ExecutorApproval)
	}
	if updatedWorker.ExecutorLastControl == nil || updatedWorker.ExecutorLastControl.Action != string(executor.ControlActionDeny) {
		t.Fatalf("ExecutorLastControl = %#v, want denied action", updatedWorker.ExecutorLastControl)
	}

	for _, want := range []string{
		"command: workers deny",
		"executor_approval_state: denied",
		"executor_control_action: denied",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workers deny output missing %q\n%s", want, stdout.String())
		}
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 16), "worker.approval.denied")
	if event.Type != "worker.approval.denied" {
		t.Fatal("journal missing worker.approval.denied event")
	}
	if event.ExecutorControlAction != string(executor.ControlActionDeny) {
		t.Fatalf("event.ExecutorControlAction = %q, want denied", event.ExecutorControlAction)
	}
}

func TestRunWorkersInterruptRoutesToWorkerExecutorTurn(t *testing.T) {
	layout, run, worker := newWorkerControlRuntime(t, state.Worker{
		WorkerName:            "interrupt-worker",
		WorkerStatus:          state.WorkerStatusExecutorActive,
		AssignedScope:         "interrupt scope",
		ExecutorThreadID:      "worker_thread_interrupt",
		ExecutorTurnID:        "worker_turn_interrupt",
		ExecutorTurnStatus:    string(executor.TurnStatusInProgress),
		ExecutorInterruptible: true,
	})

	fakeClient := &fakeExecutorControlClient{}
	restoreClient := stubExecutorControlClient(fakeClient)
	defer restoreClient()

	var stdout bytes.Buffer
	err := runWorkers(context.Background(), Invocation{
		Args:    []string{"interrupt", "--worker-id", worker.ID},
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Layout:  layout,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("runWorkers(interrupt) error = %v", err)
	}

	if fakeClient.lastAction != "interrupt" {
		t.Fatalf("lastAction = %q, want interrupt", fakeClient.lastAction)
	}
	if fakeClient.lastRequest.RepoPath != worker.WorktreePath {
		t.Fatalf("lastRequest.RepoPath = %q, want %q", fakeClient.lastRequest.RepoPath, worker.WorktreePath)
	}
	if fakeClient.lastRequest.RepoPath == layout.RepoRoot {
		t.Fatalf("lastRequest.RepoPath unexpectedly used main repo root %q", layout.RepoRoot)
	}

	updatedWorker := latestWorkerForLayout(t, layout, worker.ID)
	if updatedWorker.ExecutorLastControl == nil || updatedWorker.ExecutorLastControl.Action != string(executor.ControlActionInterrupt) {
		t.Fatalf("ExecutorLastControl = %#v, want interrupted action", updatedWorker.ExecutorLastControl)
	}

	for _, want := range []string{
		"command: workers interrupt",
		"executor_control_action: interrupted",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workers interrupt output missing %q\n%s", want, stdout.String())
		}
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 16), "worker.interrupt.requested")
	if event.Type != "worker.interrupt.requested" {
		t.Fatal("journal missing worker.interrupt.requested event")
	}
	if event.ExecutorControlAction != string(executor.ControlActionInterrupt) {
		t.Fatalf("event.ExecutorControlAction = %q, want interrupted", event.ExecutorControlAction)
	}
}

func TestRunWorkersSteerPersistsRawNoteAndTargetsWorkerTurn(t *testing.T) {
	layout, run, worker := newWorkerControlRuntime(t, state.Worker{
		WorkerName:         "steer-worker",
		WorkerStatus:       state.WorkerStatusExecutorActive,
		AssignedScope:      "steer scope",
		ExecutorThreadID:   "worker_thread_steer",
		ExecutorTurnID:     "worker_turn_steer",
		ExecutorTurnStatus: string(executor.TurnStatusInProgress),
		ExecutorSteerable:  true,
	})

	fakeClient := &fakeExecutorControlClient{}
	restoreClient := stubExecutorControlClient(fakeClient)
	defer restoreClient()

	var stdout bytes.Buffer
	err := runWorkers(context.Background(), Invocation{
		Args:    []string{"steer", "--worker-id", worker.ID, "--message", "stay inside src/ui for this worker"},
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Layout:  layout,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("runWorkers(steer) error = %v", err)
	}

	if fakeClient.lastAction != "steer" {
		t.Fatalf("lastAction = %q, want steer", fakeClient.lastAction)
	}
	if fakeClient.lastRequest.RepoPath != worker.WorktreePath {
		t.Fatalf("lastRequest.RepoPath = %q, want %q", fakeClient.lastRequest.RepoPath, worker.WorktreePath)
	}
	if fakeClient.lastSteerNote != "stay inside src/ui for this worker" {
		t.Fatalf("lastSteerNote = %q, want raw note", fakeClient.lastSteerNote)
	}

	updatedWorker := latestWorkerForLayout(t, layout, worker.ID)
	if updatedWorker.ExecutorLastControl == nil {
		t.Fatal("ExecutorLastControl = nil, want steered action")
	}
	if updatedWorker.ExecutorLastControl.Action != string(executor.ControlActionSteer) {
		t.Fatalf("ExecutorLastControl.Action = %q, want steered", updatedWorker.ExecutorLastControl.Action)
	}
	if updatedWorker.ExecutorLastControl.Payload != "stay inside src/ui for this worker" {
		t.Fatalf("ExecutorLastControl.Payload = %q, want raw steer note", updatedWorker.ExecutorLastControl.Payload)
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 16), "worker.steer.sent")
	if event.Type != "worker.steer.sent" {
		t.Fatal("journal missing worker.steer.sent event")
	}
	if event.ExecutorControlAction != string(executor.ControlActionSteer) {
		t.Fatalf("event.ExecutorControlAction = %q, want steered", event.ExecutorControlAction)
	}
}

func TestRunWorkersKillReportsUnsupportedMechanically(t *testing.T) {
	layout, run, worker := newWorkerControlRuntime(t, state.Worker{
		WorkerName:         "kill-worker",
		WorkerStatus:       state.WorkerStatusExecutorActive,
		AssignedScope:      "kill scope",
		ExecutorThreadID:   "worker_thread_kill",
		ExecutorTurnID:     "worker_turn_kill",
		ExecutorTurnStatus: string(executor.TurnStatusInProgress),
	})

	var stdout bytes.Buffer
	err := runWorkers(context.Background(), Invocation{
		Args:    []string{"kill", "--worker-id", worker.ID},
		Stdout:  &stdout,
		Stderr:  &bytes.Buffer{},
		Layout:  layout,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("runWorkers(kill) error = %v", err)
	}

	updatedWorker := latestWorkerForLayout(t, layout, worker.ID)
	if updatedWorker.ExecutorLastControl == nil || updatedWorker.ExecutorLastControl.Action != string(executor.ControlActionKill) {
		t.Fatalf("ExecutorLastControl = %#v, want kill_unsupported action", updatedWorker.ExecutorLastControl)
	}

	for _, want := range []string{
		"command: workers kill",
		"executor_control_action: kill_unsupported",
		"force kill is unsupported for the codex app-server primary executor transport",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workers kill output missing %q\n%s", want, stdout.String())
		}
	}

	event := latestJournalEvent(readContinueEvents(t, layout, run.ID, 16), "worker.kill.requested")
	if event.Type != "worker.kill.requested" {
		t.Fatal("journal missing worker.kill.requested event")
	}
	if event.ExecutorControlAction != string(executor.ControlActionKill) {
		t.Fatalf("event.ExecutorControlAction = %q, want kill_unsupported", event.ExecutorControlAction)
	}
}

func TestRunWorkersListShowsApprovalStateAndControlFields(t *testing.T) {
	layout, _, _ := newWorkerControlRuntime(t, state.Worker{
		WorkerName:                "listed-worker",
		WorkerStatus:              state.WorkerStatusApprovalRequired,
		AssignedScope:             "listed scope",
		ExecutorThreadID:          "worker_thread_list",
		ExecutorTurnID:            "worker_turn_list",
		ExecutorTurnStatus:        string(executor.TurnStatusApprovalRequired),
		ExecutorApprovalState:     string(executor.ApprovalStateRequired),
		ExecutorApprovalKind:      string(executor.ApprovalKindCommandExecution),
		ExecutorApprovalPreview:   "approval required for listed worker",
		ExecutorInterruptible:     true,
		ExecutorSteerable:         false,
		ExecutorLastControlAction: string(executor.ControlActionInterrupt),
		ExecutorLastControl: &state.ExecutorControl{
			Action: string(executor.ControlActionInterrupt),
			At:     time.Now().UTC(),
		},
		ExecutorApproval: &state.ExecutorApproval{
			State: string(executor.ApprovalStateRequired),
			Kind:  string(executor.ApprovalKindCommandExecution),
		},
	})

	run := latestRunForLayout(t, layout)

	var stdout bytes.Buffer
	if err := runWorkers(context.Background(), Invocation{
		Args:   []string{"list", "--run-id", run.ID},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		Layout: layout,
	}); err != nil {
		t.Fatalf("runWorkers(list) error = %v", err)
	}

	for _, want := range []string{
		"worker.1.approval_required: true",
		"worker.1.approval_kind: command_execution",
		"worker.1.executor_thread_id: worker_thread_list",
		"worker.1.executor_turn_id: worker_turn_list",
		"worker.1.executor_interruptible: true",
		"worker.1.executor_steerable: false",
		"worker.1.executor_last_control_action: interrupted",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("workers list output missing %q\n%s", want, stdout.String())
		}
	}
}

func newWorkerControlRuntime(t *testing.T, seed state.Worker) (state.Layout, state.Run, state.Worker) {
	t.Helper()

	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "control a worker executor turn",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     2,
			Stage:        "planner",
			Label:        "planner_turn_post_executor",
			SafePause:    true,
			PlannerTurn:  1,
			ExecutorTurn: 1,
			CreatedAt:    time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	workerName := strings.TrimSpace(seed.WorkerName)
	if workerName == "" {
		workerName = "controlled-worker"
	}
	workerPath := filepath.Join(layout.WorkersDir, workerName)
	if err := os.MkdirAll(workerPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", workerPath, err)
	}

	workerStatus := seed.WorkerStatus
	if workerStatus == "" {
		workerStatus = state.WorkerStatusExecutorActive
	}
	assignedScope := strings.TrimSpace(seed.AssignedScope)
	if assignedScope == "" {
		assignedScope = "worker control scope"
	}

	worker, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    workerName,
		WorkerStatus:  workerStatus,
		AssignedScope: assignedScope,
		WorktreePath:  workerPath,
	})
	if err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}

	worker.WorkerStatus = workerStatus
	worker.ExecutorThreadID = firstNonEmptyWorkerValue(seed.ExecutorThreadID, "worker_thread")
	worker.ExecutorTurnID = firstNonEmptyWorkerValue(seed.ExecutorTurnID, "worker_turn")
	worker.ExecutorTurnStatus = firstNonEmptyWorkerValue(seed.ExecutorTurnStatus, string(executor.TurnStatusInProgress))
	worker.ExecutorApprovalState = strings.TrimSpace(seed.ExecutorApprovalState)
	worker.ExecutorApprovalKind = strings.TrimSpace(seed.ExecutorApprovalKind)
	worker.ExecutorApprovalPreview = strings.TrimSpace(seed.ExecutorApprovalPreview)
	worker.ExecutorInterruptible = seed.ExecutorInterruptible
	worker.ExecutorSteerable = seed.ExecutorSteerable
	worker.ExecutorFailureStage = strings.TrimSpace(seed.ExecutorFailureStage)
	worker.ExecutorLastControlAction = strings.TrimSpace(seed.ExecutorLastControlAction)
	worker.ExecutorApproval = seed.ExecutorApproval
	worker.ExecutorLastControl = seed.ExecutorLastControl
	worker.WorkerResultSummary = strings.TrimSpace(seed.WorkerResultSummary)
	worker.WorkerErrorSummary = strings.TrimSpace(seed.WorkerErrorSummary)
	worker.UpdatedAt = time.Now().UTC()
	if workerApprovalRequired(worker) && worker.ExecutorApprovalPreview == "" {
		worker.ExecutorApprovalPreview = "worker approval required"
	}
	if err := store.SaveWorker(context.Background(), worker); err != nil {
		t.Fatalf("SaveWorker() error = %v", err)
	}
	if workerApprovalRequired(worker) {
		if err := store.SaveLatestStopReason(context.Background(), run.ID, orchestration.StopReasonExecutorApprovalReq); err != nil {
			t.Fatalf("SaveLatestStopReason() error = %v", err)
		}
	}

	loadedRun, found, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if !found {
		t.Fatal("GetRun() found = false, want persisted run")
	}
	loadedWorker, found, err := store.GetWorker(context.Background(), worker.ID)
	if err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	}
	if !found {
		t.Fatal("GetWorker() found = false, want persisted worker")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	return layout, loadedRun, loadedWorker
}

func latestWorkerForLayout(t *testing.T, layout state.Layout, workerID string) state.Worker {
	t.Helper()

	store, err := openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()

	worker, found, err := store.GetWorker(context.Background(), workerID)
	if err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	}
	if !found {
		t.Fatalf("GetWorker(%s) found = false, want worker", workerID)
	}
	return worker
}

func firstNonEmptyWorkerValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type workerExecutorStub func(context.Context, executor.TurnRequest) (executor.TurnResult, error)

func (s workerExecutorStub) Execute(ctx context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	return s(ctx, req)
}

func containsEventType(events []journal.Event, want string) bool {
	for _, event := range events {
		if event.Type == want {
			return true
		}
	}
	return false
}

func installFakeGitTooling(t *testing.T) {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	mustMkdirAll(t, binDir)

	if runtime.GOOS == "windows" {
		script := `@echo off
setlocal EnableDelayedExpansion
if "%1"=="-C" (
  set REPO=%2
  shift
  shift
)
if "%1"=="rev-parse" (
  if "%FAKE_GIT_FAIL_REVPARSE%"=="1" (
    echo fake rev-parse failure 1>&2
    exit /b 1
  )
  echo true
  exit /b 0
)
if "%1"=="worktree" (
  if "%2"=="list" (
    echo !REPO!
    exit /b 0
  )
  if "%2"=="add" (
    if "%FAKE_GIT_FAIL_ADD%"=="1" (
      echo fake worktree add failure 1>&2
      exit /b 1
    )
    set TARGET=%4
    mkdir "!TARGET!" >nul 2>nul
    >"!TARGET!\.git" echo gitdir: fake
    exit /b 0
  )
  if "%2"=="remove" (
    if "%FAKE_GIT_FAIL_REMOVE%"=="1" (
      echo fake worktree remove failure 1>&2
      exit /b 1
    )
    set TARGET=%4
    if exist "!TARGET!" rmdir /s /q "!TARGET!"
    exit /b 0
  )
)
echo unsupported fake git invocation 1>&2
exit /b 1
`
		mustWriteFile(t, filepath.Join(binDir, "git.cmd"), script)
	} else {
		script := `#!/bin/sh
if [ "$1" = "-C" ]; then
  REPO="$2"
  shift 2
fi
if [ "$1" = "rev-parse" ]; then
  if [ "${FAKE_GIT_FAIL_REVPARSE:-0}" = "1" ]; then
    echo "fake rev-parse failure" >&2
    exit 1
  fi
  echo true
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "list" ]; then
  echo "${REPO:-.}"
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "add" ]; then
  if [ "${FAKE_GIT_FAIL_ADD:-0}" = "1" ]; then
    echo "fake worktree add failure" >&2
    exit 1
  fi
  TARGET="$4"
  mkdir -p "$TARGET"
  printf 'gitdir: fake\n' > "$TARGET/.git"
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "remove" ]; then
  if [ "${FAKE_GIT_FAIL_REMOVE:-0}" = "1" ]; then
    echo "fake worktree remove failure" >&2
    exit 1
  fi
  TARGET="$4"
  rm -rf "$TARGET"
  exit 0
fi
echo "unsupported fake git invocation" >&2
exit 1
`
		writeExecutableFile(t, filepath.Join(binDir, "git"), script)
	}

	pathValue := binDir
	if existing := os.Getenv("PATH"); existing != "" {
		pathValue += string(os.PathListSeparator) + existing
	}
	t.Setenv("PATH", pathValue)
}
