# AGENTS.md

This repo is being built through the orchestrator.

## Read Before Non-Trivial Changes
1. `.orchestrator/brief.md`
2. `.orchestrator/roadmap.md`
3. `.orchestrator/decisions.md`
4. `.orchestrator/human-notes.md`

## Working Rules
- Keep diffs tight and bounded.
- Prefer real vertical slices over placeholder scaffolding.
- Run the narrowest relevant tests after changes.
- Keep orchestration artifacts under `.orchestrator/artifacts/`.
- Do not write orchestration-only summaries into repo root unless the task explicitly asks for that path.
