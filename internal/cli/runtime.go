package cli

import (
	"context"
	"os"

	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

func ensureRuntime(ctx context.Context, layout state.Layout) (*state.Store, *journal.Journal, error) {
	if err := state.EnsureRuntimeDirs(layout); err != nil {
		return nil, nil, err
	}

	store, err := state.Open(layout.DBPath)
	if err != nil {
		return nil, nil, err
	}

	if err := store.EnsureSchema(ctx); err != nil {
		_ = store.Close()
		return nil, nil, err
	}

	journalWriter, err := journal.Open(layout.JournalPath)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}

	return store, journalWriter, nil
}

func openExistingStore(layout state.Layout) (*state.Store, error) {
	if !pathExists(layout.DBPath) {
		return nil, os.ErrNotExist
	}

	return state.Open(layout.DBPath)
}

func openExistingJournal(layout state.Layout) (*journal.Journal, error) {
	if !pathExists(layout.JournalPath) {
		return nil, os.ErrNotExist
	}

	return journal.Open(layout.JournalPath)
}
