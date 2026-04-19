# ORCHESTRATOR CLI UPDATED SPEC

Status: Accepted primary architecture source

## Purpose

Define the canonical v1 architecture for the orchestrator. This document is the primary source when architecture documents conflict.

## Core Model

The orchestrator is a durable loop built around three distinct roles:

- Planner: the only component allowed to make workflow decisions.
- Executor: the component that performs implementation work and writes artifacts.
- CLI: an inert bridge and runtime harness that connects humans, planner, executor, persistence, and terminal UX.

The CLI exists to transport data, persist state, manage subprocesses or sessions, and render visibility. It does not own policy.

## Architecture Truths

- The CLI is inert and not a brain.
- The planner makes decisions.
- The executor writes code.
- The CLI does not invent stop conditions.
- Human replies are forwarded raw.
- Safe pause points occur after AI turns.
- Visibility, verbosity control, and hotkeys are desired operator features.
- Parallel executor workers may later be supported when boundaries are clear.

## Role Contracts

### Planner

The planner owns:

- Selecting the next action.
- Deciding whether to ask the human for input.
- Deciding whether to run the executor again.
- Deciding whether the task is complete.
- Deciding when to pause, resume, or escalate.

The planner must not be treated as optional glue. If the system needs a decision, the planner is the authority.

### Executor

The executor owns:

- Producing code, documentation, patches, commands, and implementation artifacts.
- Reporting outcomes back to the orchestrator.
- Staying within the task boundary assigned by the planner.

The executor does not decide whether the overall task is done unless the planner explicitly delegates a bounded check and then validates the result.

### CLI

The CLI owns:

- Starting and resuming sessions.
- Capturing terminal input and output.
- Forwarding human replies without rewriting them.
- Persisting durable state.
- Managing runtime integrations.
- Providing operator visibility, verbosity controls, and hotkeys.

The CLI must not:

- Decide that the task is complete.
- Invent heuristics like "probably done" or "stopped responding".
- Rewrite human input.
- Merge planner and executor roles into a single implicit behavior.

## v1 Scope

v1 includes:

- Single planner.
- Single executor.
- Durable loop.
- Resume.
- Notification bridge via `ntfy`.
- Terminal visibility.
- Fixed hotkeys.
- Bootstrap flow.
- `AGENTS.md` support.

v1 excludes:

- Orchestrator-managed multi-worker mode.
- Dynamic worker topologies.
- CLI-owned decision logic.

## Primary Integration Targets

- Primary executor integration target: `codex app-server`
- Fallback executor integration target: `codex exec --json`

The primary architecture should be written so the executor transport can change without changing planner authority or persistence guarantees.

## Session and Turn Model

The orchestrator loop proceeds in durable turns.

1. A session starts or resumes.
2. The planner receives the current task state plus any new human input.
3. The planner emits a bounded next action.
4. If the action requires execution, the CLI launches or resumes the executor integration.
5. The executor returns output and artifacts.
6. The CLI persists the turn result.
7. The planner decides the next step.
8. The system may pause only after the current AI turn has completed and been durably recorded.

AI turns in this spec means planner turns and executor turns.

## Allowed Planner Outcomes

The planner should emit one of a small number of explicit outcomes:

- Run executor on a bounded slice.
- Ask the human a question.
- Mark the task complete.
- Pause safely.
- Resume or continue from persisted state.

If the system needs another outcome type, add it explicitly rather than hiding it in CLI behavior.

## Stop Conditions

Stop conditions are planner-owned.

The CLI may react to operator interrupts, process failures, or transport loss, but it must represent those as events for the planner or runtime state. It must not silently convert them into completion or abandonment decisions.

## Human Input Contract

- Human replies are stored and forwarded as raw text.
- Raw means no summarization, paraphrase, cleanup, or policy filtering by the CLI.
- If summarization is ever needed for context management, that is planner work and must remain traceable.

## Persistence Contract

Persistence must support durable resume and operator visibility.

- SQLite is the indexed system of record for sessions, task state, checkpoints, and resumable metadata.
- JSONL is the append-only event stream for planner turns, executor turns, human input, runtime events, and notifications.
- A safe resume point must exist after every completed AI turn.
- Resume must rebuild from persisted state rather than from terminal guesswork.

The exact schema can evolve, but these guarantees cannot be dropped.

## Visibility and Verbosity

Visibility features are required because the operator needs to understand what the loop is doing without turning the CLI into the decision maker.

- Verbosity controls change presentation, not authority.
- The terminal should be able to show planner state, executor activity, and recent events.
- Hidden state is a bug unless intentionally redacted for safety.

## Hotkeys

v1 requires fixed hotkeys, not a configurable keybinding system.

Hotkeys should support operator control over visibility and safe pausing. The exact key map may be chosen during implementation, but the semantics must remain small, fixed, and documented.

## Notification Bridge

The `ntfy` bridge is a transport for human awareness and inbound replies. It is not a planner substitute.

- Outbound notifications may inform the human of pause points, questions, or status.
- Inbound replies are treated as raw human input.
- Notification delivery failures are runtime events, not planner decisions.

## `AGENTS.md` Support

The orchestrator should load and honor repo-local `AGENTS.md` guidance during task execution and resume. This is part of bootstrap correctness in v1.

## Non-Goals

- No fake autonomy hidden in the CLI.
- No silent role collapse between planner and executor.
- No orchestrator-managed multi-worker scheduling in v1.
- No architecture that depends on ephemeral terminal state for correctness.
