package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"orchestrator/internal/state"
)

func TestRunInitScaffoldsTargetRepoContract(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)

	var stdout bytes.Buffer
	err := runInit(context.Background(), Invocation{
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
	})
	if err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	requiredPaths := []string{
		filepath.Join(repoRoot, "AGENTS.md"),
		filepath.Join(repoRoot, ".orchestrator", "brief.md"),
		filepath.Join(repoRoot, ".orchestrator", "roadmap.md"),
		filepath.Join(repoRoot, ".orchestrator", "constraints.md"),
		filepath.Join(repoRoot, ".orchestrator", "decisions.md"),
		filepath.Join(repoRoot, ".orchestrator", "human-notes.md"),
		filepath.Join(repoRoot, ".orchestrator", "goal.md"),
		filepath.Join(repoRoot, ".orchestrator", "state"),
		filepath.Join(repoRoot, ".orchestrator", "logs"),
		filepath.Join(repoRoot, ".orchestrator", "artifacts"),
		layout.WorkersDir,
		layout.DBPath,
		layout.JournalPath,
	}
	for _, path := range requiredPaths {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected scaffold path %s: %v", path, statErr)
		}
	}
	if contract := inspectTargetRepoContract(repoRoot); !contract.Ready {
		t.Fatalf("repo contract should be ready after init, missing %#v", contract.Missing)
	}

	for _, want := range []string{
		"command: init",
		"scaffold.brief_md: created",
		"scaffold.roadmap_md: created",
		"scaffold.constraints_md: created",
		"scaffold.decisions_md: created",
		"scaffold.human_notes_md: created",
		"scaffold.goal_md: created",
		"scaffold.agents_md: created",
		"next_operator_action: fill_repo_contract",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("init output missing %q\n%s", want, stdout.String())
		}
	}

	brief := mustReadFile(t, filepath.Join(repoRoot, ".orchestrator", "brief.md"))
	for _, want := range []string{
		"## App Name",
		"## Success Criteria",
		"## Platform / Stack Constraints",
		"## Security / Privacy Constraints",
		"## Immediate Next Goal",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief.md missing %q\n%s", want, brief)
		}
	}

	roadmap := mustReadFile(t, filepath.Join(repoRoot, ".orchestrator", "roadmap.md"))
	for _, want := range []string{
		"## Mission",
		"## Phase 0: Repo Truth / Discovery",
		"## Phase 1: App Foundation",
		"## Phase 10: Final Review / Completion",
		"## Planner Guidance",
		"## Executor Guidance",
	} {
		if !strings.Contains(roadmap, want) {
			t.Fatalf("roadmap.md missing %q\n%s", want, roadmap)
		}
	}
	if count := strings.Count(roadmap, "### Acceptance Criteria"); count < 11 {
		t.Fatalf("roadmap.md has %d Acceptance Criteria sections, want at least 11", count)
	}

	constraints := mustReadFile(t, filepath.Join(repoRoot, ".orchestrator", "constraints.md"))
	for _, want := range []string{
		"## Technical Constraints",
		"## Business / Product Constraints",
		"## Security / Privacy Constraints",
		"## Workflow Constraints",
		"## Non-Goals",
	} {
		if !strings.Contains(constraints, want) {
			t.Fatalf("constraints.md missing %q\n%s", want, constraints)
		}
	}

	decisions := mustReadFile(t, filepath.Join(repoRoot, ".orchestrator", "decisions.md"))
	for _, want := range []string{
		"## Product Decisions",
		"## Architecture Decisions",
		"## Decision Log Format",
		"- Alternatives Considered:",
		"- Revisit Trigger:",
	} {
		if !strings.Contains(decisions, want) {
			t.Fatalf("decisions.md missing %q\n%s", want, decisions)
		}
	}

	humanNotes := mustReadFile(t, filepath.Join(repoRoot, ".orchestrator", "human-notes.md"))
	for _, want := range []string{
		"Append new notes at the bottom.",
		"YYYY-MM-DD HH:MM",
		"## Examples",
		"Append new notes below this line.",
	} {
		if !strings.Contains(humanNotes, want) {
			t.Fatalf("human-notes.md missing %q\n%s", want, humanNotes)
		}
	}

	goal := mustReadFile(t, filepath.Join(repoRoot, ".orchestrator", "goal.md"))
	for _, want := range []string{
		"## Current Goal",
		"## Acceptance Signal",
		"structured complete outcome",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("goal.md missing %q\n%s", want, goal)
		}
	}
}

func TestRunInitPreservesExistingFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)
	existingFiles := map[string]string{
		filepath.Join(repoRoot, ".orchestrator", "brief.md"):       "# Custom Brief\n\nKeep this exact text.\n",
		filepath.Join(repoRoot, ".orchestrator", "roadmap.md"):     "# Custom Roadmap\n\nDo not overwrite.\n",
		filepath.Join(repoRoot, ".orchestrator", "constraints.md"): "# Custom Constraints\n\nDo not overwrite.\n",
		filepath.Join(repoRoot, ".orchestrator", "decisions.md"):   "# Custom Decisions\n\nDo not overwrite.\n",
		filepath.Join(repoRoot, ".orchestrator", "human-notes.md"): "# Custom Human Notes\n\nDo not overwrite.\n",
		filepath.Join(repoRoot, ".orchestrator", "goal.md"):        "# Custom Goal\n\nDo not overwrite.\n",
		filepath.Join(repoRoot, "AGENTS.md"):                       "# Existing AGENTS\n",
	}
	for path, contents := range existingFiles {
		mustWriteFile(t, path, contents)
	}

	var stdout bytes.Buffer
	err := runInit(context.Background(), Invocation{
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
		RepoRoot: repoRoot,
		Layout:   layout,
	})
	if err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	for path, want := range existingFiles {
		if got := mustReadFile(t, path); got != want {
			t.Fatalf("%s was overwritten\n%s", path, got)
		}
	}

	for _, want := range []string{
		"scaffold.brief_md: preserved",
		"scaffold.roadmap_md: preserved",
		"scaffold.constraints_md: preserved",
		"scaffold.decisions_md: preserved",
		"scaffold.human_notes_md: preserved",
		"scaffold.goal_md: preserved",
		"scaffold.agents_md: preserved",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("init output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRepairSafeRepoContractDirsRestoresMissingArtifactsIdempotently(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)
	mustMkdirAll(t, filepath.Join(repoRoot, ".orchestrator", "state"))
	mustMkdirAll(t, filepath.Join(repoRoot, ".orchestrator", "logs"))

	first, err := repairSafeRepoContractDirs(layout)
	if err != nil {
		t.Fatalf("repairSafeRepoContractDirs() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".orchestrator", "artifacts")); err != nil {
		t.Fatalf("expected artifacts directory to be repaired: %v", err)
	}
	if len(first.Created) == 0 {
		t.Fatal("first repair should report created directories")
	}

	second, err := repairSafeRepoContractDirs(layout)
	if err != nil {
		t.Fatalf("second repairSafeRepoContractDirs() error = %v", err)
	}
	if len(second.Created) != 0 {
		t.Fatalf("second repair created %#v, want no-op", second.Created)
	}
}

func TestTargetRepoTemplatesIncludeAppBuildGuidance(t *testing.T) {
	t.Parallel()

	brief := targetRepoBriefTemplate()
	for _, want := range []string{
		"## App Name",
		"## Core User Problems",
		"## MVP Scope",
		"## Platform / Stack Constraints",
		"## Integration Requirements",
		"## Success Criteria",
		"## Human Notes for Planner",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief template missing %q", want)
		}
	}

	roadmap := targetRepoRoadmapTemplate()
	for _, want := range []string{
		"## Source of Truth / How to Use This Roadmap",
		"## Current Status Snapshot",
		"## Definition of Done",
		"## Phase 0: Repo Truth / Discovery",
		"## Phase 1: App Foundation",
		"## Phase 2: Core Domain Model",
		"## Phase 3: Primary User Flow",
		"## Phase 4: Persistence / Data / Integrations",
		"## Phase 5: UI / UX Completion",
		"## Phase 6: Validation / Testing",
		"## Phase 7: Error Handling / Edge Cases",
		"## Phase 8: Polish / Performance / Accessibility",
		"## Phase 9: Release Readiness",
		"## Phase 10: Final Review / Completion",
		"## Milestones",
		"## Backlog",
		"## Risks",
		"## Validation Checklist",
		"## Next Implementation Slice",
		"## Planner Guidance",
		"## Executor Guidance",
		"## Human Review Points",
	} {
		if !strings.Contains(roadmap, want) {
			t.Fatalf("roadmap template missing %q", want)
		}
	}
	for _, want := range []string{
		"### Objective",
		"### Why It Matters",
		"### Tasks",
		"### Acceptance Criteria",
		"### Validation",
		"### Notes",
	} {
		if count := strings.Count(roadmap, want); count < 11 {
			t.Fatalf("roadmap template has %d %q sections, want at least 11", count, want)
		}
	}

	constraints := targetRepoConstraintsTemplate()
	for _, want := range []string{
		"## Technical Constraints",
		"## Business / Product Constraints",
		"## Security / Privacy Constraints",
		"## UX / Accessibility Constraints",
		"## Workflow Constraints",
		"## Non-Goals",
	} {
		if !strings.Contains(constraints, want) {
			t.Fatalf("constraints template missing %q", want)
		}
	}

	decisions := targetRepoDecisionsTemplate()
	for _, want := range []string{
		"## Product Decisions",
		"## Technical Decisions",
		"## Architecture Decisions",
		"## UX Decisions",
		"## Workflow Decisions",
		"## Testing Decisions",
		"## Deferred Decisions",
		"## Decision Log Format",
		"- Date:",
		"- Decision:",
		"- Reason:",
		"- Alternatives Considered:",
		"- Impact:",
		"- Revisit Trigger:",
	} {
		if !strings.Contains(decisions, want) {
			t.Fatalf("decisions template missing %q", want)
		}
	}

	humanNotes := targetRepoHumanNotesTemplate()
	for _, want := range []string{
		"Append new notes at the bottom.",
		"YYYY-MM-DD HH:MM",
		"Preference:",
		"Scope:",
		"Constraint:",
		"Thing not to do:",
		"Temporary priority:",
		"Append new notes below this line.",
	} {
		if !strings.Contains(humanNotes, want) {
			t.Fatalf("human-notes template missing %q", want)
		}
	}

	goal := targetRepoGoalTemplate()
	for _, want := range []string{
		"## Current Goal",
		"## Acceptance Signal",
		"structured complete outcome",
	} {
		if !strings.Contains(goal, want) {
			t.Fatalf("goal template missing %q", want)
		}
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(contents)
}

func TestTargetRepoPreflightStopsCommandsWhenContractMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(context.Context, Invocation) error
		args []string
	}{
		{name: "run", run: runRun, args: []string{"--goal", "build something real"}},
		{name: "resume", run: runResume},
		{name: "continue", run: runContinue, args: []string{"--max-cycles", "2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			layout := state.ResolveLayout(repoRoot)

			var stdout bytes.Buffer
			err := tt.run(context.Background(), Invocation{
				Args:     tt.args,
				Stdout:   &stdout,
				Stderr:   &bytes.Buffer{},
				RepoRoot: repoRoot,
				Layout:   layout,
			})
			if err != nil {
				t.Fatalf("%s() error = %v", tt.name, err)
			}

			for _, want := range []string{
				"command: " + tt.name,
				"repo_contract.ready: false",
				"repo_contract.missing:",
				"next_operator_action: initialize_target_repo",
			} {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("%s output missing %q\n%s", tt.name, want, stdout.String())
				}
			}

			if _, statErr := os.Stat(layout.DBPath); !os.IsNotExist(statErr) {
				t.Fatalf("%s unexpectedly created runtime state at %s", tt.name, layout.DBPath)
			}
		})
	}
}
