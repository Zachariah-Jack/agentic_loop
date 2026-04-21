package orchestration

import (
	"context"
	"strings"

	"orchestrator/internal/executor"
	"orchestrator/internal/journal"
	"orchestrator/internal/planner"
	"orchestrator/internal/plugins"
	"orchestrator/internal/state"
)

func (c Cycle) pluginToolDescriptors() []planner.PluginToolDescriptor {
	if c.Plugins == nil {
		return nil
	}
	return c.Plugins.ToolDescriptors()
}

func (c Cycle) executePluginToolCalls(
	ctx context.Context,
	run state.Run,
	plannerTurn planner.Result,
	checkpoint state.Checkpoint,
	toolCalls []planner.PluginToolCall,
) ([]state.PluginToolResult, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	results := make([]state.PluginToolResult, 0, len(toolCalls))
	for _, call := range toolCalls {
		toolName := strings.TrimSpace(call.Tool)
		if toolName == "" {
			continue
		}

		result, err := c.Plugins.ExecuteToolCall(ctx, run, call)
		pluginName := pluginNameFromTool(toolName)
		calledEvent := journal.Event{
			Type:               "plugin.tool.called",
			RunID:              run.ID,
			RepoPath:           run.RepoPath,
			Goal:               run.Goal,
			Status:             string(run.Status),
			Message:            valueOrDefault(strings.TrimSpace(result.Message), "plugin tool executed"),
			ResponseID:         plannerTurn.ResponseID,
			PreviousResponseID: run.PreviousResponseID,
			PlannerOutcome:     string(plannerTurn.Output.Outcome),
			PluginName:         pluginName,
			PluginTool:         toolName,
			ArtifactPath:       strings.TrimSpace(result.ArtifactPath),
			ArtifactPreview:    strings.TrimSpace(result.ArtifactPreview),
			Checkpoint:         checkpointRef(&checkpoint),
		}
		if appendErr := c.Journal.Append(calledEvent); appendErr != nil {
			return results, appendErr
		}

		if err != nil || !result.Success {
			failedEvent := calledEvent
			failedEvent.Type = "plugin.tool.failed"
			failedEvent.Message = valueOrDefault(strings.TrimSpace(result.Message), "plugin tool failed")
			if appendErr := c.Journal.Append(failedEvent); appendErr != nil {
				return results, appendErr
			}
		}

		results = append(results, result)
	}

	return results, nil
}

func (c Cycle) runPluginHooks(
	ctx context.Context,
	point string,
	run state.Run,
	plannerTurn *planner.Result,
	executorTurn *executor.TurnResult,
	stopReason string,
	cycleErr error,
) {
	if c.Plugins == nil || strings.TrimSpace(run.ID) == "" {
		return
	}

	results := c.Plugins.RunHooks(ctx, point, plugins.HookRequest{
		Run:            run,
		PlannerResult:  plannerTurn,
		ExecutorResult: executorTurn,
		StopReason:     strings.TrimSpace(stopReason),
		CycleError:     errorString(cycleErr),
	})
	for _, result := range results {
		if result.Success {
			continue
		}

		_ = c.Journal.Append(journal.Event{
			Type:               "plugin.hook.failed",
			RunID:              run.ID,
			RepoPath:           run.RepoPath,
			Goal:               run.Goal,
			Status:             string(run.Status),
			Message:            valueOrDefault(strings.TrimSpace(result.Message), "plugin hook failed"),
			PreviousResponseID: run.PreviousResponseID,
			PluginName:         strings.TrimSpace(result.Plugin),
			PluginHook:         strings.TrimSpace(result.Hook),
			ArtifactPath:       strings.TrimSpace(result.ArtifactPath),
			ArtifactPreview:    strings.TrimSpace(result.ArtifactPreview),
			Checkpoint:         checkpointRef(&run.LatestCheckpoint),
		})
	}
}

func preferredPlannerResult(result Result) *planner.Result {
	switch {
	case result.PostExecutorPlannerTurn != nil:
		return result.PostExecutorPlannerTurn
	case result.SecondPlannerTurn != nil:
		return result.SecondPlannerTurn
	case result.ReconsiderationPlannerTurn != nil:
		return result.ReconsiderationPlannerTurn
	case strings.TrimSpace(result.FirstPlannerResult.ResponseID) != "":
		return &result.FirstPlannerResult
	default:
		return nil
	}
}

func pluginNameFromTool(toolName string) string {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return ""
	}
	parts := strings.SplitN(trimmed, ".", 2)
	return parts[0]
}

func valueOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
