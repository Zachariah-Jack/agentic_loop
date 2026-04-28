# Aurora GUI Redesign Spec

Reference image: `docs/design/gui/aurora-orchestrator-reference.png`

## Visual Direction

Aurora Orchestrator is the premium Windows mission-control dashboard for planner-led AI builds. The default surface is dark, glassy, high-contrast, calm, and reassuring. Panels use deep navy/black backgrounds, subtle borders, soft shadows, and restrained glow. Accent colors may include cyan, blue, purple, green, amber, and red.

The GUI must feel like a polished AI command center rather than a developer debug panel. Raw JSON, traces, and long payloads stay behind details controls or logs.

## Core Layout

- Left vertical navigation rail for sections.
- Left Project System drawer for canonical repo files, saved goal, and setup checks.
- Center Mission Run dashboard with a large analog-style gauge, run metadata, timers, status chips, controls, and live activity timeline.
- Right AI Conversation panel with visible Side Chat, raw note injection, and explicit context controls.
- Lower-center mission controls for Start, Continue, Pause at Safe Point, Graceful Stop, Snapshot, Inject Note, and View Logs.

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
- Current Step Time tracks the active step/event and resets per active step.
- Timers are visibility only. They must never stop Codex or executor work.
- Executor/Codex turns must not receive a hidden hard timeout. Emergency kill behavior, if added, must be explicit and separately labeled.

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

Goal editing is separate from saved state. The user must explicitly Save Goal. Saved status and unsaved edits are visible.

## AI File Generation

“Use AI to Generate Files & Goal” opens the setup-file generation workflow, not a build run. It uses the planner-assisted autofill path with a setup prompt that tells the model:

> You are helping the user prepare an Orchestrator project before a build run. The app requires project files and a goal: brief.md, roadmap.md, constraints.md, decisions.md, and goal. Ask or use the user’s setup answers until there is enough detail to draft those files. Return structured content only. Do not start a build run. Do not claim files were written; the engine writes only after explicit user confirmation.

Generated content is previewed first and saved only through explicit file-save actions.

## Startup Checklist

On repo open/connect, the GUI shows mechanical setup checks:

- Git initialized
- repository trusted for Git
- Orchestrator initialized
- required project files exist
- Codex available/authenticated
- planner key/config available
- ntfy configured when applicable
- state/log folders writable

Safe one-click actions may run `git init`, create Orchestrator templates, initialize runtime folders, check Codex, verify planner config presence, and run `git config --global --add safe.directory "<repo path>"`. Codex trust must not be faked; if it cannot be automated reliably, show manual guidance.

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
