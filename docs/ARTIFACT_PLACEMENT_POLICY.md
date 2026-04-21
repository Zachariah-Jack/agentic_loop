# Artifact Placement Policy

Status: Current v1 artifact hygiene note

Orchestration-owned generated files should live under `.orchestrator/artifacts/`, not in the target repo root, unless the planner explicitly requests another path and the write scope clearly allows it.

Current artifact categories:
- `.orchestrator/artifacts/planner/` for planner validation and planner-side raw artifacts
- `.orchestrator/artifacts/executor/` for large executor-only summaries or output captures
- `.orchestrator/artifacts/context/` for large or truncated collected-context dumps
- `.orchestrator/artifacts/human/` for large raw human-reply captures
- `.orchestrator/artifacts/reports/` for orchestration-only reports and summaries

Policy notes:
- Inline previews in state and journal events stay small.
- When large content is externalized, journal events should carry the artifact path and a short preview.
- Actual app-building outputs may still be written in the target repo when the planner chooses that work and the write scope allows it.
- This policy applies to orchestration-generated analysis/reporting files, not to normal product code, tests, assets, or docs created as part of the user task itself.
