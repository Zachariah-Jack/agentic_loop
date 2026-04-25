# Orchestrator V2 Shell

This is the first minimal desktop shell for the orchestrator V2 control protocol.

It is intentionally small. It proves that the desktop shell can talk to the real engine protocol without bypassing engine internals.

## What It Supports

- Connection to `orchestrator control serve`
- A guided Home dashboard that answers: am I connected, what repo is loaded, is there a run, does the system need me, and what should I click next
- An always-visible top status strip with plain `Connection Status` and `Loop Status`, timers, control-server address, repo root, current run id, blocker, verbosity, update, and connect controls
- Real left-side tabs for Home, Run, Action Required, Chat, Files, Live Output, Workers, Terminal, Settings, and Dogfood Notes
- A simplified Run Control card with protocol-backed Start Build / Continue Build buttons using `start_run` and `continue_run`; exact terminal commands are hidden as backup help
- Loop Status distinguishes active work from safe pauses. `Running` means the backend is actively advancing planner/executor work. `Ready to Continue` means the planner has selected an execute task and Codex has not started yet; click `Continue Build / Dispatch Executor`. `Waiting at Safe Point` means the run is durably paused and can be continued from the GUI.
- Full-width progress and roadmap-alignment pane driven by planner-safe operator status, with long focus/basis/roadmap text in expandable sections
- Status pane with elapsed time, model health, and executor failure details when available
- Pending action detail pane
- What Happened summary pane with plain-English stop reason translations, recommended next actions, Copy Debug Bundle, Copy Latest Error, and Open Live Output controls
- Artifact browser with raw text/JSON viewer for surfaced current-run artifacts
- Contract file editor for the canonical repo files only
- AI autofill wizard for the canonical contract files, with draft preview before save
- Read-only repo browser for one-directory-at-a-time tree listing and file opening
- Multiple embedded operator terminal tabs
- Action Required pane for primary executor approval-required state and planner `ask_human` pauses, with plain-English explanation plus technical details
- Planner question recovery from the GUI: type the raw answer in Action Required, then use `Send Answer and Continue` to queue `inject_control_message` and resume with `continue_run`
- Model Health / Codex readiness surfaces planner model state, the Codex executable path/version/config source seen by the control server, required `gpt-5.5` executor model state, full-access probe state, explicit unavailable-model errors, test buttons, and truthful `Not verified` states when the protocol cannot prove details
- Automatic model-health checks on connect/reconnect and Start Build / Continue Build preflight, so fresh successful planner/Codex checks override stale older component errors instead of blocking from old state
- Copy Model Health for a safe backend/planner/Codex diagnostic bundle with secrets excluded
- Backend identity and stale-backend visibility for dogfood runs, with a `Recover Backend / Unlock Repo` button when the shell was launched with owned-backend metadata
- Target repo binding for dogfood runs: the helper passes the intended `-RepoPath` into the shell, verifies the backend reports that repo before opening the UI, and the shell shows a `Wrong Repo Backend` warning instead of displaying runs from another repo
- Mechanical stale-run recovery: old active-run guards from previous dogfood backends are surfaced as Action Required and can be cleared without deleting run history/artifacts or changing the run's planner/executor outcome
- SQLite busy/locked hardening for common shell reads; persistent lock contention returns a plain state-database-locked error instead of a confusing raw SQLite exception
- Side Chat context-agent foundation backed by the real protocol. It persists raw messages and answers from observable runtime context only; it does not queue Control Chat, set stop flags, change pending actions, or affect planner/Codex flow.
- Runtime Timeouts & Autonomy settings backed by `get_runtime_config` / `set_runtime_config`, including timeout presets, per-timeout edits, and permission profiles (`Guided`, `Balanced`, `Autonomous`, `Full Send / Lab Mode`).
- Update status card backed by GitHub release checks/changelog protocol actions. Install is shown truthfully as deferred until safe signed/checksummed Windows assets are published.
- Dogfood Notes pane for quick timestamped friction capture tied to the repo and latest run when available
- Worker Panel backed by real worker listing data from the engine protocol, with create, dispatch, remove, and integration-preview controls
- Live Output pane with readable planner/executor/worker/approval/model rows, pinned latest error, category/current-run/text filtering, verbosity-aware detail, and raw payloads under Trace/details
- Control Chat message injection
- Safe stop / clear stop controls, plus Action Required guidance for `operator_stop_requested` states with a `Clear Stop and Continue` path
- Verbosity changes that apply immediately when selected and change what appears in Live Output
- Local shell-session persistence for the last control-server address, auto-reconnect preference, last selected artifact/contract/repo file/worker, side-chat context policy, and activity filters
- Reconnect and rehydration behavior for status, pending action context, workers, artifacts, side chat history, dogfood notes, contract-file selections, and recent event backlog where available
- Inline protocol issue reporting so failed actions stay visible and actionable during daily use

## What It Does Not Support Yet

- Full LLM-backed side-chat escalation/tool backend
- Worker apply controls
- Worker-specific approval controls
- Packaged installer polish

## Development

1. Start the engine protocol server in the target repo:

```powershell
orchestrator control serve
```

2. In this folder, install dependencies:

```powershell
npm install
```

3. Start the shell:

```powershell
npm run dev
```

By default the shell connects to `http://127.0.0.1:44777`, but the address is editable in the UI.

When the shell opens, start on Home:

- If the top banner says Not connected, start `orchestrator control serve` in the target repo and click Connect / Reconnect.
- The top status strip shows the control-server address, repo root, current run id or "No active run", run state, blocker/stop reason, and verbosity.
- The "What should I do now?" card is UI guidance derived from real protocol data. It does not decide for the engine.
- If no run exists or an unfinished run is resumable, the Run Launcher uses real `start_run` / `continue_run` protocol actions. The request returns after launch, and the control-server process keeps running the foreground unattended loop while events/status update the shell.
- The Current Repo card shows the repo root and canonical contract file status so you do not need to hunt through advanced panes.
- The Live Output tab is the default place to watch real-time planner, Codex/executor, worker, approval, model/config, and shell events. Quiet shows only major state changes and blockers; Normal shows readable progress; Verbose adds more planner/executor/artifact detail; Trace exposes raw safe event payloads.
- If a run stops or looks confusing, click `Copy Debug Bundle` from Home or What Happened and paste it into ChatGPT/Codex. The bundle includes repo/run/model/status/error/progress/pending/approval/artifact paths and recent Live Output summaries, while excluding API keys, auth tokens, hidden chain-of-thought, and full artifact contents.
- The Settings tab includes Model Health. `Test Planner Model` checks the configured planner model or `gpt-5-latest` alias; aliases resolve through OpenAI model discovery, then the resolved/requested model must complete a tiny Responses API probe. Planner models below `gpt-5.4` are invalid. `Test Codex Config` runs a real probe using the same Codex command path the control server sees, requiring `gpt-5.5`, `danger-full-access`, approval `never`, and effort `xhigh`. Neither action silently falls back to a weaker model.
- The Settings tab also includes Runtime Timeouts & Autonomy. Timeout values accept durations such as `30m`, `2h`, or `unlimited`; `executor_turn_timeout` and `human_wait_timeout` default to `unlimited`.
- The Updates card can check GitHub Releases, display/copy release notes, and report update availability. Self-install remains disabled until release assets are signed/checksummed and safe to stage on Windows.
- Model Health runs automatically after connect/reconnect and before Start Build / Continue Build. If both checks pass, the shell shows Overall verified / Blocking no even when an older run contains a stale model error. If a newer test or run fails, Action Required shows the current failure.
- Use `Copy Model Health` to capture backend PID/start time/binary path/version, planner verification, Codex path/version/config, `gpt-5.5` model verification, full-access status, and recommended action. Secrets and tokens are redacted.
- Run elapsed time appears in Home, Status, and What Happened. Active control-server-launched runs show "running for ..."; stopped runs show "stopped after ..." when durable timestamps are available.

For everyday local dogfooding from the repo root, you can also use:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-v2-dogfood.ps1
```

That convenience script builds the CLI unless you skip it, stops any previous dogfood-owned backend for the same repo/address, kills the owned backend process tree if the first stop does not clear the listener, waits for the port to clear, launches the owned control server hidden by default, and opens the Electron shell without extra backend PowerShell windows.
It builds and launches `dist\orchestrator.exe`; portable release artifacts remain under `dist\windows-amd64\portable\`.
It launches the backend with `-RepoPath` as the working directory, verifies `/v2/control get_status_snapshot` reports that same repo root before the shell opens, and passes both the chosen control-server address and expected repo path into the shell bootstrap.
Logs are written under `.orchestrator\logs` in the target repo. When the Electron shell exits, the helper requests safe stop and stops only the control-server process it launched.
The helper tracks ownership in `.orchestrator\state\dogfood-backend.json`, including PID, repo path, control address, binary path, binary modified time at launch, start time, and owner session id. Normal launches start `dist\orchestrator.exe` directly so metadata points at the actual backend process rather than a PowerShell wrapper. Unknown user-started processes on the same port are not killed automatically; startup stops with PID, path, command line, ownership match, and next-action diagnostics so you can decide what to do.

If the connected backend reports a repo root that does not match the expected dogfood target, the shell shows `Wrong Repo Backend`, disables Start Build and Continue Build for that backend, avoids treating the wrong repo's latest run as actionable, and recommends `Restart Backend for Target Repo`. This is a mechanical safety check only; it does not decide anything about the project.

If the shell shows stale state, `SQLITE_BUSY`, or a run that is "already active" but not progressing, use `Recover Backend / Unlock Repo` from Home or Settings. If the backend is healthy but the active-run guard is stale, the shell asks the backend to clear that guard directly so a resumable safe-pause run can continue. In dogfood-owned launches that also need a restart, it stops only the owned backend and terminal sessions, verifies the port is clear, kills only dogfood-owned backend listeners if a child process survived, starts a fresh backend for the same repo/address, reconnects, reruns model health, and refreshes panels. It preserves run status, checkpoints, history, artifacts, and repo files. If the port is held by an unknown process, recovery refuses to kill it and reports the current PID/path/command line.

Use visible backend windows only when debugging:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-v2-dogfood.ps1 -DebugVisibleWindows
```

For a double-click app-like launch, use `scripts\Launch-Orchestrator-V2-Shell.vbs` from the repo root or create a shortcut to it and pin the shortcut to the taskbar.

## Recommended Daily Dogfood Flow

1. Use the repo-root helper script when you want the fastest startup:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-v2-dogfood.ps1 -RepoPath D:\Projects\target-repo
```

2. Keep auto-reconnect enabled in the shell unless you are intentionally testing disconnect behavior.
3. Use the shell for live operator visibility, Control Chat, artifacts, contract editing, approvals, and worker actions.
4. Use Home first: confirm `Connection Status: Ready`, verify the repo root, read the recommended-action card, then use the obvious primary button.
   If the Current Repo card shows a mismatch, use `Restart Backend for Target Repo` before starting or continuing. Do not act on run state from the wrong repo.
5. Use Start Build / Continue Build for `start_run` / `continue_run`. The backup terminal-command controls are collapsed by default for troubleshooting or direct headless CLI use.
   If Loop Status says `Ready to Continue`, the run is not coding in the background. Click `Continue Build / Dispatch Executor` to hand the planner-selected task to Codex.
6. Use the headless CLI directly for `run`, `continue`, `status`, `history`, and `doctor` when you want a purely terminal flow.
7. If the shell or control server restarts, reconnect to the same address and let the shell rehydrate the current run view.
8. Capture daily friction in the Dogfood Notes pane instead of keeping ad hoc scratch notes elsewhere; it keeps the note timestamped and associated with the repo/run.

If reconnect fails:

- confirm `orchestrator control serve` is still running for the same repo
- verify the address in the shell matches the control server listen address
- use `Connect / Reconnect` once after the server is back
- fall back to headless `orchestrator status` / `history` if you need immediate visibility while the shell is reattaching

If approval is needed:

- use Action Required when the primary executor is waiting on approval
- if the planner asks a question, use Action Required's answer box. `Send Answer and Continue` forwards your exact text as `reason: ask_human_answer` and calls `continue_run`; the shell does not interpret the answer.
- if the run stopped because Safe Stop was requested, use Action Required's `Clear Stop and Continue`; it clears the mechanical stop flag and then resumes through `continue_run` when the run is resumable.
- if worker-specific approval is the blocker, use the headless CLI for now because worker-specific shell approval UI is still deferred

Artifact browsing is limited to artifact paths the engine already surfaced through the real protocol. Contract editing is limited to:

- `.orchestrator/brief.md`
- `.orchestrator/roadmap.md`
- `.orchestrator/decisions.md`
- `.orchestrator/human-notes.md`
- `AGENTS.md`

The AI autofill wizard drafts selected canonical contract files through the real engine protocol and previews the generated content in the shell. Nothing is saved until you explicitly save a selected draft through the existing contract-file save path.

The repo browser is intentionally read-only in this slice. It lists one directory at a time through the control server and can open repo files for viewing, but arbitrary repo-file saving is still deferred.

The terminal pane is a local operator utility shell with separate local shell tabs. It does not bypass the engine protocol for run control. Action Required is focused on the primary executor only; worker-specific approval UI remains deferred.

Run Control starts and continues runs through the control server, not by typing into the terminal. A process-local active-run guard prevents overlapping unattended loops launched from the same control server; if one is truly active, the shell shows "Loop Running." If the active-run guard belongs to a previous backend/session, the shell shows "Recovery needed" and guides you to `Recover Backend / Unlock Repo` instead of requiring manual PowerShell process cleanup.

The Side Chat pane is intentionally non-interfering. In this slice it answers from visible runtime context only: repo, latest run, planner/operator status, executor errors, timeout settings, permission profile, and related safe status fields. It does not affect the live run, does not set stop flags, and does not create control-intervention messages. Use Control Chat or Action Required when you want to affect the active run.

The Worker Panel now supports explicit operator-triggered worker actions through the real engine protocol: create worker, dispatch one bounded worker turn, remove an idle worker, and build an integration preview artifact. Manual apply and worker-specific approval controls are still deferred.

The activity timeline is a more usable filtered view over the real NDJSON event stream, plus shell-local terminal session lifecycle events. It supports category filters, a current/latest-run toggle, text filtering, readable timestamps, and copy-payload buttons.

The Dogfood Notes pane is a lightweight local issue-capture path for daily use. It stores a short title plus a richer note through the real engine protocol, timestamps it, and ties it to the repo and latest run when available. It does not yet create GitHub issues automatically.

For daily use, the shell now remembers useful local state across restarts and can auto-reconnect to the last control server when enabled. Terminal processes themselves are intentionally not restored across restarts; only the surrounding shell selections and filters are.

## Known Limitations

- Side Chat is a context-agent foundation, not the final LLM/tooling backend. It does not perform hidden actions or semantic run steering.
- Terminal tabs are local operator utilities and are not restored as live processes after a shell restart.
- Event replay after reconnect is limited to the control server's recent in-memory history window.
- Dogfood Notes are local repo-scoped capture only in this slice; there is no GitHub-issue export flow yet.
- Worker apply controls and worker-specific approval controls are still deferred from the shell.
- Codex model/full-access readiness remains `Not verified` until `Test Codex Config` succeeds in the current control-server environment. If you update Codex, restart the dogfood/control server and test again so stale app-server processes do not keep old binaries or environment.
- This is still an Electron proof shell, not the final polished console.
