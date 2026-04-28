# Changelog

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
