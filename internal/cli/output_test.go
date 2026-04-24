package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestWriteCommandReportRendersOperatorStatusByVerbosity(t *testing.T) {
	baseReport := commandReport{
		Command:   "run",
		RunAction: "created_new_run",
		Run: state.Run{
			ID:     "run_123",
			Goal:   "ship the next bounded slice",
			Status: state.StatusInitialized,
			LatestCheckpoint: state.Checkpoint{
				Sequence:  2,
				Stage:     "planner",
				Label:     "planner_turn_completed",
				SafePause: true,
				CreatedAt: time.Now().UTC(),
			},
		},
		FirstPlannerResult: planner.Result{
			ResponseID: "resp_123",
			Output: planner.OutputEnvelope{
				ContractVersion: planner.ContractVersionV1,
				Outcome:         planner.OutcomeExecute,
				Execute: &planner.ExecuteOutcome{
					Task:               "implement the bounded slice",
					AcceptanceCriteria: []string{"tests still pass"},
				},
				OperatorStatus: &planner.OperatorStatus{
					OperatorMessage:    "Implementing the next bounded slice.",
					CurrentFocus:       "operator-status rendering",
					NextIntendedStep:   "persist and print safe planner summaries",
					WhyThisStep:        "live operator visibility now depends on real planner status output.",
					ProgressPercent:    58,
					ProgressConfidence: planner.ProgressConfidenceMedium,
					ProgressBasis:      "runtime plumbing exists and the current slice is surfacing it through CLI and protocol.",
				},
			},
		},
	}

	tests := []struct {
		name      string
		verbosity outputVerbosity
		wants     []string
		avoids    []string
	}{
		{
			name:      "quiet",
			verbosity: outputVerbosityQuiet,
			avoids: []string{
				"planner.operator_message:",
				"planner.current_focus:",
				"planner.status_json:",
			},
		},
		{
			name:      "normal",
			verbosity: outputVerbosityNormal,
			wants: []string{
				"planner.operator_message: Implementing the next bounded slice.",
				"planner.progress_percent: 58",
			},
			avoids: []string{
				"planner.current_focus:",
				"planner.progress_confidence:",
				"planner.status_json:",
			},
		},
		{
			name:      "verbose",
			verbosity: outputVerbosityVerbose,
			wants: []string{
				"planner.operator_message: Implementing the next bounded slice.",
				"planner.current_focus: operator-status rendering",
				"planner.next_intended_step: persist and print safe planner summaries",
				"planner.why_this_step: live operator visibility now depends on real planner status output.",
			},
			avoids: []string{
				"planner.progress_confidence:",
				"planner.status_json:",
			},
		},
		{
			name:      "trace",
			verbosity: outputVerbosityTrace,
			wants: []string{
				"planner.progress_confidence: medium",
				"planner.progress_basis: runtime plumbing exists and the current slice is surfacing it through CLI and protocol.",
				"planner.status_json:",
				"\"operator_message\": \"Implementing the next bounded slice.\"",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := writeCommandReport(&stdout, tc.verbosity, baseReport); err != nil {
				t.Fatalf("writeCommandReport() error = %v", err)
			}

			output := stdout.String()
			for _, want := range tc.wants {
				if !strings.Contains(output, want) {
					t.Fatalf("output missing %q\n%s", want, output)
				}
			}
			for _, avoid := range tc.avoids {
				if strings.Contains(output, avoid) {
					t.Fatalf("output unexpectedly contained %q\n%s", avoid, output)
				}
			}
		})
	}
}
