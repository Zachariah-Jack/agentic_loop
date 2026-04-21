# ADR-006: Bounded Planner To Executor Cycle

Status: Accepted
Date: 2026-04-19

## Context

The repository now has:

- a durable run and checkpoint spine,
- a live planner client using `planner.v1`,
- a real primary executor adapter using `codex app-server`.

The next narrow slice is not the full autonomous loop. It is a bounded cycle shape that can prove one planner-selected bounded action plus one follow-up planner turn without giving the CLI hidden control, plus a foreground `continue` command that may invoke that same bounded cycle repeatedly on an existing run under explicit operator-supplied limits.

## Decision

Lock the first bounded planner to executor cycle shape for `orchestrator run --goal ...`, `orchestrator resume`, and the foreground `orchestrator continue` command.

For this slice:

1. `run` creates a durable run.
2. `resume` loads the latest unfinished run without creating a new one.
3. `continue` loads the latest unfinished run without creating a new one and may invoke repeated bounded cycles in the foreground.
4. Each bounded cycle performs one live planner turn from persisted run state.
5. If a planner turn returns `complete`, that planner result is persisted, the run is marked `completed`, a completion checkpoint is recorded, and the bounded cycle stops.
6. If the first planner outcome is neither `execute`, `collect_context`, nor `complete`, that planner result is persisted and the bounded cycle stops.
7. If the first planner outcome is `collect_context`, the CLI deterministically inspects the requested repo-relative paths, persists the collected context data, performs one second planner turn using that persisted context as planner input, and then stops unless that second turn declares `complete`.
8. If the first planner outcome is `execute`, the CLI dispatches exactly one executor turn through the primary executor adapter.
9. The executor result is persisted durably.
10. The bounded cycle performs one second planner turn using persisted executor result data as planner input.
11. The bounded cycle stops after that second planner turn without executing its outcome unless that second turn declares `complete`, in which case the run is marked `completed`.
12. `continue` may chain those existing bounded cycles only at safe-pause boundaries and only until one explicit stop condition occurs: planner-declared completion, a first-turn `ask_human`, an operator-supplied max-cycle bound, or a persisted transport/process error.
13. When a planner turn returns `ask_human`, the CLI may use the configured `ntfy` bridge to publish the exact planner question and wait for one raw inbound reply; if the bridge is not configured or fails mechanically, the CLI falls back to terminal input without rewriting the reply.

This slice supports `execute` dispatch only on the first planner turn.

- `execute` is the only planner outcome that triggers executor transport activity in this slice.
- `collect_context` is handled deterministically by repo-root-relative inspection only and feeds one bounded second planner turn.
- `ask_human` remains planner-owned and may use `ntfy` as a notification/input bridge when configured, while terminal input remains the fallback path.
- `complete` is planner-owned termination authority. The CLI records and reflects that declared state but does not infer completion on its own.
- Other non-`execute` planner outcomes are persisted and surfaced to the operator.
- The second planner outcome is persisted and surfaced, but it is not executed or reinterpreted by the CLI except that an explicit second-turn `complete` outcome marks the run completed.

This slice is not the full autonomous loop.

- It does not continue beyond the second planner turn.
- It does not execute the second planner outcome.
- It does not add hidden background autonomy, retry policy, or operator notification features.
- `continue` is a foreground operator command that repeats the existing bounded cycle shape; it is not a background loop or CLI-owned planner policy.

Safe pause points remain post-AI-turn durability boundaries.

- The planner turn is durably recorded before any executor dispatch happens.
- If an executor turn completes, its result is durably recorded and a safe-pause checkpoint is committed.
- If the post-executor planner turn completes, its result is durably recorded and becomes the latest safe pause point.

## Consequences

- The planner remains the decision-maker.
- The executor remains the implementation actor.
- The CLI remains an inert bridge that dispatches only the planner-selected `execute` outcome for this bounded slice, resumes only from persisted state, and may repeat bounded cycles only when the operator explicitly invokes `continue`.
- Future full-loop work should build on this shape rather than replacing it with CLI-owned control flow.
