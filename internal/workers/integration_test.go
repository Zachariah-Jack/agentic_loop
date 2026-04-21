package workers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"orchestrator/internal/state"
)

func TestBuildIntegrationSummaryCollectsWorkerOutputsAndConflicts(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeIntegrationFile(t, filepath.Join(repoRoot, "src", "shared.txt"), "base shared\n")
	writeIntegrationFile(t, filepath.Join(repoRoot, "src", "main.txt"), "base main\n")

	workerOnePath := filepath.Join(t.TempDir(), "worker-one")
	workerTwoPath := filepath.Join(t.TempDir(), "worker-two")
	writeIntegrationFile(t, filepath.Join(workerOnePath, "src", "shared.txt"), "worker one shared\n")
	writeIntegrationFile(t, filepath.Join(workerOnePath, "src", "ui.txt"), "worker one ui\n")
	writeIntegrationFile(t, filepath.Join(workerOnePath, ".orchestrator", "artifacts", "ignore.txt"), "ignore me\n")
	writeIntegrationFile(t, filepath.Join(workerTwoPath, "src", "shared.txt"), "worker two shared\n")
	writeIntegrationFile(t, filepath.Join(workerTwoPath, "src", "api.txt"), "worker two api\n")

	summary, err := BuildIntegrationSummary(repoRoot, []state.Worker{
		{
			ID:                  "worker_one",
			WorkerName:          "worker-one",
			WorktreePath:        workerOnePath,
			WorkerResultSummary: "ui slice complete",
		},
		{
			ID:                  "worker_two",
			WorkerName:          "worker-two",
			WorktreePath:        workerTwoPath,
			WorkerResultSummary: "api slice complete",
		},
	})
	if err != nil {
		t.Fatalf("BuildIntegrationSummary() error = %v", err)
	}

	if len(summary.WorkerIDs) != 2 {
		t.Fatalf("len(summary.WorkerIDs) = %d, want 2", len(summary.WorkerIDs))
	}
	if len(summary.Workers) != 2 {
		t.Fatalf("len(summary.Workers) = %d, want 2", len(summary.Workers))
	}
	if len(summary.ConflictCandidates) == 0 {
		t.Fatal("ConflictCandidates = 0, want at least one same-file/shared-path candidate")
	}
	if !strings.Contains(summary.IntegrationPreview, "Read-only integration preview") {
		t.Fatalf("IntegrationPreview = %q", summary.IntegrationPreview)
	}

	if containsPath(summary.Workers[0].FileList, ".orchestrator/artifacts/ignore.txt") {
		t.Fatalf("integration file list unexpectedly included orchestration artifact file: %#v", summary.Workers[0].FileList)
	}
	if !containsDiffSummary(summary.Workers, "modified: src/shared.txt") {
		t.Fatalf("integration diff summary missing modified shared file: %#v", summary.Workers)
	}
	if !containsConflict(summary.ConflictCandidates, "src/shared.txt", "same_file_touched") {
		t.Fatalf("integration conflicts missing same-file candidate: %#v", summary.ConflictCandidates)
	}
	if !containsConflict(summary.ConflictCandidates, "src", "shared_top_level_path") {
		t.Fatalf("integration conflicts missing shared top-level candidate: %#v", summary.ConflictCandidates)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "src", "ui.txt")); !os.IsNotExist(err) {
		t.Fatalf("main repo unexpectedly changed during integration preview: %v", err)
	}
}

func writeIntegrationFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func containsDiffSummary(workers []state.IntegrationWorkerSummary, want string) bool {
	for _, worker := range workers {
		for _, item := range worker.DiffSummary {
			if item == want {
				return true
			}
		}
	}
	return false
}

func containsConflict(candidates []state.ConflictCandidate, wantPath string, wantReason string) bool {
	for _, candidate := range candidates {
		if candidate.Path == wantPath && candidate.Reason == wantReason {
			return true
		}
	}
	return false
}
