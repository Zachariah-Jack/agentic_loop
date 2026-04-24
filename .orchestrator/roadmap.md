# Orchestrator Roadmap

## Mission
- Build a durable planner-led orchestrator that can drive real app-building progress while keeping role boundaries clear and recoverable.
- Make the headless CLI reliable first, then make the optional V2 shell a clear operator cockpit over the same engine protocol.

## Source of Truth / How to Use This Roadmap
- `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md` is the primary architecture source.
- `docs/ORCHESTRATOR_NON_NEGOTIABLES.md` defines hard boundaries.
- `docs/CLI_ENGINE_EXECPLAN.md` guides persistence discipline and anti-patterns.
- V2 console work must also follow the V2 spec, control protocol, planner contract, roadmap, and ADR-008.
- This file is the repo-local working roadmap for dogfooding priorities and implementation sequencing.

## Current Status Snapshot
- Current phase: V2 dogfood hardening.
- Current product surface: Go CLI/control engine plus optional Electron V2 shell.
- Recent completed areas: control protocol, event stream, dynamic verbosity, pending actions, control messages, model health, recovery flows, artifact/contract panes, worker panel, terminal tabs, dogfood notes, repo browser, and guided Home dashboard.
- Current risk: confusing or stale GUI state can still mislead the operator during real unattended runs.
- Current validation expectation: run `go test ./...`; run shell tests when console files change.

## Definition of Done
- Headless CLI can initialize, run, continue, inspect, and recover durable runs.
- Planner remains the only workflow decision-maker.
- Executor/Codex performs bounded implementation only after explicit planner `execute`.
- GUI/control protocol never bypasses engine state or invents semantic project decisions.
- Operators can understand connection state, repo binding, loop state, model health, action-required states, and recovery options without reading raw logs.
- Runtime artifacts, checkpoints, and event history are durable and useful for debugging.

## Phase 0: Architecture Lock

### Objective
- Keep the planner/executor/CLI roles explicit and documented.

### Why It Matters
- Every autonomy feature becomes dangerous if the runtime or GUI silently becomes the decision-maker.

### Tasks
- Preserve primary architecture docs and ADRs.
- Update docs when decisions become locked.
- Reject hidden CLI/GUI semantic overrides.

### Acceptance Criteria
- New runtime or GUI features route through explicit contracts.
- Planner authority is preserved in code, docs, tests, and UI copy.

### Validation
- Review diffs for hidden policy, completion logic, or human-input rewriting.

### Notes
- Visibility and recovery are operator features, not workflow authority.

## Phase 1: Persistence Spine

### Objective
- Maintain durable state for sessions, checkpoints, events, artifacts, runtime config, dogfood notes, workers, and recovery guards.

### Why It Matters
- Resume and recovery cannot depend on terminal scrollback or process memory.

### Tasks
- Harden SQLite and JSONL write/read paths.
- Keep checkpoints after AI turns.
- Retry transient SQLite busy states safely.
- Preserve history and artifacts during mechanical recovery.

### Acceptance Criteria
- A run can be inspected and resumed from persisted state.
- Recovery never deletes user work, history, or artifacts.

### Validation
- Run persistence, status, history, and recovery tests.

### Notes
- SQLite busy errors should become clear operator diagnostics, not dead ends.

## Phase 2: Planner Contract

### Objective
- Keep planner input/output strict, durable, and expressive enough for autonomous progress.

### Why It Matters
- The planner needs enough state to decide correctly without the CLI becoming a hidden brain.

### Tasks
- Maintain strict JSON schema compatibility.
- Keep operator_status safe and visible.
- Include control interventions and pending actions when relevant.
- Ensure completion discipline for execution-oriented goals.

### Acceptance Criteria
- Planner outputs validate strictly.
- Every planner decision is explicit and persisted.
- The planner receives enough state to reason about executor work, human replies, pending actions, and blockers.

### Validation
- Run planner schema, validation, and orchestration-flow tests.

### Notes
- Optional schema fields in strict mode must be represented as nullable required fields.

## Phase 3: Executor Integration

### Objective
- Drive Codex reliably as the implementation worker.

### Why It Matters
- Real app-building progress requires executor turns that start, report, fail, and recover transparently.

### Tasks
- Keep Codex executable, version, model, effort, approval, and sandbox checks visible.
- Verify required model/access before autonomous runs.
- Surface executor errors plainly in CLI and GUI.
- Avoid silent fallback to weaker model/access settings.

### Acceptance Criteria
- Codex model health can be tested through the same environment the orchestrator uses.
- Executor dispatch starts only after planner `execute`.
- Failures produce actionable status and debug bundles.

### Validation
- Run Codex probe tests and manual probe when needed.

### Notes
- Required dogfood baseline: planner model at least gpt-5.4; Codex executor model gpt-5.5 with full autonomous access when verified.

## Phase 4: Durable Loop / Continue Behavior

### Objective
- Make run and continue behavior advance through healthy planner/executor cycles without babysitting.

### Why It Matters
- The product goal is unattended progress, not repeated manual invocation at ordinary safe points.

### Tasks
- Distinguish active processing from safe-pause waiting.
- Dispatch ready executor work before max-cycle termination.
- Honor ask_human, approval, stop flag, and model/config blockers.
- Keep stop reasons operator-meaningful.

### Acceptance Criteria
- Safe-pause execute-ready states are shown as Ready to Continue, not Running.
- Continue is enabled when a resumable run is waiting at a safe point and no blocker exists.
- Healthy loops auto-advance where designed, and pause clearly where manual action is required.

### Validation
- Run orchestration cycle and GUI loop-state tests.

### Notes
- The CLI can mechanically continue or pause based on explicit planner/runtime state; it must not infer project success.

## Phase 5: Control Protocol / V2 Shell Foundation

### Objective
- Provide explicit local protocol actions and events for the optional operator console.

### Why It Matters
- The GUI must be useful without bypassing engine internals.

### Tasks
- Keep `/v2/control` actions consistent across backend, Electron preload, protocol client, renderer, and docs.
- Keep event stream useful for live output.
- Expose backend identity, repo binding, model health, pending action, workers, artifacts, approvals, and recovery state.

### Acceptance Criteria
- Visible GUI buttons call implemented protocol actions or are disabled with a clear explanation.
- Unsupported actions are not exposed as active controls.

### Validation
- Run Go control protocol tests and shell protocol-client tests.

### Notes
- Stale backend processes are a first-class dogfood risk and must be detectable.

## Phase 6: Operator Experience

### Objective
- Make the V2 shell obvious, action-oriented, and honest.

### Why It Matters
- Operators need to know what is connected, what repo is controlled, what the loop is doing, whether action is needed, and what to click next.

### Tasks
- Keep Home dashboard focused on next action.
- Keep Live Output discoverable.
- Keep Action Required limited to real actionable states.
- Provide Copy Debug Bundle and Copy Model Health.
- Keep Side Chat recorded-only unless a real non-interfering backend is added.

### Acceptance Criteria
- GUI cannot silently attach to the wrong repo.
- GUI does not display stale approval/action badges.
- GUI recovery actions have clear success/failure steps.
- Stopped runs explain what happened and what to do next.

### Validation
- Run shell view-model tests and manual dogfood smoke.

### Notes
- Advanced panes should not crowd out the basic state questions.

## Phase 7: Recovery / Dogfood Hardening

### Objective
- Ensure stale processes, stale active-run guards, stop flags, SQLite locks, and wrong-repo backends do not trap the user.

### Why It Matters
- Dogfooding fails if the operator must manually kill ports, inspect DB locks, or guess which process owns a run.

### Tasks
- Track owned backend metadata.
- Stop owned stale backend process trees safely.
- Detect unknown port owners without killing them blindly.
- Clear stale active-run guards mechanically while preserving history.
- Provide GUI recovery buttons for recover backend, clear stop and continue, and ask_human answer/continue.

### Acceptance Criteria
- Recovery preserves artifacts/history and enables the correct next action.
- Unknown processes are protected and diagnosed clearly.
- No terminal-only recovery is required for ordinary owned stale states.

### Validation
- Run recovery unit tests and manual dogfood startup/restart smoke.

### Notes
- Recovery is mechanical only; it must not mark planner completion or success.

## Phase 8: Target Repo Bootstrap

### Objective
- Give new target repos useful default `.orchestrator` contract files.

### Why It Matters
- Better contracts reduce planner loops, unclear goals, and low-quality executor prompts.

### Tasks
- Keep `orchestrator init` preserving existing human-authored files.
- Improve default templates for brief, roadmap, decisions, and human notes.
- Ensure roadmap templates teach phased app-building, validation, risks, and done criteria.

### Acceptance Criteria
- Missing contract files are created with useful generic app-building guidance.
- Existing files are preserved.
- Tests verify template content and preservation behavior.

### Validation
- Run init tests and `go test ./...`.

### Notes
- Do not hardcode target-app content into orchestrator repo contracts.

## Milestones
- Milestone 1: CLI architecture and persistence spine established.
- Milestone 2: Planner schema and executor handoff working.
- Milestone 3: V2 control protocol and event stream usable.
- Milestone 4: V2 shell dogfoodable for start/continue/status/recovery.
- Milestone 5: Model health, repo binding, and stale recovery reliable.
- Milestone 6: Target repo scaffolds produce high-quality contracts.

## Backlog
- Harden app-server executor stream recovery.
- Improve event log summarization without exposing hidden reasoning.
- Add richer but non-interfering side-chat backend only when architecture is clear.
- Package shell more like a normal desktop app.
- Expand worker controls only through explicit planner/runtime contracts.
- Continue shrinking confusing UI states discovered during dogfood.

## Risks
- Hidden semantic drift in GUI or control server.
- Stale backend processes serving old code after rebuilds.
- Model/access mismatch between manual Codex CLI and orchestrator-launched Codex.
- SQLite busy states from multiple processes.
- Wrong repo binding causing the shell to show unrelated runs.
- Accidentally committing target-app contract content into this orchestrator repo.

## Validation Checklist
- `go test ./...`:
- `cd console/v2-shell; npm test` when shell changes:
- `node --check` touched JS files when shell changes:
- Rebuild `dist/orchestrator.exe` and `bin/orchestrator.exe` when Go runtime behavior changes:
- Manual dogfood startup with target repo:
- Model health verified:
- Repo binding verified:
- Recovery path verified:

## Next Implementation Slice
- Slice title: Continue dogfood hardening.
- Goal: Remove the next confusing or blocking state found in live use without expanding architecture.
- Acceptance criteria: user can recover or proceed from the observed state entirely through the GUI or documented CLI path.
- Validation command(s): run focused tests plus `go test ./...`; add shell tests when renderer/protocol code changes.

## Planner Guidance
- Use collect_context for repo facts that can be gathered mechanically.
- Use ask_human only when blocked on human-only knowledge, approval, credentials, or taste.
- Use execute when enough context exists to make bounded implementation progress.
- Do not repeatedly rediscover facts already captured in docs, ADRs, or this roadmap.
- Do not emit complete for execution-oriented goals before the requested work is actually fulfilled.

## Executor Guidance
- Keep diffs tight and reviewable.
- Preserve planner/executor/CLI boundaries.
- Do not overwrite human-authored `.orchestrator` contract files unless explicitly asked.
- Add or update tests for behavior changes.
- Report exact files changed and validation run.

## Human Review Points
- Any change that moves semantic authority into CLI or GUI.
- Any model/access requirement change.
- Any recovery action that could kill processes not owned by dogfood metadata.
- Any packaging or installer claim.
- Final acceptance for V2 shell dogfood readiness.
