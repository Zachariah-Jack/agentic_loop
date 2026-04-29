# Aurora Visual Fidelity Review

Reference image: `D:\Projects\agentic_loop\docs\design\gui\aurora-orchestrator-reference.png`

Current failed screenshots inspected:

- `D:\Projects\agentic_loop\docs\design\gui\aurora-current-failed-screenshot-home.png`
- `D:\Projects\agentic_loop\docs\design\gui\aurora-current-failed-screenshot-run.png`
- `D:\Projects\agentic_loop\docs\design\gui\aurora-current-failed-screenshot-action-required.png`
- `D:\Projects\agentic_loop\docs\design\gui\aurora-current-failed-screenshot-chat.png`
- `D:\Projects\agentic_loop\docs\design\gui\aurora-current-failed-screenshot-files.png`
- `D:\Projects\agentic_loop\docs\design\gui\aurora-current-failed-screenshot-terminal.png`
- `D:\Projects\agentic_loop\docs\design\gui\aurora-current-failed-screenshot-settings.png`

## What Was Wrong Before

- The visible GUI was still a centered scrolling web-page layout with wide unused side margins.
- The native Electron menu bar was visible as `File / Edit / View / Window`.
- The Home surface still had a large landing-page intro block instead of an app cockpit.
- The left navigation was a text sidebar, not a slim mission-control rail.
- Project System, Mission Run, and AI Conversation were cards inside a page instead of persistent left, center, and right dashboard zones.
- The mission gauge was too small and not the dominant visual anchor.
- Several controls could inherit browser/default light surfaces or low-contrast active states.
- The startup path still felt developer-first and required a manual build/run ritual.
- The top header/status strip was too verbose, included distracting timers, and exposed Connect/Update Dashboard as dominant concepts.
- Home allowed Project System, ntfy, setup, mission facts, controls, and gauge content to overlap or underlap.
- Action Required could repeatedly pull the operator back while they were trying to inspect other screens.
- `Clear Stop and Continue` combined two separate actions and resumed work without the clearer Home `Continue Build` step.
- Run, Chat, Workers, Terminal, and Settings had unclear or wasteful information architecture.

## What Changed In This Pass

- Reworked the renderer into a fixed-height full-window shell with compact brand, session tabs, status strip, left icon rail, Project System drawer, central Mission Run dashboard, and right AI Conversation panel.
- Hid the normal Electron application menu, with `ORCHESTRATOR_SHOW_ELECTRON_MENU=1` kept as a development escape hatch.
- Added explicit Aurora theme tokens for root backgrounds, shell surfaces, raised panels, inputs, text, borders, accents, and focus rings.
- Restyled file cards, selected/active states, contract editor, generated previews, text inputs, textareas, selects/options, disabled controls, ntfy fields, warning boxes, setup rows, context menus, tabs, chat composer, and mission controls for dark-readable contrast.
- Enlarged and restyled the mission gauge with a glowing conic progress arc, visible ticks, numeric labels, needle, glass depth, and central progress text driven by the existing shared gauge normalization path.
- Preserved completed runs inside the Mission Run dashboard and surfaces `Planner declared run complete` without replacing the cockpit with a flat results page.
- Added an Aurora startup launcher with version/update status, Read Me, repo folder selection, disabled-until-valid Start Aurora, and quiet backend build/start handoff.
- Reduced the top strip to concise connection/run/repo/run-id/attention status and removed the connection timer from the visible badge.
- Rebalanced Home density so Project System, Saved Goal, ntfy, setup, gauge, status chips, timers, timeline, and AI Conversation each have protected space.
- Changed Action Required behavior to badge/announce instead of repeatedly forcing navigation.
- Renamed safe-stop recovery to `Clear Stop` and made it clear only the stop flag; Continue Build remains separate.
- Reframed secondary screens: Run as operational detail, Chat as archive/orientation, Workers as Worker Activity, Terminal as a wider behind-the-scenes pane, Settings as broader groups, and Live Output as denser monitoring.
- Added click-to-copy for high-value status/mission values and demoted repetitive successful planner/Codex health-check noise.

## Layout Checklist

- [x] Electron menu hidden.
- [x] Full-window shell.
- [x] Left nav rail.
- [x] Project drawer.
- [x] Central mission dashboard.
- [x] Right AI conversation.
- [x] Gauge fidelity improved while retaining 0/25/50/75/100 geometry.
- [x] Contrast/readability tokens applied to controls and selected states.
- [x] Dark Aurora theme with glass panels, blue/cyan/purple glow, and subtle borders.
- [x] No centered web-page layout on normal desktop viewport.
- [x] Startup launcher replaces the manual-first app entry path.
- [x] Header/status strip simplified.
- [x] Action Required no longer hijacks navigation.
- [x] Clear Stop no longer continues the run.
- [x] Secondary screens have clearer purpose and density.
- [x] Click-to-copy affordances added for useful status values.

## Remaining Limitations

- This remains the Electron proof shell, not a packaged final console.
- The visual pass is CSS/renderer-focused; no workflow authority moved into the GUI.
- Inactive multi-session tabs still rehydrate when selected rather than maintaining separate live background event streams.
- Safe Windows self-install remains deferred until signed/checksummed release assets exist; the launcher reports update status but does not pretend install support is finished.
- Worker apply and worker-specific approval controls remain deferred and intentionally secondary.
