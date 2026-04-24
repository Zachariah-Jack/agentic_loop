# Orchestrator Brief

## App Name
- Agentic Loop Orchestrator

## One-Sentence Summary
- A Windows-friendly Go orchestrator that runs a durable planner-led loop around a Codex executor while keeping the CLI and GUI inert.

## Product Vision
- Make autonomous app-building practical enough that a user can start a run, walk away, and return to meaningful progress, clear status, durable artifacts, and actionable next steps.
- Keep the architecture trustworthy: planner decides, executor implements, CLI/GUI transport state and make work visible.

## Target Users
- Primary users: developers dogfooding autonomous app-building workflows on local repositories.
- Secondary users: operators who want a guided desktop cockpit for long-running build sessions.
- User expectations: clear status, no hidden semantic decisions, no terminal spelunking for ordinary recovery.

## Core User Problems
- Long-running AI build loops are hard to supervise without reliable state, checkpoints, and visibility.
- Manual terminal-driven orchestration causes confusion around run state, model health, approvals, stale processes, and recovery.
- Planner/executor authority can drift if runtime or UI layers start making hidden semantic decisions.

## Primary Use Cases
- Initialize a target repo with useful contract files and runtime state.
- Start or continue a planner-led run against a target repo.
- Dispatch Codex executor work only after explicit planner outcomes.
- Recover from stale owned backend processes, stop flags, safe pauses, model issues, or SQLite busy states without manual process killing.
- Use the optional V2 shell to inspect status, artifacts, events, workers, approvals, dogfood notes, and control messages through the engine protocol.

## MVP Scope
- Durable single-planner, single-executor loop.
- Strict planner output schema and validation.
- Codex executor integration with model/access health checks.
- SQLite plus JSONL persistence with safe checkpoints.
- Headless CLI commands for init, run, continue, status/history, doctor, control server, and recovery.
- Optional Electron V2 shell using only explicit control protocol actions.
- Target repo scaffolding that creates strong default `.orchestrator` contract files without overwriting human-authored files.

## Out of Scope
- GUI semantic authority over project direction, completion, or success.
- Hidden fallback to weaker planner or Codex models.
- Unowned process killing.
- Deleting run history or artifacts during recovery.
- Exposing hidden chain-of-thought.
- Treating Side Chat as live-run control unless a message is explicitly promoted to Control Chat.

## Platform / Stack Constraints
- Language/runtime: Go CLI and engine.
- Persistence: SQLite plus JSONL event journal.
- Primary executor integration target: `codex app-server`.
- Fallback executor path: `codex exec --json` or equivalent Codex CLI probe path.
- V2 shell: Electron for the current desktop proof surface.
- Headless CLI must remain first-class.
- Windows dogfood flow must be reliable.

## Integration Requirements
- OpenAI Responses API for planner turns.
- Codex CLI/app-server for executor turns.
- Local control protocol and NDJSON event stream for GUI integration.
- Optional `ntfy` bridge for notifications.
- Local filesystem access for target repo state, artifacts, contract files, and dogfood notes.

## UX / Design Expectations
- Console should answer immediately: connected, repo, run state, whether action is needed, and what to click next.
- Technical details must be available, but plain-English status comes first.
- Action Required should appear only for real human-action states.
- Live Output should make planner/executor/runtime progress discoverable.
- Recovery actions should be mechanical, explicit, and safe.

## Performance / Reliability Expectations
- Safe checkpoint after completed AI turns.
- Resume from persisted state, not terminal scrollback.
- Bounded SQLite busy retries and clear lock diagnostics.
- Owned backend cleanup should avoid manual port/process killing.
- No silent model/access fallback.

## Security / Privacy Constraints
- Do not print or store API keys, bearer tokens, auth tokens, or passwords.
- Debug bundles must exclude secrets and hidden chain-of-thought.
- Full Codex access requirements must be visible and verified before autonomous runs.
- GUI, control protocol, and docs must not imply unverified model/access state.

## Success Criteria
- A user can initialize a target repo, run/continue the orchestrator, inspect progress, recover common dogfood failures, and understand next steps without terminal hacks.
- Planner remains the decision-maker; executor writes code; CLI and GUI remain inert transport/control surfaces.
- Contract scaffolds are useful for real app builds and preserve existing human-authored files.
- Tests cover schema, runtime flow, recovery, control protocol, and shell view-model behavior for core risks.

## Current Build Status
- The CLI, control server, event stream, model health, recovery flows, and V2 Electron shell are under active dogfood hardening.
- Recent focus has been stale backend recovery, model health truth, repo binding, ask_human recovery, side-chat non-interference, loop-state clarity, and better target repo scaffolds.
- Repo-local `.orchestrator` contract files were accidentally overwritten with target-app content and have been restored to orchestrator-specific guidance.

## Immediate Next Goal
- Continue dogfooding-hardening slices that remove confusing GUI states and make recovery reliable without compromising planner-owned decisions.

## Human Notes for Planner
- Treat this repository as the orchestrator product, not a target app being built by it.
- Keep fixes focused, testable, and aligned with the inert CLI / planner-owned architecture.
