# ADR-001: Primary Architecture Source

Status: Accepted
Date: 2026-04-11

## Context

The repository carries both an updated orchestrator CLI architecture spec and an older execution plan document. They overlap, but they do not serve the same role.

## Decision

`docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md` is the primary architecture source for this repository.

If a conflict exists between that document and `docs/CLI_ENGINE_EXECPLAN.md`, the updated spec wins for:

- planner versus executor versus CLI semantics,
- workflow authority,
- role boundaries,
- v1 architecture shape.

`docs/CLI_ENGINE_EXECPLAN.md` remains authoritative for:

- persistence expectations,
- anti-patterns,
- acceptance criteria,
- implementation discipline.

## Consequences

- Future contributors must consult the updated spec before making non-trivial changes.
- The older exec plan remains useful, but it cannot override the newer architecture.
- When drift is discovered, the fix is to align implementation and supporting docs with the updated spec.
