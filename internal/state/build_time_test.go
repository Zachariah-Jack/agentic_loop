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
