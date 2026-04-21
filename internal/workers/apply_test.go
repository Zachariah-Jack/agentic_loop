package workers

import (
	"os"
	"path/filepath"
	"testing"

	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestApplyIntegrationNonConflictingWritesExpectedFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeIntegrationFile(t, filepath.Join(repoRoot, "src", "shared.txt"), "base shared\n")

	workerOnePath := filepath.Join(t.TempDir(), "worker-one")
	workerTwoPath := filepath.Join(t.TempDir(), "worker-two")
	writeIntegrationFile(t, filepath.Join(workerOnePath, "src", "shared.txt"), "worker one shared\n")
	writeIntegrationFile(t, filepath.Join(workerOnePath, "src", "ui.txt"), "worker one ui\n")
	writeIntegrationFile(t, filepath.Join(workerTwoPath, "src", "shared.txt"), "worker two shared\n")
	writeIntegrationFile(t, filepath.Join(workerTwoPath, "src", "api.txt"), "worker two api\n")

	summary, err := BuildIntegrationSummary(repoRoot, []state.Worker{
		{ID: "worker_one", WorkerName: "worker-one", WorktreePath: workerOnePath},
		{ID: "worker_two", WorkerName: "worker-two", WorktreePath: workerTwoPath},
	})
	if err != nil {
		t.Fatalf("BuildIntegrationSummary() error = %v", err)
	}

	applySummary, err := ApplyIntegration(repoRoot, summary, ".orchestrator/artifacts/integration/run_123/integration_preview.json", string(planner.WorkerApplyModeNonConflicting))
	if err != nil {
		t.Fatalf("ApplyIntegration() error = %v", err)
	}

	if applySummary.Status != integrationApplyStatusCompleted {
		t.Fatalf("applySummary.Status = %q, want %q", applySummary.Status, integrationApplyStatusCompleted)
	}
	if len(applySummary.FilesApplied) != 2 {
		t.Fatalf("len(applySummary.FilesApplied) = %d, want 2", len(applySummary.FilesApplied))
	}
	if !containsAppliedPath(applySummary.FilesApplied, "src/ui.txt", "added") {
		t.Fatalf("FilesApplied missing src/ui.txt add: %#v", applySummary.FilesApplied)
	}
	if !containsAppliedPath(applySummary.FilesApplied, "src/api.txt", "added") {
		t.Fatalf("FilesApplied missing src/api.txt add: %#v", applySummary.FilesApplied)
	}
	if !containsSkippedPath(applySummary.FilesSkipped, "src/shared.txt", "conflict_candidate") {
		t.Fatalf("FilesSkipped missing shared conflict skip: %#v", applySummary.FilesSkipped)
	}

	assertFileContents(t, filepath.Join(repoRoot, "src", "ui.txt"), "worker one ui\n")
	assertFileContents(t, filepath.Join(repoRoot, "src", "api.txt"), "worker two api\n")
	assertFileContents(t, filepath.Join(repoRoot, "src", "shared.txt"), "base shared\n")
}

func TestApplyIntegrationAbortIfConflictsRefusesWithoutWriting(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeIntegrationFile(t, filepath.Join(repoRoot, "src", "shared.txt"), "base shared\n")

	workerOnePath := filepath.Join(t.TempDir(), "worker-one")
	workerTwoPath := filepath.Join(t.TempDir(), "worker-two")
	writeIntegrationFile(t, filepath.Join(workerOnePath, "src", "shared.txt"), "worker one shared\n")
	writeIntegrationFile(t, filepath.Join(workerOnePath, "src", "ui.txt"), "worker one ui\n")
	writeIntegrationFile(t, filepath.Join(workerTwoPath, "src", "shared.txt"), "worker two shared\n")
	writeIntegrationFile(t, filepath.Join(workerTwoPath, "src", "api.txt"), "worker two api\n")

	summary, err := BuildIntegrationSummary(repoRoot, []state.Worker{
		{ID: "worker_one", WorkerName: "worker-one", WorktreePath: workerOnePath},
		{ID: "worker_two", WorkerName: "worker-two", WorktreePath: workerTwoPath},
	})
	if err != nil {
		t.Fatalf("BuildIntegrationSummary() error = %v", err)
	}

	applySummary, err := ApplyIntegration(repoRoot, summary, ".orchestrator/artifacts/integration/run_123/integration_preview.json", string(planner.WorkerApplyModeAbortIfConflicts))
	if err != nil {
		t.Fatalf("ApplyIntegration() error = %v", err)
	}

	if applySummary.Status != integrationApplyStatusFailed {
		t.Fatalf("applySummary.Status = %q, want %q", applySummary.Status, integrationApplyStatusFailed)
	}
	if len(applySummary.FilesApplied) != 0 {
		t.Fatalf("len(applySummary.FilesApplied) = %d, want 0", len(applySummary.FilesApplied))
	}
	if len(applySummary.FilesSkipped) == 0 {
		t.Fatal("FilesSkipped = 0, want refused skipped file list")
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "src", "ui.txt")); !os.IsNotExist(err) {
		t.Fatalf("main repo unexpectedly wrote ui.txt during abort_if_conflicts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "src", "api.txt")); !os.IsNotExist(err) {
		t.Fatalf("main repo unexpectedly wrote api.txt during abort_if_conflicts: %v", err)
	}
	assertFileContents(t, filepath.Join(repoRoot, "src", "shared.txt"), "base shared\n")
}

func containsAppliedPath(items []state.IntegrationAppliedFile, wantPath string, wantChangeKind string) bool {
	for _, item := range items {
		if item.Path == wantPath && item.ChangeKind == wantChangeKind {
			return true
		}
	}
	return false
}

func containsSkippedPath(items []state.IntegrationSkippedFile, wantPath string, wantReason string) bool {
	for _, item := range items {
		if item.Path == wantPath && item.Reason == wantReason {
			return true
		}
	}
	return false
}

func assertFileContents(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("contents(%q) = %q, want %q", path, string(content), want)
	}
}
