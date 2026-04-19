# Orchestrator Brief

Build a Go orchestrator that runs a durable planner-led loop around a Codex executor.

- Planner decides.
- Executor does the work.
- CLI stays inert and handles transport, persistence, and visibility.
- v1 is single-planner, single-executor with resume, `ntfy`, fixed hotkeys, terminal visibility, bootstrap flow, and `AGENTS.md` support.
