# Agentic Loop / Orchestrator

This repository is building a Go orchestrator around a planner-led execution loop.

- The planner will make workflow decisions.
- Codex will act as the executor.
- The CLI is an inert bridge and runtime harness, not the brain.

The current slice provides the first real CLI shell, config bootstrap, and logging skeleton. Core planner logic, executor integration, and persistence are intentionally deferred.
