# ADR-006: Bounded Planner To Executor Cycle

Status: Accepted
Date: 2026-04-19

## Context

The repository now has:

- a durable run and checkpoint spine,
- a live planner client using `planner.v1`,
- a real primary executor adapter using `codex app-server`.

The next narrow slice is not the full autonomous loop. It is a bounded `run` invocation that can prove one planner-selected bounded action plus one follow-up planner turn without giving the CLI hidden control.

## Decision

Lock the first bounded planner to executor cycle shape for both `orchestrator run --goal ...` and `orchestrator resume`.

For this slice:

1. `run` creates a durable run.
2. `resume` loads the latest unfinished run without creating a new one.
3. The invoked command performs one live planner turn from persisted run state.
4. If the first planner outcome is neither `execute` nor `collect_context`, that planner result is persisted and the invocation stops.
5. If the first planner outcome is `collect_context`, the CLI deterministically inspects the requested repo-relative paths, persists the collected context data, performs one second planner turn using that persisted context as planner input, and then stops.
6. If the first planner outcome is `execute`, the CLI dispatches exactly one executor turn through the primary executor adapter.
7. The executor result is persisted durably.
8. The invocation performs one second planner turn using persisted executor result data as planner input.
9. The invocation stops after that second planner turn without executing its outcome.

This slice supports `execute` dispatch only on the first planner turn.

- `execute` is the only planner outcome that triggers executor transport activity in this slice.
- `collect_context` is handled deterministically by repo-root-relative inspection only and feeds one bounded second planner turn.
- Other non-`execute` planner outcomes are persisted and surfaced to the operator.
- The second planner outcome is persisted and surfaced, but it is not executed or reinterpreted by the CLI.

This slice is not the full autonomous loop.

- It does not continue beyond the second planner turn.
- It does not execute the second planner outcome.
- It does not implement planner-owned completion handling beyond surfacing the returned planner output.
- It does not implement repeated planner to executor cycling, background autonomy, or operator notification features.

Safe pause points remain post-AI-turn durability boundaries.

- The planner turn is durably recorded before any executor dispatch happens.
- If an executor turn completes, its result is durably recorded and a safe-pause checkpoint is committed.
- If the post-executor planner turn completes, its result is durably recorded and becomes the latest safe pause point.

## Consequences

- The planner remains the decision-maker.
- The executor remains the implementation actor.
- The CLI remains an inert bridge that dispatches only the planner-selected `execute` outcome for this bounded slice and resumes only from persisted state.
- Future full-loop work should build on this shape rather than replacing it with CLI-owned control flow.
