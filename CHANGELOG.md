# Changelog

## v1.4.0-dev - 2026-04-29

- Added an app-first Aurora startup launcher with branding, version/update status, Read Me, repo folder selection, disabled-until-ready Start Aurora, and quiet backend build/start handoff for normal Windows use.
- Reworked the Home dashboard around dogfood feedback: compact header/status strip, calmer first-load state, protected central gauge space, cleaner Project System/ntfy/setup density, right-side Run Q&A, and no dominant Connect/Update Dashboard controls.
- Changed safe-stop recovery so `Clear Stop` only clears the mechanical stop flag; `Continue Build` remains a separate explicit protocol action.
- Stopped Action Required from repeatedly hijacking navigation. Outstanding issues now remain visible through badges/status while the operator can inspect other screens.
- Redesigned secondary surfaces for purpose and density: Run as operational detail, Chat as conversation archive/orientation, Workers as worker activity with advanced manual controls collapsed, Terminal as a larger behind-the-scenes pane, Settings as broader grouped cards, and Live Output as denser monitoring rows.
- Added click-to-copy affordances for useful mission/status values and reduced noisy successful planner/Codex health-check flashes.

Known limits:

- The launcher can check release status and quietly build/start the local backend, but safe self-install remains disabled until signed/checksummed Windows assets exist.
- Worker apply and worker-specific approvals remain deferred; the Worker Activity screen shows available state and keeps advanced manual controls clearly secondary.

## v1.3.0-dev - 2026-04-29

- Rebuilt the Aurora GUI Home surface as a full-window mission-control dashboard with a compact command bar, session tabs, top status strip, far-left icon rail, persistent Project System drawer, dominant Mission Run panel, and integrated right AI Conversation panel.
- Hid the default Electron File/Edit/View/Window menu in normal launches while preserving an explicit `ORCHESTRATOR_SHOW_ELECTRON_MENU=1` development escape hatch.
- Added a tokenized dark Aurora theme for root/background, panels, inputs, text, borders, accents, and focus states; restyled selected file cards, editors, inputs, ntfy fields, chat composer, warnings, previews, context menus, tabs, and active/disabled states for readable contrast.
- Improved Mission Run visual fidelity with a larger central gauge, fixed completed-run dashboard state, status chips, timer cards, timeline, and mission controls inside the same shell layout.
- Added renderer tests for the full-window Aurora zones, token coverage, readable selected/control states, and Electron menu hiding.

Known limits:

- This is still the Electron proof shell rather than the final packaged console. Inactive multi-session tabs continue to rehydrate when selected instead of maintaining background event streams.

## v1.2.2-dev - 2026-04-29

- Added explicit V2 control-protocol capability fields for runtime config, `ntfy` runtime config, `test_ntfy`, and backend compatibility so the GUI can detect older backends instead of failing mysteriously.
- Kept the canonical Home ntfy payload as `{"ntfy":{"server_url":"...","topic":"...","auth_token":"..."}}`, with optional auth token and masked status responses.
- Changed strict payload failures to return friendly protocol mismatch messages instead of raw Go JSON decoder text.
- Updated the Aurora renderer to show “Backend is running an older protocol. Restart Aurora GUI.” guidance for stale backends and to avoid showing raw `json: unknown field "ntfy"` errors.
- Added backend binary/protocol version and ntfy runtime-config support to debug bundles.
- Hardened dogfood GUI launch/recovery so an owned backend that is actively processing a run is not stopped automatically.

Known limits:

- A backend already running from an older binary cannot learn the new protocol fields in place. If active work is processing, wait for a safe boundary or request Safe Stop before restarting the GUI/backend.

## v1.2.1-dev - 2026-04-28

- Added `orchestrator install-global` and `orchestrator repair-global` to build the current checkout binary into repo-relative `bin\orchestrator.exe`, move that bin folder to the front of the Windows User PATH, update the current process PATH, and report the winning `orchestrator` executable.
- Added `orchestrator setup --repair-global` and an interactive setup prompt for stale/missing global launchers.
- Expanded `orchestrator doctor` with global launcher diagnostics: current binary path, desired global binary, current PATH winner, stale installs, and the exact repair command.
- Added a GUI setup-health `Global launcher` check with a `Repair Global Launcher` action backed by the same mechanical repair flow.

Known limits:

- The repair flow does not require admin rights and does not delete old installs. If an old machine-wide PATH entry outranks the User PATH, the tool reports it clearly and updates the current process PATH, but removing or reordering that machine entry remains an admin/system PATH task.

## v1.2.0-dev - 2026-04-28

- Added Aurora multi-session tabs with folder-picker `+ New`, close confirmation for active work, right-click rename, and per-repo session routing.
- Reworked Home goal handling into a read-only Saved Goal card with View more/View less and explicit Edit Goal Save/Cancel controls.
- Changed `Use AI to Generate Files & Goal` to reveal a first-message composer before entering the planner-assisted setup/autofill preview flow.
- Corrected mission-gauge normalization so 0/25/50/75/100 map to bottom/left/top/right/bottom and the completed arc stops at the needle.
- Added plain-language cycle narration, live Current Cycle Time, recent cycle durations, and active-only Total Build Time rendering.
- Added Home ntfy configuration with masked token status, runtime-config persistence, `Save & Test ntfy`, and the new `test_ntfy` control action.
- Hardened Aurora dark-theme readability for selected file buttons, editors, inputs, selects, popovers, previews, and active/focus states.

Known limits:

- Inactive Aurora session tabs rehydrate when selected rather than maintaining separate live background event streams.
- Recent cycle duration history depends on cycle-tagged events or persisted timing data; older runs may show only live current timing.
- ntfy listening remains tied to the existing ask-human wait path. Home `Save & Test ntfy` verifies publishing and applies settings for future human waits without claiming a background listener that is not active.

## v1.1.0-dev - 2026-04-25

- Added runtime timeout settings with `unlimited` support for planner requests, executor idle waits, executor turns, subagents, shell commands, installs, and human waits.
- Added active-only Total Build Time tracking for control-server-launched Start/Continue loops.
- Added runtime permission/autonomy profile settings for Guided, Balanced, Autonomous, and Full Send / Lab Mode.
- Upgraded Side Chat from recorded-only notes to a context-agent foundation that answers from observable runtime state while preserving raw messages, plus audited action requests for planner notes, context snapshots, and Safe Stop.
- Added GitHub Releases update-check and changelog foundation through CLI, control protocol, and the V2 shell Updates card; safe self-install remains deferred.
- Added CLI settings/update commands and expanded V2 runtime-config protocol support.
- Expanded V2 shell Settings with timeout presets, permission profiles, update status, and changelog copy support.

Known limits:

- Update install is intentionally unsupported until signed/checksummed Windows release assets and a staged install path are available.
- Side Chat does not yet include the final LLM-backed tooling backend; escalation is currently limited to explicit audited control actions.
- Broad permission-profile enforcement across every future installer/test/Git workflow remains staged behind the persisted profile foundation.
