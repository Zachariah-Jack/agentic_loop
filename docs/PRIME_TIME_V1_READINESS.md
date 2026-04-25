# Prime-Time v1 Readiness

This document is the operator-facing readiness and release audit for the current prime-time v1 build.

## In Scope For Prime-Time v1

- `setup`, `init`, `run`, `auto`, `resume`, `continue`, `status`, `history`, and `doctor`
- single planner using the OpenAI Responses API
- single executor using `codex app-server`
- bounded `execute`, `collect_context`, `ask_human`, `pause`, and `complete` flows
- terminal ask-human plus `ntfy` ask-human with terminal fallback
- SQLite run state plus JSONL journal
- predictable artifact placement under `.orchestrator/artifacts/`
- target-repo scaffolding for real app-building work
- portable Windows release build and first installer path
- runtime timeout/profile settings, active-only Total Build Time status, Side Chat context answers, and GitHub Releases update-check/changelog foundation

## Explicitly Deferred

- hotkeys and live operator controls
- executor approval, interrupt, and steer flows
- fallback `codex exec --json` runtime path
- plugins, reviewer agents, and drift-watch agents
- orchestrator-managed multi-worker mode
- signed-binary distribution and automated release publishing
- safe self-install/update replacement for a running Windows binary
- final LLM-backed Side Chat tooling backend
- hidden background or daemonized autonomy

## Prerequisites

- Windows-friendly shell environment
- `OPENAI_API_KEY` available in the environment
- Codex installed and logged in
- target repo available locally
- optional `ntfy` server and topic if remote ask-human delivery is desired

## Setup Checklist

1. Run `orchestrator setup`.
2. Confirm `planner_api_key.environment: present`.
3. Confirm the saved planner model is what you want.
4. Configure `ntfy` only if needed.
5. Keep API keys out of config, chat logs, and commits.

## Target-Repo Init Checklist

1. Change into the target repo root.
2. Run `orchestrator init`.
3. Confirm these files exist:
   - `.orchestrator/brief.md`
   - `.orchestrator/roadmap.md`
   - `.orchestrator/decisions.md`
   - `.orchestrator/human-notes.md`
   - `.orchestrator/state/`
   - `.orchestrator/logs/`
   - `.orchestrator/artifacts/`
   - `AGENTS.md`
4. Fill in `brief.md`, `roadmap.md`, and `decisions.md` before the first real app build.

## Live Smoke Checklist

1. `orchestrator doctor`
2. `orchestrator status`
3. `orchestrator run --goal "..."` on a real target repo
4. `orchestrator auto start --goal "..."` to confirm foreground autonomous continuation
5. `orchestrator resume` if the run pauses after one bounded cycle
6. `orchestrator continue --max-cycles 3` or `orchestrator auto continue`
7. terminal `ask_human` reply path
8. `ntfy` ask-human reply path if configured
9. `orchestrator history`
10. inspect `.orchestrator/logs/events.jsonl`
11. inspect `.orchestrator/artifacts/`
12. `orchestrator settings show`
13. `orchestrator update status`

## Failure Triage Checklist

- `missing_required_config`
  - confirm `OPENAI_API_KEY`
  - rerun `orchestrator doctor`
- repo contract missing
  - run `orchestrator init`
  - fill required `.orchestrator/` files
- planner transport failure
  - inspect `status`, journal, and last error
- planner validation failure
  - inspect planner artifact under `.orchestrator/artifacts/planner/`
- executor failure
  - inspect `status`, journal, and executor artifact or preview
- `ntfy` issue
  - verify `setup` values
  - confirm terminal fallback still works

## Release Gate / Signoff Criteria

All of these should pass before calling the current build prime-time v1 ready:

- `go test ./...`
- `orchestrator setup`
- `orchestrator doctor`
- `orchestrator init` on a clean target repo
- one successful real `run`
- one successful real foreground `auto` invocation
- one successful `resume` or `continue`
- one terminal `ask_human` round trip
- one `ntfy` `ask_human` round trip if `ntfy` is configured
- one portable Windows release build
- one Windows installer build when Inno Setup is available
- `status` and `history` show truthful operator state
- artifacts stay under `.orchestrator/artifacts/`
- planner-owned `complete` marks the run completed mechanically

## Most Common Operator Commands

- `orchestrator setup`
- `orchestrator init`
- `orchestrator doctor`
- `orchestrator run --goal "..."`
- `orchestrator auto start --goal "..."`
- `orchestrator resume`
- `orchestrator continue --max-cycles 3`
- `orchestrator auto continue`
- `orchestrator status`
- `orchestrator history`
- `orchestrator settings show`
- `orchestrator update status`
