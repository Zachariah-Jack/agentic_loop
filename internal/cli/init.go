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
		{label: "decisions_md", path: filepath.Join(repoRoot, ".orchestrator", "decisions.md"), content: targetRepoDecisionsTemplate()},
		{label: "human_notes_md", path: filepath.Join(repoRoot, ".orchestrator", "human-notes.md"), content: targetRepoHumanNotesTemplate()},
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

Describe the app in plain language before asking the planner or executor to build it.

## What This App Is
- One short paragraph describing the product.

## Target Users
- Who it is for.
- Why they need it.

## Constraints
- Platform, stack, performance, legal, or integration constraints.

## Success Criteria
- The minimum bar for calling the current phase successful.

## Current Focus
- What the next bounded implementation slice should improve.
`) + "\n"
}

func targetRepoRoadmapTemplate() string {
	return strings.TrimSpace(`
# App Roadmap

Keep this short, current, and product-focused.

## Now
- Define the first bounded slice to build.

## Next
- List the next few operator-visible milestones.

## Later
- Capture non-immediate work without over-planning it.

## Risks
- Note the current product or delivery risks worth watching.
`) + "\n"
}

func targetRepoDecisionsTemplate() string {
	return strings.TrimSpace(`
# Stable Decisions

Record decisions that should stay fixed unless intentionally changed.

## Product
- Decision:
  Reason:

## Technical
- Decision:
  Reason:

## Workflow
- Decision:
  Reason:
`) + "\n"
}

func targetRepoHumanNotesTemplate() string {
	return strings.TrimSpace(`
# Human Notes

Append new notes at the bottom.

## YYYY-MM-DD HH:MM
- note
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
3. .orchestrator/decisions.md
4. .orchestrator/human-notes.md

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
