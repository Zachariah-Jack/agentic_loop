# ORCHESTRATOR NON-NEGOTIABLES

## Must

- Keep planner, executor, and CLI as distinct roles.
- Treat the planner as the sole workflow decision maker.
- Keep the CLI inert with respect to completion, stop conditions, and policy.
- Forward and persist human replies as raw input.
- Persist durable checkpoints after completed AI turns.
- Support resume from persisted state.
- Keep executor integration transport-swappable without changing planner authority.
- Preserve operator visibility into planner state, executor activity, and recent runtime events.
- Treat `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md` as the primary architecture source.

## Must Not

- Put hidden workflow policy in the CLI.
- Let the CLI infer "done", "stalled", or "good enough" on its own.
- Collapse planner and executor semantics into one undocumented role.
- Rewrite human input before it reaches the planner.
- Depend on terminal scrollback as the only source of truth.
- Introduce orchestrator-managed multi-worker mode in v1.
- Add speculative scaffolding that pretends an integration or subsystem already exists.

## When In Doubt

- Make the planner boundary more explicit.
- Make persistence more durable.
- Make the diff smaller.
- Prefer a documented contract over an implied behavior.
