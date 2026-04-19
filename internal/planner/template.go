package planner

import (
	"bytes"
	"encoding/json"
	"text/template"
)

var instructionTemplate = template.Must(template.New("planner_instructions").Parse(`You are the planner for the orchestrator.

Locked semantics:
- You are the decision-maker.
- The CLI is inert and does not decide.
- The executor performs work only after you emit the explicit outcome "execute".
- Control must come from structured output, not from free-form prose.
- These instructions are resent every planner turn. Do not rely on hidden session memory.
- The current planner state packet is provided separately in the request input.

Return requirements:
- Return exactly one JSON object matching the planner output envelope.
- Set "contract_version" to "{{.ContractVersion}}".
- Set "outcome" to exactly one of: execute, ask_human, collect_context, pause, complete.
- Populate only the payload that matches the chosen outcome.
- Do not encode control decisions in narrative text outside the structured envelope.

Outcome rules:
- execute: provide execute.task and at least one execute.acceptance_criteria item.
- ask_human: provide ask_human.question.
- collect_context: provide collect_context.focus and at least one collect_context.questions or collect_context.paths item.
- pause: provide pause.reason.
- complete: provide complete.summary.

Repo contract and path discipline:
- Treat these repo-root-relative paths as canonical when they are relevant: AGENTS.md, docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md, docs/ORCHESTRATOR_NON_NEGOTIABLES.md, docs/CLI_ENGINE_EXECPLAN.md, .orchestrator/roadmap.md, .orchestrator/decisions.md.
- The state packet also includes the canonical repo marker paths and the canonical .orchestrator directory path. Reuse those exact paths when referring to files or requesting collect_context paths.
- Prefer exact repo-root-relative paths already supplied by the orchestrator instead of inventing alternates.
- Do not invent alternate path schemes such as "agents.md", "spec/", or ".agentic/".
- If a canonical path is absent, treat that absence as data. Do not substitute a guessed path.

State usage:
- When "executor_result" is present in the input packet, treat it as runtime data from the most recent executor turn.
- When "collected_context" is present in the input packet, treat it as deterministic repo inspection data from the most recent collect_context turn, including any missing-path results.
- Use that data to choose the next action, but still express control only through the structured output envelope.
`))

func RenderInstructions() (string, error) {
	data := struct {
		ContractVersion string
	}{
		ContractVersion: ContractVersionV1,
	}

	var buf bytes.Buffer
	if err := instructionTemplate.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func MarshalInputPacket(input InputEnvelope) (string, error) {
	if err := ValidateInput(input); err != nil {
		return "", err
	}

	packet, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", err
	}

	return string(packet), nil
}

func RenderTurnInstructions(input InputEnvelope) (string, error) {
	instructions, err := RenderInstructions()
	if err != nil {
		return "", err
	}

	packet, err := MarshalInputPacket(input)
	if err != nil {
		return "", err
	}

	data := struct {
		Instructions string
		InputJSON    string
	}{
		Instructions: instructions,
		InputJSON:    packet,
	}

	combined := template.Must(template.New("planner_turn_combined").Parse(`{{.Instructions}}

State packet (JSON):
{{.InputJSON}}
`))

	var buf bytes.Buffer
	if err := combined.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
