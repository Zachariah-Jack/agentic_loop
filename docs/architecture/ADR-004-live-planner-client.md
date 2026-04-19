# ADR-004: Live Planner Client

Status: Accepted
Date: 2026-04-13

## Context

The repository now has a durable persistence spine and a locked `planner.v1` contract, but it still needs a real live planner transport.

The planner must remain the decision-maker, and the CLI must remain inert. A live planner call therefore needs to:

- use the Responses API as transport,
- send the locked planner contract explicitly,
- validate planner output before runtime use,
- persist transport state needed for the next planner turn.

## Decision

The live planner transport for v1 is the OpenAI Responses API.

The live planner client must:

- send `planner.v1` instructions and input explicitly on every planner turn,
- re-send planner instructions every turn instead of relying on hidden session memory,
- include `previous_response_id` when a prior successful planner response exists,
- persist the latest successful planner response ID as durable run state for the next planner turn,
- validate the returned planner output against `planner.v1` before any runtime use.

For this slice, one live planner turn means:

1. create or load a run,
2. build a planner input packet from persisted state,
3. call the Responses API once,
4. validate the returned `planner.v1` output,
5. persist the new `previous_response_id`,
6. persist a checkpoint for the completed planner turn,
7. stop without executing the chosen outcome.

## Consequences

- Planner transport is now real while executor transport remains deferred.
- Planner instructions are explicit and repeatable across turns.
- `previous_response_id` becomes durable runtime state rather than transient process memory.
- Runtime code must reject invalid planner envelopes even if the API call itself succeeded.
