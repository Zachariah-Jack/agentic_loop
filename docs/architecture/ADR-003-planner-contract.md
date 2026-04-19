# ADR-003: Planner Contract

Status: Accepted
Date: 2026-04-13

## Context

The orchestrator now has a durable persistence spine, but it still needs an explicit planner contract before any live planner integration is added.

The planner is the decision-maker. The CLI is inert and must not derive control from free-form assistant prose. If the planner is going to drive the loop safely, its inputs and outputs must be explicit, versioned, and machine-oriented.

## Decision

Lock the initial planner contract at version `planner.v1`.

`planner.v1` includes:

- a versioned planner input envelope,
- a versioned planner output envelope,
- explicit outcome kinds,
- validation rules that reject invalid or ambiguous control,
- a reusable instruction/template renderer that can be re-sent on every planner turn.

The locked outcome kinds for `planner.v1` are:

- `execute`
- `ask_human`
- `collect_context`
- `pause`
- `complete`

Planner control is structured, not prose-based.

- The planner must return a structured output envelope.
- Control flow must come from the explicit outcome kind plus its matching payload.
- The system must not infer control by parsing free-form narrative text.

Live Responses integration comes later and must use this contract instead of inventing a second control model.

## Consequences

- Planner turns can be validated before they affect runtime behavior.
- The core planner instructions can be re-sent every turn without depending on hidden session memory.
- Future live planner clients must map their request and response handling onto `planner.v1`.
- If the planner contract changes later, the change should happen through a new ADR and a new contract version.
