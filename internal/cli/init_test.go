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
		filepath.Join(repoRoot, ".orchestrator", "decisions.md"),
		filepath.Join(repoRoot, ".orchestrator", "human-notes.md"),
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

	for _, want := range []string{
		"command: init",
		"scaffold.brief_md: created",
		"scaffold.roadmap_md: created",
		"scaffold.decisions_md: created",
		"scaffold.human_notes_md: created",
		"scaffold.agents_md: created",
		"next_operator_action: fill_repo_contract",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("init output missing %q\n%s", want, stdout.String())
		}
	}
}

func TestRunInitPreservesExistingFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	layout := state.ResolveLayout(repoRoot)
	customBrief := "# Custom Brief\n\nKeep this exact text.\n"
	mustWriteFile(t, filepath.Join(repoRoot, ".orchestrator", "brief.md"), customBrief)
	mustWriteFile(t, filepath.Join(repoRoot, "AGENTS.md"), "# Existing AGENTS\n")

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

	briefContents, err := os.ReadFile(filepath.Join(repoRoot, ".orchestrator", "brief.md"))
	if err != nil {
		t.Fatalf("ReadFile(brief.md) error = %v", err)
	}
	if string(briefContents) != customBrief {
		t.Fatalf("brief.md was overwritten\n%s", string(briefContents))
	}

	for _, want := range []string{
		"scaffold.brief_md: preserved",
		"scaffold.agents_md: preserved",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("init output missing %q\n%s", want, stdout.String())
		}
	}
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
