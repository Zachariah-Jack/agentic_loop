# Orchestrator V2 Shell

This is the first minimal desktop shell for the orchestrator V2 control protocol.

It is intentionally small. It proves that the desktop shell can talk to the real engine protocol without bypassing engine internals.

## What It Supports

- Connection to `orchestrator control serve`
- A guided Home dashboard that answers: am I connected, what repo is loaded, is there a run, does the system need me, and what should I click next
- An always-visible top status strip with plain `Connection Status` and `Loop Status`, timers, control-server address, repo root, current run id, blocker, verbosity, update, and connect controls
- Real left-side tabs for Home, Run, Action Required, Chat, Files, Live Output, Workers, Terminal, Settings, and Dogfood Notes
- A simplified Run Control card with protocol-backed Start Build / Continue Build buttons using `start_run` and `continue_run`; exact terminal commands are hidden as backup help
- Progress and roadmap-alignment pane driven by planner-safe operator status plus surfaced roadmap context
- Status pane with elapsed time, model health, and executor failure details when available
- Pending action detail pane
- What Happened summary pane with plain-English stop reason translations and recommended next actions
- Artifact browser with raw text/JSON viewer for surfaced current-run artifacts
- Contract file editor for the canonical repo files only
- AI autofill wizard for the canonical contract files, with draft preview before save
- Read-only repo browser for one-directory-at-a-time tree listing and file opening
- Multiple embedded operator terminal tabs
- Action Required pane for primary executor approval-required state, with plain-English explanation plus technical details
- Model Health / Codex readiness surfaces planner model state, Codex model/access state, explicit unavailable-model errors, test buttons, and truthful `Not verified` states when the protocol cannot prove details
- Side Chat pane skeleton backed by the real protocol, with recorded-only truthful stub behavior
- Dogfood Notes pane for quick timestamped friction capture tied to the repo and latest run when available
- Worker Panel backed by real worker listing data from the engine protocol, with create, dispatch, remove, and integration-preview controls
- Live Output pane with readable planner/executor/worker/approval/model rows, category/current-run/text filtering, verbosity-aware detail, and raw payloads under Trace/details
- Control Chat message injection
- Safe stop / clear stop controls
- Verbosity changes that apply immediately when selected and change what appears in Live Output
- Local shell-session persistence for the last control-server address, auto-reconnect preference, last selected artifact/contract/repo file/worker, side-chat context policy, and activity filters
- Reconnect and rehydration behavior for status, pending action context, workers, artifacts, side chat history, dogfood notes, contract-file selections, and recent event backlog where available
- Inline protocol issue reporting so failed actions stay visible and actionable during daily use

## What It Does Not Support Yet

- Full side-chat backend
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
- The Settings tab includes Model Health. `Test Planner Model` checks the configured planner model or `gpt-5-latest` alias through OpenAI model discovery. `Test Codex Config` reports what the engine can detect about Codex launch/config and any model-unavailable error from the latest executor failure. Neither action silently falls back to a weaker model.
- Run elapsed time appears in Home, Status, and What Happened. Active control-server-launched runs show "running for ..."; stopped runs show "stopped after ..." when durable timestamps are available.

For everyday local dogfooding from the repo root, you can also use:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\start-v2-dogfood.ps1
```

That convenience script builds the CLI unless you skip it, launches the owned control server hidden by default, and opens the Electron shell without extra backend PowerShell windows.
It builds and launches `dist\orchestrator.exe`; portable release artifacts remain under `dist\windows-amd64\portable\`.
It also passes the chosen control-server address into the shell bootstrap so non-default ports do not require a manual first reconnect.
Logs are written under `.orchestrator\logs` in the target repo. When the Electron shell exits, the helper requests safe stop and stops only the control-server process it launched.

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
5. Use Start Build / Continue Build for `start_run` / `continue_run`. The backup terminal-command controls are collapsed by default for troubleshooting or direct headless CLI use.
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

Run Control starts and continues runs through the control server, not by typing into the terminal. A process-local active-run guard prevents overlapping unattended loops launched from the same control server; if one is already active, the shell shows "A run is already active for this repo. Watch progress or safe stop it first."

The Side Chat pane is intentionally non-interfering. In this slice it records side-chat messages through the real control protocol and shows a truthful backend-unavailable response. It does not affect the live run.

The Worker Panel now supports explicit operator-triggered worker actions through the real engine protocol: create worker, dispatch one bounded worker turn, remove an idle worker, and build an integration preview artifact. Manual apply and worker-specific approval controls are still deferred.

The activity timeline is a more usable filtered view over the real NDJSON event stream, plus shell-local terminal session lifecycle events. It supports category filters, a current/latest-run toggle, text filtering, readable timestamps, and copy-payload buttons.

The Dogfood Notes pane is a lightweight local issue-capture path for daily use. It stores a short title plus a richer note through the real engine protocol, timestamps it, and ties it to the repo and latest run when available. It does not yet create GitHub issues automatically.

For daily use, the shell now remembers useful local state across restarts and can auto-reconnect to the last control server when enabled. Terminal processes themselves are intentionally not restored across restarts; only the surrounding shell selections and filters are.

## Known Limitations

- Side Chat is still recorded-only and does not have a real conversational backend yet.
- Terminal tabs are local operator utilities and are not restored as live processes after a shell restart.
- Event replay after reconnect is limited to the control server's recent in-memory history window.
- Dogfood Notes are local repo-scoped capture only in this slice; there is no GitHub-issue export flow yet.
- Worker apply controls and worker-specific approval controls are still deferred from the shell.
- Codex model/effort values are externally managed by Codex and may remain `Not verified` until Codex reports them or fails with a model-specific error.
- This is still an Electron proof shell, not the final polished console.
