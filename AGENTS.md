# AGENTS.md

This repository is being bootstrapped as a planner-led orchestrator project.

## Mandatory Reading Before Non-Trivial Changes

Before making any non-trivial code, schema, workflow, or architecture change, read these files first:

1. `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md`
2. `docs/ORCHESTRATOR_NON_NEGOTIABLES.md`
3. `docs/architecture/ADR-001-primary-architecture-source.md`
4. `docs/architecture/ADR-002-canonical-repo-contract.md`
5. `docs/CLI_ENGINE_EXECPLAN.md`

Before making any non-trivial GUI, console, protocol, planner-status, or operator-intervention change, also read:

6. `docs/ORCHESTRATOR_V2_CONSOLE_SPEC.md`
7. `docs/ORCHESTRATOR_V2_CONTROL_PROTOCOL.md`
8. `docs/ORCHESTRATOR_V2_PLANNER_CONTRACT.md`
9. `docs/ORCHESTRATOR_V2_ROADMAP.md`
10. `docs/architecture/ADR-008-v2-operator-console.md`

If those documents conflict, `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md` is the primary architecture source.

## Repo Working Rules

- Keep diffs tight, intentional, and reviewable.
- Do not add placeholder code, fake adapters, or package structures that only pretend to work.
- Prefer small implementation slices with explicit acceptance criteria.
- Update the architecture docs or ADRs when a decision becomes locked.
- Use `docs/CLI_ENGINE_EXECPLAN.md` for persistence guidance, anti-patterns, acceptance criteria, and implementation discipline.

## Core Semantics

- The CLI is inert. It is a bridge and runtime harness, not a brain.
- The planner is the decision maker. It decides what happens next, when to ask a human, when to continue, and when work is done.
- The executor writes code and other artifacts. In this repo, the executor integration target is Codex.
- The CLI must not invent stop conditions, completion decisions, or workflow policy.
- Human replies are forwarded raw. The CLI must not rewrite, summarize, or editorialize them.
- Safe pause points occur after AI turns have completed and been durably recorded.
- Visibility features, verbosity control, and hotkeys are operator features, not planner authority.
- Parallel executor workers may be added later only when boundaries are explicit and durable. They are not part of v1.

## V2 Console Guardrails

- Do not put semantic decisions in the GUI or console shell.
- Do not bypass the engine protocol for console actions.
- Do not expose hidden chain-of-thought.
- Do not print secrets or store them carelessly.
- Keep headless CLI mode fully working while adding console features.

## v1 Guardrails

- v1 is single planner, single executor.
- v1 must support a durable loop and resume.
- v1 must support the notification bridge, terminal visibility, fixed hotkeys, bootstrap flow, and `AGENTS.md` support.
- v1 must not introduce orchestrator-managed multi-worker execution.
