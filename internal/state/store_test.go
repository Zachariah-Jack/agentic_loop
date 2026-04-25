package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBusyRetryRetriesTransientSQLiteBusy(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := WithBusyRetry(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return errors.New("SQLITE_BUSY: database is locked")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithBusyRetry() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if !IsBusyError(errors.New("database is locked")) {
		t.Fatal("IsBusyError() should recognize database locked errors")
	}
}

func TestMarkRunAbandonedPreservesRunAndMarksCancelled(t *testing.T) {
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
		Goal:     "recover stale run",
		Status:   StatusInitialized,
		Checkpoint: Checkpoint{
			Sequence:  1,
			Stage:     "planner",
			Label:     "planner_turn_started",
			SafePause: true,
			CreatedAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := store.MarkRunAbandoned(context.Background(), run.ID, "operator_recovery", "mechanically abandoned"); err != nil {
		t.Fatalf("MarkRunAbandoned() error = %v", err)
	}
	updated, found, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if !found {
		t.Fatalf("run %s should be preserved", run.ID)
	}
	if updated.Status != StatusCancelled {
		t.Fatalf("status = %q, want cancelled", updated.Status)
	}
	if updated.LatestStopReason != "operator_recovery" {
		t.Fatalf("latest stop reason = %q, want operator_recovery", updated.LatestStopReason)
	}
	if updated.RuntimeIssueReason != "operator_recovery" {
		t.Fatalf("runtime issue reason = %q, want operator_recovery", updated.RuntimeIssueReason)
	}
}
