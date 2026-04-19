# ORCHESTRATOR PROJECT CONTEXT PACK

## Project Intent

Build a Go-based orchestrator that runs a durable planner-led execution loop around a Codex executor while keeping the CLI intentionally inert.

## One-Sentence Architecture

Planner decides, executor produces work, CLI transports and persists.

## Why This Repo Exists

Most agent shells blur runtime, policy, and execution into one opaque loop. This project separates them on purpose so that:

- decision authority is explicit,
- execution can be swapped or upgraded,
- runtime behavior is durable and resumable,
- the operator can see what is happening.

## Canonical v1 Shape

- Language target: Go
- Persistence target: SQLite plus JSONL
- Primary executor integration: `codex app-server`
- Fallback executor integration: `codex exec --json`
- Topology: one planner, one executor
- Required features: durable loop, resume, `ntfy` bridge, terminal visibility, fixed hotkeys, bootstrap flow, `AGENTS.md` support

## Role Boundaries

### Planner

- Owns decisions.
- Emits explicit next actions.
- Determines completion.

### Executor

- Performs implementation work.
- Writes artifacts.
- Reports results.

### CLI

- Captures input and output.
- Runs integrations.
- Persists state.
- Renders visibility.

The CLI is deliberately not the place for hidden heuristics.

## Durable Loop Expectations

- Every completed planner or executor turn must become resumable.
- Resume must recover from persisted state, not from memory of the terminal.
- Human replies must remain raw and traceable.

## Operator Experience Expectations

- The operator must be able to see planner state and executor activity.
- Verbosity controls are display controls, not workflow policy.
- Pauses must happen at safe checkpoints after AI turns.
- Hotkeys are in scope for v1, but kept fixed and small.

## v1 Non-Goals

- Orchestrator-managed multi-worker mode.
- Dynamic executor fan-out.
- Decision logic hidden inside the CLI.

## Source Priority

Use the docs in this order when questions arise:

1. `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md`
2. `docs/ORCHESTRATOR_NON_NEGOTIABLES.md`
3. `docs/architecture/ADR-002-canonical-repo-contract.md`
4. `docs/CLI_ENGINE_EXECPLAN.md`

## How To Use This Context Pack

Use this file to orient a new contributor quickly. Use the updated spec and ADRs when making actual architecture or implementation choices.
