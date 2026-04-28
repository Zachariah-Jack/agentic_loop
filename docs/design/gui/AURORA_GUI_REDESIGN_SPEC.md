# Aurora GUI Redesign Spec

Reference image: `docs/design/gui/aurora-orchestrator-reference.png`

## Visual Direction

Aurora Orchestrator is the premium Windows mission-control dashboard for planner-led AI builds. The default surface is dark, glassy, high-contrast, calm, and reassuring. Panels use deep navy/black backgrounds, subtle borders, soft shadows, and restrained glow. Accent colors may include cyan, blue, purple, green, amber, and red.

The GUI must feel like a polished AI command center rather than a developer debug panel. Raw JSON, traces, and long payloads stay behind details controls or logs.

## Core Layout

- Top session tab strip for one Aurora window with multiple independent repo sessions.
- Left vertical navigation rail for sections.
- Left Project System drawer for canonical repo files, saved goal, and setup checks.
- Center Mission Run dashboard with a large analog-style gauge, run metadata, timers, status chips, controls, and live activity timeline.
- Right AI Conversation panel with visible Side Chat, raw note injection, and explicit context controls.
- Lower-center mission controls for Start, Continue, Pause at Safe Point, Graceful Stop, Snapshot, Inject Note, and View Logs.

## Multi-Session Tabs

Aurora supports multiple repo sessions inside one desktop window. Each tab has its own repo path, control-server address, status snapshot, run id, event timeline, project files, goal, setup health, ntfy config view, timers, and local chat/setup state. Switching tabs must route every protocol action through the active tab's address/repo identity.

The `+ New` tab opens a folder picker, initializes safe Orchestrator runtime folders for the selected repo, starts a repo-scoped control server when possible, and opens a new tab. Tabs can be closed without closing the whole window. If a tab appears to have active work, closing it must ask for confirmation and must not silently kill executor work. Right-clicking a tab exposes Rename and Close. Custom tab labels are local UI state and do not affect repo identity.

## Required State

The dashboard surfaces real mechanical state when available:

- repo path and current branch
- run id
- current run state
- current cycle
- current stage/checkpoint
- current action or pending action
- planner outcome
- executor status
- latest checkpoint
- stop reason
- build timers
- setup health checks
- project file saved/missing status
- timeline events and artifacts

If exact planner progress is unavailable, the gauge shows phase/status instead of fake precision.

## Timer Rules

- Total Build Time accumulates only while the loop is actively running.
- Total Build Time pauses while paused, safe-stopped, stopped, waiting in a non-running state, or closed.
- Reopening the GUI for the same repo/run resumes from persisted engine build-time totals.
- Current Cycle Time tracks the visible cycle/loop and resets only when the next cycle begins.
- Recent Cycle Times show the last several completed cycle/loop durations when event timestamps expose enough data.
- Timers are visibility only. They must never stop Codex or executor work.
- Executor/Codex turns must not receive a hidden hard timeout. Emergency kill behavior, if added, must be explicit and separately labeled.

## Gauge Geometry

The mission gauge reads like a speedometer. One shared progress normalization path drives numeric text, needle angle, tick labels, and colored arc.

- 0 percent is at the dead bottom.
- 25 percent points directly left.
- 50 percent points dead top.
- 75 percent points directly right.
- 100 percent ends back at the dead bottom after a full circle.
- The completed arc fills only from the 0 percent mark to the needle.
- If progress is unavailable, the gauge shows phase/status and avoids fake precision.

## Activity Timeline

Default timeline rows are concise natural-language events, for example:

- Orchestrator initialized successfully
- Prompt sent to planner
- Awaiting planner response
- Planner response received
- Planner requested repo context
- Context collected
- Prompt sent to Codex
- Codex turn started
- Waiting on Codex
- Codex response received
- Files changed
- Tests started
- Tests completed
- Waiting on human input
- Human reply received
- Safe pause requested
- Paused at safe point
- Snapshot captured
- Run complete

Rows include timestamp, type/source badge, label, optional elapsed time, and a details affordance for raw payloads. Filters include All Events, Planner, Executor, Human, Files, Tests, and System. Auto-scroll is enabled by default and user-toggleable.

## Project System

The Project System panel shows:

- `brief.md`: project brief and desired outcome
- `roadmap.md`: build plan and milestones
- `constraints.md`: technical and business guardrails
- `decisions.md`: stable decisions and rationale
- `human-notes.md`: extra user context and notes
- `goal.md`: current run/build objective

Each card shows filename, purpose, saved/missing status, last updated time when available, and opens the explicit editor. Missing files are handled through setup checklist/template actions.

The Saved Goal card is read-only by default. Long goals are clipped with a View more/View less affordance. Edit Goal expands an explicit textarea with Save and Cancel only. The edit draft stays separate from the saved goal until Save is clicked.

## AI File Generation

"Use AI to Generate Files & Goal" expands a first-message composer on Home. The operator enters what they want to build, then Submit routes that message into the dedicated planner-assisted setup/autofill session. It does not start a build run.

The setup flow uses a setup prompt that tells the planner:

> You are helping the user prepare an Orchestrator project before a build run. The app requires project files and a goal: brief.md, roadmap.md, constraints.md, decisions.md, and goal. Ask or use the user's setup answers until there is enough detail to draft those files. Return structured content only. Do not start a build run. Do not claim files were written; the engine writes only after explicit user confirmation.

Generated content is previewed first and saved only through explicit file-save actions.

## ntfy Mobile Human Bridge

Home includes first-class ntfy configuration for the active repo/session:

- server URL
- topic
- optional auth token entry
- Save & Test ntfy
- configured/test/listening/last-reply status without exposing the saved token

Saving ntfy config is a mechanical runtime-config action and may take effect immediately for future planner ask-human waits. Test sends a real ntfy notification and must report failure truthfully. Inbound ntfy replies are persisted raw and forwarded through the same human-input path as GUI replies. The GUI must not parse reply prose as control, and the planner must continue through structured planner outcomes such as execute, collect_context, ask_human, pause, or complete.

## Startup Checklist

On repo open/connect, the GUI shows mechanical setup checks:

- Git initialized
- repository trusted for Git
- Orchestrator initialized
- required project files exist
- Codex available/authenticated
- planner key/config available
- global launcher points to the current checkout binary
- ntfy configured when applicable
- state/log folders writable

Safe one-click actions may run `git init`, create Orchestrator templates, initialize runtime folders, check Codex, verify planner config presence, repair the Windows User PATH global launcher, and run `git config --global --add safe.directory "<repo path>"`. Codex trust must not be faked; if it cannot be automated reliably, show manual guidance.

The GUI and control backend may automatically repair safe runtime directories when missing:

- `.orchestrator/state/`
- `.orchestrator/logs/`
- `.orchestrator/artifacts/`
- the repo worker runtime folder

Missing `.orchestrator/artifacts/` must not trap the user behind a raw Continue error. Status/health refresh should repair the directory, refresh readiness, and update Start/Continue controls. Missing contract files should show `Repair Project Setup`, which runs the idempotent scaffold path and preserves existing files. The manual fallback command is:

```powershell
orchestrator init
```

## Global Launcher

Primary launch command:

```powershell
orchestrator gui
```

Expected behavior:

- Works from any current directory once the orchestrator binary folder is on `PATH`.
- If launched in a Git repo, selects that repo.
- Outside a repo, reuses the last GUI repo if known.
- If no repo is known, opens against the current folder so the setup checklist can guide first-run setup.
- Uses the local control protocol and Electron shell launcher.
- Reuses/clears only owned backend processes where supported and refuses to kill unknown listeners.
- Repairs safe repo setup before backend launch and fails loudly with shell/control log paths if the Electron shell exits before a window can remain open.

`orchestrator doctor` reports GUI launch readiness. `orchestrator setup` prints PATH/global launch guidance.

## Architecture Boundaries

- The GUI, CLI, and backend are not decision-makers.
- The planner decides workflow, completion, pauses, questions, and next steps.
- Codex/executor performs code and artifact work.
- The GUI displays state, routes explicit operator actions, edits/saves user-provided files, starts setup actions, and surfaces logs/artifacts.
- Human replies and notes are saved/routed raw.
- No semantic stop conditions may be added to the GUI.
- No assistant prose may be parsed as runtime control.
- Headless CLI mode must remain fully working.
