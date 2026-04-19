package planner

import (
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
	if !strings.Contains(err.Error(), "collect_context must include at least one non-empty question or path") {
		t.Fatalf("expected collect_context validation error, got: %v", err)
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
	} {
		if !strings.Contains(rendered, snippet) {
			t.Fatalf("rendered template missing %q\n%s", snippet, rendered)
		}
	}
}

func TestMarshalInputPacketIncludesState(t *testing.T) {
	input := validInputEnvelope()

	packet, err := MarshalInputPacket(input)
	if err != nil {
		t.Fatalf("MarshalInputPacket returned error: %v", err)
	}

	for _, snippet := range []string{
		`"contract_version": "planner.v1"`,
		`"run_id": "run_123"`,
		`"goal": "stabilize the planner contract"`,
	} {
		if !strings.Contains(packet, snippet) {
			t.Fatalf("input packet missing %q\n%s", snippet, packet)
		}
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
			HasAgentsMD:       true,
			HasUpdatedSpec:    true,
			HasNonNegotiables: true,
			HasExecPlan:       true,
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
