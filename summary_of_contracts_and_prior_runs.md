# Summary Of Contracts And Prior Runs

## Requested paths vs actual repo files loaded

The requested paths `agents.md`, `spec/updated_spec.md`, `spec/non_negotiables.md`, and `spec/exec_plan.md` do not exist as written in this checkout.

I loaded and summarized the repo's actual canonical files:

- `AGENTS.md`
- `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md`
- `docs/ORCHESTRATOR_NON_NEGOTIABLES.md`
- `docs/CLI_ENGINE_EXECPLAN.md`

I also consulted `docs/architecture/ADR-006-bounded-planner-executor-cycle.md` because it defines the exact bounded-cycle shape that the requested summary asks about.

## File summaries

### `AGENTS.md`

- Declares the repo as a planner-led orchestrator project.
- Says the primary architecture source is `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md`.
- Requires small, reviewable slices and points to `docs/CLI_ENGINE_EXECPLAN.md` for persistence and implementation discipline.
- Repeats the core semantic split: planner decides, executor writes artifacts, CLI is inert and must not invent stop conditions or completion.
- v1 must support a durable loop, resume, notification bridge, terminal visibility, fixed hotkeys, bootstrap flow, and `AGENTS.md` support.

### `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md`

- Defines the canonical v1 architecture.
- The loop is durable and turn-based: start/resume session, planner emits bounded next action, CLI runs executor only when instructed, results are persisted, planner decides next step.
- Planner outcomes are explicit: run executor on a bounded slice, ask human, mark complete, pause safely, or resume/continue from persisted state.
- Stop conditions are planner-owned. The CLI must not infer completion, abandonment, or policy.
- Persistence contract requires SQLite plus JSONL, with a safe resume point after every completed AI turn.

### `docs/ORCHESTRATOR_NON_NEGOTIABLES.md`

- Reinforces the hard boundaries: distinct planner/executor/CLI roles, raw human input forwarding, durable checkpoints, resumable state, no CLI-owned completion logic, no terminal-only truth.
- Explicitly says not to add hidden CLI policy or speculative scaffolding.

### `docs/CLI_ENGINE_EXECPLAN.md`

- Adds persistence expectations and anti-patterns.
- Requires durable persistence of planner turns, executor turns, human messages, runtime events, and notification events.
- Requires resumable checkpoints after each completed AI turn and resume from persisted state, not scrollback.
- Anti-patterns include CLI-invented stop conditions, executor self-looping without planner approval, and ephemeral-only state.

### `docs/architecture/ADR-006-bounded-planner-executor-cycle.md`

- Locks the first bounded `run`/`resume` slice.
- `run` creates a durable run; `resume` loads the latest unfinished run without creating a new one.
- One invocation performs one live planner turn, then:
  - stops immediately if the first outcome is neither `execute` nor `collect_context`,
  - or handles one bounded `collect_context` pass and one second planner turn,
  - or dispatches exactly one executor turn and then one second planner turn.
- The invocation stops after that second planner turn and does not execute the second planner outcome.

## How bounded cycle tests are supposed to work

- A bounded cycle test is a single bounded `run` or `resume` invocation over the durable planner/executor loop.
- The CLI may only transport, persist, inspect requested repo-relative paths for `collect_context`, and dispatch the first-turn `execute` outcome.
- The planner owns workflow decisions, including whether to continue, pause, ask a human, resume, or complete.
- The executor only performs the bounded task it is assigned.

## How cycles are bounded and what termination conditions are required

- The invocation is bounded to one first planner turn plus at most one follow-up action path:
  - `collect_context` on the first planner turn can trigger one deterministic context collection pass and one second planner turn.
  - `execute` on the first planner turn can trigger exactly one executor turn and one second planner turn.
- The CLI must stop after the second planner turn in that invocation.
- The second planner outcome is persisted and surfaced, but not executed by the CLI.
- Stop and completion semantics remain planner-owned. The CLI must not decide "done", "stalled", or "good enough" on its own.
- Safe pause points are required after completed AI turns, with durable checkpoints persisted each time.

## Resume procedure for an unfinished bounded cycle test

- `resume` must load the latest unfinished run from persisted state instead of creating a new run.
- Resume must rebuild from durable state, not terminal scrollback.
- The resumed invocation then performs the next bounded slice from the latest safe checkpoint.
- Repo-local `AGENTS.md` guidance must still be honored on resume.

## `.agentic/runs` inspection

- `.agentic` does not exist in this checkout.
- `.agentic/runs` does not exist in this checkout.
- There are no subdirectories or files to list under `.agentic/runs`.
- There is no `.agentic/runs/run_f3c83f8c87779a1a7e795a9e897b0be6`.

## Existing persisted state relevant to prior runs

Although `.agentic/runs` is absent, the repo does contain persisted orchestrator state under:

- `.orchestrator/logs/events.jsonl`
- `.orchestrator/state/orchestrator.db`

Recent bounded-cycle-related runs visible in `.orchestrator/logs/events.jsonl` include:

- `run_f1b293a25657b28cebed1d47f1e126c3` with goal `bounded planner executor cycle smoke test`
- `run_529b1bd946d6f8e1a8a84670e030ec7a` with goal `collect context bounded test`
- `run_f3c83f8c87779a1a7e795a9e897b0be6` with goal `resume bounded cycle test`

## State of `run_f3c83f8c87779a1a7e795a9e897b0be6`

From `.orchestrator/logs/events.jsonl`, this run already has persisted state:

- `run.created`
- initial bootstrap checkpoint persisted at sequence 1
- first planner turn completed with `planner_outcome: collect_context` and a safe-pause checkpoint at sequence 2
- context collection recorded six results, all unresolved because the requested file inputs were absolute paths or missing paths
- post-collect-context second planner turn completed with `planner_outcome: collect_context` and checkpoint sequence 3
- `run.resumed` from the latest unfinished checkpoint
- another planner turn completed with `planner_outcome: execute` and checkpoint sequence 4
- `executor.turn.dispatched` for the current task

## Resume or start fresh?

- For `.agentic/runs`: there is nothing to resume because that directory tree does not exist.
- For the repo's actual durable state: this specific run already has persisted state in `.orchestrator`, so it should be treated as resumable rather than started fresh.
- The main issue visible in prior progress is not missing persistence; it is bad context path selection in earlier collect-context requests:
  - absolute paths were rejected
  - `spec/*` paths do not exist in this checkout
  - `.agentic/runs/run_f3c83f8c87779a1a7e795a9e897b0be6` does not exist

## Concise synthesis

- Bounded cycles in this repo are deliberate, planner-owned slices: one planner turn, then at most one executor turn or one deterministic context collection pass, then one second planner turn, then stop.
- Termination is required after that second planner turn for the invocation; the CLI must not continue autonomously or invent completion.
- Resume is defined as loading the latest unfinished run from durable state and continuing from the last safe checkpoint.
- There is no prior state under `.agentic/runs`, but there is real persisted state for `run_f3c83f8c87779a1a7e795a9e897b0be6` under `.orchestrator`, so the run appears resumable from actual repo persistence rather than needing a fresh start.
