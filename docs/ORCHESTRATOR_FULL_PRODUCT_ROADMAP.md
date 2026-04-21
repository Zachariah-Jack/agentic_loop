# Orchestrator CLI — Full Product Roadmap and Drift Guard

**Version:** 2026-04-20 master roadmap  
**Purpose:** keep the project, ChatGPT planner, Codex executor, and human operator aligned through the complete build.  
**Source priority:** this roadmap is an active build guide. It does **not** override the architecture contract, non-negotiables, or ADRs. If conflicts arise, use the source priority below.

## 0. Source-of-truth order

1. `docs/ORCHESTRATOR_CLI_UPDATED_SPEC.md` — primary architecture and role boundaries.
2. `docs/ORCHESTRATOR_NON_NEGOTIABLES.md` — hard rules.
3. ADRs in `docs/architecture/` — locked decisions.
4. This file — active full product roadmap and remaining build plan.
5. `docs/CLI_ENGINE_EXECPLAN.md` — implementation discipline, persistence expectations, anti-patterns, and acceptance criteria.
6. `.orchestrator/decisions.md` — stable project decisions.
7. `.orchestrator/human-notes.md` — append-only human context.

## 1. Product definition

The final product is a Windows-friendly orchestration CLI that coordinates:

- **Planner**: OpenAI Responses API model. Owns decisions, strategy, completion, human questions, and next-step selection.
- **Executor**: Codex running locally in the target repo. Owns implementation work, repo inspection, commands, code edits, and tests.
- **Orchestrator CLI**: inert bridge/runtime harness. Owns transport, state, persistence, visibility, notifications, waits, resume, and operator controls.

The CLI must never become the brain. It validates structured payloads, routes explicit outcomes, persists events, waits when told, and exposes status. It must not infer meaning, judge quality, invent completion, or interpret human replies.

## 2. Non-negotiable invariants

These must survive every implementation chunk.

1. CLI is inert and not a brain.
2. Planner decides what happens next.
3. Executor performs coding/repo work.
4. Planner control must be structured and validated, not prose-parsed.
5. Human replies are persisted and forwarded raw.
6. Safe pause points occur after AI turns and durable checkpoints.
7. Tool/transport/process errors become data; the CLI does not semantically fail the project.
8. `complete` means planner-declared completion only.
9. `continue`/loops stop only on explicit mechanical stop reasons.
10. Parallelism requires isolation, clear boundaries, and planned integration.
11. Visibility and ease of use are product requirements.
12. Secrets must not be committed or printed in full.

## 3. Current implementation state

### Completed foundation

- Repo documentation contract and source priority established.
- Go CLI scaffold exists.
- Commands exist for setup, doctor, init, run, resume, continue, status, history, version, and executor probe.
- SQLite state and JSONL journal exist.
- `planner.v1` contract exists with structured outcomes:
  - `execute`
  - `collect_context`
  - `ask_human`
  - `pause`
  - `complete`
- Live planner client uses the Responses API and persists `previous_response_id`.
- Planner instructions are resent every turn.
- Primary executor adapter uses `codex app-server`.
- Executor thread/turn metadata is durably persisted.
- Bounded cycles exist:
  - `planner -> collect_context -> planner -> stop`
  - `planner -> execute -> executor -> planner -> stop`
  - `planner -> ask_human -> terminal/ntfy reply -> planner -> stop`
  - `planner -> complete -> completed run -> stop`
- `resume` continues the latest unfinished run by one bounded cycle.
- `continue` advances the latest unfinished run through repeated bounded cycles until explicit stop conditions.
- Terminal ask-human and ntfy ask-human paths exist, with terminal fallback.
- `setup`, `doctor`, `status`, and `history` have real v1 behavior.
- Planner-owned completion is persisted mechanically.

### Current maturity assessment

- **Core architecture:** done.
- **Usable v1 core:** mostly done.
- **Prime-time readiness:** needs hardening, E2E confidence, artifact hygiene, operator polish, and release packaging.
- **Full final product:** still requires hotkeys, richer visibility, plugins, reviewer/drift agents, stronger executor controls, installer, and parallel worker features.

## 4. Drift audit

### Green — on track

- Planner/executor/CLI split is preserved.
- Current bounded outcomes are structured and validated.
- Completion is planner-owned.
- Human replies are raw.
- State is durable and resumable.
- App-server is the primary executor path.
- ntfy is integrated as the async human bridge with terminal fallback.

### Yellow — must tighten before prime time

- Failure handling and stop reason standardization need a final hardening pass.
- Root-level/generated artifacts need better placement under `.orchestrator/artifacts/` or equivalent.
- Operator output should consistently state stop reason and next safe action.
- E2E confidence tests need broader branch coverage.
- Executor approval, interrupt, and steering are not fully handled.
- `pause` outcome is in the contract but not yet a complete operator workflow.

### Red — not final-product complete yet

- Hotkeys and live operator controls are not built.
- Plugin/module system is not built.
- Drift watcher/reviewer subagents are not built.
- Multi-worker orchestration is not built.
- Installer and clean Windows package are not built.
- App-building templates and target-repo onboarding are not fully productized.

## 5. Build tracks

This roadmap has two nested completion targets.

### Target A — Prime-time single-machine v1

Enough to start using the orchestrator to build apps with a controlled local run loop.

Must include:
- hardened bounded and continue flows
- trustworthy stop reasons
- E2E confidence tests
- artifact hygiene
- setup/doctor/status/history confidence
- target-app onboarding templates
- release notes and runbook

### Target B — Full final product

Complete product vision including:
- live operator controls/hotkeys
- richer visibility modes
- Codex approval/interrupt/steer handling
- plugin system
- reviewer/drift subagents
- Codex subagent prompting patterns
- orchestrator-managed multi-worker mode with worktree isolation
- installer/package/distribution
- final E2E validation matrix

## 6. Remaining roadmap overview

| Phase | Status | Outcome |
|---|---:|---|
| Phase 0: Source-of-truth cleanup | Next | Prevent memory drift and repo drift |
| Phase 1: Hardening and stop reasons | Next | Reliable failure behavior |
| Phase 2: E2E confidence tests | Next | Prove current v1 branches |
| Phase 3: Artifact/write-scope hygiene | Next | Keep target repos clean |
| Phase 4: Operator output polish | Next | Clear status, stop reasons, next action |
| Phase 5: Prime-time app-building workflow | Next | Use on real target apps |
| Phase 6: Pause/verbosity/hotkeys | Later | Better live control |
| Phase 7: Executor controls | Later | Approval, interrupt, steering |
| Phase 8: Reviewer/drift agents | Later | Quality and roadmap alignment |
| Phase 9: Plugin/module system | Later | Extensibility |
| Phase 10: Parallelism | Later | Codex subagents and multi-worker mode |
| Phase 11: Packaging/install | Later | Normal-user installable product |
| Phase 12: Final validation/release | Later | Full product release |

## 7. Detailed phase plan

---

# Phase 0 — Source-of-truth cleanup and roadmap installation

## Goal
Install this roadmap into the repo and update project docs so future ChatGPT/Codex sessions do not drift.

## Files
- `docs/ORCHESTRATOR_FULL_PRODUCT_ROADMAP.md`
- `.orchestrator/roadmap.md`
- `.orchestrator/decisions.md`
- `AGENTS.md`
- `NEW_CHAT_HANDOFF_PROMPT.md`
- optional: `docs/architecture/ADR-007-full-roadmap-and-v1-readiness.md`

## Codex steps
1. Add this roadmap to `docs/ORCHESTRATOR_FULL_PRODUCT_ROADMAP.md`.
2. Replace or update `.orchestrator/roadmap.md` with a short active-status summary pointing to this roadmap.
3. Add ADR-007 locking this roadmap as the active full-product roadmap, subordinate to updated spec/non-negotiables/ADRs.
4. Update `AGENTS.md` to require reading this roadmap before multi-file changes.
5. Update `NEW_CHAT_HANDOFF_PROMPT.md` to include this roadmap in the required source list.
6. Review root-level generated files like `summary_of_contracts_and_prior_runs.md`; move them to `.orchestrator/artifacts/` or delete if obsolete.

## Human actions
- Download this file and upload it to project sources/settings.
- Give Codex the prompt to install/update repo files.
- Commit the source-of-truth update.

## Acceptance
- Roadmap is in repo and project sources.
- Future sessions know the current status and remaining plan.
- Generated run artifacts are not mistaken for source-of-truth docs.

---

# Phase 1 — Hardening and stop reasons

## Goal
Make bounded and continue flows reliable under expected failure cases.

## Files
- `internal/orchestration/cycle.go`
- `internal/cli/cycle.go`
- `internal/cli/continue.go`
- `internal/state/store.go`
- `internal/journal/journal.go`
- tests in `internal/orchestration` and `internal/cli`

## Codex steps
1. Standardize stop reasons:
   - `planner_complete`
   - `planner_ask_human`
   - `planner_pause`
   - `max_cycles_reached`
   - `transport_or_process_error`
   - `planner_validation_failed`
   - `missing_required_config`
   - `executor_failed`
   - `ntfy_failed_terminal_fallback_used`
2. Persist stop reason on run/cycle result where appropriate.
3. Ensure no fake safe-pause checkpoint is written for incomplete executor/planner work.
4. Ensure these failures create journal events and clear output:
   - missing OpenAI key
   - planner HTTP error
   - invalid planner output
   - executor timeout/failure
   - context read/list failure
   - ntfy failure with fallback
   - terminal input failure
5. Ensure status/history show latest failure truthfully without semantic judgment.

## Human actions
- None by default. Codex should run tests.
- Human may need to perform one manual network/auth smoke test if Codex cannot access the same environment.

## Acceptance
- Tests cover common failure surfaces.
- Operator output includes mechanical stop reason.
- Resume/continue do not duplicate side-effectful work after failure.

---

# Phase 2 — End-to-end confidence tests

## Goal
Prove the current v1 branches with fast local fake/mocked tests and a small optional live smoke suite.

## Files
- `internal/e2e/` or `test/e2e/`
- test helpers/fakes for planner, executor, ntfy, terminal input
- optional `scripts/smoke.ps1`

## Codex steps
1. Add fake planner client that returns controlled outcome sequences.
2. Add fake executor adapter that records prompt and returns controlled result.
3. Add fake human input and fake ntfy failure path.
4. Cover:
   - `run` with first-turn `complete`
   - `run` with `collect_context -> complete`
   - `run` with `ask_human -> complete`
   - `run` with `execute -> complete`
   - `resume` on unfinished run
   - `continue` stopping on complete
   - `continue` stopping at max cycles
   - missing API key
   - invalid planner output
   - executor failure
   - ntfy failure -> terminal fallback
5. Add optional manual smoke script for live planner/executor/ntfy.

## Human actions
- Run optional live smoke script if Codex cannot exercise local auth/ntfy/Codex reliably.
- Confirm live ntfy reply from phone/browser if configured.

## Acceptance
- `go test ./...` covers all core v1 branches.
- Manual live smoke checklist exists.

---

# Phase 3 — Artifact and write-scope hygiene

## Goal
Keep target repositories clean and make generated outputs easy to inspect.

## Files
- `.orchestrator/artifacts/`
- artifact manager/helper package if needed
- planner template/guidance
- executor prompt template/instructions
- `AGENTS.md`

## Codex steps
1. Define artifact locations:
   - `.orchestrator/artifacts/planner/`
   - `.orchestrator/artifacts/executor/`
   - `.orchestrator/artifacts/context/`
   - `.orchestrator/artifacts/human/`
   - `.orchestrator/artifacts/reports/`
2. Route large context previews, executor summaries, logs, and analysis files there by default.
3. Update planner/executor guidance so run-analysis files are not written to repo root unless explicitly requested by planner and permitted by write scope.
4. Add status/history references to key artifacts.
5. Add cleanup/archive policy for old run artifacts.

## Human actions
- Decide whether generated orchestration artifacts should be committed or gitignored in target repos.
- Approve repo-specific artifact policy.

## Acceptance
- No accidental root-level analysis files.
- Artifacts are structured by run.
- Planner receives artifact paths as data.

---

# Phase 4 — Operator output polish

## Goal
Make the CLI pleasant and unambiguous during real use.

## Files
- `internal/cli/status.go`
- `internal/cli/history.go`
- `internal/cli/continue.go`
- `internal/cli/run.go`
- `internal/cli/resume.go`
- `internal/terminal/` if introduced

## Codex steps
1. Normalize output shape for run/resume/continue:
   - command
   - run id
   - cycle number
   - first outcome
   - branch taken
   - second outcome if any
   - stop reason
   - latest checkpoint
   - next suggested operator command
2. Add verbosity levels:
   - quiet
   - normal
   - verbose
   - trace
3. Ensure trace can show raw envelopes/artifact refs without flooding normal output.
4. Make help text accurate and short.
5. Add examples to README.

## Human actions
- Manually inspect output and approve wording.

## Acceptance
- User always knows why a command stopped and what to do next.
- Normal output is readable.
- Verbose/trace exposes enough debug detail.

---

# Phase 5 — Prime-time app-building workflow

## Goal
Make the orchestrator usable for building real target apps, not just itself.

## Files
- target-repo scaffolding templates
- `.orchestrator/brief.md`
- `.orchestrator/roadmap.md`
- `.orchestrator/decisions.md`
- `AGENTS.md` template
- README/runbook

## Codex steps
1. Improve `init` or add `project-init` behavior for target repos.
2. Generate target app docs:
   - project brief
   - roadmap
   - constraints
   - decisions
   - human notes
   - AGENTS.md
3. Add target repo validation before `run`:
   - git repo present
   - contract files present
   - Codex can see repo
   - state/log dirs writable
4. Add a practical “build an app” runbook:
   - configure env
   - run setup
   - init target repo
   - fill brief/roadmap
   - run/continue
   - answer ask-human
   - inspect status/history
   - stop/resume
5. Add example app specs and expected workflow.

## Human actions
- Provide at least one real target app brief.
- Run first real target app smoke build.
- Decide default planner model and executor model/provider.

## Acceptance
- A clean target repo can be initialized and worked by the orchestrator.
- User can start building an app using documented commands.

---

# Phase 6 — Pause, verbosity, and hotkeys

## Goal
Add live operator control without making the CLI semantic.

## Files
- `internal/operator/`
- `internal/terminal/`
- `internal/cli/continue.go`
- executor adapter for interrupt/kill/steer if needed

## Codex steps
1. Implement fixed operator controls:
   - pause at next safe point
   - graceful stop after current AI turn
   - print status snapshot
   - cycle verbosity
   - inject human note
   - emergency kill current executor turn
2. Ensure pause/stop are safe-point flags, not semantic decisions.
3. Persist queued operator notes raw.
4. Route injected notes mechanically:
   - to planner on next planner turn
   - to executor steer only if steering is explicitly invoked and supported
5. Add tests for state transitions and key routing.

## Human actions
- Manually test hotkeys in Windows terminal/Codex terminal.
- Approve final key combinations if defaults conflict.

## Acceptance
- Operator can safely pause/stop/inject/status without corrupting runs.
- CLI remains inert.

---

# Phase 7 — Executor controls and fallback transport

## Goal
Make executor integration robust enough for long coding sessions.

## Files
- `internal/executor/appserver/`
- `internal/executor/codexexec/`
- executor interface/types
- state/journal updates

## Codex steps
1. Handle app-server approval requests explicitly:
   - surface to human/planner as data
   - support manual approval routing where appropriate
2. Implement turn interrupt/kill if app-server supports it.
3. Implement steer-turn path as explicit operator/planner action.
4. Add fallback `codex exec --json` adapter behind same interface.
5. Add health check choosing app-server primary and exec fallback.
6. Persist executor session/thread/turn state consistently across both transports.

## Human actions
- Test with a task that triggers Codex approval.
- Confirm Codex login/auth health.
- Confirm fallback behavior if app-server unavailable.

## Acceptance
- App-server remains primary.
- Exec fallback is real and machine-readable.
- Approval/interrupt/steer are explicit, not hidden.

---

# Phase 8 — Reviewer and drift-watch agents

## Goal
Improve output quality without making the CLI a judge.

## Files
- `internal/review/`
- planner outcome additions only if necessary via new ADR/contract version
- reviewer prompt templates
- artifacts for reviewer outputs

## Codex steps
1. Add reviewer request/response abstraction.
2. Add drift watcher:
   - compares current intended move to roadmap/decisions/history
   - returns critique as data to planner
3. Add diff reviewer:
   - reviews executor diffs or changed-file summaries
4. Add test-output explainer:
   - summarizes failed tests into planner-consumable data
5. Add optional prompt refiner:
   - improves executor prompts before dispatch if planner explicitly asks
6. Ensure reviewers never directly stop or override planner.

## Human actions
- Approve reviewer default model choices and cost/performance tradeoffs.
- Inspect sample reviewer outputs.

## Acceptance
- Planner can request reviewer data.
- Reviewer outputs are persisted as artifacts/data.
- CLI does not decide based on reviewer output.

---

# Phase 9 — Plugin/module system

## Goal
Allow extensions without reopening the engine/brain split.

## Files
- `internal/plugins/`
- `internal/hooks/`
- `schemas/plugin/`
- sample plugin

## Codex steps
1. Define plugin manifest format.
2. Support plugin registration for tools/actions/hooks.
3. Add hook points:
   - run start/end
   - planner before/after
   - executor before/after
   - wait start/end
   - fault recorded
4. Ensure plugin failures become structured data/events.
5. Add sample plugin.
6. Add doctor checks for plugin load failures.

## Human actions
- Decide default plugin directory and trust policy.
- Review security implications before enabling arbitrary plugin execution.

## Acceptance
- Sample plugin works.
- Plugin cannot secretly define completion semantics.

---

# Phase 10 — Parallelism and integration

## Goal
Support parallel execution safely.

## Files
- `internal/workers/`
- `internal/integration/`
- planner templates/outcome contract updates if needed
- worktree management helpers

## Codex steps
1. First support Codex subagent prompting patterns inside a single executor task.
2. Add concurrency spike for multiple local Codex app-server sessions under current auth.
3. Add worker registry.
4. Add strict boundaries:
   - separate Git worktrees, or
   - explicit non-overlapping paths
5. Add result aggregation.
6. Add integration worker/merge coordinator.
7. Add conflict routing back to planner.
8. Add tests/spikes for collision prevention.

## Human actions
- Run local concurrency/auth spike.
- Approve worktree location and cleanup policy.
- Manually review first multi-worker merge.

## Acceptance
- Multiple workers do not write the same tree blindly.
- Planner owns task partitioning and integration decisions.

---

# Phase 11 — Packaging, install, and Windows readiness

## Goal
Make the product installable and usable outside the development repo.

## Files
- `install/windows/`
- build scripts
- release scripts
- README/user docs
- config migration docs

## Codex steps
1. Add version metadata.
2. Build release `.exe` script.
3. Add portable zip packaging.
4. Add installer path with Inno Setup or WiX.
5. Add config migration/versioning.
6. Add Windows native and WSL mode diagnostics.
7. Add setup wizard improvements:
   - detect Git
   - detect Go only for development mode
   - detect Codex
   - detect Codex auth
   - detect ntfy config
   - detect OpenAI env key
8. Add uninstall/cleanup documentation.

## Human actions
- Install packaging tooling if required.
- Run installer on a clean Windows profile/VM if available.
- Verify Codex login in packaged environment.

## Acceptance
- Fresh Windows user can install and run setup/doctor.
- No development toolchain required to run the built binary.

---

# Phase 12 — Final validation and release gate

## Goal
Prove the full product before relying on it for serious app builds.

## Validation matrix

1. Fresh repo initialization.
2. Setup and doctor green path.
3. Live planner call.
4. Live Codex app-server execution.
5. `collect_context` path.
6. `execute` path.
7. `ask_human` terminal path.
8. `ask_human` ntfy path.
9. Planner `complete` path.
10. Resume after process stop.
11. Continue to completion.
12. Executor failure surfaced mechanically.
13. Invalid planner output rejected.
14. Missing API key handled.
15. Artifact placement clean.
16. Status/history useful.
17. Optional hotkeys/manual interruption.
18. Optional plugin smoke.
19. Optional reviewer smoke.
20. Optional multi-worker smoke.

## Human actions
- Run final smoke script with live credentials.
- Build one real small app end-to-end.
- Review logs/artifacts.
- Tag release.

## Acceptance
- All must-pass v1 matrix items pass.
- Deferred features are explicitly documented.
- Release tag exists.

## 8. Codex chunk sequence from current state

Use this exact order unless a failure requires a fix slice.

1. Install roadmap into repo and update source-of-truth files.
2. Hardening and stop reasons.
3. End-to-end confidence tests.
4. Artifact/write-scope hygiene.
5. Operator output polish.
6. Prime-time app-building workflow/templates.
7. Final prime-time v1 audit and release notes.
8. Hotkeys and live controls.
9. Executor approval/interrupt/steer + fallback `codex exec --json`.
10. Reviewer/drift agents.
11. Plugin system.
12. Parallelism/concurrency spike.
13. Multi-worker worktree mode.
14. Packaging/installer.
15. Full final validation/release gate.

## 9. Human action checklist

These are the places where the human operator likely must act.

### Credentials and config
- Create/rotate OpenAI API key if exposed.
- Set `OPENAI_API_KEY` in Windows user environment.
- Run `orchestrator setup` to configure planner model and ntfy settings.
- Never paste API keys into chat or commit them.

### ntfy
- Choose ntfy server and topic.
- If using auth, provide token during setup.
- Test phone/browser reply once.

### Codex
- Ensure Codex is installed.
- Ensure Codex is logged in.
- Approve any Codex auth or permission prompts.
- Manually verify app-server health if doctor reports trouble.

### Repo/project setup
- Provide real app project brief.
- Provide roadmap/constraints/decisions for target app.
- Approve generated `AGENTS.md` for target repo.
- Decide whether `.orchestrator/artifacts` should be committed or ignored.

### Terminal/manual tests
- Run live smoke scripts when Codex cannot verify external credentials/devices.
- Test hotkeys manually once implemented.
- Test installer/portable binary on a clean machine/profile.
- Review first real app build output before trusting unattended continues.

### Release
- Commit after major green chunks.
- Tag v1 release after validation matrix passes.
- Upload/update project source files after roadmap/doc changes.

## 10. Definition of done

### Prime-time v1 done

- `setup`, `doctor`, `status`, `history`, `run`, `resume`, and `continue` are reliable.
- Planner live calls work.
- Executor live turns work.
- `execute`, `collect_context`, `ask_human`, and `complete` branches work.
- ntfy ask-human works with terminal fallback.
- State and journal are durable.
- Stop reasons are mechanical and visible.
- Failure cases are hardened.
- Artifacts are organized.
- Target app repo can be initialized and used.
- One real app build smoke test succeeds.

### Full final product done

- Prime-time v1 done.
- Hotkeys and live controls done.
- Executor approval/interrupt/steer/fallback done.
- Reviewer/drift agents done.
- Plugin system done.
- Parallel worker mode done with safe isolation.
- Windows packaging/installer done.
- Full validation matrix passes.

## 11. Immediate next Codex prompt after this file is installed

After installing this roadmap, the immediate next implementation chunk should be:

**Phase 1 — Hardening and stop reasons.**

Do not move to plugins, hotkeys, parallelism, or installer until hardening and E2E confidence are complete.
