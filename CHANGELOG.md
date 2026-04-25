# Changelog

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
