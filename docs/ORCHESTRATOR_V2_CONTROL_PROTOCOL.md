# ORCHESTRATOR V2 CONTROL PROTOCOL

Status: Proposed V2 engine protocol with the first live engine slices partially implemented

## Purpose

Define the explicit local control protocol between the optional console and the existing inert engine.

The protocol exists so:

- the console does not mutate engine internals directly
- headless CLI and GUI use the same engine/state model
- control messages, status snapshots, artifacts, and live activity updates are consistent

## Transport Recommendation

Canonical protocol model:

- JSON envelopes
- request/response actions
- event stream for live updates

Current implemented transport:

- loopback HTTP request/response endpoint at `/v2/control`
- loopback NDJSON event stream at `/v2/events`
- public engine entrypoint: `orchestrator control serve [--addr HOST:PORT]`

Recommended later transport evolution:

- optional loopback WebSocket session for bidirectional requests, responses, and events once a richer console client exists

Optional compatibility adapters later:

- stdio transport for tests or embedded shells
- loopback HTTP for large artifact fetches if needed

The transport must remain local-only by default.

## Current Implementation Status

Implemented in the engine now:

- `start_run`
- `continue_run`
- `get_status_snapshot`
- `test_planner_model`
- `test_executor_model` / `test_codex_config`
- `approve_executor`
- `deny_executor`
- `set_verbosity`
- `set_stop_flag` / `stop_safe`
- `clear_stop_flag`
- `get_pending_action`
- `get_artifact`
- `list_recent_artifacts`
- `list_contract_files`
- `open_contract_file`
- `save_contract_file`
- `run_ai_autofill`
- `list_repo_tree`
- `open_repo_file`
- `get_runtime_config`
- `set_runtime_config` for verbosity-related fields only
- `inject_control_message`
- `list_control_messages`
- `send_side_chat_message` as a truthful recorded-only stub
- `list_side_chat_messages`
- `capture_dogfood_issue`
- `list_dogfood_issues`
- `list_workers`
- `create_worker`
- `dispatch_worker`
- `remove_worker`
- `integrate_workers`
- NDJSON event streaming through `/v2/events`

Implemented engine behavior in this slice:

- durable pending-action state
- async control-server-launched foreground run loops for `start_run` and `continue_run`
- a process-local active-run guard that rejects overlapping GUI-launched unattended loops for the same control server
- durable control-message queue
- safe-point intervention routing that packages raw control messages plus pending action context into the next planner turn
- live planner-safe operator-status fields on the current runtime path
- public protocol demo client via `orchestrator control demo ...`

Implemented on top of the protocol now:

- a first minimal optional desktop shell under `console/v2-shell/`
- live status rendering from `get_status_snapshot`
- protocol-backed Home dashboard Start Run / Continue Run controls
- a dogfood-hardened guided Home dashboard with plain connection/loop status, real tab navigation, translated stop reasons, Action Required visibility, and readable activity summaries
- pending-action detail rendering from `get_pending_action`
- approval-center skeleton rendering from `get_status_snapshot`
- protocol-backed primary executor approve/deny actions
- compact run-summary rendering from status snapshots plus recent events
- protocol-backed artifact browsing and raw text/JSON viewing
- protocol-backed canonical contract-file opening and saving
- protocol-backed AI autofill drafting for canonical contract files, with preview-before-save in the shell
- protocol-backed read-only repo browsing for tree listing and file opening
- embedded local operator terminal tabs for shell convenience
- a Side Chat pane skeleton that records side-chat messages through the real protocol without affecting the active run
- a protocol-backed Dogfood Notes pane for quick timestamped issue capture tied to the repo and latest run when available
- a richer progress and roadmap-alignment panel driven by planner-safe operator status plus surfaced roadmap context
- Worker Panel controls for explicit create, dispatch, remove, and integration-preview actions through the engine protocol
- a richer activity timeline built over `/v2/events`, with category/current-run/text filtering plus shell-local terminal lifecycle events
- protocol-backed control-message injection
- protocol-backed safe-stop and verbosity controls
- practical shell-dogfooding behavior on top of the same protocol: local session persistence, reconnect/rehydration, inline error reporting, and a one-command startup helper

Not implemented yet:

- full desktop console
- side chat conversational backend
- direct GitHub issue filing from the shell
- full pending-action replacement/proceed/cancel event set
- worker-specific approval controls in the shell
- multi-run orchestration from one control server
- WebSocket transport

## Protocol Principles

1. Commands are explicit actions.
2. Engine state is authoritative.
3. Console sends requests; engine returns responses and events.
4. Planner, executor, and human input remain data sources, not UI-owned logic.
5. Intervention happens at safe points.
6. Pending actions are inspectable and holdable.

## Envelope Shapes

### Request

```json
{
  "id": "req_123",
  "type": "request",
  "action": "get_status_snapshot",
  "payload": {}
}
```

### Success response

```json
{
  "id": "req_123",
  "type": "response",
  "ok": true,
  "payload": {}
}
```

### Error response

```json
{
  "id": "req_123",
  "type": "response",
  "ok": false,
  "error": {
    "code": "worker_not_found",
    "message": "worker id was not found in durable state"
  }
}
```

### Event

```json
{
  "type": "event",
  "event": "planner.turn.completed",
  "sequence": 148,
  "at": "2026-04-22T16:35:42Z",
  "payload": {}
}
```

## Core Actions

### `start_run`

Purpose:

- start a new run in the selected repo

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "goal": "Build the next milestone",
  "mode": "build",
  "verbosity": "normal"
}
```

Response payload:

- `accepted: true`
- `async: true`
- `action: "start_run"`
- `run_id`
- `status: "started"`
- `repo_path`
- `started_at`
- operator message

Current behavior:

- the control server creates the durable run synchronously enough to return a real `run_id`
- it then launches the same foreground unattended loop used by default CLI `run` inside the control-server process
- the request returns immediately after launch so the shell and event stream do not freeze
- `repo_path`, when supplied, must match the control server repo root in this slice
- if another run loop was launched through this control server and is still active, the action fails mechanically with a truthful "run already active" error
- this is not a daemon or scheduler; if the control server process exits, the in-process run loop exits with it

### `continue_run`

Purpose:

- continue the latest unfinished run or a specific run id

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "run_id": "optional_specific_run_id",
  "mode": "build"
}
```

Response and current behavior:

- same async acknowledgement shape as `start_run`
- if `run_id` is omitted, the latest unfinished run is selected mechanically
- if `repo_path` is supplied, it must match the control server repo root
- the control server launches the same foreground unattended loop used by default CLI `continue`
- overlapping control-server-launched run loops are rejected with the same active-run guard

### `test_planner_model`

Purpose:

- verify the configured planner model or a supplied model id before a serious unattended run

Payload:

```json
{
  "model": "optional-model-id"
}
```

Response payload:

- `planner.configured_model`
- `planner.requested_model`
- `planner.resolved_model` when an alias resolves
- `planner.verified_model` when the Models API confirms availability
- `planner.verification_state`, such as `verified`, `missing_api_key`, `discovery_failed`, `invalid`, or `unavailable`
- `planner.last_error` when verification fails
- `planner.plain_english`
- `planner.recommended_action`
- combined `model_health` style `needs_attention`, `blocking`, and `message`

Current behavior:

- `gpt-5-latest` is an orchestrator planner-model alias that resolves through the OpenAI Models API to the newest available mainline GPT-5 model visible to the account
- exact model ids are checked directly through the Models API
- if discovery or verification fails, the engine reports that truthfully and does not silently fall back to another model

### `test_executor_model` / `test_codex_config`

Purpose:

- surface what the engine can detect about Codex executor configuration and recent model failures

Payload:

```json
{
  "model": "reserved-for-future-explicit-codex-model"
}
```

Response payload:

- `executor.configured_model`, currently `external Codex configuration`
- `executor.requested_model` when Codex reported or errored with a model id
- `executor.verification_state`, such as `launch_ready_model_not_verified`, `invalid`, `unavailable`, or `not_verified`
- `executor.access_mode`, reflecting the current app-server execution policy the engine can see
- `executor.effort`, currently `not reported` unless Codex exposes it
- `executor.model_unavailable`
- `executor.last_error`
- `executor.plain_english`
- `executor.recommended_action`

Current behavior:

- the orchestrator does not silently change the Codex model
- if the latest executor failure says a model does not exist or the account lacks access, the status snapshot and test action mark executor model health as `invalid` and `blocking`
- if no model-specific failure is known, the test can verify the Codex app-server launch path, but the exact external Codex model/effort may remain `not_verified`

### `pause_at_safe_point`

Purpose:

- request that the engine pause before executing the next pending action at the next safe boundary

Payload:

```json
{
  "run_id": "run_123",
  "reason": "operator_requested_pause"
}
```

### `stop_safe`

Purpose:

- request a clean stop after the current bounded cycle or safe point

Payload:

```json
{
  "run_id": "run_123",
  "reason": "operator_requested_safe_stop"
}
```

### `stop_hard`

Purpose:

- request an emergency stop or kill where mechanically supported

Payload:

```json
{
  "run_id": "run_123",
  "target": "run_or_active_executor"
}
```

This action stays mechanical. Unsupported hard-stop paths must report that truthfully.

### `resume`

Purpose:

- resume from a paused/stopped safe boundary using existing durable state

Payload:

```json
{
  "run_id": "optional_specific_run_id",
  "autonomous": true
}
```

### `inject_control_message`

Purpose:

- queue a raw Control Chat message that may affect the active run

Payload:

```json
{
  "run_id": "run_123",
  "message": "Make the wall red, not blue.",
  "source": "control_chat",
  "reason": "operator_intervention"
}
```

Behavior:

- message is persisted raw
- engine does not reinterpret it
- message becomes planner input at the next safe point with pending action context

### `send_side_chat_message`

Purpose:

- send a non-interfering side chat message

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "message": "What remains before release?",
  "context_policy": "repo_and_latest_run_summary"
}
```

Behavior:

- does not alter active run unless explicitly promoted later
- current slice records the raw side-chat message durably, returns a truthful recorded-only stub response, and does not affect the active run

### `list_side_chat_messages`

Purpose:

- list recorded side-chat messages for the current repo

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "limit": 20
}
```

Behavior:

- returns the side-chat log recorded so far for this repo
- current slice does not produce real side-chat assistant replies; recorded messages include a truthful backend-unavailable note

### `capture_dogfood_issue`

Purpose:

- record a quick dogfood friction note tied to the current repo and latest run when available

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "run_id": "optional_run_guard",
  "source": "operator_shell",
  "title": "Reconnect leaves stale artifact selected",
  "note": "After reconnect, the artifact pane still showed the old path until a manual refresh."
}
```

Behavior:

- stores the note durably with a timestamp
- associates it with the repo and run when provided or mechanically discoverable
- emits `dogfood_issue_recorded`
- does not create a GitHub issue or alter the active run

### `list_dogfood_issues`

Purpose:

- retrieve recent dogfood notes for the current repo

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "limit": 20
}
```

Behavior:

- returns recent timestamped notes in reverse chronological order
- current slice is local repo-scoped issue capture, not a remote tracker integration

### `list_workers`

Purpose:

- fetch a fuller worker list for the current run than the compact status snapshot provides

Payload:

```json
{
  "run_id": "run_123",
  "limit": 20
}
```

Behavior:

- returns worker ids, names, statuses, scopes, worktree paths, approval state, thread/turn ids, control flags, and bounded summary fields
- current slice now powers shell-side worker inspection plus explicit create, dispatch, remove, and integration-preview controls

### `create_worker`

Purpose:

- create one isolated worker worktree and durable worker record for a run

Payload:

```json
{
  "run_id": "run_123",
  "name": "code-survey",
  "scope": "Inspect the UI shell boundaries"
}
```

### `dispatch_worker`

Purpose:

- send one bounded executor turn into the selected worker worktree

Payload:

```json
{
  "worker_id": "worker_123",
  "prompt": "Inspect the shell and summarize the next bounded edit."
}
```

### `remove_worker`

Purpose:

- remove one idle or completed worker and its isolated worktree

Payload:

```json
{
  "worker_id": "worker_123"
}
```

### `integrate_workers`

Purpose:

- build a read-only integration preview artifact from explicit worker outputs

Payload:

```json
{
  "worker_ids": ["worker_123", "worker_456"]
}
```

Behavior:

- writes an integration artifact under `.orchestrator/artifacts/integration/...`
- returns truthful conflict candidate data
- does not apply worker outputs into the main repo

### `set_verbosity`

Purpose:

- update operator-facing verbosity

Payload:

```json
{
  "scope": "session_or_repo",
  "verbosity": "verbose"
}
```

### `set_config`

Purpose:

- apply selected configuration updates

Payload:

```json
{
  "planner_model": "gpt-5-latest",
  "side_chat_model": "gpt-5.4-mini",
  "worker_concurrency_limit": 2,
  "ntfy": {
    "enabled": true,
    "server_url": "https://ntfy.example.com",
    "topic": "orchestrator",
    "auth_token_ref": "credential_store_key"
  }
}
```

Config updates should become active at safe points, not by mutating an in-flight turn.

`gpt-5-latest` is the orchestrator's planner-model alias. Live OpenAI calls resolve it through the Models API to the newest available mainline versioned GPT-5 model visible to the account. Exact model ids still pin behavior. If discovery fails, the engine reports the failure; it does not silently fall back to an unverified model.

### `get_status_snapshot`

Purpose:

- fetch the current durable state needed for UI rendering

Payload:

```json
{
  "run_id": "optional_specific_run_id"
}
```

Response should include:

- repo/runtime readiness
- latest run summary
- run start/stop/elapsed-time fields when available
- latest checkpoint
- stop reason
- pending action buffer
- worker summary
- approval state
- latest artifacts
- planner operator-status fields when present
- model-health fields for planner and executor/Codex configuration, including configured/requested/verified model state, unavailable-model errors, and recommended operator action
- current live runtime path carries operator-status additively on `planner.v1`; `planner.v2` remains the future stricter contract

### `approve_executor`

Purpose:

- approve the latest unfinished run's active primary executor approval request

Payload:

```json
{
  "run_id": "optional_latest_run_guard"
}
```

Notes:

- current implementation is intentionally limited to the latest unfinished run in this slice
- current shell wiring is for the primary executor approval path only

### `deny_executor`

Purpose:

- deny the latest unfinished run's active primary executor approval request

Payload:

```json
{
  "run_id": "optional_latest_run_guard"
}
```

Notes:

- current implementation is intentionally limited to the latest unfinished run in this slice
- denial remains structured machine data, not GUI-owned failure semantics

### `get_activity_stream`

Purpose:

- subscribe to live engine events

Payload:

```json
{
  "from_sequence": 0,
  "run_id": "optional_filter"
}
```

### `get_artifact`

Purpose:

- retrieve one artifact by path or id

Payload:

```json
{
  "artifact_path": ".orchestrator/artifacts/context/run_123/collected_context_latest.json"
}
```

### `list_recent_artifacts`

Purpose:

- list the latest surfaced artifacts for a run, optionally filtered by category

Payload:

```json
{
  "run_id": "run_123",
  "category": "integration",
  "limit": 12
}
```

Notes:

- current implementation is truthful and scoped to artifacts already surfaced by the engine for the selected run
- it does not fake a full repo-wide artifact index

### `list_contract_files`

Purpose:

- list the canonical contract files supported by the first editor slice

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo"
}
```

Notes:

- current implementation is intentionally limited to:
  - `.orchestrator/brief.md`
  - `.orchestrator/roadmap.md`
  - `.orchestrator/decisions.md`
  - `.orchestrator/human-notes.md`
  - `AGENTS.md`

### `open_contract_file`

Purpose:

- fetch current contents of a contract file for editor display

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "path": ".orchestrator/brief.md"
}
```

### `save_contract_file`

Purpose:

- write an updated contract file

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "path": ".orchestrator/brief.md",
  "content": "# Brief\n...",
  "expected_mtime": "optional_conflict_guard"
}
```

### `run_ai_autofill`

Purpose:

- draft selected canonical contract files from guided operator answers without silently saving them

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "targets": [
    ".orchestrator/brief.md",
    ".orchestrator/roadmap.md",
    ".orchestrator/decisions.md"
  ],
  "answers": {
    "project_summary": "Planner-led operator shell for autonomous app-building",
    "desired_outcome": "Reduce setup friction and improve repo navigation",
    "users_platform": "Windows operators using the repo locally",
    "constraints": "Keep the engine inert and headless CLI first-class",
    "milestones": "Get the shell to a trustworthy everyday operator tool",
    "decisions": "Use the real engine protocol for all shell state and actions",
    "notes": "Keep the wizard as a review-before-save authoring flow"
  }
}
```

Behavior:

- targets are limited to the canonical contract files in this slice
- the engine uses the OpenAI/Responses-backed autofill path to draft content
- drafts are returned synchronously for preview
- saving still goes through the separate `save_contract_file` action
- the autofill flow does not alter the active run implicitly

### `list_repo_tree`

Purpose:

- browse one repo directory at a time through the real engine protocol

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "path": ".orchestrator",
  "limit": 200
}
```

Behavior:

- returns a read-only listing for the requested directory under the repo root
- current slice is one-directory-at-a-time, not a full recursive index
- entries include whether a file can be opened in the contract editor later

### `open_repo_file`

Purpose:

- fetch one repo file for read-only viewing in the shell

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "path": "README.md"
}
```

Behavior:

- reads a repo file under the active repo root
- current slice is read-only for arbitrary repo files
- canonical contract files may still be opened in the dedicated contract editor flow

### `get_pending_action`

Purpose:

- inspect the engine's next pending action buffer

Payload:

```json
{
  "run_id": "run_123"
}
```

Response payload should include:

- pending planner action
- pending executor prompt
- pending dispatch target
- pending reason
- pending hold state
- last intervention reason if any

## Pending Action Model

The pending action buffer is a durable engine-side concept.

Suggested payload shape:

```json
{
  "present": true,
  "turn_type": "executor_dispatch",
  "planner_outcome": "execute",
  "planner_response_id": "resp_123",
  "dispatch_target": {
    "kind": "primary_executor",
    "worker_id": null
  },
  "executor_prompt": "Apply the bounded edit...",
  "reason": "planner_selected_execute",
  "artifact_refs": [],
  "held": false,
  "hold_reason": null
}
```

When a control message arrives:

1. engine marks pending action as held at the next safe point
2. emits `pending_action.held`
3. re-runs planner with:
   - raw human message
   - pending action
   - current run state
   - reason for intervention

Possible planner outcomes after reconsideration:

- proceed unchanged
- replace pending action
- cancel pending action
- ask_human
- collect_context
- execute something else
- pause
- complete

## Safe-Point Semantics

Safe points remain:

- after a completed planner turn
- after a completed executor turn
- after a completed second planner turn in the existing bounded-cycle model

Protocol guarantees:

- intervention may queue immediately
- effect occurs only at the next safe point
- engine must not interrupt semantic control mid-turn unless the existing mechanical executor control path explicitly supports it

## Event Stream

The console needs a first-class event model.

Target event types:

- `run.started`
- `planner.turn.started`
- `planner.turn.completed`
- `planner.operator_message`
- `collect_context.started`
- `collect_context.completed`
- `executor.turn.started`
- `executor.turn.completed`
- `executor.turn.failed`
- `model.health.tested`
- `model.health.failed`
- `file.changes.detected`
- `artifact.created`
- `human.message.queued`
- `safe_point.reached`
- `pending_action.held`
- `pending_action.replaced`
- `pending_action.cancelled`
- `pending_action.proceeded`
- `ask_human`
- `approval.required`
- `pause.requested`
- `pause.reached`
- `stop.requested`
- `run.completed`
- `fault.recorded`

Current engine event identifiers in the first implementation use underscore-style names such as:

- `run_started`
- `run_completed`
- `planner_turn_started`
- `planner_turn_completed`
- `executor_turn_started`
- `executor_turn_completed`
- `executor_turn_failed`
- `executor_approval_required`
- `model_health_tested`
- `model_health_failed`
- `verbosity_changed`
- `safe_point_reached`
- `fault_recorded`
- `control_message_queued`
- `control_message_consumed`
- `safe_point_intervention_pending`
- `planner_intervention_turn_started`
- `planner_intervention_turn_completed`
- `pending_action_updated`
- `pending_action_cleared`

The future console may still normalize these into a higher-level dotted event vocabulary later, but the current protocol should be treated as underscore-based.

The first desktop shell renders these current underscore-style identifiers directly. A future richer console may normalize them later.

Suggested common event payload fields:

```json
{
  "run_id": "run_123",
  "cycle_number": 4,
  "checkpoint": {
    "sequence": 12,
    "stage": "planner",
    "label": "planner_turn_post_executor",
    "safe_pause": true
  },
  "summary": "planner completed post-executor turn",
  "artifact_path": ".orchestrator/artifacts/executor/run_123/executor_summary.json"
}
```

## Status Snapshot Shape

The UI should be able to render from one structured snapshot.

Suggested shape:

```json
{
  "runtime": {
    "engine_mode": "headless_or_console_attached",
    "repo_ready": true,
    "planner_ready": true,
    "executor_ready": true,
    "ntfy_ready": false
  },
  "run": {
    "id": "run_123",
    "goal": "Build the next milestone",
    "status": "initialized",
    "stop_reason": "planner_pause",
    "started_at": "2026-04-23T14:00:00Z",
    "stopped_at": "2026-04-23T14:14:32Z",
    "elapsed_seconds": 872,
    "elapsed_label": "stopped after 14:32",
    "executor_last_error": "",
    "latest_checkpoint": {},
    "next_operator_action": "continue_existing_run"
  },
  "model_health": {
    "planner": {
      "configured_model": "gpt-5-latest",
      "requested_model": "gpt-5-latest",
      "verification_state": "not_verified"
    },
    "executor": {
      "configured_model": "external Codex configuration",
      "requested_model": "not reported yet",
      "verification_state": "not_verified",
      "access_mode": "workspace-write sandbox, approval on-request"
    },
    "needs_attention": false,
    "blocking": false,
    "message": "Model health has not been fully verified."
  },
  "planner_status": {
    "operator_message": "Implementing input handling next.",
    "current_focus": "game loop and input integration",
    "next_intended_step": "dispatch bounded input-handler implementation",
    "why_this_step": "core progress now depends on input plumbing",
    "progress_percent": 32,
    "progress_confidence": "medium",
    "progress_basis": "scene exists; input, collisions, and scoring remain"
  },
  "pending_action": {},
  "workers": [],
  "approvals": [],
  "artifacts": []
}
```

## Security And Secret Handling

- protocol must never echo full secrets by default
- loopback-only by default
- no direct filesystem mutation outside explicit protocol actions
- console should prefer credential-store references over plain secret payloads where feasible
- the embedded terminal is an operator utility shell and not a hidden engine backdoor for run-control authority

## Non-Goals For First Implementation

- no remote multi-user control server
- no cloud-hosted control plane
- no GUI-owned semantic policy
- no hidden background daemon requirement

## Recommended First Engine Chunk

Implement the protocol skeleton before the GUI:

1. local engine control service shell
2. `get_status_snapshot`
3. `get_activity_stream`
4. `set_verbosity`
5. `set_config` applied at safe points
6. `inject_control_message`
7. `get_pending_action`

That will prove the engine boundary before any Tauri shell is built.

## Implemented Entry Point In This Slice

Start the local engine protocol server with:

```powershell
orchestrator control serve
```

It prints:

- `control.listen: http://127.0.0.1:44777`
- `control.action_endpoint: http://127.0.0.1:44777/v2/control`
- `control.events_endpoint: http://127.0.0.1:44777/v2/events`
