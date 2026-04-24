package planner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidateOutputAcceptsExecute(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeExecute,
		Execute: &ExecuteOutcome{
			Task:               "Implement a durable event append path",
			AcceptanceCriteria: []string{"events are written to JSONL", "tests pass"},
			WriteScope:         []string{"internal/journal"},
		},
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputAcceptsPlannerV2OperatorStatus(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV2,
		Outcome:         OutcomeExecute,
		OperatorStatus: &OperatorStatus{
			OperatorMessage:    "Implementing the next bounded slice.",
			CurrentFocus:       "runtime control protocol foundation",
			NextIntendedStep:   "dispatch the bounded control-protocol engine slice",
			WhyThisStep:        "V2 console work depends on a real engine seam first.",
			ProgressPercent:    18,
			ProgressConfidence: ProgressConfidenceMedium,
			ProgressBasis:      "protocol skeleton and safe-point config reload are next; GUI is still deferred.",
		},
		Execute: &ExecuteOutcome{
			Task:               "Implement the first V2 engine seam",
			AcceptanceCriteria: []string{"tests cover the engine control slice"},
		},
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputAcceptsPlannerV1OperatorStatus(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomePause,
		OperatorStatus: &OperatorStatus{
			OperatorMessage:    "Collecting the current runtime view for the operator.",
			CurrentFocus:       "operator-status live-path migration",
			NextIntendedStep:   "persist the safe planner summary for CLI and protocol rendering",
			WhyThisStep:        "operator-facing visibility needs durable planner-safe status data.",
			ProgressPercent:    42,
			ProgressConfidence: ProgressConfidenceMedium,
			ProgressBasis:      "runtime and protocol plumbing exist; live planner rendering is being connected.",
		},
		Pause: &PauseOutcome{Reason: "waiting at a safe boundary"},
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputAcceptsPlannerV1NullOperatorStatus(t *testing.T) {
	var output OutputEnvelope
	raw := []byte(`{
		"contract_version": "planner.v1",
		"outcome": "pause",
		"operator_status": null,
		"execute": null,
		"ask_human": null,
		"collect_context": null,
		"pause": {"reason": "waiting at a safe boundary"},
		"complete": null
	}`)
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputAcceptsCollectContextWithPluginToolCall(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Collect artifact metadata before proceeding",
			ToolCalls: []PluginToolCall{
				{Tool: "artifact_index.write"},
			},
		},
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputAcceptsCollectContextWithWorkerAction(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Create and dispatch an isolated worker",
			WorkerActions: []WorkerAction{
				{
					Action:     WorkerActionCreate,
					WorkerName: "frontend-worker",
					Scope:      "ui shell",
				},
				{
					Action:         WorkerActionDispatch,
					WorkerName:     "frontend-worker",
					TaskSummary:    "Implement the isolated UI shell slice",
					ExecutorPrompt: "Implement the isolated UI shell slice in this worker workspace only.",
				},
			},
		},
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputRejectsDispatchWithoutTaskSummary(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Dispatch an isolated worker",
			WorkerActions: []WorkerAction{
				{
					Action:         WorkerActionDispatch,
					WorkerName:     "frontend-worker",
					ExecutorPrompt: "Implement the isolated UI shell slice in this worker workspace only.",
				},
			},
		},
	}

	err := ValidateOutput(output)
	if err == nil {
		t.Fatal("ValidateOutput unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "collect_context.worker_actions[0].task_summary is required for dispatch") {
		t.Fatalf("expected dispatch task_summary validation error, got: %v", err)
	}
}

func TestValidateOutputRejectsDispatchWithoutExecutorPrompt(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Dispatch an isolated worker",
			WorkerActions: []WorkerAction{
				{
					Action:      WorkerActionDispatch,
					WorkerName:  "frontend-worker",
					TaskSummary: "Implement the isolated UI shell slice",
				},
			},
		},
	}

	err := ValidateOutput(output)
	if err == nil {
		t.Fatal("ValidateOutput unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "collect_context.worker_actions[0].executor_prompt is required for dispatch") {
		t.Fatalf("expected dispatch executor_prompt validation error, got: %v", err)
	}
}

func TestValidateOutputAcceptsCollectContextWithIntegrationAction(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Build a read-only integration preview",
			WorkerActions: []WorkerAction{
				{
					Action:    WorkerActionIntegrate,
					WorkerIDs: []string{"worker_1", "worker_2"},
				},
			},
		},
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputAcceptsCollectContextWithApplyAction(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Apply previously prepared worker integration output",
			WorkerActions: []WorkerAction{
				{
					Action:       WorkerActionApply,
					ArtifactPath: ".orchestrator/artifacts/integration/run_123/integration_20260420T120000Z.json",
					ApplyMode:    string(WorkerApplyModeAbortIfConflicts),
				},
			},
		},
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputAcceptsCollectContextWithWorkerPlan(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Partition isolated worker slices for one bounded turn",
			WorkerPlan: &WorkerPlan{
				Workers: []PlannedWorker{
					{
						Name:           "ui-worker",
						Scope:          "ui shell",
						TaskSummary:    "Implement the ui shell slice in an isolated worker.",
						ExecutorPrompt: "Implement the ui shell slice in this worker workspace only.",
					},
					{
						Name:           "api-worker",
						Scope:          "api slice",
						TaskSummary:    "Implement the api slice in an isolated worker.",
						ExecutorPrompt: "Implement the api slice in this worker workspace only.",
					},
				},
				IntegrationRequested: true,
				ApplyMode:            string(WorkerApplyModeNonConflicting),
			},
		},
	}

	if err := ValidateOutput(output); err != nil {
		t.Fatalf("ValidateOutput returned error: %v", err)
	}
}

func TestValidateOutputRejectsWorkerPlanApplyWithoutIntegration(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Try an invalid worker plan",
			WorkerPlan: &WorkerPlan{
				Workers: []PlannedWorker{
					{
						Name:           "ui-worker",
						Scope:          "ui shell",
						TaskSummary:    "Implement the ui shell slice in an isolated worker.",
						ExecutorPrompt: "Implement the ui shell slice in this worker workspace only.",
					},
				},
				IntegrationRequested: false,
				ApplyMode:            string(WorkerApplyModeNonConflicting),
			},
		},
	}

	err := ValidateOutput(output)
	if err == nil {
		t.Fatal("ValidateOutput unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "collect_context.worker_plan.integration_requested must be true when apply_mode is not unavailable") {
		t.Fatalf("expected worker plan integration validation error, got: %v", err)
	}
}

func TestValidateOutputRejectsMismatchedPayloads(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeExecute,
		Execute: &ExecuteOutcome{
			Task:               "Do one bounded implementation slice",
			AcceptanceCriteria: []string{"slice is reviewable"},
		},
		AskHuman: &AskHumanOutcome{
			Question: "Should we widen scope?",
		},
	}

	err := ValidateOutput(output)
	if err == nil {
		t.Fatal("ValidateOutput unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "exactly one outcome payload") {
		t.Fatalf("expected payload count error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "only execute payload may be set") {
		t.Fatalf("expected outcome mismatch error, got: %v", err)
	}
}

func TestValidateOutputRejectsInvalidCollectContext(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeCollectContext,
		CollectContext: &CollectContextOutcome{
			Focus: "Inspect persistence state",
		},
	}

	err := ValidateOutput(output)
	if err == nil {
		t.Fatal("ValidateOutput unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "collect_context must include at least one non-empty question, path, tool_call, worker_action, or worker_plan") {
		t.Fatalf("expected collect_context validation error, got: %v", err)
	}
}

func TestValidateOutputRejectsPlannerV2WithoutOperatorStatus(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV2,
		Outcome:         OutcomePause,
		Pause:           &PauseOutcome{Reason: "waiting for a later slice"},
	}

	err := ValidateOutput(output)
	if err == nil {
		t.Fatal("ValidateOutput unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "operator_status is required when contract_version=planner.v2") {
		t.Fatalf("expected operator_status validation error, got: %v", err)
	}
}

func TestValidateOutputRejectsEmptyCompleteSummary(t *testing.T) {
	output := OutputEnvelope{
		ContractVersion: ContractVersionV1,
		Outcome:         OutcomeComplete,
		Complete: &CompleteOutcome{
			Summary: "   ",
		},
	}

	err := ValidateOutput(output)
	if err == nil {
		t.Fatal("ValidateOutput unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "complete.summary is required") {
		t.Fatalf("expected complete summary validation error, got: %v", err)
	}
}

func TestRenderInstructionsIncludesContractRules(t *testing.T) {
	rendered, err := RenderInstructions()
	if err != nil {
		t.Fatalf("RenderInstructions returned error: %v", err)
	}

	for _, snippet := range []string{
		`These instructions are resent every planner turn`,
		`Set "contract_version" to "planner.v1".`,
		`Set "outcome" to exactly one of: execute, ask_human, collect_context, pause, complete.`,
		`Include every root payload key required by the schema; set non-matching outcome payloads to null.`,
		`Plugin tools, when present, are callable only through collect_context.tool_calls.`,
		`Worker actions, when used, are callable only through collect_context.worker_actions.`,
		`Planner-owned multi-worker partitioning, when used, is callable only through collect_context.worker_plan.`,
		`Use collect_context only when the missing information can still be gathered mechanically from the repo, artifacts, plugin tools, or worker actions available to you.`,
		`Use ask_human only when you are blocked on information that only the human can provide, such as intent, preference, missing external facts, or approval.`,
		`Use execute when the current state already contains enough information to choose the next highest-value bounded implementation step.`,
		`Do not emit near-identical collect_context outcomes repeatedly.`,
		`After repeated similar collect_context turns, either request one specific still-missing item with a concrete reason, transition to execute, or transition to ask_human.`,
		`Distinguish understanding the repo or state from actually fulfilling the run goal.`,
		`Use complete only when the requested objective for this run is truly satisfied, not merely because you now understand what should happen next.`,
		`If the user goal explicitly includes implementation, execution, code changes, building, fixing, or "do the next step," do not emit complete merely because repo exploration or planning is now sufficient.`,
		`For those execution-oriented goals, prefer execute for the next highest-value bounded task unless the requested work has actually been fulfilled.`,
		`Inspection-only, explanation-only, or analysis-only goals may complete without execute, but only when that requested inspection, explanation, or analysis has actually been delivered.`,
		`action="integrate" and explicit worker_ids.`,
		`action="apply", an explicit apply_mode, and either artifact_path or worker_ids.`,
		`If you need multiple isolated worker slices in one bounded turn, request collect_context.worker_plan`,
		`Worker-plan execution uses isolated worker workspaces and may run concurrently up to a bounded runtime limit.`,
		`apply_mode="apply_non_conflicting" applies only files outside the recorded conflict candidates`,
		`AGENTS.md, docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md, docs/ORCHESTRATOR_NON_NEGOTIABLES.md, docs/CLI_ENGINE_EXECPLAN.md, .orchestrator/roadmap.md, .orchestrator/decisions.md`,
		`Do not invent alternate path schemes such as "agents.md", "spec/", or ".agentic/".`,
		`Once canonical repo contract files or repo structure have already been provided in the state packet or collected_context results, do not keep re-exploring them unless you need one specific new missing detail.`,
		`When "raw_human_replies" is present in the input packet, treat it as raw terminal human input forwarded by the CLI without rewriting or summarization.`,
		`When "executor_result" is present in the input packet`,
		`latest_checkpoint.executor_turn tells you how many executor turns have already completed in this run.`,
		`If latest_checkpoint.executor_turn is zero and the goal is execution-oriented, repo understanding alone is not enough for complete.`,
		`If executor_result is present, use it to judge whether executor work has actually moved the run goal toward fulfillment.`,
		`When "collected_context" is present in the input packet`,
		`When "plugin_tools" is present in the input packet`,
		`When "collected_context.tool_results" is present in the input packet`,
		`Workers are isolated workspaces only. They do not share the main working tree, and they do not merge automatically.`,
		`For action="create", provide worker_name and scope.`,
		`For action="dispatch", provide worker_id or worker_name, plus both task_summary and executor_prompt.`,
		`For action="list", provide only action="list" unless a specific worker filter is already part of the contract state.`,
		`For action="remove", provide worker_id or worker_name.`,
		`For action="integrate", provide explicit worker_ids.`,
		`For action="apply", provide apply_mode and either artifact_path or worker_ids.`,
		`Do not emit partially populated worker action objects that are guaranteed to fail validation.`,
		`never emit action="dispatch" without task_summary and executor_prompt.`,
		`dispatch.task_summary should summarize the bounded worker task`,
		`When "collected_context.worker_results" is present in the input packet`,
		`When "collected_context.worker_plan" is present in the input packet`,
		`Integration artifacts are read-only decision input.`,
		`Integration apply results are mechanical write results only.`,
		`When "drift_review" is present in the input packet, treat it as reviewer critique data about roadmap or repo-contract alignment. It is not a control decision.`,
		`Recent event summaries may mention artifact paths under .orchestrator/artifacts/`,
	} {
		if !strings.Contains(rendered, snippet) {
			t.Fatalf("rendered template missing %q\n%s", snippet, rendered)
		}
	}
}

func TestRenderInstructionsHardensCollectContextProgression(t *testing.T) {
	rendered, err := RenderInstructions()
	if err != nil {
		t.Fatalf("RenderInstructions returned error: %v", err)
	}

	for _, snippet := range []string{
		`Use collect_context only when the missing information can still be gathered mechanically`,
		`Use ask_human only when you are blocked on information that only the human can provide`,
		`Use execute when the current state already contains enough information`,
		`Do not emit near-identical collect_context outcomes repeatedly.`,
		`After repeated similar collect_context turns, either request one specific still-missing item with a concrete reason, transition to execute, or transition to ask_human.`,
		`If recent turns already gathered the relevant repo contract files, repo markers, path listings, or other requested context, treat that context as available instead of re-requesting it.`,
	} {
		if !strings.Contains(rendered, snippet) {
			t.Fatalf("rendered template missing %q\n%s", snippet, rendered)
		}
	}
}

func TestRenderInstructionsHardensCompletionDiscipline(t *testing.T) {
	rendered, err := RenderInstructions()
	if err != nil {
		t.Fatalf("RenderInstructions returned error: %v", err)
	}

	for _, snippet := range []string{
		`Distinguish understanding the repo or state from actually fulfilling the run goal.`,
		`Use complete only when the requested objective for this run is truly satisfied, not merely because you now understand what should happen next.`,
		`If the user goal explicitly includes implementation, execution, code changes, building, fixing, or "do the next step," do not emit complete merely because repo exploration or planning is now sufficient.`,
		`For those execution-oriented goals, prefer execute for the next highest-value bounded task unless the requested work has actually been fulfilled.`,
		`Inspection-only, explanation-only, or analysis-only goals may complete without execute, but only when that requested inspection, explanation, or analysis has actually been delivered.`,
		`latest_checkpoint.executor_turn tells you how many executor turns have already completed in this run.`,
		`If latest_checkpoint.executor_turn is zero and the goal is execution-oriented, repo understanding alone is not enough for complete.`,
		`If executor_result is present, use it to judge whether executor work has actually moved the run goal toward fulfillment.`,
	} {
		if !strings.Contains(rendered, snippet) {
			t.Fatalf("rendered template missing %q\n%s", snippet, rendered)
		}
	}
}

func TestRenderInstructionsForContractV2IncludesOperatorStatusGuidance(t *testing.T) {
	rendered, err := RenderInstructionsForContract(ContractVersionV2)
	if err != nil {
		t.Fatalf("RenderInstructionsForContract returned error: %v", err)
	}

	for _, snippet := range []string{
		`Include operator_status as a safe operator-visible summary block on every turn.`,
		`operator_status must not expose hidden chain-of-thought.`,
		`operator_status.progress_percent must be an integer from 0 to 100.`,
		`progress reaching 100 is not, by itself, completion.`,
	} {
		if !strings.Contains(rendered, snippet) {
			t.Fatalf("rendered template missing %q\n%s", snippet, rendered)
		}
	}
}

func TestRenderInstructionsForContractV1RequiresNullableOperatorStatusGuidance(t *testing.T) {
	rendered, err := RenderInstructionsForContract(ContractVersionV1)
	if err != nil {
		t.Fatalf("RenderInstructionsForContract returned error: %v", err)
	}

	for _, snippet := range []string{
		`Include operator_status on every turn.`,
		`operator_status must be either a populated safe operator-facing object or null when unavailable.`,
		`operator_status must not expose hidden chain-of-thought.`,
		`progress reaching 100 is not, by itself, completion.`,
	} {
		if !strings.Contains(rendered, snippet) {
			t.Fatalf("rendered template missing %q\n%s", snippet, rendered)
		}
	}
}

func TestMarshalInputPacketIncludesState(t *testing.T) {
	input := validInputEnvelope()
	input.ExecutorResult = &ExecutorResultSummary{
		FinalMessage: "Implemented the bounded slice.",
		Success:      true,
		ThreadID:     "thr_123",
	}
	input.CollectedContext = &CollectedContextSummary{
		Focus:     "Inspect the planner persistence seam.",
		Questions: []string{"What changed under internal/state?"},
		Results: []CollectedContextResult{
			{
				RequestedPath: "internal/state",
				ResolvedPath:  `D:\Projects\agentic_loop\internal\state`,
				Kind:          "dir",
				Entries:       []string{"layout.go", "store.go"},
			},
		},
	}
	input.DriftReview = &DriftReviewSummary{
		Reviewer:                      "drift_watcher",
		Aligned:                       false,
		Concerns:                      []string{"planner task summary has no obvious lexical overlap with brief, roadmap, or decisions"},
		MissingContext:                []string{".orchestrator/decisions.md"},
		RecommendedPlannerAdjustments: []string{"Restate how the planned work aligns with the roadmap before forward work."},
		EvidencePaths:                 []string{".orchestrator/brief.md", ".orchestrator/roadmap.md"},
	}
	input.PluginTools = []PluginToolDescriptor{
		{
			Name:        "artifact_index.write",
			Description: "Write an artifact index under .orchestrator/artifacts/reports/<run-id>/.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
		},
	}
	input.CollectedContext.ToolResults = []PluginToolResult{
		{
			Tool:         "artifact_index.write",
			Success:      true,
			Message:      "artifact index written with 1 item(s)",
			ArtifactPath: ".orchestrator/artifacts/reports/run_123/artifact_index.json",
		},
	}
	input.CollectedContext.WorkerResults = []WorkerActionResult{
		{
			Action:       WorkerActionDispatch,
			Success:      true,
			Message:      "worker executor turn completed",
			ArtifactPath: ".orchestrator/artifacts/integration/run_123/integration_preview.json",
			Worker: &WorkerResultSummary{
				WorkerID:              "worker_123",
				WorkerName:            "frontend-worker",
				WorkerStatus:          "completed",
				AssignedScope:         "ui shell",
				WorktreePath:          `D:\Projects\agentic_loop.workers\frontend-worker`,
				WorkerTaskSummary:     "Implement the UI shell",
				ExecutorPromptSummary: "Implement the isolated UI shell slice in this worker workspace only.",
				WorkerResultSummary:   "worker executor turn completed",
				ExecutorThreadID:      "worker_thread_123",
				ExecutorTurnID:        "worker_turn_123",
			},
			Integration: &IntegrationSummary{
				WorkerIDs: []string{"worker_123", "worker_456"},
				Workers: []IntegrationWorkerSummary{
					{
						WorkerID:            "worker_123",
						WorkerName:          "frontend-worker",
						WorktreePath:        `D:\Projects\agentic_loop.workers\frontend-worker`,
						WorkerResultSummary: "worker executor turn completed",
						FileList:            []string{"src/shared.txt", "src/ui.txt"},
						DiffSummary:         []string{"modified: src/shared.txt", "added: src/ui.txt"},
					},
				},
				ConflictCandidates: []ConflictCandidate{
					{
						Path:      "src/shared.txt",
						Reason:    "same_file_touched",
						WorkerIDs: []string{"worker_123", "worker_456"},
					},
				},
				IntegrationPreview: "Read-only integration preview for 2 worker(s): 2 changed file(s), 1 conflict candidate(s).",
			},
			Apply: &IntegrationApplySummary{
				Status:             "completed",
				SourceArtifactPath: ".orchestrator/artifacts/integration/run_123/integration_preview.json",
				ApplyMode:          string(WorkerApplyModeNonConflicting),
				FilesApplied: []IntegrationAppliedFile{
					{
						WorkerID:   "worker_123",
						WorkerName: "frontend-worker",
						Path:       "src/ui.txt",
						ChangeKind: "added",
					},
				},
				FilesSkipped: []IntegrationSkippedFile{
					{
						WorkerID:   "worker_456",
						WorkerName: "backend-worker",
						Path:       "src/shared.txt",
						ChangeKind: "modified",
						Reason:     "conflict_candidate",
					},
				},
				ConflictCandidates: []ConflictCandidate{
					{
						Path:      "src/shared.txt",
						Reason:    "same_file_touched",
						WorkerIDs: []string{"worker_123", "worker_456"},
					},
				},
				BeforeSummary: "integration apply input: 2 worker(s), 2 candidate file change(s), 1 conflict candidate(s).",
				AfterSummary:  "integration apply completed: applied 1 file(s), skipped 1 file(s) using apply_non_conflicting.",
			},
		},
	}
	input.CollectedContext.WorkerPlan = &WorkerPlanResult{
		Status:                  "completed",
		WorkerIDs:               []string{"worker_123", "worker_456"},
		Workers:                 []WorkerResultSummary{*input.CollectedContext.WorkerResults[0].Worker},
		ConcurrencyLimit:        2,
		IntegrationRequested:    true,
		IntegrationArtifactPath: ".orchestrator/artifacts/integration/run_123/integration_preview.json",
		IntegrationPreview:      "Read-only integration preview for 2 worker(s): 2 changed file(s), 1 conflict candidate(s).",
		ApplyMode:               string(WorkerApplyModeNonConflicting),
		ApplyArtifactPath:       ".orchestrator/artifacts/integration/run_123/integration_apply.json",
		Apply:                   input.CollectedContext.WorkerResults[0].Apply,
		Message:                 "worker plan completed across 2 isolated worker(s) with concurrency limit=2",
	}

	packet, err := MarshalInputPacket(input)
	if err != nil {
		t.Fatalf("MarshalInputPacket returned error: %v", err)
	}

	for _, snippet := range []string{
		`"contract_version": "planner.v1"`,
		`"run_id": "run_123"`,
		`"goal": "stabilize the planner contract"`,
		`"executor_turn": 0`,
		`"executor_result": {`,
		`"final_message": "Implemented the bounded slice."`,
		`"collected_context": {`,
		`"requested_path": "internal/state"`,
		`"drift_review": {`,
		`"reviewer": "drift_watcher"`,
		`"recommended_planner_adjustments": [`,
		`"plugin_tools": [`,
		`"name": "artifact_index.write"`,
		`"tool_results": [`,
		`"artifact_path": ".orchestrator/artifacts/reports/run_123/artifact_index.json"`,
		`"worker_results": [`,
		`"worker_plan": {`,
		`"concurrency_limit": 2`,
		`"integration_requested": true`,
		`"apply_artifact_path": ".orchestrator/artifacts/integration/run_123/integration_apply.json"`,
		`"worker_name": "frontend-worker"`,
		`"worker_status": "completed"`,
		`"worker_task_summary": "Implement the UI shell"`,
		`"integration": {`,
		`"apply": {`,
		`"apply_mode": "apply_non_conflicting"`,
		`"files_applied": [`,
		`"files_skipped": [`,
		`"conflict_candidates": [`,
		`"reason": "same_file_touched"`,
		`"agents_md_path": "AGENTS.md"`,
		`"updated_spec_path": "docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md"`,
		`"orchestrator_dir_path": ".orchestrator"`,
		`"roadmap_path": ".orchestrator/roadmap.md"`,
		`"decisions_path": ".orchestrator/decisions.md"`,
	} {
		if !strings.Contains(packet, snippet) {
			t.Fatalf("input packet missing %q\n%s", snippet, packet)
		}
	}
}

func TestMarshalInputPacketExposesExecutorProgressWithoutExecutorResult(t *testing.T) {
	input := validInputEnvelope()
	input.Goal = "Implement the next bounded code change"
	input.LatestCheckpoint.ExecutorTurn = 1

	packet, err := MarshalInputPacket(input)
	if err != nil {
		t.Fatalf("MarshalInputPacket returned error: %v", err)
	}

	if !strings.Contains(packet, `"executor_turn": 1`) {
		t.Fatalf("input packet missing executor_turn progress\n%s", packet)
	}
	if strings.Contains(packet, `"executor_result": {`) {
		t.Fatalf("input packet unexpectedly included executor_result\n%s", packet)
	}
}

func TestMarshalInputPacketRejectsInvalidInput(t *testing.T) {
	input := validInputEnvelope()
	input.ContractVersion = "planner.v0"
	input.Capabilities.Executor = CapabilityStatus("unknown")

	_, err := MarshalInputPacket(input)
	if err == nil {
		t.Fatal("MarshalInputPacket unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), `contract_version must be "planner.v1"`) {
		t.Fatalf("expected contract version validation error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "capabilities.executor must be a known capability status") {
		t.Fatalf("expected capability validation error, got: %v", err)
	}
}

func TestMarshalInputPacketRejectsMissingCanonicalRepoPaths(t *testing.T) {
	input := validInputEnvelope()
	input.RepoContracts.AgentsMDPath = ""
	input.RepoContracts.OrchestratorDirPath = ""

	_, err := MarshalInputPacket(input)
	if err == nil {
		t.Fatal("MarshalInputPacket unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "repo_contracts.agents_md_path is required") {
		t.Fatalf("expected agents_md_path validation error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "repo_contracts.orchestrator_dir_path is required") {
		t.Fatalf("expected orchestrator_dir_path validation error, got: %v", err)
	}
}

func TestMarshalInputPacketAcceptsRawWhitespaceHumanReply(t *testing.T) {
	input := validInputEnvelope()
	input.RawHumanReplies = []RawHumanReply{
		{
			ID:         "human_2",
			Source:     "terminal",
			ReceivedAt: time.Date(2026, 4, 13, 15, 0, 0, 0, time.UTC),
			Payload:    "   \r\n",
		},
	}

	packet, err := MarshalInputPacket(input)
	if err != nil {
		t.Fatalf("MarshalInputPacket returned error: %v", err)
	}
	if !strings.Contains(packet, `"payload": "   \r\n"`) {
		t.Fatalf("packet missing raw human payload\n%s", packet)
	}
}

func validInputEnvelope() InputEnvelope {
	now := time.Date(2026, 4, 13, 14, 30, 0, 0, time.UTC)
	return InputEnvelope{
		ContractVersion: ContractVersionV1,
		RunID:           "run_123",
		RepoPath:        `D:\Projects\agentic_loop`,
		Goal:            "stabilize the planner contract",
		RunStatus:       "initialized",
		LatestCheckpoint: Checkpoint{
			Sequence:     1,
			Stage:        "bootstrap",
			Label:        "run_initialized",
			SafePause:    false,
			PlannerTurn:  0,
			ExecutorTurn: 0,
			CreatedAt:    now,
		},
		RecentEvents: []EventPreview{
			{
				At:      now,
				Type:    "run.created",
				Summary: "A durable run record was created.",
			},
		},
		RepoContracts: RepoContractAvailability{
			HasAgentsMD:         true,
			AgentsMDPath:        "AGENTS.md",
			HasUpdatedSpec:      true,
			UpdatedSpecPath:     "docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md",
			HasNonNegotiables:   true,
			NonNegotiablesPath:  "docs/ORCHESTRATOR_NON_NEGOTIABLES.md",
			HasExecPlan:         true,
			ExecPlanPath:        "docs/CLI_ENGINE_EXECPLAN.md",
			OrchestratorDirPath: ".orchestrator",
			RoadmapPath:         ".orchestrator/roadmap.md",
			DecisionsPath:       ".orchestrator/decisions.md",
		},
		RawHumanReplies: []RawHumanReply{
			{
				ID:         "human_1",
				Source:     "terminal",
				ReceivedAt: now,
				Payload:    "Implement the planner contract next.",
			},
		},
		Capabilities: CapabilityMarkers{
			Planner:  CapabilityContractOnly,
			Executor: CapabilityDeferred,
			NTFY:     CapabilityDeferred,
		},
	}
}
