# Orchestrator Roadmap

## Phase 0: Documentation Bootstrap

- Establish the primary architecture source.
- Lock canonical repo decisions in ADRs.
- Create repo contracts for future executor sessions.

## Phase 1: Persistence Spine

- Define session, checkpoint, and event contracts.
- Implement SQLite plus JSONL persistence with resumable checkpoints after AI turns.
- Prove that a session can resume from persisted state.

## Phase 2: Planner Contract

- Define the planner input and output envelope.
- Support explicit planner outcomes for execute, ask-human, pause, resume, and complete.
- Keep completion authority planner-owned.

## Phase 3: Primary Executor Integration

- Integrate the executor through `codex app-server`.
- Capture executor activity and results durably.
- Preserve planner and CLI boundaries.

## Phase 4: Fallback Executor Path

- Add `codex exec --json` as a fallback transport.
- Keep persistence and planner behavior transport-neutral.

## Phase 5: Durable Loop And Resume

- Wire the end-to-end planner to executor loop.
- Support safe pause points after AI turns.
- Resume from the latest committed checkpoint.

## Phase 6: Operator Experience

- Add terminal visibility for planner state, executor activity, and recent events.
- Add fixed hotkeys for safe pause and visibility control.
- Add verbosity controls that affect presentation only.

## Phase 7: Notification Bridge And Bootstrap

- Add the `ntfy` bridge for outbound notifications and inbound human replies.
- Load repo-local `AGENTS.md` guidance into task execution.
- Harden bootstrap behavior for a brand new repo.
