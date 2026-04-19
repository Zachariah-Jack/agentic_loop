package cli

import (
	"bytes"
	"strings"
	"testing"

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

	if err := writeBoundedCycleReport(&stdout, inv, result, boundedCycleMode{
		Command:   "resume",
		RunAction: "resumed_existing_run",
	}); err != nil {
		t.Fatalf("writeBoundedCycleReport() error = %v", err)
	}

	for _, want := range []string{
		"command: resume",
		"run_action: resumed_existing_run",
		"run_id: run_123",
		"second_planner_response_id: resp_2",
		"next_step: bounded run stopped after the post-executor planner turn; that outcome was persisted and not executed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("report missing %q\n%s", want, stdout.String())
		}
	}
}
