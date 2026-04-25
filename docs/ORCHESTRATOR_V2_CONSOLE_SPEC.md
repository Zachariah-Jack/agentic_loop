# ORCHESTRATOR V2 CONSOLE SPEC

Status: Proposed V2 product spec with a first minimal desktop proof shell implemented

## Purpose

Define the V2 optional Operator Console product around the existing inert engine. This document is a source-of-truth product spec for the planned Windows-friendly console. It does not authorize GUI-side semantic control.

Current implementation note:

- the repo now includes a first minimal desktop proof shell under `console/v2-shell/`
- it now includes a guided "What should I do now?" Home dashboard, always-visible connection/repo/run status strip, connection, progress and roadmap alignment, status, a verbosity-aware Live Output timeline, Control Chat injection, artifact browsing, canonical contract editing, a draft-first AI autofill wizard, a read-only repo browser, embedded operator terminal tabs, approval-center skeleton, Side Chat context-agent foundation, a local Dogfood Notes capture pane, runtime timeout/autonomy settings, GitHub release update checks, and protocol-backed worker create/dispatch/remove/integration-preview controls
- it now also includes practical dogfooding hardening: remembered local shell state, auto-reconnect, control-server rehydration, inline protocol issue surfacing, a one-command local startup helper, and repo/run-scoped friction capture
- shell run start/continue is now protocol-backed: the Home Run Launcher calls explicit async `start_run` and `continue_run` actions, while terminal commands remain visible only as fallback help
- model health is now operator-visible: planner model verification requires `gpt-5.4` or newer, and Codex/executor health surfaces the exact Codex path/version/config source plus required `gpt-5.5`, full-access, approval-never, xhigh-effort probe state through explicit protocol actions and status snapshots; unavailable model errors become Action Required/What Happened/Live Output items without silent fallback
- model health now auto-runs on shell connect/reconnect and Start/Continue preflight; fresh successful planner/Codex probes clear stale older component errors, while newer failures stay blocking
- backend identity/staleness is now visible for dogfood runs, and dogfood-owned stale backends or proven owned backend listener process trees for the same repo/address are cleaned up by the startup helper before a fresh backend starts
- stale active-run recovery is now visible/actionable: `Recover Backend / Unlock Repo` mechanically clears stale active-run guards while preserving run status, checkpoint, history, and artifacts; it restarts only dogfood-owned backend processes when needed after verifying the port is clear, then reconnects, reruns model health, and refreshes panels
- common SQLite busy/locked shell reads now use busy timeout, WAL, retry, and a plain `state_database_locked` recovery message when another process keeps the state DB locked
- Total Build Time is surfaced as active build time only when durable timing data is available; executor-turn and human-wait timeouts default to `unlimited` and are configurable through runtime config
- update checks can read GitHub Releases and show/copy changelogs; self-install remains deferred until safe signed/checksummed Windows assets are published
- the broader console described here is still future work

V2 must preserve:

- planner as decision-maker
- executor/Codex as implementation worker
- engine/CLI as inert bridge and runtime harness
- raw human input forwarding
- safe pause/interruption only at AI-turn or cycle boundaries
- full headless CLI operation without the console

## Scope

V2 adds an optional Operator Console that sits on top of the same engine, state model, and durable loop used by the headless CLI.

V2 does not:

- turn the GUI into a decision-maker
- bypass planner or executor authority
- expose hidden chain-of-thought
- require the console for normal CLI operation
- silently mutate active runs outside explicit engine protocol actions

## Product Goals

The intended operator experience is:

- start a run once
- walk away during healthy autonomous progress
- return later to meaningful app-building output
- intervene only for real blockers, approvals, explicit `ask_human`, missing secrets, or final review

The console should feel like an operator cockpit for autonomous app-building, not like a second planner.

## Product Principles

1. Engine remains inert.
2. Planner remains authoritative.
3. Executor remains the only implementation worker.
4. Console actions are explicit engine protocol calls.
5. Console may improve visibility and ergonomics, not meaning.
6. All operator-visible summaries must come from durable state, planner-safe fields, or explicit artifact content.
7. Secrets must never be printed in full or stored carelessly.
8. Headless CLI must remain first-class.

## Main User Experience

### Core operator loop

1. Operator selects a target repo.
2. Operator runs setup or confirms settings.
3. Operator starts a run from the console or headless CLI.
4. Engine advances autonomously through bounded cycles.
5. Console shows live planner, executor, artifact, worker, and approval activity.
6. Operator intervenes only through explicit controls.
7. Engine pauses only at safe points and re-consults the planner when intervention changes pending work.

### Two chat modes

#### Control Chat / Live Run Intervention

Purpose:

- send a raw human message that may affect the active run

Behavior:

- the console sends the raw message to the engine as a control-chat message
- the engine queues it durably
- at the next safe point, the engine pauses before executing any pending next step
- the engine sends the planner:
  - the raw human message
  - current run state
  - the pending action buffer
  - the reason for intervention
- the planner decides whether to proceed, alter, cancel, ask follow-up, collect context, execute, pause, or complete

Control Chat must not:

- directly mutate run internals
- rewrite the message
- skip the planner

#### Side Chat / Non-Interfering Conversation

Purpose:

- let the operator ask questions, brainstorm, inspect code, or discuss the app without altering the active run

Behavior:

- side chat runs against selected context with a helper model or assistant mode
- side chat messages do not alter the active run
- the user may explicitly promote a side-chat message into Control Chat

Side Chat must not:

- silently feed active-run control
- rewrite or summarize itself into the run without explicit promotion

## Console Layout

The default console layout should support docked panes and future tab expansion.

### Primary panes

1. Main chat area
   - Control Chat tab
   - Side Chat tab
   - planner operator messages inline with run timeline where appropriate

2. Terminal area
   - PowerShell-like tabs
   - tab kinds:
     - PowerShell shell
     - engine log stream
     - executor/Codex stream
     - planner/operator activity stream

3. File explorer
   - repo tree
   - artifact shortcuts
   - pinned files

4. Inspector side panel
   - latest run snapshot
   - pending action buffer
   - current stop reason
   - progress gauge
   - Action Required when approval or human attention is needed
   - worker panel when relevant

Current dogfood shell direction:

- Home is the default guided dashboard.
- The left sidebar behaves as real tab navigation, not scroll anchors.
- The top strip uses plain-language `Connection Status` and `Loop Status` labels with timers and repo/run identity.
- Dogfood-launched shells carry an expected target repo path. The shell compares that expected path with the backend-reported repo root on connect/reconnect; a mismatch is rendered as `Wrong Repo Backend`, Start/Continue are disabled, and the primary action is to restart the dogfood-owned backend for the target repo.
- Start/Continue are shown as Start Build / Continue Build and call explicit protocol actions.
- Loop status must distinguish `Running` from safe-pause states. A planner safe checkpoint with `planner_outcome:"execute"` and no executor thread/turn is `Ready to Continue`, not actively running; the primary action is `Continue Build / Dispatch Executor`.
- Active-run guards are displayed as running only when the status snapshot says `currently_processing:true`; a guard or unfinished run record by itself is not enough.
- Side Chat is currently a non-interfering context assistant foundation. It persists raw messages and answers from observable runtime context only; it must plainly say that side messages do not queue Control Chat, set stop flags, change pending actions, or affect planner/Codex flow unless explicitly promoted through a future audited control action.
- If a safe stop flag or `operator_stop_requested` state is present, Action Required shows `Safe stop was requested` and offers `Clear Stop and Continue`, which clears the mechanical flag and then uses the explicit `continue_run` protocol action when the run can resume.
- Backup terminal commands are hidden in advanced help, not presented as the primary path.
- Stop reasons are translated for display while preserving the technical code in detail views.

### Editor panes

The console should support opening text editor tabs for:

- `.orchestrator/brief.md`
- `.orchestrator/roadmap.md`
- `.orchestrator/decisions.md`
- `.orchestrator/human-notes.md`
- `AGENTS.md`
- other repo files selected from the explorer

Editors should support:

- plain text editing
- save/cancel
- external-change warning
- easy copy/paste

## Repo Picker And Setup UX

The console must include:

- repo path browser/select button
- repo/project name field where relevant
- target-repo validation summary
- missing contract file warnings
- clear link to `setup`, `init`, and contract editing

Validation should show:

- repo root valid or not
- git repo present or missing
- contract files present/missing
- state/log/artifact directories ready or missing
- placeholder/empty contract files when detectable mechanically

The console must not infer project direction from missing files. It only reports readiness.

## Settings Panel

The settings panel should expose:

- OpenAI API key entry/reference
- ntfy server URL
- ntfy topic
- ntfy auth token
- ntfy enable toggle
- planner model
- planner model test state: configured, requested, resolved, verified, last error, and last tested result
- Codex/executor model/config health: requested model when known, access mode when detectable, effort when detectable, unavailable-model errors, and last tested result
- side-chat model
- worker concurrency
- verbosity
- run mode / temperament
- runtime timeouts, including unlimited/no-limit values for long executor or human-wait phases
- permission/autonomy profile: Guided, Balanced, Autonomous, or Full Send / Lab Mode
- update channel and auto-check/download/install preferences
- update status, latest release/changelog, and safe install support state

Secret handling requirements:

- never print secrets in full
- prefer environment variable or OS-safe credential storage
- only store secrets in config when explicitly chosen and documented
- show masked values
- never claim full Codex access or strongest model unless the engine can verify it
- never silently fall back to a weaker model; configured/requested/verified model state must stay visible

## Run Controls

The console should expose explicit operator controls that map to engine protocol actions:

- Start run
- Continue run
- Safe stop at next cycle boundary
- Hard stop / emergency kill
- Pause at next safe point
- Pause at cycle end
- Resume
- Inject human note
- Toggle verbosity
- Show status snapshot
- Open latest artifact
- Open latest diff
- Open latest run summary

These controls must stay mechanical. They do not grant GUI authority to reinterpret the run.

## Pending Action Buffer

V2 adds a first-class pending action buffer to the engine model.

The buffer must capture what the engine is about to do next, such as:

- `pending_planner_action`
- `pending_executor_prompt`
- `pending_dispatch_target`
- `pending_reason`
- `pending_artifact_refs`
- `pending_turn_type`

Purpose:

- make imminent work inspectable
- let control-chat intervention happen before the next action fires
- give the planner explicit context when a human interrupts a pending step

If a control message arrives before the pending action executes:

1. the engine holds the pending action at the next safe point
2. the planner receives the pending action plus the raw human message
3. the planner decides whether to proceed, replace, pause, or ask follow-up

The engine must not decide on its own whether the pending action is still good.

## Planner-Safe Human-Readable Status

V2 should add planner-provided operator-facing fields that are safe to display:

- `operator_message`
- `current_focus`
- `next_intended_step`
- `why_this_step`
- `progress_percent`
- `progress_confidence`
- `progress_basis`

These fields are:

- planner-authored
- human-readable
- non-CoT summaries
- display-safe in console and CLI verbose modes

The console must never request or display hidden chain-of-thought.

## Progress Gauge

The console should show progress using planner-provided fields only.

Required fields:

- `progress_percent`: integer 0-100
- `progress_confidence`: low | medium | high
- `progress_basis`: short factual explanation

The console must not invent its own percent.

Example:

- `progress_percent: 32`
- `progress_confidence: medium`
- `progress_basis: "Static scene and domain model exist; game loop, input, collisions, and scoring remain."`

## Return Summary / While-You-Were-Gone View

The console should present a concise unattended summary after long progress windows:

- what was accomplished
- elapsed time from run start/continue to stop when durable timestamps are available
- files changed
- tests run and results
- current blocker if any
- current progress
- next intended step
- important artifacts

This summary should be built from:

- durable run state
- planner-safe status fields
- structured executor/test/artifact metadata

It must not invent semantic meaning outside those sources.

## Live Output / Verbosity

The shell should provide an obvious Live Output location for real-time activity:

- planner turn started/completed
- planner operator messages, current focus, and next intended step
- executor/Codex started/completed/failed
- Codex/model/config errors
- approvals and ask-human states
- artifacts created
- worker events
- safe-stop and control-message events

Verbosity controls what is shown:

- Quiet: major state changes, blockers, approvals, and failures only
- Normal: readable progress updates without raw protocol payloads
- Verbose: planner/executor focus, artifact, worker, and command summaries
- Trace: raw safe event payloads and technical details

The console may expose raw event payloads in expandable details, but must not expose hidden chain-of-thought.

## Action Required

If the executor requires approval, the console should clearly show:

- requested action
- scope
- files/commands involved
- reason
- approve / deny controls
- technical details copy action

The current shell names this area Action Required. It stays hidden/de-emphasized when no action is needed and shows a badge when the latest protocol state requires operator attention.

Approval remains explicit operator action routed through engine protocol.

Planner `ask_human` pauses are also Action Required states. When the status snapshot exposes an ask-human question or blocker, the shell shows "Planner needs your answer", displays the question/blocker in plain English, and provides a raw answer box. "Send Answer and Continue" queues the exact operator text through `inject_control_message` with `reason:"ask_human_answer"` and then calls `continue_run` for the same run. The shell does not interpret the answer or decide whether it resolves the blocker; the planner receives the raw intervention packet and decides the next step.

Safe stop flags are also surfaced as Action Required because the operator must clear them before expecting unattended progress. `Clear Stop and Continue` must be a protocol-only mechanical sequence: `clear_stop_flag`, status refresh, then `continue_run` if the run is resumable. It must not infer why the stop was requested.

Codex readiness should also be visible to the operator. The required executor configuration is `gpt-5.5`, `danger-full-access`, approval `never`, and effort `xhigh`. The shell may only show model, effort, sandbox, or full-access state as verified after the engine's Codex probe succeeds using the same control-server environment. If not verifiable, it must say `Not verified` and provide a concrete check path such as model-test protocol actions or `orchestrator doctor`; it must not silently claim the strongest model or full access.

If Codex reports that a model does not exist or the account lacks access, Action Required must explain the model problem plainly, keep the technical error available, recommend changing/testing the configured Codex model, and avoid any silent fallback.

After Codex CLI updates, operators should restart the dogfood/control server before testing or running again so old app-server processes cannot retain a stale binary, auth profile, or environment.

The dogfood shell should make this practical rather than ceremonial:

- show backend PID, start time, binary path, binary modified time, version, and stale restart warning
- auto-test planner/Codex health after reconnect
- provide Copy Model Health for safe support diagnostics
- stop only dogfood-owned stale backend processes and proven owned backend listener process trees for the same repo/address automatically
- show port-cleanup diagnostics with current PID/path/command line when the port does not clear
- never kill unknown user-started orchestrator/Codex processes blindly

## Worker Panel

When workers are used, the console should show:

- worker id and name
- worker status
- worker scope
- current task
- latest artifact
- integration/apply state

The UI must not show ghost workers. It should only surface durable worker records.

## AI Autofill Wizard

The AI Autofill Wizard is a guided setup flow for contract files.

Responsibilities:

- inspect suitable repo/background files
- ask the user targeted questions
- draft:
  - `brief.md`
  - `roadmap.md`
  - `decisions.md`
  - `human-notes.md`
  - optional `AGENTS.md`
- preview content before saving or save only with explicit approval

Constraints:

- must not silently mutate an active run
- must not silently overwrite contract files
- must use explicit save/apply operations

## Run Modes / Temperament

The console should expose run-mode selections such as:

- survey
- build
- harden
- release
- autonomous
- cautious

These modes influence planner instructions and config only.

They must not grant the engine or GUI semantic authority.

## Tech Direction Recommendation

Longer-term preferred direction: Tauri console shell with the existing Go engine exposed through a local control protocol and event stream.

Current implementation choice for the first proof shell: Electron.

### Why Tauri

- lighter Windows footprint than Electron
- good fit for docked panes, explorer, editors, and chat UI
- easier packaging story for a Windows-focused operator console
- keeps the Go engine as a separate authoritative runtime

### Electron tradeoffs

Pros:

- mature desktop web-app ecosystem
- broad terminal/editor component availability

Cons:

- heavier runtime
- larger packaging footprint
- more overhead for a local-operator console that already has a Go engine

### Native Windows UI tradeoffs

Pros:

- native look and feel

Cons:

- slower product iteration
- more expensive custom UI work
- weaker reuse of web-style panes and event-stream UI patterns

### Recommendation

- keep the longer-term target pointed at Tauri for the fuller console
- keep the engine in Go
- connect them through an explicit local engine protocol
- preserve full headless CLI parity

### Why the first proof shell uses Electron

- the current repo already has a working Node/npm environment
- the current development environment does not yet have the Rust toolchain needed for a real Tauri build
- Electron let this slice ship a real clickable shell now without faking a desktop stack or bypassing the protocol
- the shell remains narrow enough that a later move to Tauri is still feasible if that becomes the product direction

## Non-Goals For This Spec Slice

- no full GUI implementation yet
- no hidden background daemon mode
- no semantic merge/conflict resolution
- no GUI-owned completion logic
- no chain-of-thought display
- no console-only engine features that bypass the CLI

## Acceptance Criteria For Future Implementation

This spec is successful if future chunks preserve:

- optional console, not mandatory console
- Control Chat versus Side Chat separation
- pending action buffer as an engine concept
- planner-provided operator status fields
- explicit engine protocol actions
- safe-point intervention semantics
- headless CLI compatibility
