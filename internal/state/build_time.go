package state

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type BuildTime struct {
	RepoPath                      string
	TotalBuildTimeMS              int64
	CurrentActiveSessionStartedAt time.Time
	LastActiveSessionEndedAt      time.Time
	CurrentStepStartedAt          time.Time
	CurrentRunStartedAt           time.Time
	CurrentStepLabel              string
	PlannerActiveDurationMS       int64
	ExecutorActiveDurationMS      int64
	ExecutorThinkingDurationMS    int64
	CommandActiveDurationMS       int64
	InstallActiveDurationMS       int64
	TestActiveDurationMS          int64
	HumanWaitDurationMS           int64
	BlockedDurationMS             int64
}

func (s *Store) GetBuildTime(ctx context.Context, repoPath string) (BuildTime, bool, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return BuildTime{}, false, errors.New("repo path is required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT repo_path, total_build_time_ms, current_active_session_started_at, last_active_session_ended_at,
		        current_step_started_at, current_run_started_at, current_step_label,
		        planner_active_duration_ms, executor_active_duration_ms, executor_thinking_duration_ms,
		        command_active_duration_ms, install_active_duration_ms, test_active_duration_ms,
		        human_wait_duration_ms, blocked_duration_ms
		   FROM build_time
		  WHERE repo_path = ?`,
		repoPath,
	)
	var item BuildTime
	var currentActiveRaw, lastEndedRaw, stepStartedRaw, runStartedRaw string
	if err := row.Scan(
		&item.RepoPath,
		&item.TotalBuildTimeMS,
		&currentActiveRaw,
		&lastEndedRaw,
		&stepStartedRaw,
		&runStartedRaw,
		&item.CurrentStepLabel,
		&item.PlannerActiveDurationMS,
		&item.ExecutorActiveDurationMS,
		&item.ExecutorThinkingDurationMS,
		&item.CommandActiveDurationMS,
		&item.InstallActiveDurationMS,
		&item.TestActiveDurationMS,
		&item.HumanWaitDurationMS,
		&item.BlockedDurationMS,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BuildTime{RepoPath: repoPath}, false, nil
		}
		return BuildTime{}, false, err
	}
	item.CurrentActiveSessionStartedAt = parseStoredTime(currentActiveRaw)
	item.LastActiveSessionEndedAt = parseStoredTime(lastEndedRaw)
	item.CurrentStepStartedAt = parseStoredTime(stepStartedRaw)
	item.CurrentRunStartedAt = parseStoredTime(runStartedRaw)
	return item, true, nil
}

func (s *Store) StartBuildSession(ctx context.Context, repoPath string, runID string, stepLabel string, at time.Time) error {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return errors.New("repo path is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	stepLabel = strings.TrimSpace(stepLabel)
	if stepLabel == "" {
		stepLabel = "Orchestrator loop active"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO build_time (
			repo_path, total_build_time_ms, current_active_session_started_at, current_step_started_at,
			current_run_started_at, current_step_label, updated_at
		) VALUES (?, 0, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_path) DO UPDATE SET
			current_active_session_started_at = CASE
				WHEN build_time.current_active_session_started_at = '' THEN excluded.current_active_session_started_at
				ELSE build_time.current_active_session_started_at
			END,
			current_step_started_at = excluded.current_step_started_at,
			current_run_started_at = excluded.current_run_started_at,
			current_step_label = excluded.current_step_label,
			updated_at = excluded.updated_at`,
		repoPath,
		formatTime(at),
		formatTime(at),
		formatTime(at),
		stepLabel,
		formatTime(at),
	)
	_ = runID
	return err
}

func (s *Store) EndBuildSession(ctx context.Context, repoPath string, stepLabel string, at time.Time) error {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return errors.New("repo path is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	item, found, err := s.GetBuildTime(ctx, repoPath)
	if err != nil {
		return err
	}
	if !found || item.CurrentActiveSessionStartedAt.IsZero() {
		return nil
	}
	elapsed := at.Sub(item.CurrentActiveSessionStartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE build_time
		    SET total_build_time_ms = total_build_time_ms + ?,
		        current_active_session_started_at = '',
		        last_active_session_ended_at = ?,
		        current_step_started_at = '',
		        current_run_started_at = '',
		        current_step_label = ?,
		        updated_at = ?
		  WHERE repo_path = ?`,
		int64(elapsed/time.Millisecond),
		formatTime(at),
		strings.TrimSpace(stepLabel),
		formatTime(at),
		repoPath,
	)
	return err
}

func parseStoredTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
