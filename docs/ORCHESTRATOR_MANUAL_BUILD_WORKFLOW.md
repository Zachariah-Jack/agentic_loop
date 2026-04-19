# ORCHESTRATOR MANUAL BUILD WORKFLOW

## Purpose

Define how humans and executor sessions should build this repo incrementally before the orchestrator can automate itself.

## Working Loop

1. Read the current architecture source documents.
2. Choose one bounded slice.
3. State the acceptance criteria for that slice.
4. Change only the files needed for that slice.
5. Review the diff for contract drift.
6. Stop at the first clean checkpoint.

## Slice Requirements

Each slice should:

- have a single clear objective,
- fit in a reviewable diff,
- preserve the planner versus executor versus CLI boundary,
- include or reference its acceptance criteria,
- avoid scaffolding for future phases that are not being implemented now.

## Recommended Build Order

1. Source-of-truth docs and ADRs.
2. Persistence contracts and event model.
3. Planner action contract.
4. Executor integration spine through `codex app-server`.
5. Fallback executor path through `codex exec --json`.
6. Durable resume loop.
7. Terminal visibility and fixed hotkeys.
8. `ntfy` bridge.
9. Bootstrap and `AGENTS.md` loading behavior.

## Review Checklist For Each Slice

- Is the authority boundary still clear?
- Did the CLI stay inert?
- Did the planner remain the decision maker?
- Is the change durable and resumable where required?
- Is any new decision recorded in the canonical docs?
- Is the diff smaller than it could have been?

## Stop Rules

Stop and get human confirmation when:

- a slice would lock a new user-facing workflow contract,
- a slice would change the persistence guarantees,
- a slice would force a hotkey map or notification behavior without prior agreement,
- a slice would blur planner and executor responsibilities.

## Anti-Patterns

- Building the whole system skeleton before the first real working path exists.
- Adding fake adapters or placeholder packages.
- Combining persistence, planner policy, and terminal UX in one undifferentiated change.
- Treating the CLI as the place to "just make it work" with hidden logic.
