package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"orchestrator/internal/activity"
	"orchestrator/internal/config"
	"orchestrator/internal/control"
	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/runtimecfg"
	"orchestrator/internal/state"
	workerctl "orchestrator/internal/workers"
)

func TestLocalControlServerGetStatusSnapshotAction(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "inspect V2 status snapshot plumbing",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.SavePlannerOperatorStatus(context.Background(), run.ID, &state.PlannerOperatorStatus{
		ContractVersion:    "planner.v1",
		OperatorMessage:    "Implementing the next bounded slice.",
		CurrentFocus:       "control protocol snapshot rendering",
		NextIntendedStep:   "expose safe planner progress to the operator",
		WhyThisStep:        "the demo surface needs a live planner-safe status block.",
		ProgressPercent:    24,
		ProgressConfidence: "medium",
		ProgressBasis:      "control protocol and runtime persistence already exist; this slice is surfacing them.",
	}); err != nil {
		t.Fatalf("SavePlannerOperatorStatus() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}

	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_status",
		"type":"request",
		"action":"get_status_snapshot",
		"payload":{"run_id":"`+run.ID+`"}
	}`)

	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}

	payload := response.Payload.(map[string]any)
	runtimeSnapshot := payload["runtime"].(map[string]any)
	if runtimeSnapshot["verbosity"] != "normal" {
		t.Fatalf("runtime.verbosity = %#v, want normal", runtimeSnapshot["verbosity"])
	}
	plannerStatus := payload["planner_status"].(map[string]any)
	if plannerStatus["present"] != true {
		t.Fatalf("planner_status.present = %#v, want true", plannerStatus["present"])
	}
	if plannerStatus["contract_version"] != "planner.v1" {
		t.Fatalf("planner_status.contract_version = %#v, want planner.v1", plannerStatus["contract_version"])
	}
	if plannerStatus["operator_message"] != "Implementing the next bounded slice." {
		t.Fatalf("planner_status.operator_message = %#v, want live operator message", plannerStatus["operator_message"])
	}
	roadmap := payload["roadmap"].(map[string]any)
	if roadmap["present"] != true {
		t.Fatalf("roadmap.present = %#v, want true", roadmap["present"])
	}
	if roadmap["path"] != ".orchestrator/roadmap.md" {
		t.Fatalf("roadmap.path = %#v, want .orchestrator/roadmap.md", roadmap["path"])
	}
	if !strings.Contains(roadmap["alignment_text"].(string), "roadmap") {
		t.Fatalf("roadmap.alignment_text = %#v, want roadmap preview", roadmap["alignment_text"])
	}

	runSnapshot := payload["run"].(map[string]any)
	if runSnapshot["id"] != run.ID {
		t.Fatalf("run.id = %#v, want %q", runSnapshot["id"], run.ID)
	}
	pendingAction := payload["pending_action"].(map[string]any)
	if pendingAction["available"] != true {
		t.Fatalf("pending_action.available = %#v, want true", pendingAction["available"])
	}
	if pendingAction["present"] != false {
		t.Fatalf("pending_action.present = %#v, want false", pendingAction["present"])
	}
}

func TestLocalControlServerModelHealthActionsSurfaceExecutorModelErrors(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "surface model errors",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:  1,
			Stage:     "executor",
			Label:     "executor_turn_failed",
			SafePause: true,
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	modelErr := "stream disconnected before completion: The model `gpt-5.5` does not exist or you do not have access to it."
	if err := store.SaveExecutorState(context.Background(), run.ID, state.ExecutorState{
		Transport:        "codex_app_server",
		TurnStatus:       "failed",
		LastFailureStage: "turn_stream",
		LastError:        modelErr,
	}); err != nil {
		t.Fatalf("SaveExecutorState() error = %v", err)
	}
	if err := store.SaveRuntimeIssue(context.Background(), run.ID, "executor_failed", modelErr); err != nil {
		t.Fatalf("SaveRuntimeIssue() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}

	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	statusResponse := postControlAction(t, server.URL, `{
		"id":"req_status",
		"type":"request",
		"action":"get_status_snapshot",
		"payload":{}
	}`)
	if !statusResponse.OK {
		t.Fatalf("status response failed: %#v", statusResponse.Error)
	}
	statusPayload := statusResponse.Payload.(map[string]any)
	modelHealth := statusPayload["model_health"].(map[string]any)
	if modelHealth["blocking"] != true {
		t.Fatalf("model_health.blocking = %#v, want true", modelHealth["blocking"])
	}
	executorHealth := modelHealth["executor"].(map[string]any)
	if executorHealth["verification_state"] != "invalid" {
		t.Fatalf("executor.verification_state = %#v, want invalid", executorHealth["verification_state"])
	}
	if executorHealth["requested_model"] != "gpt-5.5" {
		t.Fatalf("executor.requested_model = %#v, want gpt-5.5", executorHealth["requested_model"])
	}

	testResponse := postControlAction(t, server.URL, `{
		"id":"req_executor_model",
		"type":"request",
		"action":"test_executor_model",
		"payload":{}
	}`)
	if !testResponse.OK {
		t.Fatalf("test_executor_model response failed: %#v", testResponse.Error)
	}
	testPayload := testResponse.Payload.(map[string]any)
	testExecutor := testPayload["executor"].(map[string]any)
	if testExecutor["model_unavailable"] != true {
		t.Fatalf("test executor.model_unavailable = %#v, want true", testExecutor["model_unavailable"])
	}
}

func TestLocalControlServerGetPendingActionReturnsDurableState(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "inspect persisted pending action",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.SavePendingAction(context.Background(), run.ID, &state.PendingAction{
		TurnType:               "executor_dispatch",
		PlannerOutcome:         string(planner.OutcomeExecute),
		PlannerResponseID:      "resp_pending",
		PendingActionSummary:   "dispatch the bounded executor step",
		PendingExecutorPrompt:  "Apply the bounded edit now.",
		PendingExecutorSummary: "Apply the bounded edit now.",
		PendingDispatchTarget:  &state.PendingDispatchTarget{Kind: "primary_executor"},
		PendingReason:          "planner_selected_execute",
		Held:                   true,
		HoldReason:             "control_message_queued",
		UpdatedAt:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SavePendingAction() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_pending",
		"type":"request",
		"action":"get_pending_action",
		"payload":{"run_id":"`+run.ID+`"}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}

	payload := response.Payload.(map[string]any)
	if payload["available"] != true || payload["present"] != true {
		t.Fatalf("pending action payload = %#v, want available+present", payload)
	}
	if payload["turn_type"] != "executor_dispatch" {
		t.Fatalf("turn_type = %#v, want executor_dispatch", payload["turn_type"])
	}
	if payload["planner_outcome"] != "execute" {
		t.Fatalf("planner_outcome = %#v, want execute", payload["planner_outcome"])
	}
	if payload["held"] != true {
		t.Fatalf("held = %#v, want true", payload["held"])
	}
}

func TestLocalControlServerInjectAndListControlMessages(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "queue a control message",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     events,
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	queueResponse := postControlAction(t, server.URL, `{
		"id":"req_queue",
		"type":"request",
		"action":"inject_control_message",
		"payload":{"run_id":"`+run.ID+`","message":"Make the wall red, not blue.","source":"control_chat","reason":"operator_intervention"}
	}`)
	if !queueResponse.OK {
		t.Fatalf("queueResponse.OK = false, error = %#v", queueResponse.Error)
	}

	listResponse := postControlAction(t, server.URL, `{
		"id":"req_list",
		"type":"request",
		"action":"list_control_messages",
		"payload":{"run_id":"`+run.ID+`","status":"queued","limit":10}
	}`)
	if !listResponse.OK {
		t.Fatalf("listResponse.OK = false, error = %#v", listResponse.Error)
	}
	payload := listResponse.Payload.(map[string]any)
	if payload["count"] != float64(1) {
		t.Fatalf("count = %#v, want 1", payload["count"])
	}
	messages := payload["messages"].([]any)
	message := messages[0].(map[string]any)
	if message["raw_text"] != "Make the wall red, not blue." {
		t.Fatalf("raw_text = %#v, want queued message", message["raw_text"])
	}
	if message["status"] != "queued" {
		t.Fatalf("status = %#v, want queued", message["status"])
	}

	eventStream, cancel := events.Subscribe(0)
	defer cancel()
	foundQueuedEvent := false
	for {
		select {
		case event := <-eventStream:
			if event.Event == "control_message_queued" {
				foundQueuedEvent = true
				goto doneQueued
			}
		case <-time.After(2 * time.Second):
			goto doneQueued
		}
	}
doneQueued:
	if !foundQueuedEvent {
		t.Fatal("control_message_queued event not observed")
	}
}

func TestLocalControlServerStartRunActionLaunchesAsyncForegroundLoop(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	restoreRunner := stubBoundedCycleRunner(func(ctx context.Context, _ Invocation, store *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		return completedControlRunResult(ctx, store, currentRun, "resp_control_start_complete")
	})
	defer restoreRunner()

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     events,
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_start_run",
		"type":"request",
		"action":"start_run",
		"payload":{"goal":"Build the next bounded V2 shell slice.","repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`"}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}
	payload := response.Payload.(map[string]any)
	if payload["accepted"] != true || payload["async"] != true {
		t.Fatalf("payload = %#v, want accepted async launch", payload)
	}
	if payload["action"] != "start_run" {
		t.Fatalf("action = %#v, want start_run", payload["action"])
	}
	runID := strings.TrimSpace(payload["run_id"].(string))
	if runID == "" {
		t.Fatalf("run_id = %#v, want populated run id", payload["run_id"])
	}

	event := waitForActivityEvent(t, events, "run_completed", runID)
	if event.Payload["command"] != "control start_run" {
		t.Fatalf("run_completed command = %#v, want control start_run", event.Payload["command"])
	}
	run := latestRunForLayout(t, layout)
	if run.ID != runID {
		t.Fatalf("latest run id = %q, want %q", run.ID, runID)
	}
	if run.Status != state.StatusCompleted {
		t.Fatalf("run status = %q, want completed", run.Status)
	}
}

func TestLocalControlServerContinueRunActionLaunchesLatestUnfinishedRun(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "Continue the existing V2 run",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:  1,
			Stage:     "bootstrap",
			Label:     "run_initialized",
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restoreRunner := stubBoundedCycleRunner(func(ctx context.Context, _ Invocation, store *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		return completedControlRunResult(ctx, store, currentRun, "resp_control_continue_complete")
	})
	defer restoreRunner()

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     events,
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_continue_run",
		"type":"request",
		"action":"continue_run",
		"payload":{"run_id":"`+run.ID+`"}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}
	payload := response.Payload.(map[string]any)
	if payload["accepted"] != true || payload["async"] != true {
		t.Fatalf("payload = %#v, want accepted async launch", payload)
	}
	if payload["action"] != "continue_run" {
		t.Fatalf("action = %#v, want continue_run", payload["action"])
	}
	if payload["run_id"] != run.ID {
		t.Fatalf("run_id = %#v, want %q", payload["run_id"], run.ID)
	}

	event := waitForActivityEvent(t, events, "run_completed", run.ID)
	if event.Payload["command"] != "control continue_run" {
		t.Fatalf("run_completed command = %#v, want control continue_run", event.Payload["command"])
	}
	updatedRun := latestRunForLayout(t, layout)
	if updatedRun.Status != state.StatusCompleted {
		t.Fatalf("run status = %q, want completed", updatedRun.Status)
	}
}

func TestLocalControlServerRunActionGuardRejectsOverlappingControlRuns(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	restoreRunner := stubBoundedCycleRunner(func(ctx context.Context, _ Invocation, store *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		once.Do(func() { close(entered) })
		<-release
		return completedControlRunResult(ctx, store, currentRun, "resp_control_guard_complete")
	})
	defer restoreRunner()
	defer releaseOnce.Do(func() { close(release) })

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     events,
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	first := postControlAction(t, server.URL, `{
		"id":"req_start_guard_one",
		"type":"request",
		"action":"start_run",
		"payload":{"goal":"Start the first active run."}
	}`)
	if !first.OK {
		t.Fatalf("first.OK = false, error = %#v", first.Error)
	}
	firstPayload := first.Payload.(map[string]any)
	runID := strings.TrimSpace(firstPayload["run_id"].(string))
	if runID == "" {
		t.Fatal("first start_run did not return a run id")
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("async control run did not enter bounded cycle")
	}

	second := postControlAction(t, server.URL, `{
		"id":"req_start_guard_two",
		"type":"request",
		"action":"start_run",
		"payload":{"goal":"Start an overlapping active run."}
	}`)
	if second.OK {
		t.Fatalf("second.OK = true, want run already active rejection: %#v", second.Payload)
	}
	if second.Error == nil || !strings.Contains(second.Error.Message, "run already active") {
		t.Fatalf("second error = %#v, want run already active message", second.Error)
	}

	status := postControlAction(t, server.URL, `{
		"id":"req_status_guard_active",
		"type":"request",
		"action":"get_status_snapshot",
		"payload":{"run_id":"`+runID+`"}
	}`)
	if !status.OK {
		t.Fatalf("status.OK = false, error = %#v", status.Error)
	}
	statusPayload := status.Payload.(map[string]any)
	runPayload := statusPayload["run"].(map[string]any)
	if runPayload["status"] != "active" {
		t.Fatalf("active overlay status = %#v, want active", runPayload["status"])
	}
	if runPayload["next_operator_action"] != "watch_progress" {
		t.Fatalf("next_operator_action = %#v, want watch_progress", runPayload["next_operator_action"])
	}

	releaseOnce.Do(func() { close(release) })
	waitForActivityEvent(t, events, "run_completed", runID)
}

func TestLocalControlServerSideChatStoresMessagesAndRemainsTruthful(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	if _, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "record a side chat message",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     events,
		Version:    "test",
	}

	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_side_chat",
		"type":"request",
		"action":"send_side_chat_message",
		"payload":{"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`","message":"What remains before release?","context_policy":"repo_and_latest_run_summary"}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}
	payload := response.Payload.(map[string]any)
	if payload["available"] != false {
		t.Fatalf("available = %#v, want false", payload["available"])
	}
	if payload["stored"] != true {
		t.Fatalf("stored = %#v, want true", payload["stored"])
	}
	if !strings.Contains(payload["message"].(string), "not implemented") {
		t.Fatalf("message = %#v, want truthful stub", payload["message"])
	}
	entry := payload["entry"].(map[string]any)
	if entry["raw_text"] != "What remains before release?" {
		t.Fatalf("entry.raw_text = %#v, want recorded side chat text", entry["raw_text"])
	}
	if entry["backend_state"] != "unavailable" {
		t.Fatalf("entry.backend_state = %#v, want unavailable", entry["backend_state"])
	}

	listResponse := postControlAction(t, server.URL, `{
		"id":"req_side_chat_list",
		"type":"request",
		"action":"list_side_chat_messages",
		"payload":{"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`","limit":10}
	}`)
	if !listResponse.OK {
		t.Fatalf("listResponse.OK = false, error = %#v", listResponse.Error)
	}
	listPayload := listResponse.Payload.(map[string]any)
	if listPayload["available"] != true {
		t.Fatalf("available = %#v, want true", listPayload["available"])
	}
	if listPayload["count"] != float64(1) {
		t.Fatalf("count = %#v, want 1", listPayload["count"])
	}
	items := listPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	recorded := items[0].(map[string]any)
	if recorded["response_message"] == "" {
		t.Fatal("response_message should keep the truthful unavailable note")
	}

	eventStream, cancel := events.Subscribe(0)
	defer cancel()
	foundRecordedEvent := false
	for {
		select {
		case event := <-eventStream:
			if event.Event == "side_chat_message_recorded" {
				foundRecordedEvent = true
				goto doneSideChat
			}
		case <-time.After(2 * time.Second):
			goto doneSideChat
		}
	}
doneSideChat:
	if !foundRecordedEvent {
		t.Fatal("side_chat_message_recorded event not observed")
	}
}

func TestLocalControlServerCaptureAndListDogfoodIssues(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "record a dogfood note",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     events,
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	captureResponse := postControlAction(t, server.URL, `{
		"id":"req_dogfood_capture",
		"type":"request",
		"action":"capture_dogfood_issue",
		"payload":{
			"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`",
			"run_id":"`+run.ID+`",
			"title":"Reconnect leaves stale artifact selected",
			"note":"After reconnect, the artifact pane still showed the old path until a manual refresh.",
			"source":"operator_shell"
		}
	}`)
	if !captureResponse.OK {
		t.Fatalf("captureResponse.OK = false, error = %#v", captureResponse.Error)
	}
	capturePayload := captureResponse.Payload.(map[string]any)
	if capturePayload["available"] != true || capturePayload["stored"] != true {
		t.Fatalf("capture payload = %#v, want available+stored", capturePayload)
	}
	entry := capturePayload["entry"].(map[string]any)
	if entry["title"] != "Reconnect leaves stale artifact selected" {
		t.Fatalf("entry.title = %#v, want captured title", entry["title"])
	}
	if entry["run_id"] != run.ID {
		t.Fatalf("entry.run_id = %#v, want %q", entry["run_id"], run.ID)
	}

	listResponse := postControlAction(t, server.URL, `{
		"id":"req_dogfood_list",
		"type":"request",
		"action":"list_dogfood_issues",
		"payload":{"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`","limit":10}
	}`)
	if !listResponse.OK {
		t.Fatalf("listResponse.OK = false, error = %#v", listResponse.Error)
	}
	listPayload := listResponse.Payload.(map[string]any)
	if listPayload["available"] != true {
		t.Fatalf("available = %#v, want true", listPayload["available"])
	}
	if listPayload["count"] != float64(1) {
		t.Fatalf("count = %#v, want 1", listPayload["count"])
	}
	items := listPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	recorded := items[0].(map[string]any)
	if recorded["note"] == "" {
		t.Fatal("recorded note should be preserved")
	}

	eventStream, cancel := events.Subscribe(0)
	defer cancel()
	foundRecordedEvent := false
	for {
		select {
		case event := <-eventStream:
			if event.Event == "dogfood_issue_recorded" {
				foundRecordedEvent = true
				goto doneDogfood
			}
		case <-time.After(2 * time.Second):
			goto doneDogfood
		}
	}
doneDogfood:
	if !foundRecordedEvent {
		t.Fatal("dogfood_issue_recorded event not observed")
	}
}

func TestForegroundLoopReloadsVerbosityAtSafePoint(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	initialCfg := config.Default()
	initialCfg.Verbosity = config.VerbosityQuiet
	if err := config.Save(configPath, initialCfg); err != nil {
		t.Fatalf("config.Save(initial) error = %v", err)
	}

	runtimeManager := runtimecfg.NewManager(configPath, initialCfg)
	store, journalWriter, run, err := createAutoRun(context.Background(), Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     initialCfg,
		ConfigPath: configPath,
		RuntimeCfg: runtimeManager,
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}, "reload verbosity at the next safe point")
	if err != nil {
		t.Fatalf("createAutoRun() error = %v", err)
	}
	defer store.Close()

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     initialCfg,
		ConfigPath: configPath,
		RuntimeCfg: runtimeManager,
		Events:     events,
		Version:    "test",
	}

	var stdout bytes.Buffer
	inv.Stdout = &stdout

	callCount := 0
	restoreRunner := stubBoundedCycleRunner(func(_ context.Context, _ Invocation, _ *state.Store, _ *journal.Journal, currentRun state.Run) (orchestration.Result, error) {
		callCount++
		updatedRun := currentRun
		updatedRun.LatestCheckpoint = state.Checkpoint{
			Sequence:     currentRun.LatestCheckpoint.Sequence + 1,
			Stage:        "planner",
			Label:        "planner_turn_completed",
			SafePause:    true,
			PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn + 1,
			ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
			CreatedAt:    time.Now().UTC(),
		}

		if callCount == 1 {
			updatedCfg := initialCfg
			updatedCfg.Verbosity = config.VerbosityTrace
			if err := config.Save(configPath, updatedCfg); err != nil {
				t.Fatalf("config.Save(updated) error = %v", err)
			}
			return orchestration.Result{
				Run: updatedRun,
				FirstPlannerResult: planner.Result{
					ResponseID: "resp_pause",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomePause,
						Pause:           &planner.PauseOutcome{Reason: "keep going"},
					},
				},
			}, nil
		}

		updatedRun.Status = state.StatusCompleted
		updatedRun.LatestCheckpoint.Label = "planner_declared_complete"
		return orchestration.Result{
			Run: updatedRun,
			FirstPlannerResult: planner.Result{
				ResponseID: "resp_complete",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeComplete,
					Complete:        &planner.CompleteOutcome{Summary: "done"},
				},
			},
		}, nil
	})
	defer restoreRunner()

	if err := executeForegroundLoop(context.Background(), inv, store, journalWriter, run, foregroundLoopMode{
		Command:         "run",
		RunAction:       "created_new_run",
		EventPrefix:     "run",
		InvocationLabel: "run",
		StopFlagKey:     "run.stop_flag_path",
	}); err != nil {
		t.Fatalf("executeForegroundLoop() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "first_planner_result:") {
		t.Fatalf("stdout missing trace planner result after safe-point reload\n%s", stdout.String())
	}

	eventStream, cancel := events.Subscribe(0)
	defer cancel()
	foundVerbosityChanged := false
	for {
		select {
		case event := <-eventStream:
			if event.Event == "verbosity_changed" {
				foundVerbosityChanged = true
				goto done
			}
		case <-time.After(2 * time.Second):
			goto done
		}
	}
done:
	if !foundVerbosityChanged {
		t.Fatal("verbosity_changed event not observed")
	}
}

func TestEngineEventBusEmitsCoreCycleEvents(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	cfg := config.Default()
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     cfg,
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, cfg),
		Events:     events,
		Version:    "test",
	}

	store, journalWriter, run, err := createAutoRun(context.Background(), inv, "emit core V2 engine events")
	if err != nil {
		t.Fatalf("createAutoRun() error = %v", err)
	}
	defer store.Close()

	restoreRunner := stubBoundedCycleRunner(realCycleRunner(
		&commandPlanner{
			results: []planner.Result{
				{
					ResponseID: "resp_execute",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeExecute,
						Execute: &planner.ExecuteOutcome{
							Task:               "perform one bounded executor step",
							AcceptanceCriteria: []string{"bounded step completed"},
						},
					},
				},
				{
					ResponseID: "resp_complete",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "step complete"},
					},
				},
				{
					ResponseID: "resp_complete_extra",
					Output: planner.OutputEnvelope{
						ContractVersion: planner.ContractVersionV1,
						Outcome:         planner.OutcomeComplete,
						Complete:        &planner.CompleteOutcome{Summary: "step complete"},
					},
				},
			},
		},
		engineEventExecutorStub{},
		nil,
	))
	defer restoreRunner()

	eventStream, cancel := events.Subscribe(0)
	defer cancel()

	if err := executeBoundedCycle(context.Background(), inv, store, journalWriter, run, boundedCycleMode{
		Command:   "run",
		RunAction: "created_new_run",
	}); err != nil {
		t.Fatalf("executeBoundedCycle() error = %v", err)
	}

	seen := map[string]bool{}
	required := map[string]bool{
		"run_started":             true,
		"planner_turn_started":    true,
		"planner_turn_completed":  true,
		"executor_turn_started":   true,
		"executor_turn_completed": true,
		"safe_point_reached":      true,
		"run_completed":           true,
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-eventStream:
			seen[event.Event] = true
			missing := false
			for want := range required {
				if !seen[want] {
					missing = true
					break
				}
			}
			if !missing {
				goto doneCoreEvents
			}
		case <-deadline:
			t.Fatalf("timed out waiting for core events, saw: %#v", seen)
		}
	}
doneCoreEvents:

	for _, want := range []string{
		"run_started",
		"planner_turn_started",
		"planner_turn_completed",
		"executor_turn_started",
		"executor_turn_completed",
		"safe_point_reached",
		"run_completed",
	} {
		if !seen[want] {
			t.Fatalf("missing event %q; saw %#v", want, seen)
		}
	}
}

func TestControlServeStartsAndPrintsEndpoints(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	var stdout bytes.Buffer
	inv := Invocation{
		Stdout:     &stdout,
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serveControlServer(ctx, inv, "127.0.0.1:0", func(baseURL string) {
			ready <- baseURL
		})
	}()

	var baseURL string
	select {
	case baseURL = <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control server readiness")
	}

	resp, err := http.Post(baseURL+"/v2/control", "application/json", strings.NewReader(`{
		"id":"req_runtime",
		"type":"request",
		"action":"get_runtime_config",
		"payload":{}
	}`))
	if err != nil {
		t.Fatalf("http.Post(control) error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("control status code = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveControlServer() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control server shutdown")
	}

	if !strings.Contains(stdout.String(), "control.listen: http://") {
		t.Fatalf("stdout missing control.listen\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "/v2/control") {
		t.Fatalf("stdout missing /v2/control endpoint\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "/v2/events") {
		t.Fatalf("stdout missing /v2/events endpoint\n%s", stdout.String())
	}
}

func TestControlDemoStatusUsesRealProtocol(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "inspect control demo status",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.SavePlannerOperatorStatus(context.Background(), run.ID, &state.PlannerOperatorStatus{
		ContractVersion:    "planner.v1",
		OperatorMessage:    "Implementing the next bounded slice.",
		CurrentFocus:       "demo status rendering",
		NextIntendedStep:   "print the live status snapshot through the protocol client",
		WhyThisStep:        "the demo client should prove the control protocol is usable now.",
		ProgressPercent:    31,
		ProgressConfidence: "medium",
		ProgressBasis:      "server and durable state exist; the remaining work is client rendering.",
	}); err != nil {
		t.Fatalf("SavePlannerOperatorStatus() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	err = runControlDemoStatus(context.Background(), Invocation{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	}, []string{"--addr", server.URL, "--run-id", run.ID})
	if err != nil {
		t.Fatalf("runControlDemoStatus() error = %v", err)
	}

	for _, want := range []string{
		`"operator_message": "Implementing the next bounded slice."`,
		`"contract_version": "planner.v1"`,
		`"next_intended_step": "print the live status snapshot through the protocol client"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("demo status output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestControlDemoInjectQueuesMessageThroughRealProtocol(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "queue a control message through demo client",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	events := activity.NewBroker(activity.DefaultHistoryLimit)
	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     events,
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	var stdout bytes.Buffer
	err = runControlDemoInject(context.Background(), Invocation{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	}, []string{"--addr", server.URL, "--run-id", run.ID, "--message", "Make that wall red, not blue."})
	if err != nil {
		t.Fatalf("runControlDemoInject() error = %v", err)
	}
	if !strings.Contains(stdout.String(), `"status": "queued"`) {
		t.Fatalf("demo inject output missing queued status\n%s", stdout.String())
	}

	store, err = openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	messages, err := store.ListControlMessages(context.Background(), run.ID, state.ControlMessageQueued, 10)
	if err != nil {
		t.Fatalf("ListControlMessages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("queued messages = %d, want 1", len(messages))
	}
	if messages[0].RawText != "Make that wall red, not blue." {
		t.Fatalf("raw_text = %q, want injected message", messages[0].RawText)
	}
}

func TestControlDemoEventsPrintsStreamedEvents(t *testing.T) {
	t.Parallel()

	broker := activity.NewBroker(activity.DefaultHistoryLimit)
	server := httptest.NewServer(control.Server{
		Broker: broker,
	}.Handler())
	defer server.Close()

	broker.Publish("control_message_queued", map[string]any{
		"run_id":             "run_events",
		"control_message_id": "control_123",
	})

	var stdout bytes.Buffer
	err := runControlDemoEvents(context.Background(), Invocation{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	}, []string{"--addr", server.URL, "--run-id", "run_events", "--max-events", "1"})
	if err != nil {
		t.Fatalf("runControlDemoEvents() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "event=control_message_queued") {
		t.Fatalf("demo events output missing queued event\n%s", stdout.String())
	}
}

func TestControlDemoSetVerbosityAndStopFlagUseRealProtocol(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	var verbosityOut bytes.Buffer
	if err := runControlDemoSetVerbosity(context.Background(), Invocation{
		Stdout: &verbosityOut,
		Stderr: &bytes.Buffer{},
	}, []string{"--addr", server.URL, "--verbosity", "trace"}); err != nil {
		t.Fatalf("runControlDemoSetVerbosity() error = %v", err)
	}
	if !strings.Contains(verbosityOut.String(), `"verbosity": "trace"`) {
		t.Fatalf("set-verbosity output missing updated verbosity\n%s", verbosityOut.String())
	}

	var stopOut bytes.Buffer
	if err := runControlDemoStopSafe(context.Background(), Invocation{
		Stdout: &stopOut,
		Stderr: &bytes.Buffer{},
	}, []string{"--addr", server.URL, "--reason", "operator_requested_safe_stop"}); err != nil {
		t.Fatalf("runControlDemoStopSafe() error = %v", err)
	}
	if !strings.Contains(stopOut.String(), `"present": true`) {
		t.Fatalf("stop-safe output missing present=true\n%s", stopOut.String())
	}

	var clearOut bytes.Buffer
	if err := runControlDemoClearStop(context.Background(), Invocation{
		Stdout: &clearOut,
		Stderr: &bytes.Buffer{},
	}, []string{"--addr", server.URL}); err != nil {
		t.Fatalf("runControlDemoClearStop() error = %v", err)
	}
	if !strings.Contains(clearOut.String(), `"present": false`) {
		t.Fatalf("clear-stop output missing present=false\n%s", clearOut.String())
	}
}

func TestLocalControlServerArtifactActionsExposeCurrentRunArtifacts(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "inspect artifact browser protocol actions",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	artifactPath := filepath.Join(repoRoot, ".orchestrator", "artifacts", "context", run.ID, "collected_context_test.json")
	mustWriteFile(t, artifactPath, "{\n  \"focus\": \"inspect artifact browser wiring\"\n}\n")
	if err := store.SaveCollectedContext(context.Background(), run.ID, &state.CollectedContextState{
		ArtifactPath:    ".orchestrator/artifacts/context/" + run.ID + "/collected_context_test.json",
		ArtifactPreview: "{\"focus\":\"inspect artifact browser wiring\"}",
		Focus:           "artifact browser plumbing",
	}); err != nil {
		t.Fatalf("SaveCollectedContext() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	listResponse := postControlAction(t, server.URL, `{
		"id":"req_artifacts",
		"type":"request",
		"action":"list_recent_artifacts",
		"payload":{"run_id":"`+run.ID+`","limit":8}
	}`)
	if !listResponse.OK {
		t.Fatalf("list_recent_artifacts response.OK = false, error = %#v", listResponse.Error)
	}
	listPayload := listResponse.Payload.(map[string]any)
	if listPayload["latest_path"] != ".orchestrator/artifacts/context/"+run.ID+"/collected_context_test.json" {
		t.Fatalf("latest_path = %#v, want persisted run artifact path", listPayload["latest_path"])
	}
	items, ok := listPayload["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("items = %#v, want at least one artifact", listPayload["items"])
	}

	getResponse := postControlAction(t, server.URL, `{
		"id":"req_artifact",
		"type":"request",
		"action":"get_artifact",
		"payload":{"artifact_path":".orchestrator/artifacts/context/`+run.ID+`/collected_context_test.json"}
	}`)
	if !getResponse.OK {
		t.Fatalf("get_artifact response.OK = false, error = %#v", getResponse.Error)
	}
	getPayload := getResponse.Payload.(map[string]any)
	if getPayload["available"] != true {
		t.Fatalf("available = %#v, want true", getPayload["available"])
	}
	if getPayload["content_type"] != "application/json" {
		t.Fatalf("content_type = %#v, want application/json", getPayload["content_type"])
	}
	if !strings.Contains(getPayload["content"].(string), `"focus": "inspect artifact browser wiring"`) {
		t.Fatalf("content = %q, want saved artifact body", getPayload["content"].(string))
	}
}

func TestLocalControlServerContractFileActionsOpenAndSaveCanonicalFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	listResponse := postControlAction(t, server.URL, `{
		"id":"req_contracts",
		"type":"request",
		"action":"list_contract_files",
		"payload":{"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`"}
	}`)
	if !listResponse.OK {
		t.Fatalf("list_contract_files response.OK = false, error = %#v", listResponse.Error)
	}
	listPayload := listResponse.Payload.(map[string]any)
	if listPayload["count"] != float64(5) {
		t.Fatalf("count = %#v, want 5 canonical files", listPayload["count"])
	}

	openResponse := postControlAction(t, server.URL, `{
		"id":"req_contract_open",
		"type":"request",
		"action":"open_contract_file",
		"payload":{"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`","path":".orchestrator/brief.md"}
	}`)
	if !openResponse.OK {
		t.Fatalf("open_contract_file response.OK = false, error = %#v", openResponse.Error)
	}
	openPayload := openResponse.Payload.(map[string]any)
	if openPayload["exists"] != true {
		t.Fatalf("exists = %#v, want true", openPayload["exists"])
	}
	if openPayload["content"] != "brief\n" {
		t.Fatalf("content = %#v, want seeded brief.md content", openPayload["content"])
	}

	expectedMTime := openPayload["modified_at"].(string)
	saveRequestBody, err := json.Marshal(map[string]any{
		"id":     "req_contract_save",
		"type":   "request",
		"action": "save_contract_file",
		"payload": map[string]any{
			"repo_path":      repoRoot,
			"path":           ".orchestrator/brief.md",
			"content":        "updated brief for shell save path\n",
			"expected_mtime": expectedMTime,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	saveResponse := postControlAction(t, server.URL, string(saveRequestBody))
	if !saveResponse.OK {
		t.Fatalf("save_contract_file response.OK = false, error = %#v", saveResponse.Error)
	}
	savePayload := saveResponse.Payload.(map[string]any)
	if savePayload["saved"] != true {
		t.Fatalf("saved = %#v, want true", savePayload["saved"])
	}

	updated := readFileString(t, filepath.Join(repoRoot, ".orchestrator", "brief.md"))
	if updated != "updated brief for shell save path\n" {
		t.Fatalf("saved file = %q, want updated content", updated)
	}
}

func TestLocalControlServerListWorkersExposesWorkerPanelData(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "surface worker panel data",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	worker, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "code-survey",
		WorkerStatus:  state.WorkerStatusApprovalRequired,
		AssignedScope: "inspect the UI shell",
		WorktreePath:  filepath.Join(repoRoot+".workers", "code-survey"),
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}
	worker.ExecutorThreadID = "thread_worker_panel"
	worker.ExecutorTurnID = "turn_worker_panel"
	worker.ExecutorInterruptible = true
	worker.ExecutorSteerable = false
	worker.ExecutorApprovalKind = "command_execution"
	worker.ExecutorApprovalPreview = "go test ./..."
	worker.ExecutorLastControlAction = "approved"
	worker.WorkerTaskSummary = "inspect the shell layout"
	worker.WorkerResultSummary = "survey completed"
	worker.UpdatedAt = time.Now().UTC()
	if err := store.SaveWorker(context.Background(), worker); err != nil {
		t.Fatalf("SaveWorker() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_workers",
		"type":"request",
		"action":"list_workers",
		"payload":{"run_id":"`+run.ID+`","limit":10}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}

	payload := response.Payload.(map[string]any)
	if payload["count"] != float64(1) {
		t.Fatalf("count = %#v, want 1", payload["count"])
	}
	counts := payload["counts_by_status"].(map[string]any)
	if counts["approval_required"] != float64(1) {
		t.Fatalf("counts_by_status.approval_required = %#v, want 1", counts["approval_required"])
	}
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0].(map[string]any)
	if item["worker_name"] != "code-survey" {
		t.Fatalf("worker_name = %#v, want code-survey", item["worker_name"])
	}
	if item["worktree_path"] == "" {
		t.Fatal("worktree_path should be present for worker visibility")
	}
	if item["approval_required"] != true {
		t.Fatalf("approval_required = %#v, want true", item["approval_required"])
	}
	if item["executor_thread_id"] != "thread_worker_panel" {
		t.Fatalf("executor_thread_id = %#v, want thread_worker_panel", item["executor_thread_id"])
	}
}

func TestLocalControlServerCreateWorkerActionCreatesIsolatedWorker(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	installFakeGitTooling(t)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "create worker from shell control protocol",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:  1,
			Stage:     "bootstrap",
			Label:     "run_initialized",
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_create_worker",
		"type":"request",
		"action":"create_worker",
		"payload":{"run_id":"`+run.ID+`","name":"code-survey","scope":"inspect shell layout"}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}

	payload := response.Payload.(map[string]any)
	if payload["created"] != true {
		t.Fatalf("created = %#v, want true", payload["created"])
	}
	worker := payload["worker"].(map[string]any)
	if worker["worker_name"] != "code-survey" {
		t.Fatalf("worker_name = %#v, want code-survey", worker["worker_name"])
	}
	if worker["status"] != "idle" {
		t.Fatalf("status = %#v, want idle", worker["status"])
	}
	worktreePath := worker["worktree_path"].(string)
	if worktreePath == "" {
		t.Fatal("worktree_path should be populated")
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("os.Stat(worktree_path) error = %v", err)
	}
}

func TestLocalControlServerDispatchWorkerActionUsesWorkerWorktree(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	worktreePath := filepath.Join(t.TempDir(), "code-survey")
	mustMkdirAll(t, worktreePath)

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "dispatch worker through shell protocol",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:  1,
			Stage:     "bootstrap",
			Label:     "run_initialized",
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	worker, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "code-survey",
		WorkerStatus:  state.WorkerStatusIdle,
		AssignedScope: "inspect shell layout",
		WorktreePath:  worktreePath,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	originalFactory := newWorkerExecutorClient
	defer func() { newWorkerExecutorClient = originalFactory }()
	var captured executor.TurnRequest
	newWorkerExecutorClient = func(version string) (workerExecutor, error) {
		return workerExecutorStub(func(ctx context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
			captured = req
			return executor.TurnResult{
				Transport:     executor.TransportAppServer,
				RunID:         req.RunID,
				ThreadID:      "thread_worker_dispatch",
				ThreadPath:    req.RepoPath,
				TurnID:        "turn_worker_dispatch",
				TurnStatus:    executor.TurnStatusCompleted,
				Interruptible: true,
				Steerable:     true,
				CompletedAt:   time.Now().UTC(),
				FinalMessage:  "worker dispatch complete",
			}, nil
		}), nil
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_dispatch_worker",
		"type":"request",
		"action":"dispatch_worker",
		"payload":{"worker_id":"`+worker.ID+`","prompt":"Inspect the shell and summarize the next bounded edit."}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}
	if captured.RepoPath != worktreePath {
		t.Fatalf("executor request repo path = %q, want worker worktree %q", captured.RepoPath, worktreePath)
	}

	payload := response.Payload.(map[string]any)
	if payload["dispatched"] != true {
		t.Fatalf("dispatched = %#v, want true", payload["dispatched"])
	}
	workerPayload := payload["worker"].(map[string]any)
	if workerPayload["status"] != "completed" {
		t.Fatalf("status = %#v, want completed", workerPayload["status"])
	}
	if workerPayload["executor_thread_id"] != "thread_worker_dispatch" {
		t.Fatalf("executor_thread_id = %#v, want thread_worker_dispatch", workerPayload["executor_thread_id"])
	}
}

func TestLocalControlServerRemoveWorkerActionRemovesIdleWorker(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	installFakeGitTooling(t)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "remove worker through shell protocol",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:  1,
			Stage:     "bootstrap",
			Label:     "run_initialized",
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	manager := workerctl.NewManager(repoRoot, layout.WorkersDir)
	plannedPath, err := manager.PlannedPath("remove-me")
	if err != nil {
		t.Fatalf("PlannedPath() error = %v", err)
	}
	worker, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "remove-me",
		WorkerStatus:  state.WorkerStatusIdle,
		AssignedScope: "temporary worker",
		WorktreePath:  plannedPath,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}
	if _, err := manager.Create(context.Background(), worker.WorkerName); err != nil {
		t.Fatalf("manager.Create() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_remove_worker",
		"type":"request",
		"action":"remove_worker",
		"payload":{"worker_id":"`+worker.ID+`"}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}
	payload := response.Payload.(map[string]any)
	if payload["removed"] != true {
		t.Fatalf("removed = %#v, want true", payload["removed"])
	}

	store, err = openExistingStore(layout)
	if err != nil {
		t.Fatalf("openExistingStore() error = %v", err)
	}
	defer store.Close()
	if _, found, err := store.GetWorker(context.Background(), worker.ID); err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	} else if found {
		t.Fatal("worker should be deleted after remove_worker")
	}
}

func TestLocalControlServerIntegrateWorkersActionBuildsArtifact(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	workerRoot := t.TempDir()
	workerOnePath := filepath.Join(workerRoot, "worker-one")
	workerTwoPath := filepath.Join(workerRoot, "worker-two")
	writeRepoMarkerFiles(t, workerOnePath)
	writeRepoMarkerFiles(t, workerTwoPath)
	mustWriteFile(t, filepath.Join(workerOnePath, "worker-one-output", "one.txt"), "worker one output\n")
	mustWriteFile(t, filepath.Join(workerTwoPath, "worker-two-output", "two.txt"), "worker two output\n")

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "integrate worker outputs through shell protocol",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:  1,
			Stage:     "bootstrap",
			Label:     "run_initialized",
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	workerOne, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "worker-one",
		WorkerStatus:  state.WorkerStatusCompleted,
		AssignedScope: "docs one",
		WorktreePath:  workerOnePath,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateWorker(workerOne) error = %v", err)
	}
	workerTwo, err := store.CreateWorker(context.Background(), state.CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "worker-two",
		WorkerStatus:  state.WorkerStatusCompleted,
		AssignedScope: "docs two",
		WorktreePath:  workerTwoPath,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateWorker(workerTwo) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_integrate_workers",
		"type":"request",
		"action":"integrate_workers",
		"payload":{"worker_ids":["`+workerOne.ID+`","`+workerTwo.ID+`"]}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}

	payload := response.Payload.(map[string]any)
	if payload["worker_count"] != float64(2) {
		t.Fatalf("worker_count = %#v, want 2", payload["worker_count"])
	}
	if _, ok := payload["conflict_count"].(float64); !ok {
		t.Fatalf("conflict_count = %#v, want numeric truthful conflict count", payload["conflict_count"])
	}
	artifactPath := payload["artifact_path"].(string)
	if artifactPath == "" {
		t.Fatal("artifact_path should be populated")
	}
	if strings.TrimSpace(payload["integration_preview"].(string)) == "" {
		t.Fatal("integration_preview should be populated")
	}
	_, absoluteArtifactPath, err := resolveArtifactPath(repoRoot, artifactPath)
	if err != nil {
		t.Fatalf("resolveArtifactPath() error = %v", err)
	}
	if _, err := os.Stat(absoluteArtifactPath); err != nil {
		t.Fatalf("os.Stat(artifact) error = %v", err)
	}
}

func TestLocalControlServerStatusSnapshotSurfacesApprovalCenterState(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "surface approval center data",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     2,
			Stage:        "planner",
			Label:        "planner_turn_post_executor",
			SafePause:    true,
			PlannerTurn:  1,
			ExecutorTurn: 1,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.SaveExecutorState(context.Background(), run.ID, state.ExecutorState{
		Transport:   string(executor.TransportAppServer),
		ThreadID:    "thread_approval_status",
		ThreadPath:  "thread.jsonl",
		TurnID:      "turn_approval_status",
		TurnStatus:  string(executor.TurnStatusApprovalRequired),
		LastMessage: "waiting for approval",
		Approval: &state.ExecutorApproval{
			State:      string(executor.ApprovalStateRequired),
			Kind:       string(executor.ApprovalKindCommandExecution),
			RequestID:  "req_status",
			ApprovalID: "approval_status",
			ItemID:     "item_status",
			Command:    "go test ./...",
			CWD:        repoRoot,
		},
	}); err != nil {
		t.Fatalf("SaveExecutorState() error = %v", err)
	}
	if err := store.SaveLatestStopReason(context.Background(), run.ID, orchestration.StopReasonExecutorApprovalReq); err != nil {
		t.Fatalf("SaveLatestStopReason() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_status_approval",
		"type":"request",
		"action":"get_status_snapshot",
		"payload":{"run_id":"`+run.ID+`"}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}

	payload := response.Payload.(map[string]any)
	approval := payload["approval"].(map[string]any)
	if approval["present"] != true {
		t.Fatalf("approval.present = %#v, want true", approval["present"])
	}
	if approval["state"] != "required" {
		t.Fatalf("approval.state = %#v, want required", approval["state"])
	}
	if approval["kind"] != "command_execution" {
		t.Fatalf("approval.kind = %#v, want command_execution", approval["kind"])
	}
	if approval["executor_turn_id"] != "turn_approval_status" {
		t.Fatalf("approval.executor_turn_id = %#v, want turn_approval_status", approval["executor_turn_id"])
	}
	if approval["command"] != "go test ./..." {
		t.Fatalf("approval.command = %#v, want go test ./...", approval["command"])
	}
}

func TestLocalControlServerApproveExecutorUsesRealMechanicalPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	store, _, err := ensureRuntime(context.Background(), layout)
	if err != nil {
		t.Fatalf("ensureRuntime() error = %v", err)
	}
	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: repoRoot,
		Goal:     "approve executor through the control protocol",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     2,
			Stage:        "planner",
			Label:        "planner_turn_post_executor",
			SafePause:    true,
			PlannerTurn:  1,
			ExecutorTurn: 1,
			CreatedAt:    time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := store.SaveExecutorState(context.Background(), run.ID, state.ExecutorState{
		Transport:   string(executor.TransportAppServer),
		ThreadID:    "thread_approval_protocol",
		ThreadPath:  "thread.jsonl",
		TurnID:      "turn_approval_protocol",
		TurnStatus:  string(executor.TurnStatusApprovalRequired),
		LastMessage: "waiting for approval",
		Approval: &state.ExecutorApproval{
			State:      string(executor.ApprovalStateRequired),
			Kind:       string(executor.ApprovalKindCommandExecution),
			RequestID:  "req_protocol",
			ApprovalID: "approval_protocol",
			ItemID:     "item_protocol",
			Command:    "go test ./...",
			CWD:        repoRoot,
		},
	}); err != nil {
		t.Fatalf("SaveExecutorState() error = %v", err)
	}
	if err := store.SaveLatestStopReason(context.Background(), run.ID, orchestration.StopReasonExecutorApprovalReq); err != nil {
		t.Fatalf("SaveLatestStopReason() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restoreClient := stubExecutorControlClient(&fakeExecutorControlClient{})
	defer restoreClient()

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_approve_executor",
		"type":"request",
		"action":"approve_executor",
		"payload":{"run_id":"`+run.ID+`"}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}

	updatedRun := latestRunForLayout(t, layout)
	if updatedRun.ExecutorApproval == nil || updatedRun.ExecutorApproval.State != string(executor.ApprovalStateGranted) {
		t.Fatalf("ExecutorApproval = %#v, want granted", updatedRun.ExecutorApproval)
	}
	if updatedRun.ExecutorLastControl == nil || updatedRun.ExecutorLastControl.Action != string(executor.ControlActionApprove) {
		t.Fatalf("ExecutorLastControl = %#v, want approved", updatedRun.ExecutorLastControl)
	}

	payload := response.Payload.(map[string]any)
	if payload["state"] != "granted" {
		t.Fatalf("response approval state = %#v, want granted", payload["state"])
	}
	if payload["last_control_action"] != "approved" {
		t.Fatalf("response last_control_action = %#v, want approved", payload["last_control_action"])
	}
}

type engineEventExecutorStub struct{}

func (engineEventExecutorStub) Execute(_ context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	return executor.TurnResult{
		Transport:    executor.TransportAppServer,
		ThreadID:     "thread_engine_event",
		ThreadPath:   req.RepoPath,
		TurnID:       "turn_engine_event",
		TurnStatus:   executor.TurnStatusCompleted,
		FinalMessage: "completed bounded executor step",
		CompletedAt:  time.Now().UTC(),
	}, nil
}

func completedControlRunResult(ctx context.Context, store *state.Store, currentRun state.Run, responseID string) (orchestration.Result, error) {
	checkpoint := state.Checkpoint{
		Sequence:     currentRun.LatestCheckpoint.Sequence + 1,
		Stage:        "planner",
		Label:        "planner_declared_complete",
		SafePause:    true,
		PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn + 1,
		ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.SavePlannerCompletion(ctx, currentRun.ID, responseID, checkpoint); err != nil {
		return orchestration.Result{}, err
	}
	updatedRun, found, err := store.GetRun(ctx, currentRun.ID)
	if err != nil {
		return orchestration.Result{}, err
	}
	if !found {
		return orchestration.Result{}, fmt.Errorf("run %s was not found after completion", currentRun.ID)
	}
	return orchestration.Result{
		Run: updatedRun,
		FirstPlannerResult: planner.Result{
			ResponseID: responseID,
			Output: planner.OutputEnvelope{
				ContractVersion: planner.ContractVersionV1,
				Outcome:         planner.OutcomeComplete,
				Complete:        &planner.CompleteOutcome{Summary: "control run completed"},
			},
		},
	}, nil
}

func waitForActivityEvent(t *testing.T, events *activity.Broker, eventName string, runID string) activity.Event {
	t.Helper()

	stream, cancel := events.Subscribe(0)
	defer cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-stream:
			if event.Event != eventName {
				continue
			}
			if strings.TrimSpace(runID) != "" {
				payloadRunID, _ := event.Payload["run_id"].(string)
				if payloadRunID != runID {
					continue
				}
			}
			return event
		case <-deadline:
			t.Fatalf("timed out waiting for activity event %q run_id=%q", eventName, runID)
		}
	}
}

func postControlAction(t *testing.T, baseURL string, payload string) control.ResponseEnvelope {
	t.Helper()

	resp, err := http.Post(baseURL+"/v2/control", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	defer resp.Body.Close()

	var envelope control.ResponseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("json.NewDecoder() error = %v", err)
	}
	return envelope
}

func filepathJoin(t *testing.T, root string, name string) string {
	t.Helper()
	return filepath.Join(root, name)
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", path, err)
	}
	return string(content)
}
