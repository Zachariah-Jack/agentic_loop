package orchestration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

type Planner interface {
	Plan(context.Context, planner.InputEnvelope, string) (planner.Result, error)
}

type Executor interface {
	Execute(context.Context, executor.TurnRequest) (executor.TurnResult, error)
}

type Cycle struct {
	Store    *state.Store
	Journal  *journal.Journal
	Planner  Planner
	Executor Executor
}

type Result struct {
	Run                     state.Run
	FirstPlannerResult      planner.Result
	SecondPlannerTurn       *planner.Result
	PostExecutorPlannerTurn *planner.Result
	ExecutorResult          *executor.TurnResult
	ExecutorDispatched      bool
}

const (
	maxCollectedFilePreviewBytes = 2048
	maxCollectedDirEntries       = 20
)

func (c Cycle) RunOnce(ctx context.Context, run state.Run) (Result, error) {
	if c.Store == nil {
		return Result{}, errors.New("store is required")
	}
	if c.Journal == nil {
		return Result{}, errors.New("journal is required")
	}
	if c.Planner == nil {
		return Result{}, errors.New("planner is required")
	}

	recentEvents, err := c.Journal.ReadRecent(run.ID, 5)
	if err != nil {
		return Result{}, err
	}

	firstInput := BuildPlannerInput(run, recentEvents, nil, nil)
	firstPlannerResult, err := c.Planner.Plan(ctx, firstInput, run.PreviousResponseID)
	if err != nil {
		_ = c.Journal.Append(journal.Event{
			Type:     "planner.turn.failed",
			RunID:    run.ID,
			RepoPath: run.RepoPath,
			Goal:     run.Goal,
			Status:   string(run.Status),
			Message:  err.Error(),
		})
		return Result{}, err
	}

	firstPlannerCheckpoint := state.Checkpoint{
		Sequence:     run.LatestCheckpoint.Sequence + 1,
		Stage:        "planner",
		Label:        "planner_turn_completed",
		SafePause:    true,
		PlannerTurn:  run.LatestCheckpoint.PlannerTurn + 1,
		ExecutorTurn: run.LatestCheckpoint.ExecutorTurn,
		CreatedAt:    time.Now().UTC(),
	}

	if err := c.Store.SavePlannerTurn(ctx, run.ID, firstPlannerResult.ResponseID, firstPlannerCheckpoint); err != nil {
		return Result{}, err
	}

	updatedRun, found, err := c.Store.GetRun(ctx, run.ID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Result{}, fmt.Errorf("updated run %s could not be reloaded", run.ID)
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "planner.turn.completed",
		RunID:              updatedRun.ID,
		RepoPath:           updatedRun.RepoPath,
		Goal:               updatedRun.Goal,
		Status:             string(updatedRun.Status),
		Message:            "planner response validated and persisted",
		ResponseID:         firstPlannerResult.ResponseID,
		PreviousResponseID: updatedRun.PreviousResponseID,
		PlannerOutcome:     string(firstPlannerResult.Output.Outcome),
		Checkpoint:         checkpointRef(&firstPlannerCheckpoint),
	}); err != nil {
		return Result{}, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "checkpoint.persisted",
		RunID:              updatedRun.ID,
		RepoPath:           updatedRun.RepoPath,
		Status:             string(updatedRun.Status),
		Message:            "planner checkpoint persisted",
		ResponseID:         firstPlannerResult.ResponseID,
		PreviousResponseID: updatedRun.PreviousResponseID,
		PlannerOutcome:     string(firstPlannerResult.Output.Outcome),
		Checkpoint:         checkpointRef(&firstPlannerCheckpoint),
	}); err != nil {
		return Result{}, err
	}

	result := Result{
		Run:                updatedRun,
		FirstPlannerResult: firstPlannerResult,
	}

	if ShouldCollectContext(firstPlannerResult.Output) {
		collectedContext, err := BuildCollectedContextState(updatedRun.RepoPath, firstPlannerResult.Output)
		if err != nil {
			return result, err
		}
		if err := c.Store.SaveCollectedContext(ctx, updatedRun.ID, collectedContext); err != nil {
			return result, err
		}

		latestRun, found, err := c.Store.GetRun(ctx, updatedRun.ID)
		if err != nil {
			return result, err
		}
		if !found {
			return result, fmt.Errorf("updated run %s could not be reloaded after context collection", updatedRun.ID)
		}
		result.Run = latestRun

		for _, item := range collectedContext.Results {
			if err := c.Journal.Append(journal.Event{
				Type:                 "context.collection.recorded",
				RunID:                latestRun.ID,
				RepoPath:             latestRun.RepoPath,
				Goal:                 latestRun.Goal,
				Status:               string(latestRun.Status),
				Message:              contextCollectionMessage(item),
				ResponseID:           firstPlannerResult.ResponseID,
				PreviousResponseID:   latestRun.PreviousResponseID,
				PlannerOutcome:       string(firstPlannerResult.Output.Outcome),
				ContextRequestedPath: item.RequestedPath,
				ContextResolvedPath:  item.ResolvedPath,
				ContextKind:          item.Kind,
				ContextDetail:        item.Detail,
				ContextPreview:       contextCollectionPreview(item),
				Checkpoint:           checkpointRef(&latestRun.LatestCheckpoint),
			}); err != nil {
				return result, err
			}
		}

		if err := c.Journal.Append(journal.Event{
			Type:               "context.collection.completed",
			RunID:              latestRun.ID,
			RepoPath:           latestRun.RepoPath,
			Goal:               latestRun.Goal,
			Status:             string(latestRun.Status),
			Message:            fmt.Sprintf("collected %d context result(s) from planner-requested paths", len(collectedContext.Results)),
			ResponseID:         firstPlannerResult.ResponseID,
			PreviousResponseID: latestRun.PreviousResponseID,
			PlannerOutcome:     string(firstPlannerResult.Output.Outcome),
			Checkpoint:         checkpointRef(&latestRun.LatestCheckpoint),
		}); err != nil {
			return result, err
		}

		postCollectionEvents, err := c.Journal.ReadRecent(latestRun.ID, 8)
		if err != nil {
			return result, err
		}

		postCollectionInput := BuildPlannerInput(latestRun, postCollectionEvents, nil, BuildCollectedContextInput(latestRun))
		result, err = c.runSecondPlannerTurn(
			ctx,
			latestRun,
			result,
			postCollectionInput,
			"planner_turn_post_collect_context",
			"post-collect-context planner response validated and persisted",
			"post-collect-context planner checkpoint persisted",
			"post-collect-context planner turn failed",
		)
		return result, err
	}

	if !ShouldDispatchExecutor(firstPlannerResult.Output) {
		return result, nil
	}
	if c.Executor == nil {
		return result, errors.New("executor is required when planner outcome is execute")
	}

	prompt, err := RenderExecutorPrompt(updatedRun.Goal, firstPlannerResult.Output)
	if err != nil {
		return result, err
	}

	if err := c.Journal.Append(journal.Event{
		Type:               "executor.turn.dispatched",
		RunID:              updatedRun.ID,
		RepoPath:           updatedRun.RepoPath,
		Goal:               updatedRun.Goal,
		Status:             string(updatedRun.Status),
		Message:            "planner execute outcome dispatched to primary executor: " + ExecutorTask(firstPlannerResult.Output),
		ResponseID:         firstPlannerResult.ResponseID,
		PreviousResponseID: updatedRun.PreviousResponseID,
		PlannerOutcome:     string(firstPlannerResult.Output.Outcome),
	}); err != nil {
		return result, err
	}

	executorResult, execErr := c.Executor.Execute(ctx, executor.TurnRequest{
		RunID:      updatedRun.ID,
		RepoPath:   updatedRun.RepoPath,
		Prompt:     prompt,
		ThreadID:   updatedRun.ExecutorThreadID,
		ThreadPath: updatedRun.ExecutorThreadPath,
	})

	executorErrorMessage := executorFailureMessage(executorResult)
	if executorErrorMessage == "" && execErr != nil {
		executorErrorMessage = execErr.Error()
	}

	executorState := state.ExecutorState{
		Transport:        string(executorResult.Transport),
		ThreadID:         executorResult.ThreadID,
		ThreadPath:       executorResult.ThreadPath,
		TurnID:           executorResult.TurnID,
		TurnStatus:       string(executorResult.TurnStatus),
		LastSuccess:      executorSuccess(executorResult),
		LastFailureStage: executorFailureStage(executorResult),
		LastError:        executorErrorMessage,
		LastMessage:      executorResult.FinalMessage,
	}

	var executorCheckpoint *state.Checkpoint
	if executorResult.CompletedAt.IsZero() {
		if err := c.Store.SaveExecutorState(ctx, updatedRun.ID, executorState); err != nil {
			return result, err
		}
	} else {
		checkpoint := BuildExecutorCheckpoint(updatedRun.LatestCheckpoint, executorCheckpointLabel(executorResult.TurnStatus), executorResult.CompletedAt)
		executorCheckpoint = &checkpoint
		if err := c.Store.SaveExecutorTurn(ctx, updatedRun.ID, executorState, checkpoint); err != nil {
			return result, err
		}
	}

	latestRun, found, err := c.Store.GetRun(ctx, updatedRun.ID)
	if err != nil {
		return result, err
	}
	if !found {
		return result, fmt.Errorf("updated run %s could not be reloaded after executor dispatch", updatedRun.ID)
	}

	eventType := "executor.turn.completed"
	eventMessage := "executor turn completed"
	eventAt := executorResult.CompletedAt
	if execErr != nil {
		eventType = "executor.turn.failed"
		eventMessage = executorErrorMessage
		if eventAt.IsZero() {
			eventAt = time.Now().UTC()
		}
	}

	if err := c.Journal.Append(journal.Event{
		At:                    eventAt,
		Type:                  eventType,
		RunID:                 latestRun.ID,
		RepoPath:              latestRun.RepoPath,
		Goal:                  latestRun.Goal,
		Status:                string(latestRun.Status),
		Message:               eventMessage,
		ResponseID:            firstPlannerResult.ResponseID,
		PreviousResponseID:    latestRun.PreviousResponseID,
		PlannerOutcome:        string(firstPlannerResult.Output.Outcome),
		ExecutorTransport:     string(executorResult.Transport),
		ExecutorThreadID:      executorResult.ThreadID,
		ExecutorThreadPath:    executorResult.ThreadPath,
		ExecutorTurnID:        executorResult.TurnID,
		ExecutorTurnStatus:    string(executorResult.TurnStatus),
		ExecutorFailureStage:  executorFailureStage(executorResult),
		ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
		Checkpoint:            checkpointRef(executorCheckpoint),
	}); err != nil {
		return result, err
	}

	if executorCheckpoint != nil {
		message := "executor checkpoint persisted"
		if execErr != nil {
			message = "executor failure checkpoint persisted"
		}
		if err := c.Journal.Append(journal.Event{
			At:                    executorCheckpoint.CreatedAt,
			Type:                  "checkpoint.persisted",
			RunID:                 latestRun.ID,
			RepoPath:              latestRun.RepoPath,
			Status:                string(latestRun.Status),
			Message:               message,
			ResponseID:            firstPlannerResult.ResponseID,
			PreviousResponseID:    latestRun.PreviousResponseID,
			PlannerOutcome:        string(firstPlannerResult.Output.Outcome),
			ExecutorTransport:     string(executorResult.Transport),
			ExecutorThreadID:      executorResult.ThreadID,
			ExecutorThreadPath:    executorResult.ThreadPath,
			ExecutorTurnID:        executorResult.TurnID,
			ExecutorTurnStatus:    string(executorResult.TurnStatus),
			ExecutorFailureStage:  executorFailureStage(executorResult),
			ExecutorOutputPreview: previewString(executorResult.FinalMessage, 240),
			Checkpoint:            checkpointRef(executorCheckpoint),
		}); err != nil {
			return result, err
		}
	}

	result.Run = latestRun
	result.ExecutorDispatched = true
	result.ExecutorResult = &executorResult

	if executorCheckpoint == nil {
		return result, execErr
	}

	postExecutorEvents, err := c.Journal.ReadRecent(latestRun.ID, 8)
	if err != nil {
		return result, err
	}

	postExecutorInput := BuildPlannerInput(latestRun, postExecutorEvents, BuildExecutorResultInput(latestRun), nil)
	result, err = c.runSecondPlannerTurn(
		ctx,
		latestRun,
		result,
		postExecutorInput,
		"planner_turn_post_executor",
		"post-executor planner response validated and persisted",
		"post-executor planner checkpoint persisted",
		"post-executor planner turn failed",
	)
	return result, joinErrors(execErr, err)
}

func BuildPlannerInput(run state.Run, events []journal.Event, executorResult *planner.ExecutorResultSummary, collectedContext *planner.CollectedContextSummary) planner.InputEnvelope {
	previews := make([]planner.EventPreview, 0, len(events))
	for _, event := range events {
		previews = append(previews, planner.EventPreview{
			At:      event.At,
			Type:    event.Type,
			Summary: summarizeEvent(event),
		})
	}

	return planner.InputEnvelope{
		ContractVersion: planner.ContractVersionV1,
		RunID:           run.ID,
		RepoPath:        run.RepoPath,
		Goal:            run.Goal,
		RunStatus:       string(run.Status),
		LatestCheckpoint: planner.Checkpoint{
			Sequence:     run.LatestCheckpoint.Sequence,
			Stage:        run.LatestCheckpoint.Stage,
			Label:        run.LatestCheckpoint.Label,
			SafePause:    run.LatestCheckpoint.SafePause,
			PlannerTurn:  run.LatestCheckpoint.PlannerTurn,
			ExecutorTurn: run.LatestCheckpoint.ExecutorTurn,
			CreatedAt:    run.LatestCheckpoint.CreatedAt,
		},
		RecentEvents:     previews,
		ExecutorResult:   executorResult,
		CollectedContext: collectedContext,
		RepoContracts: planner.RepoContractAvailability{
			HasAgentsMD:         pathExists(filepath.Join(run.RepoPath, "AGENTS.md")),
			AgentsMDPath:        "AGENTS.md",
			HasUpdatedSpec:      pathExists(filepath.Join(run.RepoPath, "docs", "ORCHESTRATOR_CLI_UPDATED_SPEC.md")),
			UpdatedSpecPath:     "docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md",
			HasNonNegotiables:   pathExists(filepath.Join(run.RepoPath, "docs", "ORCHESTRATOR_NON_NEGOTIABLES.md")),
			NonNegotiablesPath:  "docs/ORCHESTRATOR_NON_NEGOTIABLES.md",
			HasExecPlan:         pathExists(filepath.Join(run.RepoPath, "docs", "CLI_ENGINE_EXECPLAN.md")),
			ExecPlanPath:        "docs/CLI_ENGINE_EXECPLAN.md",
			OrchestratorDirPath: ".orchestrator",
			RoadmapPath:         ".orchestrator/roadmap.md",
			DecisionsPath:       ".orchestrator/decisions.md",
		},
		RawHumanReplies: nil,
		Capabilities: planner.CapabilityMarkers{
			Planner:  planner.CapabilityAvailable,
			Executor: planner.CapabilityAvailable,
			NTFY:     planner.CapabilityDeferred,
		},
	}
}

func BuildExecutorResultInput(run state.Run) *planner.ExecutorResultSummary {
	if run.ExecutorLastSuccess == nil {
		return nil
	}

	return &planner.ExecutorResultSummary{
		FinalMessage: strings.TrimSpace(run.ExecutorLastMessage),
		Success:      *run.ExecutorLastSuccess,
		ThreadID:     strings.TrimSpace(run.ExecutorThreadID),
	}
}

func BuildCollectedContextInput(run state.Run) *planner.CollectedContextSummary {
	if run.CollectedContext == nil {
		return nil
	}

	results := make([]planner.CollectedContextResult, 0, len(run.CollectedContext.Results))
	for _, item := range run.CollectedContext.Results {
		results = append(results, planner.CollectedContextResult{
			RequestedPath: item.RequestedPath,
			ResolvedPath:  item.ResolvedPath,
			Kind:          item.Kind,
			Detail:        item.Detail,
			Preview:       item.Preview,
			Entries:       append([]string(nil), item.Entries...),
			Truncated:     item.Truncated,
		})
	}

	return &planner.CollectedContextSummary{
		Focus:     strings.TrimSpace(run.CollectedContext.Focus),
		Questions: append([]string(nil), run.CollectedContext.Questions...),
		Results:   results,
	}
}

func ShouldCollectContext(output planner.OutputEnvelope) bool {
	return output.Outcome == planner.OutcomeCollectContext && output.CollectContext != nil
}

func ShouldDispatchExecutor(output planner.OutputEnvelope) bool {
	return output.Outcome == planner.OutcomeExecute && output.Execute != nil
}

func RenderExecutorPrompt(goal string, output planner.OutputEnvelope) (string, error) {
	if !ShouldDispatchExecutor(output) {
		return "", errors.New("planner outcome is not execute")
	}

	execute := output.Execute
	lines := []string{
		"Planner-selected executor task.",
		"",
		"Run goal:",
		strings.TrimSpace(goal),
		"",
		"Task:",
		strings.TrimSpace(execute.Task),
		"",
		"Acceptance criteria:",
	}

	for _, criterion := range nonEmpty(execute.AcceptanceCriteria) {
		lines = append(lines, "- "+criterion)
	}

	writeScope := nonEmpty(execute.WriteScope)
	if len(writeScope) > 0 {
		lines = append(lines, "", "Write scope:")
		for _, path := range writeScope {
			lines = append(lines, "- "+path)
		}
	}

	lines = append(lines,
		"",
		"Perform only this bounded task.",
		"Do not choose a new task.",
		"Do not decide whether the overall run is complete.",
		"In your final answer, report the concrete work completed and any blockers.",
	)

	return strings.Join(lines, "\n"), nil
}

func BuildExecutorCheckpoint(previous state.Checkpoint, label string, at time.Time) state.Checkpoint {
	timestamp := at.UTC()
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	return state.Checkpoint{
		Sequence:     previous.Sequence + 1,
		Stage:        "executor",
		Label:        strings.TrimSpace(label),
		SafePause:    true,
		PlannerTurn:  previous.PlannerTurn,
		ExecutorTurn: previous.ExecutorTurn + 1,
		CreatedAt:    timestamp,
	}
}

func ExecutorTask(output planner.OutputEnvelope) string {
	if output.Execute == nil {
		return ""
	}
	return strings.TrimSpace(output.Execute.Task)
}

func (c Cycle) runSecondPlannerTurn(
	ctx context.Context,
	currentRun state.Run,
	result Result,
	input planner.InputEnvelope,
	checkpointLabel string,
	completedMessage string,
	checkpointMessage string,
	failurePrefix string,
) (Result, error) {
	secondPlannerTurn, err := c.Planner.Plan(ctx, input, currentRun.PreviousResponseID)
	if err != nil {
		failureEvent := journal.Event{
			Type:               "planner.turn.failed",
			RunID:              currentRun.ID,
			RepoPath:           currentRun.RepoPath,
			Goal:               currentRun.Goal,
			Status:             string(currentRun.Status),
			Message:            failurePrefix + ": " + err.Error(),
			PreviousResponseID: currentRun.PreviousResponseID,
			PlannerOutcome:     string(result.FirstPlannerResult.Output.Outcome),
			Checkpoint:         checkpointRef(&currentRun.LatestCheckpoint),
		}
		populateExecutorEventFields(&failureEvent, currentRun)
		_ = c.Journal.Append(failureEvent)
		return result, err
	}

	checkpoint := state.Checkpoint{
		Sequence:     currentRun.LatestCheckpoint.Sequence + 1,
		Stage:        "planner",
		Label:        checkpointLabel,
		SafePause:    true,
		PlannerTurn:  currentRun.LatestCheckpoint.PlannerTurn + 1,
		ExecutorTurn: currentRun.LatestCheckpoint.ExecutorTurn,
		CreatedAt:    time.Now().UTC(),
	}

	if err := c.Store.SavePlannerTurn(ctx, currentRun.ID, secondPlannerTurn.ResponseID, checkpoint); err != nil {
		return result, err
	}

	finalRun, found, err := c.Store.GetRun(ctx, currentRun.ID)
	if err != nil {
		return result, err
	}
	if !found {
		return result, fmt.Errorf("updated run %s could not be reloaded after second planner turn", currentRun.ID)
	}

	completedEvent := journal.Event{
		Type:               "planner.turn.completed",
		RunID:              finalRun.ID,
		RepoPath:           finalRun.RepoPath,
		Goal:               finalRun.Goal,
		Status:             string(finalRun.Status),
		Message:            completedMessage,
		ResponseID:         secondPlannerTurn.ResponseID,
		PreviousResponseID: finalRun.PreviousResponseID,
		PlannerOutcome:     string(secondPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&checkpoint),
	}
	populateExecutorEventFields(&completedEvent, finalRun)
	if err := c.Journal.Append(completedEvent); err != nil {
		return result, err
	}

	checkpointEvent := journal.Event{
		Type:               "checkpoint.persisted",
		RunID:              finalRun.ID,
		RepoPath:           finalRun.RepoPath,
		Status:             string(finalRun.Status),
		Message:            checkpointMessage,
		ResponseID:         secondPlannerTurn.ResponseID,
		PreviousResponseID: finalRun.PreviousResponseID,
		PlannerOutcome:     string(secondPlannerTurn.Output.Outcome),
		Checkpoint:         checkpointRef(&checkpoint),
	}
	populateExecutorEventFields(&checkpointEvent, finalRun)
	if err := c.Journal.Append(checkpointEvent); err != nil {
		return result, err
	}

	result.Run = finalRun
	result.SecondPlannerTurn = &secondPlannerTurn
	if checkpointLabel == "planner_turn_post_executor" {
		result.PostExecutorPlannerTurn = &secondPlannerTurn
	}
	return result, nil
}

func BuildCollectedContextState(repoPath string, output planner.OutputEnvelope) (*state.CollectedContextState, error) {
	if !ShouldCollectContext(output) {
		return nil, errors.New("planner outcome is not collect_context")
	}

	collected := &state.CollectedContextState{
		Focus:     strings.TrimSpace(output.CollectContext.Focus),
		Questions: nonEmpty(output.CollectContext.Questions),
		Results:   make([]state.CollectedContextResult, 0, len(nonEmpty(output.CollectContext.Paths))),
	}

	for _, requestedPath := range nonEmpty(output.CollectContext.Paths) {
		collected.Results = append(collected.Results, inspectCollectedContextPath(repoPath, requestedPath))
	}

	return collected, nil
}

func inspectCollectedContextPath(repoPath string, requestedPath string) state.CollectedContextResult {
	result := state.CollectedContextResult{
		RequestedPath: strings.TrimSpace(requestedPath),
		Kind:          "missing",
	}

	resolvedPath, detail := resolveCollectContextPath(repoPath, result.RequestedPath)
	result.ResolvedPath = resolvedPath
	if detail != "" {
		result.Detail = detail
		return result
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			result.Detail = "path_not_found"
			return result
		}
		result.Detail = "stat_failed"
		result.Preview = err.Error()
		return result
	}

	if info.IsDir() {
		entries, truncated, readErr := readDirectoryPreview(resolvedPath, maxCollectedDirEntries)
		result.Kind = "dir"
		result.Entries = entries
		result.Truncated = truncated
		if readErr != nil {
			result.Detail = "read_failed"
			result.Preview = readErr.Error()
		}
		return result
	}

	preview, truncated, readErr := readFilePreview(resolvedPath, maxCollectedFilePreviewBytes)
	result.Kind = "file"
	result.Preview = preview
	result.Truncated = truncated
	if readErr != nil {
		result.Detail = "read_failed"
		result.Preview = readErr.Error()
	}
	return result
}

func resolveCollectContextPath(repoPath string, requestedPath string) (string, string) {
	trimmed := strings.TrimSpace(requestedPath)
	if trimmed == "" {
		return "", "empty_path"
	}
	if filepath.IsAbs(trimmed) {
		return "", "absolute_path_not_allowed"
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", "path_outside_repo_root"
	}

	resolvedPath := filepath.Join(repoPath, cleaned)
	relativePath, err := filepath.Rel(repoPath, resolvedPath)
	if err != nil {
		return "", "path_outside_repo_root"
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", "path_outside_repo_root"
	}

	return resolvedPath, ""
}

func readFilePreview(path string, limit int) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	if limit <= 0 {
		limit = maxCollectedFilePreviewBytes
	}

	data, err := io.ReadAll(io.LimitReader(file, int64(limit+1)))
	if err != nil {
		return "", false, err
	}

	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
	}

	return string(data), truncated, nil
}

func readDirectoryPreview(path string, limit int) ([]string, bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, err
	}

	if limit <= 0 {
		limit = maxCollectedDirEntries
	}

	truncated := len(entries) > limit
	if truncated {
		entries = entries[:limit]
	}

	listing := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += string(filepath.Separator)
		}
		listing = append(listing, name)
	}

	return listing, truncated, nil
}

func contextCollectionMessage(item state.CollectedContextResult) string {
	switch item.Kind {
	case "file":
		return "collected file context for " + item.RequestedPath
	case "dir":
		return "collected directory context for " + item.RequestedPath
	default:
		if strings.TrimSpace(item.Detail) != "" {
			return "context path unresolved for " + item.RequestedPath + " (" + item.Detail + ")"
		}
		return "context path unresolved for " + item.RequestedPath
	}
}

func contextCollectionPreview(item state.CollectedContextResult) string {
	switch item.Kind {
	case "file":
		return previewString(item.Preview, 240)
	case "dir":
		preview := strings.Join(item.Entries, ", ")
		if item.Truncated {
			preview += ", ..."
		}
		return preview
	default:
		return strings.TrimSpace(item.Detail)
	}
}

func populateExecutorEventFields(event *journal.Event, run state.Run) {
	if event == nil {
		return
	}

	event.ExecutorTransport = run.ExecutorTransport
	event.ExecutorThreadID = run.ExecutorThreadID
	event.ExecutorThreadPath = run.ExecutorThreadPath
	event.ExecutorTurnID = run.ExecutorTurnID
	event.ExecutorTurnStatus = run.ExecutorTurnStatus
	event.ExecutorFailureStage = run.ExecutorLastFailureStage
	event.ExecutorOutputPreview = previewString(run.ExecutorLastMessage, 240)
}

func summarizeEvent(event journal.Event) string {
	message := strings.TrimSpace(event.Message)
	outputPreview := strings.TrimSpace(event.ExecutorOutputPreview)
	contextPreview := strings.TrimSpace(event.ContextPreview)

	switch {
	case message != "" && outputPreview != "":
		return message + " | executor output: " + outputPreview
	case message != "" && contextPreview != "":
		return message + " | context: " + contextPreview
	case message != "":
		return message
	case outputPreview != "":
		return "executor output: " + outputPreview
	case contextPreview != "":
		return "context: " + contextPreview
	case strings.TrimSpace(event.ContextRequestedPath) != "":
		return event.Type + " (" + strings.TrimSpace(event.ContextRequestedPath) + ")"
	case strings.TrimSpace(event.PlannerOutcome) != "":
		return event.Type + " (" + strings.TrimSpace(event.PlannerOutcome) + ")"
	default:
		return event.Type
	}
}

func checkpointRef(checkpoint *state.Checkpoint) *journal.CheckpointRef {
	if checkpoint == nil {
		return nil
	}

	return &journal.CheckpointRef{
		Sequence:  checkpoint.Sequence,
		Stage:     checkpoint.Stage,
		Label:     checkpoint.Label,
		SafePause: checkpoint.SafePause,
	}
}

func executorCheckpointLabel(status executor.TurnStatus) string {
	if status == executor.TurnStatusCompleted {
		return "executor_turn_completed"
	}
	return "executor_turn_failed"
}

func executorFailureMessage(result executor.TurnResult) string {
	if result.Error == nil {
		return ""
	}
	if strings.TrimSpace(result.Error.Detail) == "" {
		return result.Error.Message
	}
	return result.Error.Message + " (" + strings.TrimSpace(result.Error.Detail) + ")"
}

func executorFailureStage(result executor.TurnResult) string {
	if result.Error == nil {
		return ""
	}
	return strings.TrimSpace(result.Error.Stage)
}

func executorSuccess(result executor.TurnResult) *bool {
	if result.CompletedAt.IsZero() {
		return nil
	}

	success := result.TurnStatus == executor.TurnStatusCompleted
	return &success
}

func joinErrors(left error, right error) error {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return errors.Join(left, right)
}

func previewString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "..."
}

func nonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
