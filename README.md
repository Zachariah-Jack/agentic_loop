# Orchestrator

Orchestrator is a Windows-friendly Go CLI for a planner-led app-building workflow.

- Planner: decision-maker. The live planner transport is the OpenAI Responses API.
- Executor: code worker. The primary executor transport is `codex app-server`.
- CLI: inert bridge and runtime harness. It persists state, routes structured actions, manages transport, and shows operator-visible status.

The CLI does not decide that work is complete, does not rewrite human replies, and does not invent workflow policy. Safe pause points happen only after AI turns have completed and been durably checkpointed.

## Architecture Summary

- The planner chooses what happens next.
- The executor performs repo work and returns results as data.
- The CLI validates structured planner output, routes explicit actions, persists state, and renders status.
- Human replies are forwarded raw.
- Mechanical stop reasons are persisted so the operator can see why a command stopped without the CLI making semantic judgments.

Key architecture and operator docs:

- [docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md](docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md)
- [docs/ORCHESTRATOR_NON_NEGOTIABLES.md](docs/ORCHESTRATOR_NON_NEGOTIABLES.md)
- [docs/ORCHESTRATOR_FULL_PRODUCT_ROADMAP.md](docs/ORCHESTRATOR_FULL_PRODUCT_ROADMAP.md)
- [docs/ORCHESTRATOR_V2_CONSOLE_SPEC.md](docs/ORCHESTRATOR_V2_CONSOLE_SPEC.md)
- [docs/ORCHESTRATOR_V2_CONTROL_PROTOCOL.md](docs/ORCHESTRATOR_V2_CONTROL_PROTOCOL.md)
- [docs/ORCHESTRATOR_V2_PLANNER_CONTRACT.md](docs/ORCHESTRATOR_V2_PLANNER_CONTRACT.md)
- [docs/ORCHESTRATOR_V2_ROADMAP.md](docs/ORCHESTRATOR_V2_ROADMAP.md)
- [docs/PRIME_TIME_V1_READINESS.md](docs/PRIME_TIME_V1_READINESS.md)
- [docs/REAL_APP_WORKFLOW.md](docs/REAL_APP_WORKFLOW.md)
- [docs/ARTIFACT_PLACEMENT_POLICY.md](docs/ARTIFACT_PLACEMENT_POLICY.md)
- [docs/WINDOWS_INSTALL_AND_RELEASE.md](docs/WINDOWS_INSTALL_AND_RELEASE.md)

## V2 Preview

V2 console work is planned and specified, and there is now a first minimal desktop GUI shell on top of the real local engine protocol. It is an intentionally small proof shell, not the final console.

The intended V2 direction is:

- an optional Windows-friendly Operator Console
- Control Chat for safe-point live run intervention
- Side Chat for non-interfering discussion
- terminal tabs, file/artifact explorer, and contract-file editing
- planner-safe operator messages and progress display
- explicit local engine protocol between console and engine

What exists now in the engine:

- a public loopback control server via `orchestrator control serve`
- a minimal `orchestrator control demo ...` client that uses the real local protocol for status snapshots, pending action inspection, event streaming, control-message injection, verbosity changes, and safe-stop flag control
- a local control-protocol skeleton for status snapshots, runtime config, safe-stop flag, pending-action retrieval, and control-message injection
- an in-process event bus and event-stream skeleton for future console attachment
- safe-point runtime verbosity reload (`quiet`, `normal`, `verbose`, `trace`) without restarting the process
- a durable control-message queue and safe-point intervention path that feeds raw operator messages plus pending action context back into the planner
- planner-safe operator-status/progress fields now live additively on the current `planner.v1` runtime path, while `planner.v2` remains the future stricter contract
- a truthful side-chat protocol-backed stub that records side-chat messages without affecting the active run and clearly reports that the backend is not implemented yet

What now exists in the first desktop shell:

- a minimal optional Electron shell under `console/v2-shell/`
- a guided Home dashboard that answers "am I connected, which repo is loaded, is the loop running, does it need me, what happened, and what should I click next"
- a top always-visible status strip with plain connection labels (`Ready`, `Not Connected`, `Connecting`, `Reconnecting`), connected/connecting timers, repo root, run id, loop status, blocker, verbosity, update, and connect controls
- real tab navigation for Home, Run, Action Required, Chat, Files, Live Output, Workers, Terminal, Settings, and Dogfood Notes instead of one giant scroll page
- a simplified Run Control area with protocol-backed Start Build / Continue Build actions; terminal commands are hidden behind backup help
- run elapsed-time visibility in Home, Status, What Happened, and CLI/status output when timestamps are available
- a connection/settings pane for the local control server address
- a progress and roadmap-alignment pane that shows planner-safe operator message, visual progress percent, progress confidence, progress basis, current focus, next intended step, and surfaced roadmap context when available
- a status pane for latest run id, goal, elapsed time, stop reason, completion state, current verbosity, model health, executor failure details, and pending action summary
- a pending-action detail pane for planner outcome, held state, hold reason, dispatch target, executor prompt summary, and full pending executor prompt text when available
- a run-summary pane for a compact "what changed while I was gone" view derived from real status and event data
- an artifact browser pane backed by real artifact protocol actions, including a raw text/JSON viewer
- a contract-file editor pane for `.orchestrator/brief.md`, `.orchestrator/roadmap.md`, `.orchestrator/decisions.md`, `.orchestrator/human-notes.md`, and `AGENTS.md`
- an AI autofill wizard pane that drafts selected canonical contract files through the real engine protocol and previews them before any save
- a read-only repo browser pane that lists one directory at a time and opens repo files through explicit protocol actions
- embedded operator terminal tabs for local shell sessions, kept separate from engine run authority
- an Action Required pane that surfaces primary executor approval-required state in plain English and can approve or deny through explicit protocol actions
- a Codex/model readiness card that truthfully shows planner model health, executor/Codex model health, unavailable-model errors, access-mode facts the engine can detect, and `Not verified` when the protocol cannot prove details
- a Side Chat pane skeleton that records non-interfering side-chat messages through the real protocol and shows the truthful backend-unavailable response
- a Dogfood Notes pane that records quick timestamped friction notes tied to the repo and latest run when available
- a Worker Panel that shows real worker ids, statuses, scopes, worktree paths, approval state, executor thread/turn metadata, and explicit operator-triggered worker actions for create, dispatch, remove, and integration preview
- a Live Output pane backed by `/v2/events`, with readable planner/executor/worker/approval/model rows, category/current-run/text filtering, verbosity-aware detail, and trace-only raw payloads by default
- a Control Chat pane backed by real `inject_control_message`
- a controls pane for Update Dashboard, Reload Outputs, safe stop, clear stop, and immediate verbosity changes that immediately affect Live Output
- persisted local shell state for practical dogfooding, including the last control-server address, auto-reconnect preference, last selected artifact/contract/repo file/worker/dogfood note, side-chat context policy, and activity filters
- auto-reconnect and rehydration behavior so the shell can reattach to a restarted control server, resume the current status snapshot, reload pending context, workers, artifacts, dogfood notes, contract selections, and continue the live event stream with recent-history replay where available
- a `scripts/start-v2-dogfood.ps1` helper that launches the owned control server hidden by default, opens the Electron shell, writes logs under `.orchestrator/logs`, and stops the owned control server when the shell exits
- a clickable `scripts/Launch-Orchestrator-V2-Shell.vbs` launcher for a normal app-like Windows start with no extra console window in the default path

What is still intentionally missing from the desktop shell:

- full side-chat conversational backend
- worker apply controls
- worker-specific approval controls in the shell
- packaged GUI installer polish

What is solid enough to dogfood now:

- local control server + event stream
- reconnectable shell attachment
- planner-safe status/progress visibility
- Control Chat injection
- artifact browsing
- canonical contract editing
- repo browsing
- worker visibility and basic worker actions
- AI autofill draft flow
- dogfood issue capture for daily-use friction

What is still experimental:

- Side Chat remains recorded-only
- terminal sessions are local operator utilities and are not persisted across shell restarts
- Action Required UI is still focused on the primary executor path for approve/deny; worker-specific approval controls remain deferred
- Dogfood Notes are local repo-scoped capture only in this slice; there is no built-in GitHub issue export flow yet
- the shell is still an Electron proof shell rather than the final polished console

Recommended daily dogfood workflow:

1. From this repo, launch the local control server and shell together:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-v2-dogfood.ps1 -RepoPath D:\Projects\target-repo
```

2. For a double-click style launch, run `scripts\Launch-Orchestrator-V2-Shell.vbs` or create a shortcut to it and pin that shortcut to the taskbar.
3. Use `-DebugVisibleWindows` only when you intentionally want visible backend PowerShell windows for debugging.
4. Leave auto-reconnect enabled in the shell for routine control-server restarts.
5. Use the shell for status, Control Chat, artifacts, contract editing, workers, and approvals.
6. Start on the guided Home dashboard: confirm the top strip says `Connection Status: Ready`, check the Current Repo card for the repo root, then follow the "What should I do now?" card.
7. If the Home dashboard recommends starting or continuing a run, use Start Build / Continue Build; they call explicit `start_run` / `continue_run` protocol actions and return immediately while the control-server process runs the foreground loop.
8. Capture friction and bugs in the Dogfood Notes pane while they are fresh; notes stay timestamped and tied to the repo/run context.
9. Keep the headless CLI available for direct `run`, `continue`, `status`, and `doctor` use when you do not need the GUI.
10. If the shell restarts, reconnect to the same control server and let it rehydrate current status, pending action, workers, artifacts, side-chat history, dogfood notes, and recent activity.

V2 will preserve the current architecture:

- planner remains the decision-maker
- executor remains the code worker
- engine/CLI remains inert
- headless CLI continues to work without the console

See:

- [docs/ORCHESTRATOR_V2_CONSOLE_SPEC.md](docs/ORCHESTRATOR_V2_CONSOLE_SPEC.md)
- [docs/ORCHESTRATOR_V2_CONTROL_PROTOCOL.md](docs/ORCHESTRATOR_V2_CONTROL_PROTOCOL.md)
- [docs/ORCHESTRATOR_V2_PLANNER_CONTRACT.md](docs/ORCHESTRATOR_V2_PLANNER_CONTRACT.md)
- [docs/ORCHESTRATOR_V2_ROADMAP.md](docs/ORCHESTRATOR_V2_ROADMAP.md)

## Global Usage

```text
orchestrator [--config PATH] <command> [args]
```

Global behavior:

- `--config PATH` overrides the default config file location for any command.
- `orchestrator --help` shows the top-level command surface.
- `orchestrator help <command>` shows top-level command help.
- Subcommands with flags also support `--help`, such as `orchestrator workers create --help`.

Typical flow:

```text
setup -> init -> run -> continue/status/history/doctor
```

## Complete Command Reference

### Bootstrap And Readiness

#### `control serve [--addr HOST:PORT]`

- Does: starts the local loopback V2 control endpoint and NDJSON event stream for future console attachment.
- Use it when: you want an external local client to inspect status, subscribe to engine events, update runtime verbosity, inspect pending actions, or inject control messages.
- Important flags: `--addr HOST:PORT` with default `127.0.0.1:44777`.
- Important behavior: it prints the listen URL plus `/v2/control` and `/v2/events` endpoints.
- Does not: start a GUI, change run semantics, or provide a full side-chat backend.

#### `control demo <status|pending|inject|set-verbosity|stop-safe|clear-stop|events>`

- Does: acts as a small operator-facing demo client that talks to the real local V2 control protocol over HTTP.
- Use it when: you want to prove the engine protocol locally before any desktop console exists.
- Important inputs: start `orchestrator control serve` first, then point the demo client at it with `--addr HOST:PORT` if needed.
- Important behavior: `status` prints the live status snapshot, `pending` prints the current pending action buffer, `inject` queues a control message, `events` streams NDJSON-backed engine events, `set-verbosity` updates runtime verbosity, and `stop-safe` / `clear-stop` manage the safe-stop flag.
- Does not: bypass the engine, attach a GUI shell, or provide a real side-chat conversational backend.

#### `setup [--yes]`

- Does: loads existing operator config, prompts for planner model, drift watcher enablement, optional `ntfy` settings, and repo-contract confirmation, then writes config durably.
- Use it when: setting up a machine for the first time or refreshing saved operator config.
- Important flags: `--yes` keeps current values or defaults where possible and writes without prompting.
- Does not: store `OPENAI_API_KEY`; that remains environment-only.

#### `init`

- Does: scaffolds the target-repo contract and runtime directories under `.orchestrator/`, preserves existing human-authored files, and ensures persistence exists.
- Use it when: preparing a fresh target repo before the first real `run`, `resume`, `continue`, or `auto`.
- Important outputs: `.orchestrator/brief.md`, `.orchestrator/roadmap.md`, `.orchestrator/decisions.md`, `.orchestrator/human-notes.md`, `.orchestrator/state/`, `.orchestrator/logs/`, `.orchestrator/artifacts/`, and `AGENTS.md` if missing.
- Does not: fill in the product brief or decisions for you.

#### `doctor`

- Does: prints grouped mechanical health checks for runtime, target-repo contract, config, plugins, planner readiness, executor readiness, workers, `ntfy`, and persistence.
- Use it when: checking whether the current machine and repo are ready before a live run.
- Important behavior: it distinguishes install/runtime health from target-repo contract health.
- Does not: run a live planner turn, run Codex work, or execute a full end-to-end smoke test.

#### `version`

- Does: prints binary version, revision, and build time metadata.
- Use it when: confirming which build you installed or packaged.
- Important inputs: none.
- Does not: inspect repo or runtime state.

### Core Run Control

#### `run --goal TEXT [--bounded]`

- Does: creates a new durable run and, by default, keeps advancing it automatically in the foreground through repeated bounded cycles until a real mechanical stop boundary is hit.
- Use it when: starting a brand-new goal in the current target repo and you want unattended foreground progress by default.
- Important flags: `--goal TEXT` is required; `--bounded` forces exactly one bounded cycle instead of unattended foreground progress.
- Does not: run in the background or bypass the bounded-cycle core.

#### `resume`

- Does: loads the latest unfinished run, executes one bounded continuation cycle on that same run, persists results, and stops again at the next cycle boundary.
- Use it when: you want one more bounded planner-led cycle on the latest unfinished run.
- Important inputs: no flags; it always targets the latest unfinished run.
- Does not: create a new run.

#### `continue [--max-cycles N]`

- Does: loads the latest unfinished run and, by default, keeps advancing it automatically in the foreground through repeated bounded cycles until a real mechanical stop boundary is hit.
- Use it when: you want unattended foreground progress on the latest unfinished run.
- Important flags: `--max-cycles N` keeps the invocation explicitly bounded when you want stepping instead of unattended continuation.
- Does not: create a new run, run in the background, or make semantic prioritization choices.

#### `auto start --goal TEXT`

- Does: creates a new durable run, then keeps advancing that same run automatically in the foreground by repeatedly invoking the existing bounded-cycle core.
- Use it when: you want one foreground process to keep moving a brand-new run until a mechanical stop boundary is hit.
- Important flags: `--goal TEXT` is required.
- Important behavior: it stops only at cycle boundaries, and you can request a clean foreground stop by creating `.orchestrator/state/auto.stop`.
- Does not: run in the background or bypass the bounded-cycle implementation.

#### `auto continue`

- Does: loads the latest unfinished run and keeps advancing it automatically in the foreground through repeated bounded cycles.
- Use it when: an unfinished run already exists and you want foreground autonomous continuation without creating a new run.
- Important behavior: it uses the same `.orchestrator/state/auto.stop` flag for a clean operator stop at the next cycle boundary.
- Does not: create a new run or daemonize itself.

#### `status`

- Does: shows the latest durable run summary, latest stable checkpoint, stop reason, next operator action, runtime readiness, worker summary, plugin summary, and integration/apply references when present.
- Use it when: you want the current durable snapshot for the latest run and the current runtime environment.
- Important behavior: output shape follows the saved config verbosity (`quiet`, `normal`, `verbose`, `trace`).
- Does not: replay logs or infer hidden state from terminal scrollback.

#### `history [--limit N]`

- Does: prints recent runs in reverse chronological order from persisted SQLite state in a compact format.
- Use it when: you want a short list of recent durable runs rather than the full latest-run snapshot.
- Important flags: `--limit N` with default `10`.
- Does not: dump full logs or artifact contents.

### Executor Commands

#### `executor-probe --prompt TEXT`

- Does: performs one real read-only executor probe turn through `codex app-server`, persists executor transport metadata, journals the result, and prints a structured summary.
- Use it when: checking the primary executor transport without starting a planner-led workflow.
- Important flags: `--prompt TEXT` is required.
- Does not: run the planner or continue a run loop.

#### `executor approve`

- Does: loads the latest unfinished run, targets the persisted active primary executor turn, and sends an approval response mechanically.
- Use it when: the main run-level executor turn is waiting on approval.
- Important inputs: no flags; it always targets the latest unfinished run's active primary executor turn.
- Does not: decide whether approval should be granted semantically.

#### `executor deny`

- Does: sends a denial response to the active primary executor turn and persists that denial as data.
- Use it when: the main run-level executor turn is waiting on approval and you want to deny the request.
- Important inputs: no flags.
- Does not: mark the run complete or failed semantically.

#### `executor interrupt`

- Does: requests a graceful interrupt on the active primary executor turn if the transport supports it.
- Use it when: the active primary executor turn should stop cleanly.
- Important inputs: no flags.
- Does not: invent a semantic conclusion about the run.

#### `executor kill`

- Does: records a force-kill request mechanically for the active primary executor turn.
- Use it when: you want to request a hard stop and understand that the current app-server path may not support it.
- Important behavior: the current `codex app-server` primary transport reports force kill as unsupported.
- Does not: fake a kill result when the transport cannot do it.

#### `executor steer TEXT`

- Does: sends one raw steer note to the active primary executor turn and persists the raw note and control action.
- Use it when: the active primary executor turn is steerable and you want to inject a raw operator note.
- Important arguments: the steer note is the trailing raw text argument.
- Does not: rewrite or reinterpret the note.

### Worker Commands

Workers always use isolated workspaces. They do not share the main target repo working tree for concurrent writes.

#### `workers create --name TEXT --scope TEXT [--run-id ID]`

- Does: creates a durable worker record and an isolated worker worktree for a run.
- Use it when: you want a named isolated worker workspace attached to the latest unfinished run or a specific resumable run.
- Important flags: `--name`, `--scope`, optional `--run-id`.
- Does not: dispatch executor work by itself.

#### `workers list [--run-id ID]`

- Does: lists durable workers, including status, approval state, thread/turn ids, steerable/interruptible flags, and last control action when present.
- Use it when: inspecting worker state across the repo or for a specific run.
- Important flags: optional `--run-id`.
- Does not: change worker state.

#### `workers remove --worker-id ID`

- Does: removes a worker registry entry and deletes its isolated worktree when the worker is not active.
- Use it when: cleaning up an idle or completed worker.
- Important flags: `--worker-id ID`.
- Does not: remove an active worker turn.

#### `workers dispatch --worker-id ID --prompt TEXT`

- Does: routes one primary executor turn into the selected worker's isolated workspace and persists the resulting worker executor state.
- Use it when: you want to manually send executor work to a specific worker workspace.
- Important flags: `--worker-id`, `--prompt`.
- Does not: write into the main repo working tree or merge the worker output automatically.

#### `workers integrate --worker-ids ID,ID,...`

- Does: gathers outputs from selected workers, builds a read-only integration preview artifact, and prints the integration artifact path and summary.
- Use it when: you want an inspectable integration preview without merging code.
- Important flags: `--worker-ids` as a comma-separated list.
- Does not: apply or merge worker outputs into the main repo.

#### `workers approve --worker-id ID`

- Does: sends approval for the selected worker's active approval-required executor turn and updates that worker's durable approval state.
- Use it when: one specific worker is waiting on approval.
- Important flags: `--worker-id ID`.
- Does not: approve sibling workers or make semantic decisions.

#### `workers deny --worker-id ID`

- Does: sends denial for the selected worker's active approval-required executor turn and persists denial as data.
- Use it when: one specific worker is waiting on approval and should be denied.
- Important flags: `--worker-id ID`.
- Does not: mark the worker plan complete or failed semantically.

#### `workers interrupt --worker-id ID`

- Does: requests a graceful interrupt for the selected worker's active executor turn.
- Use it when: you want one worker to stop without affecting unrelated workers.
- Important flags: `--worker-id ID`.
- Does not: interrupt the main run-level executor turn or other workers.

#### `workers kill --worker-id ID`

- Does: records a kill request for the selected worker turn mechanically.
- Use it when: you want to request a hard stop for one worker and accept the current transport limits.
- Important behavior: the current app-server path reports kill as unsupported rather than inventing a result.
- Does not: fake a successful force kill.

#### `workers steer --worker-id ID --message TEXT`

- Does: sends one raw steer note to the selected worker's active executor turn and persists the raw note.
- Use it when: a specific worker turn is steerable and needs a raw operator note.
- Important flags: `--worker-id`, `--message`.
- Does not: rewrite the note or steer the main run-level executor turn.

## Practical Workflows

### First-Time Setup

1. Set `OPENAI_API_KEY` in your environment.
2. Run:

```powershell
orchestrator setup
orchestrator doctor
orchestrator version
```

3. Confirm:
   - planner API key is present
   - the saved planner model is what you want; the default `gpt-5-latest` resolves through the OpenAI Models API to the newest available mainline `gpt-5.x` model when discovery succeeds, and it does not silently fall back to an unverified model if discovery fails
   - optional `ntfy` values are set correctly
   - Codex app-server is ready

### Target Repo Initialization

From the target repo root:

```powershell
orchestrator init
```

Then fill in:

- `.orchestrator/brief.md`
- `.orchestrator/roadmap.md`
- `.orchestrator/decisions.md`

Use `.orchestrator/human-notes.md` for append-only extra context.

### Bounded Run Workflow

Use this when you want explicit cycle boundaries:

```powershell
orchestrator run --goal "Build the first settings page" --bounded
orchestrator status
orchestrator resume
orchestrator continue --max-cycles 3
orchestrator history
```

Key points:

- `run --bounded` creates a new run and executes one bounded cycle.
- `resume` does one more bounded cycle on the latest unfinished run.
- `continue` repeats bounded cycles up to `--max-cycles`.
- `status` shows the latest stable checkpoint and next operator action.

### Autonomous Foreground Workflow

Use this when you want one foreground process to keep advancing the same run:

```powershell
orchestrator run --goal "Ship the first dashboard milestone"
orchestrator continue
```

Key points:

- `run` and `continue` now default to this foreground unattended behavior.
- `auto start` and `auto continue` remain available as explicit aliases for the same style of foreground loop.
- The loop still uses the existing bounded-cycle core internally.
- It still stops only at cycle boundaries.
- To stop after the current bounded cycle, create:

```text
.orchestrator/state/auto.stop
```

### `ask_human` Workflow

When the planner returns `ask_human`:

- the exact planner question is shown to the operator
- if `ntfy` is configured, the CLI publishes the question and waits for one inbound reply
- if `ntfy` is unavailable or not configured, the CLI falls back to terminal input
- the raw human reply is persisted unchanged
- one next planner turn runs using that raw reply as data

Mode differences:

- `run`, `resume`, and `continue` still stop after that bounded cycle.
- `auto` continues automatically after the reply and post-reply planner turn, unless another mechanical stop boundary is hit.

### Worker Workflow

Manual worker flow:

```powershell
orchestrator workers create --name api --scope "API endpoints"
orchestrator workers dispatch --worker-id WORKER_ID --prompt "Implement the health endpoint"
orchestrator workers list
orchestrator workers steer --worker-id WORKER_ID --message "Keep changes limited to the API package"
```

Planner-owned worker flow:

- planner worker plans can create isolated workers
- worker executor turns can run concurrently up to the saved worker concurrency limit
- results are persisted per worker
- workers never write blindly into the main repo tree

Current defaults:

- saved `worker_concurrency_limit` defaults to `2`
- worker worktrees are created in a sibling directory named like `<repo>.workers`

### Integration And Apply Workflow

Manual integration preview:

```powershell
orchestrator workers integrate --worker-ids WORKER_A,WORKER_B
```

What that does now:

- reads selected worker outputs
- builds a structured integration preview artifact
- reports conflict candidates and summary data

What it does not do:

- no automatic merge
- no semantic conflict resolution
- no write into the main repo

Apply behavior today:

- safe apply is planner-owned, not a manual CLI command
- the planner can request safe apply modes using the existing integration/apply foundations
- currently supported mechanical apply modes are `abort_if_conflicts` and `apply_non_conflicting`

### Packaging And Release Workflow

Portable build:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-release.ps1
```

Installer build:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-installer.ps1
```

Use:

- [docs/WINDOWS_INSTALL_AND_RELEASE.md](docs/WINDOWS_INSTALL_AND_RELEASE.md)
- [docs/PRIME_TIME_V1_READINESS.md](docs/PRIME_TIME_V1_READINESS.md)

## State, Artifacts, Config, And Output

### Config

- Default config path on Windows: `%AppData%\orchestrator\config.json`
- Override it per command with `--config PATH`
- Saved config includes current operator settings such as planner model, verbosity, worker concurrency limit, drift watcher enablement, repo contract confirmation, and optional `ntfy` settings
- The default planner model is `gpt-5-latest`, an orchestrator alias that dynamically resolves through OpenAI model discovery to the newest available mainline GPT-5 model the account can see; set an exact model id in config or `OPENAI_MODEL` when you need pinned behavior, and use the model-test action to verify availability
- `setup` manages planner model, drift watcher enablement, repo contract confirmation, and `ntfy` settings
- `OPENAI_API_KEY` remains environment-only

### Target Repo Runtime State

Inside the target repo:

- SQLite state: `.orchestrator/state/orchestrator.db`
- JSONL journal: `.orchestrator/logs/events.jsonl`
- Orchestration artifacts: `.orchestrator/artifacts/`

Worker runtime isolation:

- isolated worker worktrees live in a sibling directory named like `<repo>.workers`
- they do not share the main target repo working tree for concurrent writes

### Artifact Placement

Orchestration-owned generated files go under `.orchestrator/artifacts/`, including:

- `.orchestrator/artifacts/planner/`
- `.orchestrator/artifacts/executor/`
- `.orchestrator/artifacts/context/`
- `.orchestrator/artifacts/human/`
- `.orchestrator/artifacts/reports/`
- `.orchestrator/artifacts/integration/`
- `.orchestrator/artifacts/reviews/`

Actual app code, tests, docs, and assets may still be written in the target repo when the planner requests that work and the executor write scope allows it.

### How `stop_reason` Works

`stop_reason` is mechanical, not semantic. It tells you why the current invocation stopped.

Common values include:

- `planner_complete`
- `planner_ask_human`
- `planner_pause`
- `planner_collect_context`
- `planner_execute`
- `max_cycles_reached`
- `operator_stop_requested`
- `executor_approval_required`
- `planner_validation_failed`
- `missing_required_config`
- `executor_failed`
- `transport_or_process_error`

Related note:

- `ntfy_failed_terminal_fallback_used` can appear in `ntfy` fallback output and journal events when remote ask-human delivery fails and terminal fallback is used; the run can continue after fallback.

### How To Read `status` And `history`

- `status` is the latest durable snapshot for one repo: runtime readiness, latest run summary, stable checkpoint, stop reason, human/executor state, worker summary, and artifact references.
- `history` is the compact reverse-chronological list of recent runs from SQLite.
- `next_operator_action` is mechanical only. It describes the next allowed operator move from durable state, not what the planner semantically "wants."

Common `next_operator_action` values:

- `fill_repo_contract`
- `initialize_target_repo`
- `resume_existing_run`
- `continue_existing_run`
- `answer_human_question`
- `approve_or_deny_executor_request`
- `inspect_status`
- `inspect_history`
- `run_new_goal`
- `no_action_required_run_completed`

### Control Protocol And Live Intervention

- `orchestrator control serve` exposes the current local engine protocol on loopback HTTP.
- `orchestrator control demo ...` is the first operator-facing proof client for that protocol.
- `POST /v2/control` supports the implemented subset of actions in this slice:
  - `get_status_snapshot`
  - `test_planner_model`
  - `test_executor_model` / `test_codex_config`
  - `approve_executor`
  - `deny_executor`
  - `set_verbosity`
  - `set_stop_flag`
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
- `GET /v2/events` streams NDJSON engine events.
- `get_status_snapshot` now surfaces live planner operator-status fields, run elapsed-time fields, and model-health fields when available.
- `test_planner_model` verifies the configured planner model or `gpt-5-latest` alias through OpenAI model discovery. `test_executor_model` truthfully checks what the engine can detect about Codex launch/config and reports model-unavailable errors from the latest executor failure; it does not silently downgrade or claim unverified Codex model access.
- Pending actions are now durable engine state. When available, they describe what the engine is about to do next at a safe boundary.
- The desktop proof shell now also supports:
  - a tabbed guided Home dashboard with plain Connection Status and Loop Status indicators
  - pending-action detail viewing
  - a plain-English "What Happened?" summary with translated stop reasons and recommended next actions
  - a Live Output pane whose visible detail follows `quiet`, `normal`, `verbose`, and `trace`
  - protocol-backed artifact browsing for current-run artifacts
  - protocol-backed editing of the canonical contract files only
  - embedded operator terminal tabs for local shell convenience
  - an Action Required card with protocol-backed primary executor approve and deny buttons when approval is required
  - a non-interfering Side Chat pane backed by recorded protocol messages only
  - a Dogfood Notes pane backed by timestamped repo/run-scoped issue capture through the same protocol
  - a richer progress and roadmap-alignment panel driven by planner-safe operator status plus surfaced roadmap context
  - Worker Panel controls for explicit create, dispatch, remove, and integration-preview actions through the engine protocol
- Control messages are raw human intervention messages. At the next safe point, the engine holds the pending action, packages the raw message plus pending action into planner-visible input, and lets the planner decide whether to proceed, redirect, pause, ask the human, or do something else.
- The engine does not reinterpret or summarize the control message. The planner remains the decision-maker.

### First GUI Shell Dev Flow

1. Start the engine protocol server:

```powershell
orchestrator control serve
```

2. In a separate terminal, start the early shell:

```powershell
cd console\v2-shell
npm install
npm run dev
```

Notes:

- the shell talks only to the real loopback control server and event stream
- default control server address is `http://127.0.0.1:44777`
- the shell is still a proof console, but the Home dashboard is now the intended daily entry point
- the top strip uses plain labels: `Connection Status: Ready`, `Not Connected`, `Connecting`, or `Reconnecting`; `Loop Status: Running`, `Stopped`, `Needs You`, `Completed`, `No Run Yet`, or `Error`
- artifact browsing is limited to surfaced `.orchestrator/artifacts/...` paths known to the engine
- contract editing is limited to the canonical files only: `.orchestrator/brief.md`, `.orchestrator/roadmap.md`, `.orchestrator/decisions.md`, `.orchestrator/human-notes.md`, and `AGENTS.md`
- the AI autofill wizard drafts selected canonical contract files through the real engine protocol and previews the generated content before you save any file
- the repo browser is read-only for arbitrary repo files in this slice and lists one directory at a time through explicit protocol actions
- the terminal pane is an operator utility shell only; it supports multiple local tabs, but run control still belongs to the explicit engine protocol and control actions
- the Codex/model readiness card reports what the engine can actually verify; full access/model/effort details remain `Not verified` when the protocol cannot prove them, and unavailable-model errors are shown without silent fallback
- the Side Chat pane records non-interfering messages only in this slice; it does not alter the active run and does not yet have a real conversational backend
- the Dogfood Notes pane is a lightweight local capture path for friction and bug notes; it does not yet file GitHub issues automatically
- the Worker Panel can now create workers, dispatch one bounded worker turn, remove idle workers, and build integration previews
- the activity timeline is a filtered view over the real event stream plus shell-local terminal lifecycle events; it does not expose hidden reasoning
- the shell now remembers useful local session state and can auto-reconnect to the same control server for daily dogfooding
- on reconnect, the shell rehydrates current status, pending action context, recent artifacts, recent side-chat history, dogfood notes, workers, repo tree state, contract selection, and recent event backlog where the server still has buffered event history
- worker apply and worker-specific approval controls are still deferred from the shell

One-command local dogfood flow:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-v2-dogfood.ps1
```

The helper builds and launches the development binary at `dist\orchestrator.exe`. Portable release payloads still live under `dist\windows-amd64\portable\`.

In normal mode, the helper launches the owned control server hidden, opens only the Electron window, writes logs under `.orchestrator\logs`, requests safe stop when the shell exits, and then stops only the control-server process it launched. Use the debug flag below if you want visible backend consoles.

For a clickable Windows launch, use `scripts\Launch-Orchestrator-V2-Shell.vbs` or create a shortcut to that file and pin the shortcut. The VBS launcher starts the dogfood helper hidden so the user-facing experience is just the Electron app window.

Useful flags:

- `-RepoPath D:\Projects\target-repo`
- `-ControlAddr 127.0.0.1:44777`
- `-SkipBuild`
- `-SkipInstall`
- `-DebugVisibleWindows`

The helper also passes the chosen control-server address into the Electron shell bootstrap so the first connection target stays aligned if you use a non-default port.

## Known Limitations And Deferred Features

- The CLI is still inert by design; it does not make semantic workflow decisions.
- `auto` is foreground only. There is no background daemon or service mode in this slice.
- Hotkeys are not implemented.
- The runtime does not yet use `codex exec --json` as an automated executor fallback path.
- `executor kill` and `workers kill` are truthful mechanical requests, but the current `codex app-server` primary transport does not support true force kill.
- There is no manual `workers apply` command. Safe apply remains planner-owned.
- Conflict handling is mechanical only. There is no semantic merge resolution or "best worker" selection.
- Automatic merge into the main repo happens only when the planner explicitly requests an apply step, and only through the supported safe apply modes.
- Plugin loading exists, but there is no plugin management CLI.
- The first GUI shell exists, but it is still only a narrow protocol proof surface rather than the full V2 operator console.
- Side chat backend replies are not implemented yet; the protocol records side-chat messages and reports that truthfully.
- Arbitrary repo-file editing is not implemented in the shell; only the canonical contract files are saveable in this slice.
- The shell currently remembers local UI/session state, but it does not restore live terminal processes across restarts.
- Event backlog replay after reconnect is limited by the control server's in-memory event history window.
- Dogfood issue capture is currently local runtime state only; there is no built-in remote issue tracker export yet.

## Related Docs

- [docs/REAL_APP_WORKFLOW.md](docs/REAL_APP_WORKFLOW.md)
- [docs/PRIME_TIME_V1_READINESS.md](docs/PRIME_TIME_V1_READINESS.md)
- [docs/ARTIFACT_PLACEMENT_POLICY.md](docs/ARTIFACT_PLACEMENT_POLICY.md)
- [docs/WINDOWS_INSTALL_AND_RELEASE.md](docs/WINDOWS_INSTALL_AND_RELEASE.md)
