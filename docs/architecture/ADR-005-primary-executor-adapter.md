## ADR-005: Primary Executor Adapter

Status: Accepted
Date: 2026-04-14

## Context

The orchestrator now has durable run state, a planner contract, and a live planner client. The next narrow slice is a real primary executor adapter that can prove one safe Codex executor turn without wiring the full planner to executor loop.

The updated CLI architecture already locks `codex app-server` as the primary executor target for v1. This slice needs to preserve that architecture while staying honest about transport state, executor results, and deferred loop behavior.

## Decision

The primary executor transport for v1 remains `codex app-server`.

This repository will use a narrow executor adapter contract that:

- treats executor transport metadata as durable run state,
- records executor start, completion, and failure events durably,
- treats executor results as data for the planner to consume later,
- keeps probe behavior separate from planner-owned execution decisions.

Executor transport state must be durable.

At minimum, durable state must preserve:

- executor transport kind,
- executor thread identifier when available,
- executor thread path when available,
- latest executor turn identifier when available,
- latest executor turn status,
- latest executor failure message when present.

Executor outputs are runtime data, not CLI decisions.

- A completed executor turn may return text, identifiers, and transport status.
- That result is persisted and shown to the operator.
- The planner will consume that data in a later slice.
- The CLI must not treat executor output as completion authority.

A fallback probe path may exist later, but it does not replace the primary architecture.

- `codex exec --json` remains the locked fallback executor transport target.
- If used in a later slice, it is a fallback probe or compatibility path only.
- It does not change the fact that `codex app-server` is the primary executor transport target for v1.

## Consequences

- The orchestrator can now persist real executor thread and turn metadata without pretending the planner loop is finished.
- Operator visibility can show whether executor transport is ready and what the latest executor turn returned.
- Future planner to executor wiring should consume the durable executor result instead of inventing a second transport-state model.
