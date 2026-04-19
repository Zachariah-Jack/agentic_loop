package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type RunStatus string

const (
	StatusInitialized RunStatus = "initialized"
	StatusCompleted   RunStatus = "completed"
	StatusFailed      RunStatus = "failed"
	StatusCancelled   RunStatus = "cancelled"
)

type Run struct {
	ID                 string
	RepoPath           string
	Goal               string
	Status             RunStatus
	PreviousResponseID string
	ExecutorTransport  string
	ExecutorThreadID   string
	ExecutorThreadPath string
	ExecutorTurnID     string
	ExecutorTurnStatus string
	ExecutorLastError  string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LatestCheckpoint   Checkpoint
}

type Checkpoint struct {
	Sequence     int64     `json:"sequence"`
	Stage        string    `json:"stage"`
	Label        string    `json:"label"`
	SafePause    bool      `json:"safe_pause"`
	PlannerTurn  int64     `json:"planner_turn"`
	ExecutorTurn int64     `json:"executor_turn"`
	CreatedAt    time.Time `json:"created_at"`
}

type CreateRunParams struct {
	RepoPath   string
	Goal       string
	Status     RunStatus
	Checkpoint Checkpoint
}

type Stats struct {
	TotalRuns     int64
	ResumableRuns int64
}

type ExecutorState struct {
	Transport  string
	ThreadID   string
	ThreadPath string
	TurnID     string
	TurnStatus string
	LastError  string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if _, err := store.db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	statements := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			repo_path TEXT NOT NULL,
			goal TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			latest_checkpoint_json TEXT NOT NULL,
			previous_response_id TEXT NOT NULL DEFAULT '',
			executor_transport TEXT NOT NULL DEFAULT '',
			executor_thread_id TEXT NOT NULL DEFAULT '',
			executor_thread_path TEXT NOT NULL DEFAULT '',
			executor_turn_id TEXT NOT NULL DEFAULT '',
			executor_turn_status TEXT NOT NULL DEFAULT '',
			executor_last_error TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_status_updated_at
			ON runs(status, updated_at DESC);`,
		`CREATE TABLE IF NOT EXISTS checkpoints (
			run_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			stage TEXT NOT NULL,
			label TEXT NOT NULL,
			safe_pause INTEGER NOT NULL,
			planner_turn INTEGER NOT NULL,
			executor_turn INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (run_id, sequence),
			FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_checkpoints_run_created_at
			ON checkpoints(run_id, created_at DESC);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	if err := s.ensureRunsColumn(ctx, "previous_response_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_transport", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_thread_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_thread_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_turn_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_turn_status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_last_error", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, "PRAGMA user_version = 3;"); err != nil {
		return err
	}

	return nil
}

func (s *Store) CreateRun(ctx context.Context, params CreateRunParams) (Run, error) {
	if params.RepoPath == "" {
		return Run{}, errors.New("repo path is required")
	}
	if params.Goal == "" {
		return Run{}, errors.New("goal is required")
	}

	status := params.Status
	if status == "" {
		status = StatusInitialized
	}

	runID, err := newRunID()
	if err != nil {
		return Run{}, err
	}

	checkpoint := params.Checkpoint
	if checkpoint.Sequence == 0 {
		checkpoint.Sequence = 1
	}
	if checkpoint.Stage == "" {
		checkpoint.Stage = "bootstrap"
	}
	if checkpoint.Label == "" {
		checkpoint.Label = "run_initialized"
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	} else {
		checkpoint.CreatedAt = checkpoint.CreatedAt.UTC()
	}

	createdAt := checkpoint.CreatedAt
	updatedAt := checkpoint.CreatedAt

	checkpointJSON, err := json.Marshal(checkpoint)
	if err != nil {
		return Run{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO runs (
			id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, previous_response_id,
			executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID,
		params.RepoPath,
		params.Goal,
		string(status),
		formatTime(createdAt),
		formatTime(updatedAt),
		string(checkpointJSON),
		"",
		"",
		"",
		"",
		"",
		"",
		"",
	); err != nil {
		return Run{}, err
	}

	if err := insertCheckpointTx(ctx, tx, runID, checkpoint); err != nil {
		return Run{}, err
	}

	if err := tx.Commit(); err != nil {
		return Run{}, err
	}

	return Run{
		ID:                 runID,
		RepoPath:           params.RepoPath,
		Goal:               params.Goal,
		Status:             status,
		PreviousResponseID: "",
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
		LatestCheckpoint:   checkpoint,
	}, nil
}

func (s *Store) SaveCheckpoint(ctx context.Context, runID string, checkpoint Checkpoint) error {
	return s.saveCheckpointAndResponseID(ctx, runID, "", false, checkpoint)
}

func (s *Store) SavePlannerTurn(ctx context.Context, runID string, previousResponseID string, checkpoint Checkpoint) error {
	if strings.TrimSpace(previousResponseID) == "" {
		return errors.New("previous response id is required")
	}

	return s.saveCheckpointAndResponseID(ctx, runID, previousResponseID, true, checkpoint)
}

func (s *Store) SaveExecutorState(ctx context.Context, runID string, executorState ExecutorState) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     executor_transport = ?,
		     executor_thread_id = ?,
		     executor_thread_path = ?,
		     executor_turn_id = ?,
		     executor_turn_status = ?,
		     executor_last_error = ?
		 WHERE id = ?`,
		formatTime(time.Now().UTC()),
		strings.TrimSpace(executorState.Transport),
		strings.TrimSpace(executorState.ThreadID),
		strings.TrimSpace(executorState.ThreadPath),
		strings.TrimSpace(executorState.TurnID),
		strings.TrimSpace(executorState.TurnStatus),
		strings.TrimSpace(executorState.LastError),
		runID,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("run %s not found", runID)
	}

	return nil
}

func (s *Store) SaveExecutorTurn(ctx context.Context, runID string, executorState ExecutorState, checkpoint Checkpoint) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}
	if checkpoint.Sequence == 0 {
		return errors.New("checkpoint sequence is required")
	}
	if checkpoint.Stage == "" {
		return errors.New("checkpoint stage is required")
	}
	if checkpoint.Label == "" {
		return errors.New("checkpoint label is required")
	}

	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	} else {
		checkpoint.CreatedAt = checkpoint.CreatedAt.UTC()
	}

	checkpointJSON, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertCheckpointTx(ctx, tx, runID, checkpoint); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     latest_checkpoint_json = ?,
		     executor_transport = ?,
		     executor_thread_id = ?,
		     executor_thread_path = ?,
		     executor_turn_id = ?,
		     executor_turn_status = ?,
		     executor_last_error = ?
		 WHERE id = ?`,
		formatTime(checkpoint.CreatedAt),
		string(checkpointJSON),
		strings.TrimSpace(executorState.Transport),
		strings.TrimSpace(executorState.ThreadID),
		strings.TrimSpace(executorState.ThreadPath),
		strings.TrimSpace(executorState.TurnID),
		strings.TrimSpace(executorState.TurnStatus),
		strings.TrimSpace(executorState.LastError),
		runID,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("run %s not found", runID)
	}

	return tx.Commit()
}

func (s *Store) GetRun(ctx context.Context, runID string) (Run, bool, error) {
	return s.querySingleRun(ctx,
		`SELECT id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, previous_response_id,
		        executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_error
		 FROM runs
		 WHERE id = ?
		 LIMIT 1`,
		runID,
	)
}

func (s *Store) saveCheckpointAndResponseID(ctx context.Context, runID string, previousResponseID string, persistResponseID bool, checkpoint Checkpoint) error {
	if runID == "" {
		return errors.New("run id is required")
	}
	if checkpoint.Sequence == 0 {
		return errors.New("checkpoint sequence is required")
	}
	if checkpoint.Stage == "" {
		return errors.New("checkpoint stage is required")
	}
	if checkpoint.Label == "" {
		return errors.New("checkpoint label is required")
	}

	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	} else {
		checkpoint.CreatedAt = checkpoint.CreatedAt.UTC()
	}

	checkpointJSON, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertCheckpointTx(ctx, tx, runID, checkpoint); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx,
		updateRunStatement(persistResponseID),
		updateRunArgs(runID, checkpoint, string(checkpointJSON), previousResponseID, persistResponseID)...,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("run %s not found", runID)
	}

	return tx.Commit()
}

func (s *Store) LatestResumableRun(ctx context.Context) (Run, bool, error) {
	return s.querySingleRun(ctx,
		`SELECT id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, previous_response_id,
		        executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_error
		 FROM runs
		 WHERE status NOT IN (?, ?, ?)
		 ORDER BY updated_at DESC, created_at DESC
		 LIMIT 1`,
		string(StatusCompleted),
		string(StatusFailed),
		string(StatusCancelled),
	)
}

func (s *Store) LatestRun(ctx context.Context) (Run, bool, error) {
	return s.querySingleRun(ctx,
		`SELECT id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, previous_response_id,
		        executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_error
		 FROM runs
		 ORDER BY updated_at DESC, created_at DESC
		 LIMIT 1`,
	)
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN status NOT IN (?, ?, ?) THEN 1 ELSE 0 END), 0)
		   FROM runs`,
		string(StatusCompleted),
		string(StatusFailed),
		string(StatusCancelled),
	)

	var stats Stats
	if err := row.Scan(&stats.TotalRuns, &stats.ResumableRuns); err != nil {
		return Stats{}, err
	}

	return stats, nil
}

func insertCheckpointTx(ctx context.Context, tx *sql.Tx, runID string, checkpoint Checkpoint) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO checkpoints (
			run_id, sequence, stage, label, safe_pause, planner_turn, executor_turn, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		runID,
		checkpoint.Sequence,
		checkpoint.Stage,
		checkpoint.Label,
		boolToInt(checkpoint.SafePause),
		checkpoint.PlannerTurn,
		checkpoint.ExecutorTurn,
		formatTime(checkpoint.CreatedAt),
	)
	return err
}

func (s *Store) querySingleRun(ctx context.Context, query string, args ...any) (Run, bool, error) {
	row := s.db.QueryRowContext(ctx, query, args...)

	var (
		run                Run
		status             string
		createdAtRaw       string
		updatedAtRaw       string
		checkpointJSON     string
		previousResponseID string
		executorTransport  string
		executorThreadID   string
		executorThreadPath string
		executorTurnID     string
		executorTurnStatus string
		executorLastError  string
	)

	if err := row.Scan(
		&run.ID,
		&run.RepoPath,
		&run.Goal,
		&status,
		&createdAtRaw,
		&updatedAtRaw,
		&checkpointJSON,
		&previousResponseID,
		&executorTransport,
		&executorThreadID,
		&executorThreadPath,
		&executorTurnID,
		&executorTurnStatus,
		&executorLastError,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, false, nil
		}
		return Run{}, false, err
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return Run{}, false, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return Run{}, false, err
	}

	var checkpoint Checkpoint
	if err := json.Unmarshal([]byte(checkpointJSON), &checkpoint); err != nil {
		return Run{}, false, err
	}

	run.Status = RunStatus(status)
	run.PreviousResponseID = previousResponseID
	run.ExecutorTransport = executorTransport
	run.ExecutorThreadID = executorThreadID
	run.ExecutorThreadPath = executorThreadPath
	run.ExecutorTurnID = executorTurnID
	run.ExecutorTurnStatus = executorTurnStatus
	run.ExecutorLastError = executorLastError
	run.CreatedAt = createdAt
	run.UpdatedAt = updatedAt
	run.LatestCheckpoint = checkpoint
	return run, true, nil
}

func (s *Store) ensureRunsColumn(ctx context.Context, columnName string, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info(runs);")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal any
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return err
		}
		if name == columnName {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE runs ADD COLUMN %s %s;", columnName, definition))
	return err
}

func updateRunStatement(persistResponseID bool) string {
	if persistResponseID {
		return `UPDATE runs
		 SET updated_at = ?, latest_checkpoint_json = ?, previous_response_id = ?
		 WHERE id = ?`
	}

	return `UPDATE runs
		SET updated_at = ?, latest_checkpoint_json = ?
		WHERE id = ?`
}

func updateRunArgs(runID string, checkpoint Checkpoint, checkpointJSON string, previousResponseID string, persistResponseID bool) []any {
	if persistResponseID {
		return []any{
			formatTime(checkpoint.CreatedAt),
			checkpointJSON,
			previousResponseID,
			runID,
		}
	}

	return []any{
		formatTime(checkpoint.CreatedAt),
		checkpointJSON,
		runID,
	}
}

func newRunID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "run_" + hex.EncodeToString(bytes), nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
