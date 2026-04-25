# ORCHESTRATOR V2 ROADMAP

Status: Proposed phased V2 implementation roadmap with several dogfood foundations now implemented

Current implementation note:

- The V2 shell/control stack now includes runtime-configurable timeouts, active-only Total Build Time tracking, a Side Chat context-agent foundation with explicit audited action requests, persisted permission/autonomy profile settings, and GitHub Releases update-check/changelog support.
- Safe Windows self-install, the final LLM-backed side-chat tooling backend, and broad permission-profile enforcement across every future installer/test/Git workflow remain staged follow-up work.

## Purpose

Sequence V2 work into practical chunks that preserve:

- inert engine semantics
- planner authority
- executor authority
- headless CLI parity

This roadmap is intentionally phased. It does not authorize a one-pass GUI build.

## V2 Outcome

Target result:

- optional Windows-friendly Operator Console
- same engine/state model as headless CLI
- explicit local control protocol
- planner-safe operator messages and progress
- safe-point intervention via pending action buffer

## Phase Order

### Phase 1: Engine Protocol Skeleton

Goal:

- create the engine-side local control protocol skeleton without a GUI shell yet

Deliver:

- protocol server shell
- request/response envelope types
- event stream skeleton
- `get_status_snapshot`
- `get_activity_stream`
- `get_pending_action`

Acceptance:

- headless CLI still works
- protocol can be exercised from tests or a simple local client

### Phase 2: Dynamic Verbosity And Config Reload

Goal:

- let the engine observe selected config/verbosity changes at safe points

Deliver:

- `set_verbosity`
- `set_runtime_config`
- safe-point reload behavior
- durable journaling of config changes where relevant

Acceptance:

- no restart required for verbosity or supported config changes
- no mid-turn semantic mutation

### Phase 3: Planner Operator Status Fields

Goal:

- add planner-safe operator-visible status/progress fields behind validation and tests

Deliver:

- planner contract extension
- schema validation
- status rendering
- CLI verbose display updates

Acceptance:

- safe display fields available without chain-of-thought
- progress uses planner-supplied percent/confidence/basis

### Phase 4: Pending Action Buffer

Goal:

- add first-class engine-side pending action tracking

Deliver:

- durable pending action model
- pending action inspection API
- pending action hold/release lifecycle events

Acceptance:

- operator can inspect what the engine is about to do next
- engine can hold the next action at safe points

### Phase 5: Control Chat Intervention

Goal:

- queue raw human control messages and feed them to the planner at safe points

Deliver:

- `inject_control_message`
- durable control-message queue
- planner input wiring for pending action plus raw control message

Acceptance:

- raw control message changes planner input only through safe-point reconsideration
- no direct GUI mutation of engine internals

### Phase 6: Side Chat Service

Goal:

- add Side Chat with normal context-only messages and explicit promotion/action requests into control chat

Deliver:

- side-chat protocol action
- context policies
- explicit promote-to-control action

Acceptance:

- side chat does not affect the active run unless the operator uses an explicit audited action

### Phase 7: Console Shell

Goal:

- build the first Tauri shell around the engine protocol

Deliver:

- app shell
- connection manager
- live event timeline
- status snapshot view

Acceptance:

- console can attach to the engine and render live state

Current implementation status:

- the Electron proof shell now includes guided Home, protocol-backed Start/Continue, Action Required, Live Output with verbosity-aware filtering, model health visibility/test actions plus auto-check/preflight normalization, backend identity/stale-backend detection, owned backend process-tree cleanup with port diagnostics, SQLite busy recovery messaging/retry, stale active-run recovery, elapsed-time display, Control Chat injection, safe stop, worker controls, artifacts/files, terminals, and dogfood notes
- full side-chat backend, worker-specific approval UI, packaged installer polish, and final desktop-console polish remain later phases

### Phase 8: Terminal Tabs

Goal:

- add terminal viewer tabs

Deliver:

- shell tab
- engine log stream tab
- executor stream tab
- future planner/operator stream tab

Acceptance:

- multiple tabs supported
- no semantic control through terminal rendering

### Phase 9: Chat Panes

Goal:

- add Control Chat and Side Chat UI

Deliver:

- separate tabs or panes
- promotion flow
- intervention status indicators

Acceptance:

- distinction is obvious in UX
- live run remains unaffected by normal side chat messages

### Phase 10: Contract Editor

Goal:

- make contract file editing easy

Deliver:

- file buttons
- text editor tabs
- save/cancel
- pinned files

Acceptance:

- operator can edit contract files without leaving the console

### Phase 11: AI Autofill Wizard

Goal:

- guided AI-assisted drafting of contract files

Deliver:

- wizard flow
- targeted questions
- preview-before-save
- explicit apply/save

Acceptance:

- no silent file overwrite
- no silent active-run mutation

### Phase 12: File And Artifact Explorer

Goal:

- make repo and orchestration artifacts easy to browse

Deliver:

- repo tree
- artifact shortcuts
- latest run summary, diff, and artifact entry points

Acceptance:

- latest important files are easy to find

### Phase 13: Progress Gauge

Goal:

- render planner-provided progress cleanly

Deliver:

- percent gauge
- confidence indicator
- basis text

Acceptance:

- no fake GUI-invented progress

### Phase 14: Approval Center

Goal:

- make executor approvals visible and operable

Deliver:

- approval request view
- approve/deny controls
- scope/reason display

Acceptance:

- explicit approval remains required

### Phase 15: Worker Panel

Goal:

- make multi-worker state visible

Deliver:

- worker table
- current task and status
- latest artifact
- integration/apply state

Acceptance:

- no ghost workers
- worker state comes from durable engine data

### Phase 16: Packaging

Goal:

- ship the optional console without breaking headless CLI packaging

Deliver:

- Tauri packaging flow
- engine binary integration
- release notes/runbook update

Acceptance:

- console build and CLI build are both releasable

## Implementation Tracks

The roadmap intentionally separates these tracks:

- engine protocol work
- planner contract updates
- dynamic verbosity/config
- console shell
- terminal tabs
- chat panes
- contract editor
- AI autofill wizard
- artifact/file explorer
- progress gauge
- approval center
- worker panel
- packaging

## Cross-Cutting Guardrails

Every phase must preserve:

- no semantic decisions in GUI
- no engine bypass
- no hidden chain-of-thought exposure
- no secret leakage
- headless mode remains working

## Recommended First Implementation Chunk

Start with:

1. engine-side local control protocol skeleton and event stream
2. dynamic verbosity/config reload at safe points
3. planner operator-message/progress schema fields behind tests

That proves the V2 foundation while keeping the diff small and reviewable.
