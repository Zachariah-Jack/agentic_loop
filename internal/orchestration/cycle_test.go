package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestShouldDispatchExecutor(t *testing.T) {
	t.Parallel()

	if !ShouldDispatchExecutor(planner.OutputEnvelope{
		Outcome: planner.OutcomeExecute,
		Execute: &planner.ExecuteOutcome{Task: "implement a bounded slice", AcceptanceCriteria: []string{"it works"}},
	}) {
		t.Fatal("execute outcome should dispatch")
	}

	if ShouldDispatchExecutor(planner.OutputEnvelope{
		Outcome: planner.OutcomeAskHuman,
		AskHuman: &planner.AskHumanOutcome{
			Question: "Need clarification?",
		},
	}) {
		t.Fatal("non-execute outcome should not dispatch")
	}
}

func TestRenderExecutorPrompt(t *testing.T) {
	t.Parallel()

	prompt, err := RenderExecutorPrompt("ship the smallest real cycle", planner.OutputEnvelope{
		Outcome: planner.OutcomeExecute,
		Execute: &planner.ExecuteOutcome{
			Task:               "wire one executor dispatch",
			AcceptanceCriteria: []string{"planner execute dispatches once", "non-execute does not dispatch"},
			WriteScope:         []string{"internal/cli/run.go", "internal/orchestration/cycle.go"},
		},
	})
	if err != nil {
		t.Fatalf("RenderExecutorPrompt() error = %v", err)
	}

	for _, want := range []string{
		"ship the smallest real cycle",
		"wire one executor dispatch",
		"planner execute dispatches once",
		"internal/cli/run.go",
		"Do not choose a new task.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n%s", want, prompt)
		}
	}
}

func TestRenderExecutorPromptRejectsNonExecute(t *testing.T) {
	t.Parallel()

	if _, err := RenderExecutorPrompt("goal", planner.OutputEnvelope{
		Outcome: planner.OutcomePause,
		Pause:   &planner.PauseOutcome{Reason: "wait"},
	}); err == nil {
		t.Fatal("expected non-execute output to be rejected")
	}
}

func TestBuildExecutorCheckpoint(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	checkpoint := BuildExecutorCheckpoint(state.Checkpoint{
		Sequence:     2,
		PlannerTurn:  1,
		ExecutorTurn: 0,
	}, "executor_turn_completed", at)

	if checkpoint.Sequence != 3 {
		t.Fatalf("Sequence = %d, want 3", checkpoint.Sequence)
	}
	if checkpoint.Stage != "executor" {
		t.Fatalf("Stage = %q, want executor", checkpoint.Stage)
	}
	if checkpoint.Label != "executor_turn_completed" {
		t.Fatalf("Label = %q", checkpoint.Label)
	}
	if !checkpoint.SafePause {
		t.Fatal("SafePause should be true")
	}
	if checkpoint.ExecutorTurn != 1 {
		t.Fatalf("ExecutorTurn = %d, want 1", checkpoint.ExecutorTurn)
	}
	if checkpoint.CreatedAt != at {
		t.Fatalf("CreatedAt = %s, want %s", checkpoint.CreatedAt, at)
	}
}

func TestBuildExecutorResultInput(t *testing.T) {
	t.Parallel()

	success := true
	summary := BuildExecutorResultInput(state.Run{
		ExecutorThreadID:    "thr_123",
		ExecutorLastSuccess: &success,
		ExecutorLastMessage: "Implemented the bounded slice.",
	})
	if summary == nil {
		t.Fatal("BuildExecutorResultInput() = nil, want summary")
	}
	if !summary.Success {
		t.Fatal("summary.Success = false, want true")
	}
	if summary.ThreadID != "thr_123" {
		t.Fatalf("summary.ThreadID = %q, want thr_123", summary.ThreadID)
	}
	if summary.FinalMessage != "Implemented the bounded slice." {
		t.Fatalf("summary.FinalMessage = %q", summary.FinalMessage)
	}
}

func TestBuildCollectedContextInput(t *testing.T) {
	t.Parallel()

	summary := BuildCollectedContextInput(state.Run{
		CollectedContext: &state.CollectedContextState{
			Focus:     "Inspect planner inputs",
			Questions: []string{"What did the collector read?"},
			Results: []state.CollectedContextResult{
				{
					RequestedPath: "internal/orchestration",
					ResolvedPath:  `D:\Projects\agentic_loop\internal\orchestration`,
					Kind:          "dir",
					Entries:       []string{"cycle.go", "cycle_test.go"},
				},
			},
		},
	})
	if summary == nil {
		t.Fatal("BuildCollectedContextInput() = nil, want summary")
	}
	if summary.Focus != "Inspect planner inputs" {
		t.Fatalf("summary.Focus = %q", summary.Focus)
	}
	if len(summary.Results) != 1 || summary.Results[0].Kind != "dir" {
		t.Fatalf("summary.Results = %#v", summary.Results)
	}
}

func TestCycleRunOnceExecuteDispatchesExactlyOneExecutorTurnAndPerformsPostExecutorPlannerTurn(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_123",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeExecute,
					Execute: &planner.ExecuteOutcome{
						Task:               "implement the bounded planner to executor slice",
						AcceptanceCriteria: []string{"dispatch one executor turn", "persist the result"},
						WriteScope:         []string{"internal/cli/run.go", "internal/orchestration/cycle.go"},
					},
				},
			},
			{
				ResponseID: "resp_456",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeAskHuman,
					AskHuman: &planner.AskHumanOutcome{
						Question: "Should the next slice add resume semantics for the second planner turn?",
						Context:  "The bounded run now persists a post-executor planner outcome.",
					},
				},
			},
		},
	}

	startedAt := time.Date(2026, 4, 19, 18, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Minute)
	fakeExecutor := &stubExecutor{
		result: executor.TurnResult{
			Transport:    executor.TransportAppServer,
			RunID:        run.ID,
			ThreadID:     "thr_123",
			ThreadPath:   filepath.FromSlash("C:/Users/test/.codex/threads/thr_123.jsonl"),
			TurnID:       "turn_123",
			TurnStatus:   executor.TurnStatusCompleted,
			StartedAt:    startedAt,
			CompletedAt:  completedAt,
			FinalMessage: "Implemented the bounded planner to executor slice and persisted the executor result.",
		},
	}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if !result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = false, want true")
	}
	if fakeExecutor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", fakeExecutor.calls)
	}
	if fakePlanner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", fakePlanner.calls)
	}
	if fakePlanner.inputs[0].Capabilities.Executor != planner.CapabilityAvailable {
		t.Fatalf("first planner input executor capability = %q, want available", fakePlanner.inputs[0].Capabilities.Executor)
	}
	if fakePlanner.inputs[0].ExecutorResult != nil {
		t.Fatal("first planner input unexpectedly included executor result")
	}
	if fakePlanner.previousResponseIDs[0] != "" {
		t.Fatalf("first planner previous_response_id = %q, want empty", fakePlanner.previousResponseIDs[0])
	}
	if fakePlanner.previousResponseIDs[1] != "resp_123" {
		t.Fatalf("second planner previous_response_id = %q, want resp_123", fakePlanner.previousResponseIDs[1])
	}
	if fakePlanner.inputs[1].ExecutorResult == nil {
		t.Fatal("second planner input missing executor result summary")
	}
	if !fakePlanner.inputs[1].ExecutorResult.Success {
		t.Fatal("second planner executor result success = false, want true")
	}
	if fakePlanner.inputs[1].ExecutorResult.ThreadID != "thr_123" {
		t.Fatalf("second planner executor result thread_id = %q, want thr_123", fakePlanner.inputs[1].ExecutorResult.ThreadID)
	}
	if fakePlanner.inputs[1].ExecutorResult.FinalMessage != fakeExecutor.result.FinalMessage {
		t.Fatalf("second planner executor result final_message = %q", fakePlanner.inputs[1].ExecutorResult.FinalMessage)
	}
	if !strings.Contains(fakeExecutor.lastRequest.Prompt, "implement the bounded planner to executor slice") {
		t.Fatalf("executor prompt missing task:\n%s", fakeExecutor.lastRequest.Prompt)
	}

	if result.PostExecutorPlannerTurn == nil {
		t.Fatal("PostExecutorPlannerTurn = nil, want second planner result")
	}
	if result.SecondPlannerTurn == nil {
		t.Fatal("SecondPlannerTurn = nil, want second planner result")
	}
	if result.Run.PreviousResponseID != "resp_456" {
		t.Fatalf("PreviousResponseID = %q, want resp_456", result.Run.PreviousResponseID)
	}
	if result.Run.LatestCheckpoint.Stage != "planner" {
		t.Fatalf("LatestCheckpoint.Stage = %q, want planner", result.Run.LatestCheckpoint.Stage)
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_post_executor" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_post_executor", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.LatestCheckpoint.Sequence != 4 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 4", result.Run.LatestCheckpoint.Sequence)
	}
	if result.Run.ExecutorTransport != string(executor.TransportAppServer) {
		t.Fatalf("ExecutorTransport = %q", result.Run.ExecutorTransport)
	}
	if result.Run.ExecutorTurnStatus != string(executor.TurnStatusCompleted) {
		t.Fatalf("ExecutorTurnStatus = %q", result.Run.ExecutorTurnStatus)
	}
	if result.Run.ExecutorLastSuccess == nil || !*result.Run.ExecutorLastSuccess {
		t.Fatalf("ExecutorLastSuccess = %#v, want true", result.Run.ExecutorLastSuccess)
	}
	if result.Run.ExecutorLastMessage != fakeExecutor.result.FinalMessage {
		t.Fatalf("ExecutorLastMessage = %q, want final message", result.Run.ExecutorLastMessage)
	}

	events, err := journalWriter.ReadRecent(run.ID, 12)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}

	for _, want := range []string{
		"planner.turn.completed",
		"executor.turn.dispatched",
		"executor.turn.completed",
	} {
		if !containsEventType(events, want) {
			t.Fatalf("journal missing %q event", want)
		}
	}
	if countEventType(events, "planner.turn.completed") != 2 {
		t.Fatalf("planner.turn.completed count = %d, want 2", countEventType(events, "planner.turn.completed"))
	}
}

func TestCycleRunOncePersistsAndSurfacesNonExecuteOutcomeWithoutDispatch(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_ask",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeAskHuman,
					AskHuman: &planner.AskHumanOutcome{
						Question: "Which runtime invariant should be implemented first?",
						Context:  "A bounded executor turn is ready, but the next slice needs direction.",
					},
				},
			},
		},
	}
	fakeExecutor := &stubExecutor{}

	cycle := Cycle{
		Store:    store,
		Journal:  journalWriter,
		Planner:  fakePlanner,
		Executor: fakeExecutor,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = true, want false")
	}
	if fakeExecutor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", fakeExecutor.calls)
	}
	if result.Run.PreviousResponseID != "resp_ask" {
		t.Fatalf("PreviousResponseID = %q, want resp_ask", result.Run.PreviousResponseID)
	}
	if result.Run.LatestCheckpoint.Stage != "planner" {
		t.Fatalf("LatestCheckpoint.Stage = %q, want planner", result.Run.LatestCheckpoint.Stage)
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_completed" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_completed", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.LatestCheckpoint.Sequence != 2 {
		t.Fatalf("LatestCheckpoint.Sequence = %d, want 2", result.Run.LatestCheckpoint.Sequence)
	}
	if result.Run.ExecutorTurnStatus != "" {
		t.Fatalf("ExecutorTurnStatus = %q, want empty", result.Run.ExecutorTurnStatus)
	}
	if result.PostExecutorPlannerTurn != nil {
		t.Fatal("PostExecutorPlannerTurn should be nil for non-execute outcome")
	}
	if result.SecondPlannerTurn != nil {
		t.Fatal("SecondPlannerTurn should be nil for non-execute outcome")
	}

	events, err := journalWriter.ReadRecent(run.ID, 10)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}

	if containsEventType(events, "executor.turn.dispatched") {
		t.Fatal("journal unexpectedly recorded executor dispatch for non-execute outcome")
	}
	if !containsEventType(events, "planner.turn.completed") {
		t.Fatal("journal missing planner.turn.completed event")
	}
}

func TestCycleRunOnceCollectContextPersistsResultsAndPerformsSecondPlannerTurn(t *testing.T) {
	t.Parallel()

	store, journalWriter, run := newTestRuntime(t)
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := os.Mkdir(filepath.Join(run.RepoPath, "docs"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.RepoPath, "docs", "note.txt"), []byte("bounded context preview"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(run.RepoPath, "internal"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(run.RepoPath, "internal", "keep.go"), []byte("package internal"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	fakePlanner := &stubPlanner{
		results: []planner.Result{
			{
				ResponseID: "resp_collect",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomeCollectContext,
					CollectContext: &planner.CollectContextOutcome{
						Focus:     "Inspect the requested repo paths.",
						Questions: []string{"What is inside docs and internal?"},
						Paths:     []string{"docs/note.txt", "internal", "missing.txt"},
					},
				},
			},
			{
				ResponseID: "resp_pause",
				Output: planner.OutputEnvelope{
					ContractVersion: planner.ContractVersionV1,
					Outcome:         planner.OutcomePause,
					Pause: &planner.PauseOutcome{
						Reason: "Collected context is enough for the next bounded slice.",
					},
				},
			},
		},
	}

	cycle := Cycle{
		Store:   store,
		Journal: journalWriter,
		Planner: fakePlanner,
	}

	result, err := cycle.RunOnce(context.Background(), run)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if result.ExecutorDispatched {
		t.Fatal("ExecutorDispatched = true, want false")
	}
	if fakePlanner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", fakePlanner.calls)
	}
	if result.SecondPlannerTurn == nil {
		t.Fatal("SecondPlannerTurn = nil, want second planner result")
	}
	if result.PostExecutorPlannerTurn != nil {
		t.Fatal("PostExecutorPlannerTurn should be nil for collect_context flow")
	}
	if result.Run.LatestCheckpoint.Label != "planner_turn_post_collect_context" {
		t.Fatalf("LatestCheckpoint.Label = %q, want planner_turn_post_collect_context", result.Run.LatestCheckpoint.Label)
	}
	if result.Run.PreviousResponseID != "resp_pause" {
		t.Fatalf("PreviousResponseID = %q, want resp_pause", result.Run.PreviousResponseID)
	}
	if result.Run.CollectedContext == nil {
		t.Fatal("CollectedContext = nil, want persisted collected context")
	}
	if result.Run.CollectedContext.Focus != "Inspect the requested repo paths." {
		t.Fatalf("CollectedContext.Focus = %q", result.Run.CollectedContext.Focus)
	}
	if len(result.Run.CollectedContext.Results) != 3 {
		t.Fatalf("CollectedContext.Results len = %d, want 3", len(result.Run.CollectedContext.Results))
	}

	secondInput := fakePlanner.inputs[1]
	if secondInput.CollectedContext == nil {
		t.Fatal("second planner input missing collected_context summary")
	}
	if secondInput.ExecutorResult != nil {
		t.Fatal("second planner input unexpectedly included executor_result")
	}
	if secondInput.CollectedContext.Results[0].Kind != "file" {
		t.Fatalf("first collected result kind = %q, want file", secondInput.CollectedContext.Results[0].Kind)
	}
	if !strings.Contains(secondInput.CollectedContext.Results[0].Preview, "bounded context preview") {
		t.Fatalf("file preview = %q, want file contents", secondInput.CollectedContext.Results[0].Preview)
	}
	if secondInput.CollectedContext.Results[1].Kind != "dir" {
		t.Fatalf("second collected result kind = %q, want dir", secondInput.CollectedContext.Results[1].Kind)
	}
	if len(secondInput.CollectedContext.Results[1].Entries) == 0 || secondInput.CollectedContext.Results[1].Entries[0] != "keep.go" {
		t.Fatalf("directory entries = %#v, want keep.go listing", secondInput.CollectedContext.Results[1].Entries)
	}
	if secondInput.CollectedContext.Results[2].Kind != "missing" {
		t.Fatalf("third collected result kind = %q, want missing", secondInput.CollectedContext.Results[2].Kind)
	}
	if secondInput.CollectedContext.Results[2].Detail != "path_not_found" {
		t.Fatalf("third collected result detail = %q, want path_not_found", secondInput.CollectedContext.Results[2].Detail)
	}
	if fakePlanner.previousResponseIDs[1] != "resp_collect" {
		t.Fatalf("second planner previous_response_id = %q, want resp_collect", fakePlanner.previousResponseIDs[1])
	}

	events, err := journalWriter.ReadRecent(run.ID, 16)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if !containsEventType(events, "context.collection.completed") {
		t.Fatal("journal missing context.collection.completed event")
	}
	if countEventType(events, "context.collection.recorded") != 3 {
		t.Fatalf("context.collection.recorded count = %d, want 3", countEventType(events, "context.collection.recorded"))
	}
	if countEventType(events, "planner.turn.completed") != 2 {
		t.Fatalf("planner.turn.completed count = %d, want 2", countEventType(events, "planner.turn.completed"))
	}
}

type stubPlanner struct {
	results             []planner.Result
	err                 error
	calls               int
	inputs              []planner.InputEnvelope
	previousResponseIDs []string
}

func (s *stubPlanner) Plan(_ context.Context, input planner.InputEnvelope, previousResponseID string) (planner.Result, error) {
	s.calls++
	s.inputs = append(s.inputs, input)
	s.previousResponseIDs = append(s.previousResponseIDs, previousResponseID)
	if s.err != nil {
		return planner.Result{}, s.err
	}
	index := s.calls - 1
	if index >= len(s.results) {
		return planner.Result{}, errors.New("stubPlanner called more times than configured")
	}
	return s.results[index], nil
}

type stubExecutor struct {
	result      executor.TurnResult
	err         error
	calls       int
	lastRequest executor.TurnRequest
}

func (s *stubExecutor) Execute(_ context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	s.calls++
	s.lastRequest = req
	return s.result, s.err
}

func newTestRuntime(t *testing.T) (*state.Store, *journal.Journal, state.Run) {
	t.Helper()

	root := t.TempDir()
	store, err := state.Open(filepath.Join(root, "orchestrator.db"))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	journalWriter, err := journal.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatalf("journal.Open() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), state.CreateRunParams{
		RepoPath: root,
		Goal:     "ship the smallest real bounded planner to executor cycle",
		Status:   state.StatusInitialized,
		Checkpoint: state.Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    time.Date(2026, 4, 19, 17, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	return store, journalWriter, run
}

func containsEventType(events []journal.Event, want string) bool {
	for _, event := range events {
		if event.Type == want {
			return true
		}
	}
	return false
}

func countEventType(events []journal.Event, want string) int {
	count := 0
	for _, event := range events {
		if event.Type == want {
			count++
		}
	}
	return count
}
