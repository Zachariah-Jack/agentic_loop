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
- [docs/PRIME_TIME_V1_READINESS.md](docs/PRIME_TIME_V1_READINESS.md)
- [docs/REAL_APP_WORKFLOW.md](docs/REAL_APP_WORKFLOW.md)
- [docs/ARTIFACT_PLACEMENT_POLICY.md](docs/ARTIFACT_PLACEMENT_POLICY.md)
- [docs/WINDOWS_INSTALL_AND_RELEASE.md](docs/WINDOWS_INSTALL_AND_RELEASE.md)

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
setup -> init -> run/auto start -> resume/continue/auto continue -> status/history/doctor
```

## Complete Command Reference

### Bootstrap And Readiness

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

#### `run --goal TEXT`

- Does: creates a new durable run, executes one bounded planner-led cycle, persists all results, and stops with a mechanical `stop_reason`.
- Use it when: starting a brand-new goal in the current target repo.
- Important flags: `--goal TEXT` is required.
- Does not: continue indefinitely; it runs one bounded cycle only.

#### `resume`

- Does: loads the latest unfinished run, executes one bounded continuation cycle on that same run, persists results, and stops again at the next cycle boundary.
- Use it when: you want one more bounded planner-led cycle on the latest unfinished run.
- Important inputs: no flags; it always targets the latest unfinished run.
- Does not: create a new run.

#### `continue [--max-cycles N]`

- Does: loads the latest unfinished run and executes repeated bounded cycles in the foreground until a mechanical stop boundary is hit.
- Use it when: you want more than one bounded cycle, but still want an explicit cycle limit.
- Important flags: `--max-cycles N` with default `3`.
- Does not: run forever, make semantic prioritization choices, or create a new run.

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
   - the saved planner model is what you want
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
orchestrator run --goal "Build the first settings page"
orchestrator status
orchestrator resume
orchestrator continue --max-cycles 3
orchestrator history
```

Key points:

- `run` creates a new run and executes one bounded cycle.
- `resume` does one more bounded cycle on the latest unfinished run.
- `continue` repeats bounded cycles up to `--max-cycles`.
- `status` shows the latest stable checkpoint and next operator action.

### Autonomous Foreground Workflow

Use this when you want one foreground process to keep advancing the same run:

```powershell
orchestrator auto start --goal "Ship the first dashboard milestone"
orchestrator auto continue
```

Key points:

- `auto` remains foreground only.
- It still uses the existing bounded-cycle core internally.
- It stops only at cycle boundaries.
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

## Related Docs

- [docs/REAL_APP_WORKFLOW.md](docs/REAL_APP_WORKFLOW.md)
- [docs/PRIME_TIME_V1_READINESS.md](docs/PRIME_TIME_V1_READINESS.md)
- [docs/ARTIFACT_PLACEMENT_POLICY.md](docs/ARTIFACT_PLACEMENT_POLICY.md)
- [docs/WINDOWS_INSTALL_AND_RELEASE.md](docs/WINDOWS_INSTALL_AND_RELEASE.md)
