# ADR-008: V2 Operator Console

Status: Accepted
Date: 2026-04-22

## Context

The orchestrator is moving toward a serious V2 product direction where the operator should be able to start the engine, walk away, and return to meaningful progress. The current headless CLI remains valid, but the product now needs a richer operator experience.

The project must preserve the existing architecture:

- engine/CLI is inert
- planner is the decision-maker
- executor/Codex performs implementation work
- human replies remain raw
- safe pause/interruption happens at AI-turn or cycle boundaries

The desired V2 console must improve visibility and operator ergonomics without becoming a second control brain.

## Decision

V2 adds an optional Operator Console while preserving full headless CLI operation.

The following decisions are locked:

1. The console is optional. Headless CLI must continue to work.
2. The console must communicate with the engine only through explicit engine protocol actions.
3. The GUI must not directly mutate engine internals.
4. Control Chat and Side Chat are distinct concepts:
   - Control Chat may affect the active run through raw queued human intervention at safe points.
   - Side Chat is non-interfering unless explicitly promoted.
5. The pending action buffer is a core engine concept for V2.
6. Safe-point intervention is planner-mediated:
   - a control message plus pending action state is sent to the planner
   - the planner decides whether to proceed, alter, pause, ask, or complete
7. Planner-safe operator-visible status fields may be added, but hidden chain-of-thought must not be exposed.

## Consequences

- Future console work must start with engine protocol and status/event plumbing, not direct GUI-to-engine shortcuts.
- The planner remains the only semantic authority.
- The GUI may improve visibility, editing, approvals, and ergonomics, but not meaning.
- Pending action holding and reconsideration become first-class engine responsibilities.
- The V2 console can be implemented incrementally without forcing a GUI dependency into the headless CLI.
