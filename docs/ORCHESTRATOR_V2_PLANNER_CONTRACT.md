# ORCHESTRATOR V2 PLANNER CONTRACT

Status: Proposed V2 planner contract extension spec

## Purpose

Define planner-facing extensions needed for the V2 console without shifting authority away from the planner or turning the engine into a semantic decision-maker.

This document does not replace the existing locked `planner.v1` runtime contract today. It describes the next contract evolution needed for:

- safe operator-visible planner status
- control-chat intervention
- side-chat separation
- pending action reconsideration
- progress display

## Design Goals

1. Keep planner in charge of decisions.
2. Add safe operator-visible status fields without exposing chain-of-thought.
3. Give the planner enough context to handle interventions at safe points.
4. Separate live-run control chat from side chat.
5. Keep validation strict and machine-oriented.

## Contract Direction

Recommended direction:

- add a new planner contract version rather than silently widening `planner.v1`
- keep `planner.v1` working for existing CLI behavior
- gate new fields and intervention flows behind the new version

Working name:

- `planner.v2`

## Planner Output Extensions

In addition to the existing control outcome envelope, V2 should add safe operator-visible fields.

Suggested top-level output shape:

```json
{
  "contract_version": "planner.v2",
  "outcome": "execute",
  "execute": {},
  "ask_human": null,
  "collect_context": null,
  "pause": null,
  "complete": null,
  "operator_status": {
    "operator_message": "Implementing the next bounded step.",
    "current_focus": "input handling and game loop integration",
    "next_intended_step": "dispatch bounded input-handler implementation",
    "why_this_step": "current progress depends on input plumbing before collisions or scoring",
    "progress_percent": 32,
    "progress_confidence": "medium",
    "progress_basis": "static scene exists; input, collisions, and scoring remain"
  }
}
```

## Operator Status Fields

### `operator_message`

Short safe summary for the human operator.

Rules:

- concise
- no hidden reasoning dump
- no chain-of-thought

### `current_focus`

What the planner is currently working on.

### `next_intended_step`

Human-readable description of the next bounded intended action.

### `why_this_step`

Short rationale for the current intended step.

Must be:

- brief
- factual
- not hidden chain-of-thought

### `progress_percent`

Integer 0-100 supplied by the planner.

### `progress_confidence`

Enum:

- `low`
- `medium`
- `high`

### `progress_basis`

Short factual basis for the progress estimate.

## Validation Rules For Operator Status

- `operator_status` is optional but recommended in V2
- if present, all fields should be validated strictly
- `progress_percent` must be integer 0-100
- `progress_confidence` must be one of the defined enums
- `operator_message`, `current_focus`, `next_intended_step`, `why_this_step`, and `progress_basis` must be bounded strings

## Pending Action Handling

V2 requires the planner to reason about engine-held pending work.

The engine should send planner input including:

- pending planner action if any
- pending executor prompt if any
- pending dispatch target if any
- hold reason if the action is being paused for intervention

Suggested input shape:

```json
{
  "pending_action": {
    "present": true,
    "turn_type": "executor_dispatch",
    "planner_outcome": "execute",
    "planner_response_id": "resp_123",
    "pending_executor_prompt": "Apply the bounded edit...",
    "pending_dispatch_target": {
      "kind": "primary_executor",
      "worker_id": null
    },
    "pending_reason": "planner_selected_execute",
    "held": true,
    "hold_reason": "control_message_queued"
  }
}
```

The planner then decides whether to:

- proceed with the existing pending action
- replace it
- cancel it
- switch to collect_context
- ask_human
- pause
- complete

The engine must not decide which of those is correct.

## Control Chat Intervention Input

Control Chat should be explicit planner input, not inferred from event text.

Suggested input block:

```json
{
  "control_intervention": {
    "present": true,
    "raw_message": "Make the wall red, not blue.",
    "source": "control_chat",
    "reason": "operator_intervention_at_safe_point",
    "queued_at": "2026-04-22T17:00:00Z"
  }
}
```

Planner expectations:

- treat the raw message as data
- reconsider the pending next step in light of it
- do not assume the engine already changed anything

## Side Chat Separation

Side chat must not affect the active run unless explicitly promoted.

Planner contract guidance:

- side chat should use a separate assistant or helper flow
- side-chat context may include repo snapshots or summaries
- side-chat messages must not appear in active-run planner input unless promoted into control intervention input

## Planner Input Extensions

Suggested new `planner.v2` input additions:

- `operator_mode`
- `control_intervention`
- `pending_action`
- `verbosity`
- `planner_status_requested`
- `run_mode`

### `operator_mode`

Enum:

- `headless_cli`
- `console_attached`

Purpose:

- lets the planner know whether richer operator-facing status is useful

### `verbosity`

Enum:

- `quiet`
- `normal`
- `verbose`
- `trace`

Purpose:

- lets the planner tailor safe operator messages without changing authority

### `run_mode`

Enum examples:

- `survey`
- `build`
- `harden`
- `release`
- `autonomous`
- `cautious`

Purpose:

- planner instruction/config input only

## Guidance For Completion In V2

The V2 operator-status additions must not weaken completion discipline.

Rules:

- `operator_message` is not completion
- progress reaching 100 is not by itself completion
- only structured `outcome="complete"` remains completion authority

## Suggested Schema Discipline

As with `planner.v1`, all object nodes should remain strict:

- `additionalProperties: false`
- explicit required fields
- nullability only where intentionally allowed

## Compatibility Plan

Recommended rollout:

1. keep `planner.v1` active in production
2. add tests and schema for proposed `planner.v2`
3. add engine support for optional operator-status fields
4. switch console-facing runs to `planner.v2` only after validation and status rendering are ready

Current engine migration state:

- live runtime now accepts and uses `operator_status` additively on `planner.v1`
- `planner.v2` remains the stricter future contract where operator-status is expected on every turn
- no control semantics moved into the CLI during this migration

## Non-Goals

- no chain-of-thought exposure
- no GUI-specific semantic outcomes
- no planner bypass through control chat
- no side-chat leakage into active run control without explicit promotion

## Recommended First Planner Contract Chunk

1. add `operator_status` fields behind tests
2. add `pending_action` and `control_intervention` input support
3. validate strict schema
4. keep runtime outcome semantics unchanged
