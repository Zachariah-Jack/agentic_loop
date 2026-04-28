package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"orchestrator/internal/state"
)

type repoContractStatus struct {
	Ready   bool
	Missing []string
}

type repoContractEntry struct {
	label string
	path  string
	isDir bool
}

type repoContractRepairResult struct {
	Created []string
}

func targetRepoContractEntries(repoRoot string) []repoContractEntry {
	return []repoContractEntry{
		{label: "AGENTS.md", path: filepath.Join(repoRoot, "AGENTS.md")},
		{label: ".orchestrator/brief.md", path: filepath.Join(repoRoot, ".orchestrator", "brief.md")},
		{label: ".orchestrator/roadmap.md", path: filepath.Join(repoRoot, ".orchestrator", "roadmap.md")},
		{label: ".orchestrator/constraints.md", path: filepath.Join(repoRoot, ".orchestrator", "constraints.md")},
		{label: ".orchestrator/decisions.md", path: filepath.Join(repoRoot, ".orchestrator", "decisions.md")},
		{label: ".orchestrator/human-notes.md", path: filepath.Join(repoRoot, ".orchestrator", "human-notes.md")},
		{label: ".orchestrator/goal.md", path: filepath.Join(repoRoot, ".orchestrator", "goal.md")},
		{label: ".orchestrator/state", path: filepath.Join(repoRoot, ".orchestrator", "state"), isDir: true},
		{label: ".orchestrator/logs", path: filepath.Join(repoRoot, ".orchestrator", "logs"), isDir: true},
		{label: ".orchestrator/artifacts", path: filepath.Join(repoRoot, ".orchestrator", "artifacts"), isDir: true},
	}
}

func targetRepoRuntimeDirEntries(layout state.Layout) []repoContractEntry {
	return []repoContractEntry{
		{label: ".orchestrator", path: layout.RootDir, isDir: true},
		{label: ".orchestrator/state", path: layout.StateDir, isDir: true},
		{label: ".orchestrator/logs", path: layout.LogsDir, isDir: true},
		{label: ".orchestrator/artifacts", path: filepath.Join(layout.RootDir, "artifacts"), isDir: true},
		{label: filepath.Base(layout.WorkersDir), path: layout.WorkersDir, isDir: true},
	}
}

func inspectTargetRepoContract(repoRoot string) repoContractStatus {
	status := repoContractStatus{Ready: true}
	for _, item := range targetRepoContractEntries(repoRoot) {
		info, err := os.Stat(item.path)
		if err != nil {
			status.Ready = false
			status.Missing = append(status.Missing, item.label)
			continue
		}
		if item.isDir && !info.IsDir() {
			status.Ready = false
			status.Missing = append(status.Missing, item.label)
			continue
		}
		if !item.isDir && info.IsDir() {
			status.Ready = false
			status.Missing = append(status.Missing, item.label)
		}
	}
	return status
}

func repairSafeRepoContractDirs(layout state.Layout) (repoContractRepairResult, error) {
	result := repoContractRepairResult{}
	for _, item := range targetRepoRuntimeDirEntries(layout) {
		info, err := os.Stat(item.path)
		if err == nil {
			if !info.IsDir() {
				return result, fmt.Errorf("%s exists but is not a directory", item.label)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return result, err
		}
		if err := os.MkdirAll(item.path, 0o755); err != nil {
			return result, err
		}
		result.Created = append(result.Created, item.label)
	}
	return result, nil
}

func ensureTargetRepoContractDirs(layout state.Layout) error {
	_, err := repairSafeRepoContractDirs(layout)
	return err
}

func writeMissingRepoContractReport(stdout io.Writer, command string, repoRoot string, goal string, contract repoContractStatus) error {
	fmt.Fprintf(stdout, "command: %s\n", command)
	fmt.Fprintf(stdout, "run_id: unavailable\n")
	fmt.Fprintf(stdout, "goal: %s\n", valueOrUnavailable(goal))
	fmt.Fprintf(stdout, "run_action: unavailable\n")
	fmt.Fprintf(stdout, "status: unavailable\n")
	fmt.Fprintf(stdout, "stop_reason: unavailable\n")
	fmt.Fprintf(stdout, "repo_root: %s\n", repoRoot)
	fmt.Fprintln(stdout, "repo_contract.ready: false")
	fmt.Fprintf(stdout, "repo_contract.missing: %s\n", strings.Join(contract.Missing, ", "))
	fmt.Fprintln(stdout, "next_operator_action: initialize_target_repo")
	return nil
}
