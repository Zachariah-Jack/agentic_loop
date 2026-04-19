# ADR-002: Canonical Repo Contract

Status: Accepted
Date: 2026-04-11

## Context

The repository needs a locked baseline so future implementation work does not re-open core platform and scope choices on every slice.

## Decision

The following decisions are locked for v1 unless superseded by a later ADR:

- Language target: Go
- Persistence target: SQLite plus JSONL
- Primary executor integration target: `codex app-server`
- Fallback executor integration target: `codex exec --json`
- v1 scope: single planner, single executor, durable loop, resume, `ntfy` bridge, terminal visibility, fixed hotkeys, bootstrap flow, `AGENTS.md` support
- No orchestrator-managed multi-worker mode in v1

## Consequences

- Early implementation should optimize for a single durable path instead of a generalized worker scheduler.
- Persistence and transport abstractions should preserve the locked integration targets.
- Any proposal to change these choices should be made through a new ADR instead of ad hoc drift.
