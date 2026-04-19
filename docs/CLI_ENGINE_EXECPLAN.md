# CLI ENGINE EXECPLAN

Status: Secondary implementation plan and discipline document

## Role Of This Document

This document remains useful and authoritative for:

- persistence expectations,
- anti-patterns,
- acceptance criteria,
- implementation discipline.

If this document conflicts with `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md` on architecture, the updated spec wins.

## Persistence Expectations

- Use SQLite as the indexed durable state store.
- Use JSONL as the append-only event stream.
- Persist planner turns, executor turns, human messages, runtime events, and notification events.
- Commit a resumable checkpoint after each completed AI turn.
- Resume from persisted state instead of reconstructing from terminal output.

## Event Discipline

The event log should make it possible to answer:

- what the planner decided,
- what the executor was asked to do,
- what the human said,
- what runtime failures occurred,
- where the latest safe resume point is.

If an event cannot be reconstructed later, it probably belongs in persistence.

## Acceptance Criteria For Early Implementation

Early implementation work should converge on these outcomes:

- The CLI can start or resume a durable session without owning workflow policy.
- The planner is the only component deciding next actions and completion.
- The executor can be driven through the primary integration target and report results durably.
- Human replies enter the system unchanged.
- A safe pause can happen after an AI turn and later resume cleanly.
- Operator visibility is present enough to inspect recent planner and executor activity.

## Anti-Patterns

- CLI-invented stop conditions.
- Executor self-looping without planner approval.
- Hidden summarization of human input.
- Ephemeral-only session state.
- Large speculative scaffolds that outpace the current working slice.
- Multi-worker orchestration in v1.
- Conflating presentation settings with authority or policy.

## Implementation Discipline

- Build in small slices with explicit acceptance criteria.
- Prefer the narrowest vertical path that proves a contract.
- Keep diffs reviewable.
- Record locked decisions in ADRs or canonical docs.
- When the architecture source and older plan differ, update code and docs toward the newer architecture rather than preserving drift.

## Practical Use

Use this document to keep implementation honest while the primary architecture lives in the updated spec.
