package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"orchestrator/internal/config"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestWriteBoundedCycleReportMarksResumeAction(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	inv := Invocation{}
	result := orchestration.Result{
		Run: state.Run{
			ID:                 "run_123",
			Goal:               "continue the latest unfinished run",
			Status:             state.StatusInitialized,
			PreviousResponseID: "resp_2",
			LatestCheckpoint: state.Checkpoint{
				Sequence:  4,
				Stage:     "planner",
				Label:     "planner_turn_post_executor",
				SafePause: true,
			},
		},
		FirstPlannerResult: planner.Result{
			ResponseID: "resp_1",
			Output: planner.OutputEnvelope{
				ContractVersion: planner.ContractVersionV1,
				Outcome:         planner.OutcomeExecute,
				Execute: &planner.ExecuteOutcome{
					Task:               "implement one bounded slice",
					AcceptanceCriteria: []string{"persist executor result"},
				},
			},
		},
		SecondPlannerTurn: &planner.Result{
			ResponseID: "resp_2",
			Output: planner.OutputEnvelope{
				ContractVersion: planner.ContractVersionV1,
				Outcome:         planner.OutcomePause,
				Pause:           &planner.PauseOutcome{Reason: "stop at safe pause"},
			},
		},
		ExecutorDispatched: true,
	}

	if err := writeBoundedCycleReport(&stdout, inv, result, nil, boundedCycleMode{
		Command:   "resume",
		RunAction: "resumed_existing_run",
	}); err != nil {
		t.Fatalf("writeBoundedCycleReport() error = %v", err)
	}

	for _, want := range []string{
		"command: resume",
		"run_action: resumed_existing_run",
		"cycle_number: 1",
		"run_id: run_123",
		"stop_reason: planner_pause",
		"second_planner_outcome: pause",
		"next_operator_action: continue_existing_run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("report missing %q\n%s", want, stdout.String())
		}
	}
}

func TestWriteBoundedCycleReportMarksPlannerDeclaredCompletion(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	inv := Invocation{}
	result := orchestration.Result{
		Run: state.Run{
			ID:                 "run_456",
			Goal:               "finish the bounded orchestrator slice",
			Status:             state.StatusCompleted,
			PreviousResponseID: "resp_complete",
			LatestCheckpoint: state.Checkpoint{
				Sequence:  2,
				Stage:     "planner",
				Label:     "planner_declared_complete",
				SafePause: true,
			},
		},
		FirstPlannerResult: planner.Result{
			ResponseID: "resp_complete",
			Output: planner.OutputEnvelope{
				ContractVersion: planner.ContractVersionV1,
				Outcome:         planner.OutcomeComplete,
				Complete:        &planner.CompleteOutcome{Summary: "The task is complete."},
			},
		},
	}

	if err := writeBoundedCycleReport(&stdout, inv, result, nil, boundedCycleMode{
		Command:   "run",
		RunAction: "created_new_run",
	}); err != nil {
		t.Fatalf("writeBoundedCycleReport() error = %v", err)
	}

	for _, want := range []string{
		"status: completed",
		"cycle_number: 1",
		"stop_reason: planner_complete",
		"first_planner_outcome: complete",
		"next_operator_action: no_action_required_run_completed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("report missing %q\n%s", want, stdout.String())
		}
	}
}

func TestWriteBoundedCycleReportMarksMissingRequiredConfigFailure(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	inv := Invocation{Config: config.Config{Verbosity: "verbose"}}
	result := orchestration.Result{
		Run: state.Run{
			ID:                  "run_missing_key",
			Goal:                "attempt one bounded planner turn",
			Status:              state.StatusInitialized,
			RuntimeIssueReason:  orchestration.StopReasonMissingRequiredConfig,
			RuntimeIssueMessage: "OPENAI_API_KEY is required for live planner calls",
			LatestCheckpoint: state.Checkpoint{
				Sequence:  1,
				Stage:     "bootstrap",
				Label:     "run_initialized",
				SafePause: false,
			},
		},
	}

	err := planner.ErrMissingAPIKey
	if reportErr := writeBoundedCycleReport(&stdout, inv, result, err, boundedCycleMode{
		Command:   "run",
		RunAction: "created_new_run",
	}); reportErr != nil {
		t.Fatalf("writeBoundedCycleReport() error = %v", reportErr)
	}

	for _, want := range []string{
		"stop_reason: missing_required_config",
		"cycle_number: 1",
		"cycle_error: OPENAI_API_KEY is required for live planner calls",
		"runtime_issue.reason: missing_required_config",
		"latest_checkpoint.label: run_initialized",
		"next_operator_action: inspect_status",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("report missing %q\n%s", want, stdout.String())
		}
	}
}

func TestTerminalHumanInteractorReadsRawReply(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	input := strings.NewReader("  keep this raw  \r\n")
	interactor := terminalHumanInteractor{
		input:  input,
		output: &output,
	}

	reply, err := interactor.Ask(context.Background(), state.Run{ID: "run_123"}, planner.AskHumanOutcome{
		Question: "What should we do next?",
		Context:  "Reply with one raw line.",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if reply.Source != "terminal" {
		t.Fatalf("reply.Source = %q, want terminal", reply.Source)
	}
	if reply.Payload != "  keep this raw  \r\n" {
		t.Fatalf("reply.Payload = %q, want raw line", reply.Payload)
	}

	for _, want := range []string{
		"planner_question:",
		"What should we do next?",
		"planner_question_context:",
		"Reply with one raw line.",
		"human_reply> ",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q\n%s", want, output.String())
		}
	}
}
