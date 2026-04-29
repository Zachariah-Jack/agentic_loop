# Aurora Visual Fidelity Review

Reference image: `D:\Projects\agentic_loop\docs\design\gui\aurora-orchestrator-reference.png`

Current failed screenshot inspected: `D:\Projects\agentic_loop\docs\design\gui\aurora-current-failed-screenshot.png`

## What Was Wrong Before

- The visible GUI was still a centered scrolling web-page layout with wide unused side margins.
- The native Electron menu bar was visible as `File / Edit / View / Window`.
- The Home surface still had a large landing-page intro block instead of an app cockpit.
- The left navigation was a text sidebar, not a slim mission-control rail.
- Project System, Mission Run, and AI Conversation were cards inside a page instead of persistent left, center, and right dashboard zones.
- The mission gauge was too small and not the dominant visual anchor.
- Several controls could inherit browser/default light surfaces or low-contrast active states.

## What Changed In This Pass

- Reworked the renderer into a fixed-height full-window shell with compact brand, session tabs, status strip, left icon rail, Project System drawer, central Mission Run dashboard, and right AI Conversation panel.
- Hid the normal Electron application menu, with `ORCHESTRATOR_SHOW_ELECTRON_MENU=1` kept as a development escape hatch.
- Added explicit Aurora theme tokens for root backgrounds, shell surfaces, raised panels, inputs, text, borders, accents, and focus rings.
- Restyled file cards, selected/active states, contract editor, generated previews, text inputs, textareas, selects/options, disabled controls, ntfy fields, warning boxes, setup rows, context menus, tabs, chat composer, and mission controls for dark-readable contrast.
- Enlarged and restyled the mission gauge with a glowing conic progress arc, visible ticks, numeric labels, needle, glass depth, and central progress text driven by the existing shared gauge normalization path.
- Preserved completed runs inside the Mission Run dashboard and surfaces `Planner declared run complete` without replacing the cockpit with a flat results page.

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

## Remaining Limitations

- This remains the Electron proof shell, not a packaged final console.
- The visual pass is CSS/renderer-focused; no workflow authority moved into the GUI.
- Inactive multi-session tabs still rehydrate when selected rather than maintaining separate live background event streams.
