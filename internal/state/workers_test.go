package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerRegistryPersistsWorkers(t *testing.T) {
	t.Parallel()

	layout := ResolveLayout(t.TempDir())
	if err := EnsureRuntimeDirs(layout); err != nil {
		t.Fatalf("EnsureRuntimeDirs() error = %v", err)
	}

	store, err := Open(layout.DBPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	run, err := store.CreateRun(context.Background(), CreateRunParams{
		RepoPath: layout.RepoRoot,
		Goal:     "worker registry persistence",
		Status:   StatusInitialized,
		Checkpoint: Checkpoint{
			Sequence:  1,
			Stage:     "bootstrap",
			Label:     "run_initialized",
			CreatedAt: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	worker, err := store.CreateWorker(context.Background(), CreateWorkerParams{
		RunID:         run.ID,
		WorkerName:    "frontend-worker",
		WorkerStatus:  WorkerStatusIdle,
		AssignedScope: "ui polish",
		WorktreePath:  filepath.Join(layout.WorkersDir, "frontend-worker"),
		CreatedAt:     time.Date(2026, 4, 20, 10, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateWorker() error = %v", err)
	}

	worker.ExecutorThreadID = "thread_worker"
	worker.ExecutorTurnID = "turn_worker"
	worker.WorkerTaskSummary = "Implement the isolated frontend shell"
	worker.WorkerExecutorPromptSummary = "Implement the isolated frontend shell in this worker workspace only."
	worker.WorkerResultSummary = "worker executor turn completed"
	worker.WorkerStatus = WorkerStatusCompleted
	worker.AssignedAt = time.Date(2026, 4, 20, 10, 1, 30, 0, time.UTC)
	worker.CompletedAt = time.Date(2026, 4, 20, 10, 2, 0, 0, time.UTC)
	worker.UpdatedAt = time.Date(2026, 4, 20, 10, 2, 0, 0, time.UTC)
	if err := store.SaveWorker(context.Background(), worker); err != nil {
		t.Fatalf("SaveWorker() error = %v", err)
	}

	loaded, found, err := store.GetWorker(context.Background(), worker.ID)
	if err != nil {
		t.Fatalf("GetWorker() error = %v", err)
	}
	if !found {
		t.Fatal("GetWorker() did not find persisted worker")
	}
	if loaded.WorkerName != worker.WorkerName {
		t.Fatalf("loaded.WorkerName = %q, want %q", loaded.WorkerName, worker.WorkerName)
	}
	if loaded.WorkerStatus != WorkerStatusCompleted {
		t.Fatalf("loaded.WorkerStatus = %q, want %q", loaded.WorkerStatus, WorkerStatusCompleted)
	}
	if loaded.ExecutorThreadID != "thread_worker" || loaded.ExecutorTurnID != "turn_worker" {
		t.Fatalf("loaded executor ids = (%q, %q), want thread_worker/turn_worker", loaded.ExecutorThreadID, loaded.ExecutorTurnID)
	}
	if loaded.WorkerTaskSummary != worker.WorkerTaskSummary {
		t.Fatalf("loaded.WorkerTaskSummary = %q, want %q", loaded.WorkerTaskSummary, worker.WorkerTaskSummary)
	}
	if loaded.WorkerExecutorPromptSummary != worker.WorkerExecutorPromptSummary {
		t.Fatalf("loaded.WorkerExecutorPromptSummary = %q, want %q", loaded.WorkerExecutorPromptSummary, worker.WorkerExecutorPromptSummary)
	}
	if loaded.WorkerResultSummary != worker.WorkerResultSummary {
		t.Fatalf("loaded.WorkerResultSummary = %q, want %q", loaded.WorkerResultSummary, worker.WorkerResultSummary)
	}
	if !loaded.AssignedAt.Equal(worker.AssignedAt) {
		t.Fatalf("loaded.AssignedAt = %s, want %s", loaded.AssignedAt, worker.AssignedAt)
	}
	if !loaded.CompletedAt.Equal(worker.CompletedAt) {
		t.Fatalf("loaded.CompletedAt = %s, want %s", loaded.CompletedAt, worker.CompletedAt)
	}

	workers, err := store.ListWorkers(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkers() error = %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("len(ListWorkers()) = %d, want 1", len(workers))
	}

	stats, err := store.WorkerStats(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("WorkerStats() error = %v", err)
	}
	if stats.Total != 1 {
		t.Fatalf("stats.Total = %d, want 1", stats.Total)
	}
	if stats.Active != 0 {
		t.Fatalf("stats.Active = %d, want 0", stats.Active)
	}
}
