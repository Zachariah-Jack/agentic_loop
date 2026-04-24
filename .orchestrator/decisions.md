# Orchestrator Decisions

## Product Decisions

### Planner-Led Orchestration Is The Product Core
- Date: 2026-04-11
- Decision: The orchestrator is a planner-led system, not a CLI heuristic runner.
- Reason: The user wants durable autonomous app-building without hidden runtime policy.
- Alternatives Considered: CLI-owned workflow heuristics; executor self-looping.
- Impact: Planner outcomes drive execute, ask_human, pause, and complete.
- Revisit Trigger: Only if the primary architecture spec is superseded by a later ADR.

### Headless CLI Remains First-Class
- Date: 2026-04-11
- Decision: GUI work must not replace or weaken headless CLI operation.
- Reason: The control engine must remain usable in scripts, terminals, and dogfood recovery.
- Alternatives Considered: Desktop-only workflow.
- Impact: Console actions go through explicit engine protocol actions also usable by non-GUI clients.
- Revisit Trigger: Never without a new architecture decision.

### V2 Console Is Optional Operator Surface
- Date: 2026-04-21
- Decision: The V2 shell is an optional cockpit over the engine protocol.
- Reason: Operators need clarity and controls, but not a second brain.
- Alternatives Considered: GUI directly mutating engine internals.
- Impact: GUI must not bypass the control protocol or make project decisions.
- Revisit Trigger: If a future shell architecture replaces Electron or changes transport.

## Technical Decisions

### Go CLI And Engine
- Date: 2026-04-11
- Decision: Implement the engine and CLI in Go.
- Reason: Go provides a simple deployable binary with good process and filesystem support.
- Alternatives Considered: Node-only orchestrator; Python orchestration.
- Impact: Runtime state, control protocol, planner, and executor integration live primarily in Go.
- Revisit Trigger: Only if a major platform requirement invalidates Go.

### SQLite Plus JSONL Persistence
- Date: 2026-04-11
- Decision: Use SQLite as the indexed state store and JSONL as append-only event journal.
- Reason: Durable resume needs queryable state plus inspectable event history.
- Alternatives Considered: Files only; external database.
- Impact: State recovery, history, and status read from durable local state.
- Revisit Trigger: If multi-process or remote orchestration demands a different store.

### Codex Executor Integration
- Date: 2026-04-11
- Decision: Primary executor integration target is `codex app-server`; fallback is Codex exec-style transport.
- Reason: Executor transport should be swappable without changing planner authority.
- Alternatives Considered: Direct code editing by the orchestrator.
- Impact: Executor errors, model health, and access mode are surfaced as runtime state.
- Revisit Trigger: If Codex integration APIs change enough to require a new transport contract.

## Architecture Decisions

### CLI Is Inert
- Date: 2026-04-11
- Decision: The CLI transports, persists, renders, and manages runtime mechanics; it does not decide project direction or completion.
- Reason: Hidden CLI decisions collapse the planner/runtime boundary.
- Alternatives Considered: Runtime heuristics for done/stalled/next work.
- Impact: CLI may enforce contracts and mechanical guardrails, but planner owns workflow decisions.
- Revisit Trigger: Only through a new ADR that changes the core architecture.

### Human Input Is Raw
- Date: 2026-04-11
- Decision: Human replies are persisted and forwarded raw.
- Reason: The planner needs the actual human input, and runtime summarization can alter intent.
- Alternatives Considered: CLI summarization or cleanup before planner input.
- Impact: Control Chat and ask_human answers must preserve raw text.
- Revisit Trigger: If explicit planner-owned summarization is added with durable traceability.

### Recovery Is Mechanical Only
- Date: 2026-04-23
- Decision: Recovery may clear stale owned process/run mechanics, but must not mark semantic success or completion.
- Reason: Users need practical recovery without giving the runtime project authority.
- Alternatives Considered: Treat stale states as failed or complete automatically.
- Impact: Recovery preserves history/artifacts and enables a truthful next action.
- Revisit Trigger: If future run ownership semantics change.

## UX Decisions

### Home Dashboard Answers Immediate Operator Questions
- Date: 2026-04-22
- Decision: The V2 shell Home should prioritize connection, repo, run state, action required, and next action.
- Reason: Dense panes confused dogfood users.
- Alternatives Considered: One long technical page.
- Impact: Advanced panes are available, but Home is action-oriented.
- Revisit Trigger: If future user testing identifies a better primary workflow.

### Action Required Means Real Human Action
- Date: 2026-04-23
- Decision: Action Required badges must appear only for real approvals, ask_human, model/config blockers, stop flags, or explicit human-needed states.
- Reason: False badges erode trust and block the user unnecessarily.
- Alternatives Considered: Showing all approval metadata as active.
- Impact: `state=none`, `kind=Unavailable`, and zero worker approvals are not actionable.
- Revisit Trigger: If new actionable state types are introduced.

### Side Chat Is Recorded-Only Until Real Backend Exists
- Date: 2026-04-24
- Decision: Side Chat records notes and does not affect active runs unless explicitly promoted to Control Chat.
- Reason: It must not accidentally set stop flags or queue control interventions.
- Alternatives Considered: Side Chat implicitly influencing planner state.
- Impact: UI copy must say Side Chat is notes-only for now.
- Revisit Trigger: If a non-interfering side-chat backend is implemented and tested.

## Workflow Decisions

### Init Preserves Human-Authored Contract Files
- Date: 2026-04-11
- Decision: `orchestrator init` creates missing contract files but does not overwrite existing ones.
- Reason: Repo contracts are human-authored source-of-truth once they exist.
- Alternatives Considered: Regenerating templates on every init.
- Impact: Accidental bad content in existing contract files must be repaired manually or through an explicit future regenerate path.
- Revisit Trigger: If a tested force/regenerate command is intentionally added.

### Dogfood Startup May Stop Owned Stale Backends
- Date: 2026-04-23
- Decision: Dogfood startup/recovery can stop owned stale backend process trees for the same repo/address.
- Reason: Users should not have to manually kill stale owned processes.
- Alternatives Considered: Warning only; killing all matching processes.
- Impact: Ownership metadata is required; unknown processes are not killed blindly.
- Revisit Trigger: If process ownership tracking changes.

## Testing Decisions

### Tests Follow The Touched Surface
- Date: 2026-04-11
- Decision: Go changes require relevant Go tests, shell changes require shell tests, and protocol changes require both sides where practical.
- Reason: The repo spans engine, protocol, and GUI surfaces.
- Alternatives Considered: Manual dogfood only.
- Impact: Focused unit/view-model tests are preferred for dogfood regressions.
- Revisit Trigger: If a dedicated integration test harness replaces current coverage.

## Deferred Decisions
- Packaged installer strategy.
- Final terminal architecture beyond current Electron shell sessions.
- Real side-chat conversational backend.
- Advanced worker apply/integrate UX.
- Multi-run orchestration beyond current single-run guardrails.

## Decision Log Format
Copy this format when recording a durable decision.

### YYYY-MM-DD - Short Decision Name
- Date:
- Decision:
- Reason:
- Alternatives Considered:
- Impact:
- Revisit Trigger:
