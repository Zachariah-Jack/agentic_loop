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
- `get_active_run_guard`
- `recover_stale_run`
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
- `set_runtime_config` for verbosity, runtime timeouts, permission profile, and update settings
- `inject_control_message`
- `list_control_messages`
- `send_side_chat_message` as a context-agent message
- `list_side_chat_messages`
- `side_chat_context_snapshot`
- `side_chat_action_request`
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
- a durable active-run guard file for GUI-launched loops so a run left active by a dead previous backend can be detected and mechanically recovered
- `recover_stale_run`, which clears stale active-run guards without deleting run history/artifacts or changing the run's planner/executor outcome
- SQLite busy hardening for common state reads: busy timeout, WAL mode, bounded retry, and a plain `state_database_locked` error when lock contention persists
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
- a Side Chat pane that records context-only messages through the real protocol and uses explicit audited actions for planner notes or Safe Stop
- a protocol-backed Dogfood Notes pane for quick timestamped issue capture tied to the repo and latest run when available
- a richer progress and roadmap-alignment panel driven by planner-safe operator status plus surfaced roadmap context
- Worker Panel controls for explicit create, dispatch, remove, and integration-preview actions through the engine protocol
- a richer activity timeline built over `/v2/events`, with category/current-run/text filtering plus shell-local terminal lifecycle events
- protocol-backed control-message injection
- protocol-backed safe-stop and verbosity controls
- practical shell-dogfooding behavior on top of the same protocol: local session persistence, reconnect/rehydration, inline error reporting, and a one-command startup helper
- model-health normalization across status snapshots and latest probe results, so newer successful component probes clear older stale component errors while newer failures remain blocking
- backend identity fields in status snapshots for stale-process detection: PID, start time, binary path/mtime, version, revision, build time, repo root, and stale restart recommendation
- dogfood-owned backend metadata and cleanup in the startup helper; owned stale processes and proven owned backend listener process trees for the same repo/address are stopped automatically, while unknown processes are reported with PID/path/command line and left alone
- shell recovery over the same protocol: `Recover Backend / Unlock Repo` calls `recover_stale_run` when a stale active-run guard is present, restarts only dogfood-owned backend processes when needed after verifying the port is clear, reconnects, reruns health checks, and refreshes panels
- dogfood repo binding: the startup helper launches the backend from the selected target repo, verifies `get_status_snapshot.runtime.repo_root` matches `-RepoPath`, and passes the expected repo path to the shell; the shell treats expected/actual repo mismatch as a mechanical `Wrong Repo Backend` state and disables Start/Continue until recovery restarts the backend for the target repo

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

For quick local PowerShell probes, the control endpoint also accepts a minimal action-only envelope and treats missing `type` as `"request"` and missing `payload` as `{}`:

```json
{
  "action": "test_executor_model"
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

Common dogfood recovery error:

```json
{
  "type": "response",
  "ok": false,
  "error": {
    "code": "state_database_locked",
    "message": "state database is temporarily locked by another orchestrator process; retry after recovery or restart the owned backend"
  }
}
```

The shell should show this as a recovery problem, not as a raw SQLite crash. It should recommend `Recover Backend / Unlock Repo` for dogfood-owned launches and must not kill unknown processes automatically.

If dogfood backend cleanup cannot clear the port, startup/recovery must report a diagnostic block instead of a vague failure:

- attempted owned PID
- attempted process path and command line
- kill methods used, including `taskkill /T /F` on Windows
- current port-holder PID/path/command line
- whether the current holder matches dogfood ownership metadata
- whether the current holder is different from the attempted PID
- the next safe action

Normal dogfood launches start `dist\orchestrator.exe` directly so ownership metadata tracks the actual backend process rather than a hidden PowerShell wrapper. Unknown port holders are still protected from automatic termination.

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

### `get_active_run_guard`

Purpose:

- inspect whether the repo has a durable active-run guard created by a GUI/control-server-launched run loop
- distinguish current-backend active runs from stale guards left by a previous backend process/session

Payload:

```json
{}
```

Response payload includes:

- `present`
- `run_id`
- `action`
- `status`
- `repo_path`
- `backend_pid`
- `backend_started_at`
- `session_id`
- `current_backend`
- `stale`
- `stale_reason`
- `message`

### `recover_stale_run`

Purpose:

- mechanically recover from a stale active-run guard that blocks normal dogfood use
- preserve run history, artifacts, and repo files
- avoid pretending the planner completed or that executor work succeeded

Payload:

```json
{
  "run_id": "run_123",
  "reason": "operator_recovery",
  "force": true
}
```

Behavior:

- if the selected run is unfinished, preserves its status/checkpoint/history so a safe-pause `continue_existing_run` remains resumable
- clears the matching stale active-run guard
- emits `stale_run_recovered`
- returns the refreshed next operator action, such as `continue_existing_run` for a resumable safe-pause run
- refuses to recover a run actively owned by the current backend; request Safe Stop first instead

This is recovery plumbing, not semantic completion. It must not mark `planner_complete`, delete artifacts, delete state history, or choose project direction.

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
- `planner.verified_model` when the resolved/requested model completes the Responses API probe
- `planner.verification_state`, such as `verified`, `missing_api_key`, `discovery_failed`, `invalid`, or `unavailable`
- `planner.last_error` when verification fails
- `planner.plain_english`
- `planner.recommended_action`
- combined `model_health` style `needs_attention`, `blocking`, and `message`

Current behavior:

- `gpt-5-latest` is an orchestrator planner-model alias that resolves through the OpenAI Models API to the newest available mainline GPT-5 model visible to the account
- exact planner model ids below `gpt-5.4` are invalid for this product line
- exact model ids, and resolved alias targets, are checked by a tiny Responses API call
- if discovery or verification fails, the engine reports that truthfully and does not silently fall back to another model
- the desktop shell auto-runs this check on connect/reconnect and before protocol Start Build / Continue Build preflight

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

- `executor.configured_model`, currently the required executor model `gpt-5.5`
- `executor.requested_model`
- `executor.verification_state`, such as `launch_ready_model_not_verified`, `invalid`, `unavailable`, or `not_verified`
- `executor.access_mode`, reflecting the required app-server execution policy
- `executor.effort`
- `executor.codex_executable_path`
- `executor.codex_version`
- `executor.codex_config_source`
- `executor.codex_model_verified`
- `executor.codex_permission_mode_verified`
- `executor.codex_last_probe_error`
- `executor.model_unavailable`
- `executor.last_error`
- `executor.plain_english`
- `executor.recommended_action`

Current behavior:

- the orchestrator requires Codex executor model `gpt-5.5`
- executor app-server turns request `model=gpt-5.5`, approval `never`, `danger-full-access`, and effort `xhigh`
- `test_executor_model` / `test_codex_config` runs a real probe through the Codex command path visible to the control server:

```powershell
codex exec --model gpt-5.5 --sandbox danger-full-access -c 'approval_policy="never"' -c 'model_reasoning_effort="xhigh"' --cd <repo> "Reply with only OK."
```

- the orchestrator does not silently fall back to a weaker Codex model
- if the latest executor failure says a model does not exist or the account lacks access, the status snapshot and test action mark executor model health as `invalid` and `blocking`
- if no probe has succeeded in the current process, Codex model/full-access remains `not_verified` or `launch_ready_model_not_verified`; operators should restart the control server after Codex updates and test again
- after a newer successful executor probe, the control server and shell treat older run-level unavailable-model errors as stale for model-health gating; a newer executor run failure overrides that success again

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
- when the latest snapshot reports `ask_human.present=true`, the shell may queue the operator's answer with `source:"action_required_answer"` and `reason:"ask_human_answer"`, then call `continue_run` for that run; the answer remains raw data and the planner decides how to proceed

### `send_side_chat_message`

Purpose:

- send a context-only side chat message

Payload:

```json
{
  "repo_path": "D:\\Projects\\target_repo",
  "message": "What remains before release?",
  "context_policy": "repo_and_latest_run_summary"
}
```

Behavior:

- records the raw side-chat message durably and returns an assistant response generated from observable runtime context
- does not call `inject_control_message`
- does not set `set_stop_flag` / `stop_safe`
- does not change pending actions, active-run guards, planner input, executor dispatch, or run state
- can affect a live run only through a separate explicit audited `side_chat_action_request`
- does not expose hidden chain-of-thought or secrets

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
- messages may include context-agent replies. Full LLM-backed tool use and automatic run steering are still out of scope.

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

### `get_runtime_config` / `set_runtime_config`

Purpose:

- inspect or apply selected runtime configuration updates

Payload:

```json
{
  "verbosity": "verbose",
  "permission_profile": "autonomous",
  "permissions": {
    "ask_before_planner_direction_changes": true,
    "ask_before_executor_steering": false
  },
  "timeouts": {
    "planner_request_timeout": "2m",
    "executor_idle_timeout": "unlimited",
    "executor_turn_timeout": null,
    "subagent_timeout": "unlimited",
    "shell_command_timeout": "30m",
    "install_timeout": "2h",
    "human_wait_timeout": "unlimited"
  },
  "updates": {
    "update_channel": "stable",
    "auto_check_updates": true,
    "auto_download_updates": false,
    "auto_install_updates": false,
    "ask_before_update": true,
    "include_prereleases": false,
    "update_check_interval": "24h"
  }
}
```

Timeout fields accept finite Go-style durations such as `30m` or `2h`, plus `unlimited`, `no-limit`, `none`, or JSON `null` for no limit. `executor_turn_timeout` and `human_wait_timeout` default to unlimited.

Config updates are persisted immediately. They apply to future operations immediately. In-flight transports use them only where technically possible; otherwise the response/status should say they apply at the next operation or safe boundary.

Permission profiles are mechanical policy labels and toggles:

- `guided`
- `balanced`
- `autonomous`
- `full_send`

They do not grant the GUI semantic authority; planner/executor direction still flows through the existing planner/executor/control protocol boundaries.

`gpt-5-latest` is the orchestrator's planner-model alias. Live OpenAI calls resolve it through the Models API to the newest available mainline versioned GPT-5 model visible to the account. Exact model ids still pin behavior. The `test_planner_model` action then verifies the resolved/requested model with a tiny Responses API call. If discovery or verification fails, the engine reports the failure; it does not silently fall back to an unverified model.

### Update actions

Actions:

- `check_for_updates`
- `get_update_status`
- `get_update_changelog`
- `skip_update`
- `install_update`

Update checks use GitHub Releases. The response includes current version/revision where available, latest version, release URL, changelog text, update availability, channel, and install support state.

`install_update` is currently a truthful foundation action: it reports unsupported/not-yet-implemented unless a safe signed/checksummed Windows asset and staged install path are available. It must not pretend to replace a running executable.

### Side Chat actions

Actions:

- `send_side_chat_message`
- `list_side_chat_messages`
- `side_chat_context_snapshot`
- `side_chat_action_request`

Side Chat messages are persisted raw and answered by a context-agent foundation that reads observable runtime context only. A normal `send_side_chat_message` never changes planner/executor flow.

`side_chat_context_snapshot` returns the same observable status surface the Side Chat agent may use: current repo/run, planner/executor summaries, pending actions, workers, timeout settings, permission mode, update/model health, recent side-chat messages, and recent events. It must not include hidden chain-of-thought or secrets.

`side_chat_action_request` is the only Side Chat escalation path. Supported actions in this slice are:

- `request_latest_status` / `context_snapshot`
- `queue_planner_note`
- `ask_planner_question`
- `ask_planner_reconsider`
- `inject_control_message`
- `safe_stop` / `request_safe_stop`
- `clear_safe_stop`

Planner-direction actions respect the permission profile. If `ask_before_planner_direction_changes` is true and `approved` is false, the action is persisted as `approval_required` and no control message is queued. Approved planner notes are queued raw through the existing control-message path and are delivered only at the next safe planner boundary. Safe Stop writes the same safe-stop flag as `stop_safe`.

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
- `build_time` with `Total Build Time`, current run time, current step label, and current step time
- `timeouts` with the current runtime timeout values and unlimited state
- `permissions` with the active autonomy profile/toggles
- `update_status` with the current GitHub Releases update-check state
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
  "backend": {
    "pid": 1234,
    "started_at": "2026-04-24T14:00:00Z",
    "binary_path": "D:\\Projects\\agentic_loop\\dist\\orchestrator.exe",
    "binary_modified_at": "2026-04-24T13:59:00Z",
    "binary_version": "v1.0.1-dev",
    "binary_revision": "abc123",
    "binary_build_time": "2026-04-24T13:59:00Z",
    "repo_root": "D:\\Projects\\target_repo",
    "control_address": "http://127.0.0.1:44777",
    "owner": "orchestrator-v2-dogfood",
    "owner_session_id": "session_abc",
    "owner_metadata_path": "D:\\Projects\\target_repo\\.orchestrator\\state\\dogfood-backend.json",
    "stale": false,
    "stale_reason": ""
  },
  "active_run_guard": {
    "present": true,
    "run_id": "run_123",
    "action": "continue_run",
    "status": "active",
    "backend_pid": 1234,
    "backend_started_at": "2026-04-24T14:00:00Z",
    "session_id": "session_abc",
    "currently_processing": true,
    "waiting_at_safe_point": false,
    "last_progress_at": "2026-04-24T14:03:00Z",
    "current_backend": true,
    "stale": false,
    "stale_reason": "",
    "message": "a control-server-launched run loop is currently active"
  },
  "stop_flag": {
    "present": false,
    "path": "D:\\Projects\\target_repo\\.orchestrator\\state\\auto.stop",
    "applies_at": "next_safe_point",
    "reason": ""
  },
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
    "executor_thread_id": "",
    "executor_turn_id": "",
    "executor_turn_status": "",
    "latest_checkpoint": {
      "sequence": 3,
      "stage": "planner",
      "label": "planner_turn_completed",
      "safe_pause": true
    },
    "latest_planner_outcome": "execute",
    "next_operator_action": "continue_existing_run",
    "activity_state": "ready_to_dispatch",
    "activity_message": "Planner selected the next code task. Executor has not started yet. Click Continue Build to dispatch it.",
    "actively_processing": false,
    "waiting_at_safe_point": true,
    "execute_ready": true
  },
  "model_health": {
    "planner": {
      "configured_model": "gpt-5-latest",
      "requested_model": "gpt-5-latest",
      "verification_state": "not_verified"
    },
    "executor": {
      "configured_model": "gpt-5.5",
      "requested_model": "gpt-5.5",
      "verification_state": "not_verified",
      "access_mode": "danger-full-access sandbox, approval never",
      "effort": "xhigh",
      "codex_executable_path": "C:\\Users\\me\\AppData\\Roaming\\npm\\codex.cmd",
      "codex_version": "codex-cli 0.124.0",
      "codex_config_source": "C:\\Users\\me\\.codex\\config.toml",
      "codex_model_verified": false,
      "codex_permission_mode_verified": false
    },
    "needs_attention": true,
    "blocking": false,
    "message": "Model or Codex configuration needs attention before unattended operation."
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
  "ask_human": {
    "present": true,
    "run_id": "run_123",
    "question": "Has the Codex model/access issue been fixed?",
    "blocker": "Codex was blocked by a model/access issue.",
    "action_summary": "waiting for human confirmation",
    "planner_outcome": "ask_human",
    "response_id": "resp_123",
    "source": "human.question.presented",
    "updated_at": "2026-04-24T14:14:32Z",
    "message": "Type a raw answer; the shell queues it through inject_control_message and resumes with continue_run."
  },
  "pending_action": {},
  "workers": [],
  "approvals": [],
  "artifacts": []
}
```

Loop-state fields are mechanical, not semantic project judgments:

- `actively_processing=true` means the control server is currently advancing a run loop or an executor/planner turn is active.
- `waiting_at_safe_point=true` means the run is paused at a durable safe checkpoint and no executor turn is active.
- `execute_ready=true` means the latest planner outcome is `execute`, the checkpoint is a planner safe pause, and Codex has not started the executor turn yet. The GUI should show `Ready to Continue` / `Continue Build / Dispatch Executor`, not `Running`.
- Active-run guard presence alone must not be rendered as `Running`; consumers should use `active_run_guard.currently_processing`.

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
5. `set_runtime_config` applied at safe points
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
