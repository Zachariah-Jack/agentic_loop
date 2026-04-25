package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildTimeAccumulatesCompletedActiveSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	start := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	if err := store.StartBuildSession(ctx, `D:\repo`, "run_1", "Codex is thinking", start); err != nil {
		t.Fatalf("StartBuildSession() error = %v", err)
	}
	if err := store.EndBuildSession(ctx, `D:\repo`, "loop stopped", start.Add(90*time.Second)); err != nil {
		t.Fatalf("EndBuildSession() error = %v", err)
	}

	item, found, err := store.GetBuildTime(ctx, `D:\repo`)
	if err != nil {
		t.Fatalf("GetBuildTime() error = %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if item.TotalBuildTimeMS != int64(90*time.Second/time.Millisecond) {
		t.Fatalf("TotalBuildTimeMS = %d, want 90000", item.TotalBuildTimeMS)
	}
	if !item.CurrentActiveSessionStartedAt.IsZero() {
		t.Fatalf("CurrentActiveSessionStartedAt = %s, want zero", item.CurrentActiveSessionStartedAt)
	}
	if item.LastActiveSessionEndedAt.IsZero() {
		t.Fatal("LastActiveSessionEndedAt is zero")
	}
}

func TestUpdateBuildStepChangesActiveStepOnlyDuringSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	repoPath := `D:\repo`
	start := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	if err := store.StartBuildSession(ctx, repoPath, "run_1", "Starting build loop", start); err != nil {
		t.Fatalf("StartBuildSession() error = %v", err)
	}
	stepStarted := start.Add(2 * time.Minute)
	if err := store.UpdateBuildStep(ctx, repoPath, "Codex is thinking", stepStarted); err != nil {
		t.Fatalf("UpdateBuildStep() error = %v", err)
	}
	item, found, err := store.GetBuildTime(ctx, repoPath)
	if err != nil {
		t.Fatalf("GetBuildTime() error = %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if item.CurrentStepLabel != "Codex is thinking" {
		t.Fatalf("CurrentStepLabel = %q, want Codex is thinking", item.CurrentStepLabel)
	}
	if !item.CurrentStepStartedAt.Equal(stepStarted) {
		t.Fatalf("CurrentStepStartedAt = %s, want %s", item.CurrentStepStartedAt, stepStarted)
	}

	if err := store.EndBuildSession(ctx, repoPath, "loop stopped", start.Add(5*time.Minute)); err != nil {
		t.Fatalf("EndBuildSession() error = %v", err)
	}
	if err := store.UpdateBuildStep(ctx, repoPath, "Planner is deciding next step", start.Add(6*time.Minute)); err != nil {
		t.Fatalf("UpdateBuildStep() after end error = %v", err)
	}
	item, found, err = store.GetBuildTime(ctx, repoPath)
	if err != nil {
		t.Fatalf("GetBuildTime() error = %v", err)
	}
	if !found {
		t.Fatal("found = false after end, want true")
	}
	if item.CurrentStepLabel != "loop stopped" {
		t.Fatalf("CurrentStepLabel after end = %q, want loop stopped", item.CurrentStepLabel)
	}
}
