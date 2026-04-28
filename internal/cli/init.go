package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

type scaffoldItemResult struct {
	Label  string
	State  string
	Target string
}

func newInitCommand() Command {
	return Command{
		Name:    "init",
		Summary: "Scaffold the target-repo contract and runtime directories.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator init",
			"",
			"Scaffolds the target-repo contract under .orchestrator, preserves existing",
			"human-authored files, and ensures runtime directories and persistence exist.",
		),
		Run: runInit,
	}
}

func runInit(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newInitCommand().Description)
			return nil
		}
		return err
	}

	if err := ensureTargetRepoContractDirs(inv.Layout); err != nil {
		return err
	}

	scaffoldResults, err := scaffoldTargetRepoContract(inv.RepoRoot, inv.Layout)
	if err != nil {
		return err
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := journalWriter.Append(journal.Event{
		Type:     "runtime.initialized",
		RepoPath: inv.RepoRoot,
		Message:  "target repo scaffold and runtime surfaces ready",
	}); err != nil {
		return err
	}

	fmt.Fprintln(inv.Stdout, "command: init")
	fmt.Fprintf(inv.Stdout, "repo_root: %s\n", inv.RepoRoot)
	for _, item := range scaffoldResults {
		fmt.Fprintf(inv.Stdout, "scaffold.%s: %s\n", item.Label, item.State)
	}
	fmt.Fprintf(inv.Stdout, "runtime.state_dir: %s\n", inv.Layout.StateDir)
	fmt.Fprintf(inv.Stdout, "runtime.logs_dir: %s\n", inv.Layout.LogsDir)
	fmt.Fprintf(inv.Stdout, "runtime.artifacts_dir: %s\n", filepath.Join(inv.Layout.RootDir, "artifacts"))
	fmt.Fprintf(inv.Stdout, "runtime.workers_dir: %s\n", inv.Layout.WorkersDir)
	fmt.Fprintf(inv.Stdout, "runtime.state_db: %s\n", inv.Layout.DBPath)
	fmt.Fprintf(inv.Stdout, "runtime.event_journal: %s\n", inv.Layout.JournalPath)
	fmt.Fprintln(inv.Stdout, "next_operator_action: fill_repo_contract")
	return nil
}

func scaffoldTargetRepoContract(repoRoot string, layout state.Layout) ([]scaffoldItemResult, error) {
	items := []struct {
		label   string
		path    string
		content string
	}{
		{label: "brief_md", path: filepath.Join(repoRoot, ".orchestrator", "brief.md"), content: targetRepoBriefTemplate()},
		{label: "roadmap_md", path: filepath.Join(repoRoot, ".orchestrator", "roadmap.md"), content: targetRepoRoadmapTemplate()},
		{label: "constraints_md", path: filepath.Join(repoRoot, ".orchestrator", "constraints.md"), content: targetRepoConstraintsTemplate()},
		{label: "decisions_md", path: filepath.Join(repoRoot, ".orchestrator", "decisions.md"), content: targetRepoDecisionsTemplate()},
		{label: "human_notes_md", path: filepath.Join(repoRoot, ".orchestrator", "human-notes.md"), content: targetRepoHumanNotesTemplate()},
		{label: "goal_md", path: filepath.Join(repoRoot, ".orchestrator", "goal.md"), content: targetRepoGoalTemplate()},
		{label: "agents_md", path: filepath.Join(repoRoot, "AGENTS.md"), content: targetRepoAgentsTemplate()},
	}

	results := []scaffoldItemResult{
		{Label: "state_dir", State: "ready", Target: layout.StateDir},
		{Label: "logs_dir", State: "ready", Target: layout.LogsDir},
		{Label: "artifacts_dir", State: "ready", Target: filepath.Join(layout.RootDir, "artifacts")},
	}

	for _, item := range items {
		state, err := writeTemplateIfMissing(item.path, item.content)
		if err != nil {
			return nil, err
		}
		results = append(results, scaffoldItemResult{
			Label:  item.label,
			State:  state,
			Target: item.path,
		})
	}

	return results, nil
}
func writeTemplateIfMissing(path string, content string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return "preserved", nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return "created", nil
}

func targetRepoBriefTemplate() string {
	return strings.TrimSpace(`
# App Brief

Use this file as the concise product contract for the planner, executor, and
humans. Keep it current, specific, and generic enough to survive multiple build
slices.

## App Name
- Working name:
- Repository/package name:

## One-Sentence Summary
- In one sentence, what does this app do and for whom?

## Product Vision
- What should this become when it is useful and polished?
- What should users feel or accomplish after using it?

## Target Users
- Primary users:
- Secondary users:
- User skill level, environment, or accessibility needs:

## Core User Problems
- Problem 1:
- Problem 2:
- Problem 3:

## Primary Use Cases
- Use case 1:
- Use case 2:
- Use case 3:

## MVP Scope
- Must have:
- Should have:
- Nice later:

## Out of Scope
- What should not be built in the current phase?
- What should the planner avoid optimizing for right now?

## Platform / Stack Constraints
- Target platforms:
- Required language/framework/runtime:
- Existing services, libraries, or repo conventions:
- Deployment, packaging, or distribution expectations:

## Integration Requirements
- External APIs or services:
- Local files, devices, or OS integrations:
- Auth, secrets, or account requirements:
- Notifications, webhooks, imports, or exports:

## UX / Design Expectations
- Visual direction:
- Navigation model:
- Responsive layout expectations:
- Empty, loading, error, and success states:
- Accessibility expectations:

## Performance / Reliability Expectations
- Startup/load expectations:
- Runtime performance expectations:
- Offline, retry, and failure behavior:
- Data integrity expectations:

## Security / Privacy Constraints
- Secrets and credential handling:
- User data handling:
- Local/remote storage constraints:
- Logging and redaction requirements:

## Success Criteria
- User-visible outcomes:
- Technical outcomes:
- Validation evidence:

## Current Build Status
- What already exists:
- What is missing:
- Known blockers:

## Immediate Next Goal
- The next highest-value implementation slice:
- Why this should happen next:
- How the result should be validated:

## Human Notes for Planner
- Raw context, preferences, or steering notes for the planner:
- Anything the planner should ask before proceeding:
`) + "\n"
}

func targetRepoRoadmapTemplate() string {
	return strings.TrimSpace(`
# App Roadmap

Use this roadmap as the durable app-building plan. It should guide a serious
multi-phase build without forcing the planner into stale work. Update it when
facts change, decisions are made, or milestones are completed.

## Mission
- Build a useful, reliable app that satisfies the brief and can be validated by
  a human from observable behavior.
- Keep each implementation slice bounded, testable, and easy to review.

## Source of Truth / How to Use This Roadmap
- The planner uses this roadmap to choose the next highest-value bounded step.
- The executor uses the selected slice to make concrete code or artifact changes.
- The human updates this file when priorities, scope, or facts change.
- Prefer updating status and next actions over deleting useful historical context.
- If this roadmap conflicts with .orchestrator/decisions.md, decisions.md wins
  for locked decisions.

## Current Status Snapshot
- Current phase:
- Latest completed milestone:
- Current blocker:
- Current test/validation state:
- Current user-visible app behavior:
- Current technical debt worth tracking:

## Definition of Done
- The core use case works end-to-end for the target user.
- Required platform, stack, integration, security, and privacy constraints are met.
- Important empty, loading, error, and success states are handled.
- Tests or validation evidence cover the critical behavior.
- Documentation or in-app guidance is sufficient for a new user or maintainer.
- Known risks are resolved or intentionally accepted in decisions.md.

## Phase 0: Repo Truth / Discovery

### Objective
- Establish what already exists, what stack conventions matter, and where the
  safest first implementation slice should happen.

### Why It Matters
- Good discovery prevents the executor from rewriting working systems, choosing
  the wrong architecture, or missing existing test/build commands.

### Tasks
- Inspect repo structure, app entry points, build files, tests, and docs.
- Identify existing UI routes, services, data models, and configuration.
- Identify required commands for install, build, lint, test, and run.
- Record unknowns that require human input in human-notes.md or ask_human.

### Acceptance Criteria
- Repo structure and stack are understood well enough to pick a bounded slice.
- Existing build/test/run commands are known or explicitly marked unknown.
- Important constraints are reflected in brief.md or decisions.md.

### Validation
- Run the lightest safe discovery commands.
- Confirm no unrelated files were changed during discovery.

### Notes
- Do not keep rediscovering the same repo facts once they are recorded.

## Phase 1: App Foundation

### Objective
- Create or stabilize the minimal app shell, project structure, dependency setup,
  configuration loading, and developer workflow.

### Why It Matters
- Later feature work is faster and safer when the app starts reliably and has a
  clear place for UI, domain, data, and test code.

### Tasks
- Ensure the app can install dependencies and start locally.
- Establish routing/navigation or the equivalent app entry flow.
- Add baseline styling/theme structure if UI exists.
- Add environment/config handling without committing secrets.
- Add minimal smoke tests or run instructions.

### Acceptance Criteria
- A developer can run the app from a clean checkout with documented steps.
- The app shows a deliberate starting screen or equivalent entry behavior.
- Configuration failures are visible and understandable.

### Validation
- Run install/build/start or the closest available checks.
- Capture any missing dependency or environment assumptions.

### Notes
- Keep foundation work narrow; avoid polishing before the primary flow exists.

## Phase 2: Core Domain Model

### Objective
- Define the main entities, state transitions, business rules, and data shapes
  that the app needs to support its core use cases.

### Why It Matters
- A clear domain model keeps UI and integration work from becoming a pile of
  one-off state mutations.

### Tasks
- Identify core entities and relationships.
- Implement domain types, validation, reducers/services, or equivalent logic.
- Add representative sample data or fixtures where useful.
- Document important domain assumptions in decisions.md.

### Acceptance Criteria
- Core app state can represent the MVP use cases.
- Invalid states are rejected or handled predictably.
- Domain behavior can be exercised independently from the full UI when practical.

### Validation
- Add unit tests or targeted checks for critical domain behavior.
- Run existing tests that cover affected modules.

### Notes
- Prefer simple explicit models before adding persistence or remote integration.

## Phase 3: Primary User Flow

### Objective
- Build the main end-to-end path a user must complete for the app to be useful.

### Why It Matters
- The first complete flow creates real product feedback and exposes missing
  foundation, domain, UX, and validation needs.

### Tasks
- Implement the primary screens, commands, or interaction steps.
- Wire domain state to visible behavior.
- Add clear empty, loading, success, and failure states for the primary path.
- Preserve accessibility basics such as labels, keyboard flow, and contrast.

### Acceptance Criteria
- A target user can complete the primary use case without developer assistance.
- The app communicates what happened and what to do next.
- The flow handles expected bad input or missing data gracefully.

### Validation
- Manually exercise the primary flow.
- Add automated coverage for the riskiest path if the repo supports it.

### Notes
- Avoid expanding secondary features until this path is coherent.

## Phase 4: Persistence / Data / Integrations

### Objective
- Make app data durable or connected to required services while preserving clear
  failure modes and security boundaries.

### Why It Matters
- Real apps must survive reloads, network issues, validation errors, and service
  limits without confusing users or losing important data.

### Tasks
- Add local or remote persistence for MVP data.
- Integrate required APIs, SDKs, files, devices, or background jobs.
- Add migration, seed, retry, and conflict behavior where relevant.
- Keep secrets out of source control and logs.

### Acceptance Criteria
- Required data survives the expected lifecycle.
- Integration failures are surfaced in plain language.
- Secrets and sensitive values are not printed or committed.

### Validation
- Test persistence across reload/restart where applicable.
- Run integration smoke checks with safe test data.

### Notes
- If credentials are missing, ask the human rather than faking integration.

## Phase 5: UI / UX Completion

### Objective
- Turn the working flow into a clear, navigable, accessible, and visually
  intentional experience.

### Why It Matters
- Users judge product quality through clarity, feedback, responsiveness, and
  confidence, not only whether code paths execute.

### Tasks
- Complete layout, navigation, typography, visual hierarchy, and responsive states.
- Add helpful microcopy, confirmations, and recovery paths.
- Improve form states, validation messages, and affordances.
- Ensure important actions are obvious and destructive actions are guarded.

### Acceptance Criteria
- A new user can understand where they are, what changed, and what to do next.
- Layout works at target screen sizes.
- Important accessibility basics are satisfied.

### Validation
- Manual UI walkthrough on target viewport sizes.
- Run available UI, accessibility, or snapshot checks.

### Notes
- Preserve existing design systems when present; otherwise choose a clear direction.

## Phase 6: Validation / Testing

### Objective
- Build confidence that critical behavior works and regressions will be caught.

### Why It Matters
- Autonomous changes are only useful if the project can detect broken behavior
  before the user does.

### Tasks
- Identify critical user journeys, domain rules, and integration risks.
- Add or improve unit, integration, end-to-end, or smoke tests as appropriate.
- Make test commands documented and repeatable.
- Reduce flaky or overly broad tests that slow useful iteration.

### Acceptance Criteria
- Critical MVP behavior has a practical validation path.
- Test failures are actionable.
- The build/test commands needed for daily development are documented.

### Validation
- Run the relevant test suite.
- Record skipped or unavailable checks with reasons.

### Notes
- Prefer focused tests that protect product behavior over superficial coverage.

## Phase 7: Error Handling / Edge Cases

### Objective
- Handle realistic failure, boundary, permission, and recovery scenarios.

### Why It Matters
- Edge cases decide whether the app feels trustworthy under real conditions.

### Tasks
- List expected failures: invalid input, missing config, network failure, auth
  failure, empty data, concurrency, slow operations, permission errors.
- Add user-facing recovery paths.
- Add logging that helps debugging without exposing secrets.
- Ensure irreversible actions are explicit and recoverable where possible.

### Acceptance Criteria
- Common failure cases produce clear messages and safe states.
- The app avoids data loss in expected edge cases.
- Debug information is useful and redacted.

### Validation
- Manually trigger key error states or cover them with tests.
- Confirm logs do not include secrets.

### Notes
- Do not bury blockers; ask the human when only the human can provide missing info.

## Phase 8: Polish / Performance / Accessibility

### Objective
- Improve quality, speed, accessibility, and perceived craftsmanship after the
  core product is working.

### Why It Matters
- Polish should amplify a working product, not distract from incomplete flows.

### Tasks
- Optimize slow renders, requests, startup paths, or heavy operations.
- Improve keyboard navigation, focus states, semantics, contrast, and screen reader
  labels where relevant.
- Smooth rough visual edges and confusing transitions.
- Remove dead code, noisy logs, and accidental debug UI.

### Acceptance Criteria
- The app feels responsive for expected data sizes and devices.
- Accessibility basics are intentionally addressed.
- Visible rough edges from prior phases are cleaned up.

### Validation
- Run performance, accessibility, or manual checks appropriate to the stack.
- Re-run core user flow validation after polish changes.

### Notes
- Do not optimize speculative paths before measuring or observing pain.

## Phase 9: Release Readiness

### Objective
- Prepare the app for handoff, deployment, packaging, or real user evaluation.

### Why It Matters
- A finished feature still needs reliable setup, configuration, docs, and release
  safeguards.

### Tasks
- Verify build/package/deploy commands.
- Add or update README, setup notes, environment examples, and release notes.
- Confirm secrets, licenses, assets, and configuration are handled correctly.
- Prepare rollback, backup, or support instructions if relevant.

### Acceptance Criteria
- A maintainer can build and run the app using documented steps.
- Release artifacts or deployment steps are reproducible.
- Known limitations are documented honestly.

### Validation
- Run the release/build path from a clean state where practical.
- Check generated artifacts for expected contents.

### Notes
- Do not claim production readiness unless validation supports it.

## Phase 10: Final Review / Completion

### Objective
- Confirm that the requested objective is actually fulfilled, not merely
  understood or partially implemented.

### Why It Matters
- Completion should mean the user can inspect a real result and trust the stated
  status.

### Tasks
- Compare final behavior against brief.md, decisions.md, and this roadmap.
- Run final validation commands and manual checks.
- Summarize files changed, tests run, known limitations, and recommended next work.
- Ask the human for review if acceptance depends on taste, credentials, or external
  context.

### Acceptance Criteria
- The MVP or requested build slice meets its stated success criteria.
- Evidence exists for the claimed result.
- Remaining work is clearly separated from completed work.

### Validation
- Run the broadest relevant validation that is safe and available.
- Capture final screenshots, logs, or artifacts only when useful and non-secret.

### Notes
- The planner should not mark completion just because it has enough context.

## Milestones
- Milestone 1: App starts and shows a meaningful foundation.
- Milestone 2: Core domain model exists and is testable.
- Milestone 3: Primary user flow works end-to-end.
- Milestone 4: Data/integration behavior is durable enough for real use.
- Milestone 5: UX, validation, and release readiness meet the brief.

## Backlog
- High priority:
- Medium priority:
- Low priority:
- Ideas parked for later:

## Risks
- Product risks:
- Technical risks:
- Integration risks:
- UX/accessibility risks:
- Testing/release risks:
- Unknowns requiring human input:

## Validation Checklist
- App can install/build/run:
- Primary flow manually verified:
- Critical tests pass:
- Error states checked:
- Accessibility basics checked:
- Secrets/logging checked:
- Documentation updated:
- Human review completed where needed:

## Next Implementation Slice
- Slice title:
- Goal:
- Files/areas likely involved:
- Acceptance criteria:
- Validation command(s):
- Risk/rollback notes:

## Planner Guidance
- Prefer the next highest-value bounded implementation slice.
- Use collect_context for repo facts that can be gathered mechanically.
- Use ask_human only when blocked on knowledge or approval only the human can provide.
- Use execute when enough context exists to make real progress.
- Do not repeatedly rediscover facts already captured in this roadmap, brief.md, or
  decisions.md.
- Mark complete only when the requested objective is actually fulfilled and validated.

## Executor Guidance
- Stay inside the bounded task selected by the planner.
- Preserve existing architecture and repo conventions.
- Make minimal, reviewable changes that move the app toward acceptance criteria.
- Update tests or validation evidence for behavior changes.
- Report blockers, changed files, commands run, and residual risks.

## Human Review Points
- Product direction or scope changes:
- UX/design taste decisions:
- Credential or integration approvals:
- Release readiness approval:
- Final acceptance:
`) + "\n"
}

func targetRepoConstraintsTemplate() string {
	return strings.TrimSpace(`
# Constraints

Use this file for technical, product, business, security, privacy, platform,
and workflow guardrails that should shape planner decisions and executor work.
Keep constraints direct, testable where possible, and current.

## Technical Constraints
- Required language/framework/runtime:
- Required platforms:
- Integration limits:
- Performance or reliability expectations:

## Business / Product Constraints
- Scope boundaries:
- User or market assumptions:
- Release, budget, or timeline limits:

## Security / Privacy Constraints
- Secrets handling:
- Data storage / retention:
- Authentication / authorization:
- Compliance or policy requirements:

## UX / Accessibility Constraints
- Target device or input model:
- Accessibility requirements:
- Visual or brand guardrails:

## Workflow Constraints
- Required tests or checks:
- Files or areas to avoid without explicit approval:
- External services that require human approval:

## Non-Goals
- Do not build:
- Defer until:

## Open Constraint Questions
- Question:
`) + "\n"
}

func targetRepoDecisionsTemplate() string {
	return strings.TrimSpace(`
# Decisions

Record decisions that should stay fixed unless intentionally changed. This file
is for durable choices, not temporary notes. Move raw steering notes here only
after the human or planner decides they should become source-of-truth decisions.

## Product Decisions
- Decision:
- Reason:
- Impact:

## Technical Decisions
- Decision:
- Reason:
- Impact:

## Architecture Decisions
- Decision:
- Reason:
- Impact:

## UX Decisions
- Decision:
- Reason:
- Impact:

## Workflow Decisions
- Decision:
- Reason:
- Impact:

## Testing Decisions
- Decision:
- Reason:
- Impact:

## Deferred Decisions
- Decision to defer:
- Why deferred:
- Revisit trigger:

## Decision Log Format
Copy this format when recording a durable decision.

### YYYY-MM-DD - Short Decision Name
- Date:
- Decision:
- Reason:
- Alternatives Considered:
- Impact:
- Revisit Trigger:

## Decision Log
- Add new decisions here, newest or most relevant first.
`) + "\n"
}

func targetRepoGoalTemplate() string {
	return strings.TrimSpace(`
# Goal

Use this file as the saved starting objective for the next orchestrator run.
The GUI can edit and save this separately from the currently typed run goal.
Starting a run still sends an explicit goal string through the engine protocol.

## Current Goal
- Build the next highest-value slice from the repo contract files.

## Acceptance Signal
- The planner has declared completion through the structured complete outcome.

## Notes
- Update this before starting a new mission if the objective changed.
`) + "\n"
}

func targetRepoHumanNotesTemplate() string {
	return strings.TrimSpace(`
# Human Notes

This file is append-only human steering context. Append new notes at the bottom.
These notes are raw input for the planner and executor; they are not stable
source-of-truth decisions unless later moved to decisions.md.

## How to Use
- Append, do not rewrite, unless the human intentionally cleans up old notes.
- Use this for preferences, scope nudges, temporary priorities, warnings, and
  things not to do.
- If a note becomes a durable product, technical, UX, workflow, or testing choice,
  copy it into decisions.md.

## Format
YYYY-MM-DD HH:MM
- note

## Examples
2026-01-15 09:30
- Preference: Keep the first version keyboard-first and simple.

2026-01-15 10:15
- Scope: Do not add account sync until the local flow works.

2026-01-15 11:00
- Constraint: Avoid adding paid services unless the human approves.

2026-01-15 11:30
- Thing not to do: Do not rewrite the existing data layer without asking.

2026-01-15 12:00
- Temporary priority: Focus on the onboarding flow before polish.

## Notes
Append new notes below this line.
`) + "\n"
}

func targetRepoAgentsTemplate() string {
	return strings.TrimSpace(`
# AGENTS.md

This repo is being built through a planner-led orchestrator workflow.

## Read Before Non-Trivial Changes

Before making a multi-file or workflow-shaping change, read:
1. .orchestrator/brief.md
2. .orchestrator/roadmap.md
3. .orchestrator/constraints.md
4. .orchestrator/decisions.md
5. .orchestrator/human-notes.md
6. .orchestrator/goal.md

## Working Rules

- Keep diffs tight, real, and reviewable.
- Prefer bounded implementation slices with explicit acceptance criteria.
- Preserve human-authored product context in .orchestrator/.
- Run the narrowest relevant tests after changes.
- Put orchestration-owned artifacts under .orchestrator/artifacts/.
- Do not write orchestration summaries or repo-analysis files into repo root unless the task explicitly requires it.
- Respect existing repo conventions before introducing new ones.
`) + "\n"
}
