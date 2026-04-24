package cli

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"orchestrator/internal/control"
	"orchestrator/internal/state"
)

const dogfoodIssueStoredMessage = "dogfood issue recorded for later review"

func captureDogfoodIssue(ctx context.Context, inv Invocation, request control.CaptureDogfoodIssueRequest) (controlDogfoodIssueCaptureSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlDogfoodIssueCaptureSnapshot{}, err
	}
	if strings.TrimSpace(request.Title) == "" {
		return controlDogfoodIssueCaptureSnapshot{}, errors.New("dogfood issue title is required")
	}
	if strings.TrimSpace(request.Note) == "" {
		return controlDogfoodIssueCaptureSnapshot{}, errors.New("dogfood issue note is required")
	}
	if !pathExists(inv.Layout.DBPath) {
		return controlDogfoodIssueCaptureSnapshot{
			Available: false,
			Stored:    false,
			Message:   "dogfood issue storage is unavailable because runtime state has not been initialized yet",
		}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlDogfoodIssueCaptureSnapshot{
				Available: false,
				Stored:    false,
				Message:   "dogfood issue storage is unavailable because runtime state has not been initialized yet",
			}, nil
		}
		return controlDogfoodIssueCaptureSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlDogfoodIssueCaptureSnapshot{}, err
	}

	runID := strings.TrimSpace(request.RunID)
	if runID == "" {
		if run, found, err := store.LatestRun(ctx); err != nil {
			return controlDogfoodIssueCaptureSnapshot{}, err
		} else if found && strings.EqualFold(strings.TrimSpace(run.RepoPath), strings.TrimSpace(repoRoot)) {
			runID = run.ID
		}
	}

	source := strings.TrimSpace(request.Source)
	if source == "" {
		source = "operator_shell"
	}

	recorded, err := store.RecordDogfoodIssue(ctx, state.CreateDogfoodIssueParams{
		RepoPath:  repoRoot,
		RunID:     runID,
		Source:    source,
		Title:     request.Title,
		Note:      request.Note,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return controlDogfoodIssueCaptureSnapshot{}, err
	}

	emitEngineEvent(inv, "dogfood_issue_recorded", map[string]any{
		"repo_path":     repoRoot,
		"run_id":        runID,
		"dogfood_issue": recorded.ID,
		"source":        recorded.Source,
		"title":         recorded.Title,
		"note_preview":  previewString(recorded.Note, 240),
	})

	entry := controlDogfoodIssueSnapshotFromState(recorded)
	return controlDogfoodIssueCaptureSnapshot{
		Available: true,
		Stored:    true,
		Message:   dogfoodIssueStoredMessage,
		Entry:     &entry,
	}, nil
}

func listDogfoodIssues(ctx context.Context, inv Invocation, request control.ListDogfoodIssuesRequest) (controlDogfoodIssueListSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlDogfoodIssueListSnapshot{}, err
	}
	if !pathExists(inv.Layout.DBPath) {
		return controlDogfoodIssueListSnapshot{
			Available: false,
			Count:     0,
			Message:   "dogfood issue storage is unavailable because runtime state has not been initialized yet",
		}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlDogfoodIssueListSnapshot{
				Available: false,
				Count:     0,
				Message:   "dogfood issue storage is unavailable because runtime state has not been initialized yet",
			}, nil
		}
		return controlDogfoodIssueListSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlDogfoodIssueListSnapshot{}, err
	}

	items, err := store.ListDogfoodIssues(ctx, repoRoot, request.Limit)
	if err != nil {
		return controlDogfoodIssueListSnapshot{}, err
	}

	snapshotItems := make([]controlDogfoodIssueSnapshot, 0, len(items))
	for _, item := range items {
		snapshotItems = append(snapshotItems, controlDogfoodIssueSnapshotFromState(item))
	}

	snapshot := controlDogfoodIssueListSnapshot{
		Available: true,
		Count:     len(snapshotItems),
		Items:     snapshotItems,
	}
	if len(snapshotItems) == 0 {
		snapshot.Message = "no dogfood issues have been captured for this repo yet"
	}
	return snapshot, nil
}

func controlDogfoodIssueSnapshotFromState(issue state.DogfoodIssue) controlDogfoodIssueSnapshot {
	return controlDogfoodIssueSnapshot{
		ID:        issue.ID,
		RepoPath:  strings.TrimSpace(issue.RepoPath),
		RunID:     strings.TrimSpace(issue.RunID),
		Source:    strings.TrimSpace(issue.Source),
		Title:     strings.TrimSpace(issue.Title),
		Note:      issue.Note,
		CreatedAt: formatSnapshotTime(issue.CreatedAt),
		UpdatedAt: formatSnapshotTime(issue.UpdatedAt),
	}
}
