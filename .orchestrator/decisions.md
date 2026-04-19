# Locked Decisions

- Primary architecture source: `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md`
- Secondary authority for persistence, anti-patterns, acceptance criteria, and implementation discipline: `docs/CLI_ENGINE_EXECPLAN.md`
- Language target: Go
- Persistence target: SQLite plus JSONL
- Primary executor integration target: `codex app-server`
- Fallback executor integration target: `codex exec --json`
- v1 topology: single planner, single executor
- v1 required capabilities: durable loop, resume, `ntfy` bridge, terminal visibility, fixed hotkeys, bootstrap flow, `AGENTS.md` support
- v1 excludes orchestrator-managed multi-worker mode
