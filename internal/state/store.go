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
	ID                       string
	RepoPath                 string
	Goal                     string
	Status                   RunStatus
	LatestStopReason         string
	RuntimeIssueReason       string
	RuntimeIssueMessage      string
	PreviousResponseID       string
	HumanReplies             []HumanReply
	CollectedContext         *CollectedContextState
	PlannerOperatorStatus    *PlannerOperatorStatus
	ExecutorTransport        string
	ExecutorThreadID         string
	ExecutorThreadPath       string
	ExecutorTurnID           string
	ExecutorTurnStatus       string
	ExecutorLastSuccess      *bool
	ExecutorLastFailureStage string
	ExecutorLastError        string
	ExecutorLastMessage      string
	ExecutorApproval         *ExecutorApproval
	ExecutorLastControl      *ExecutorControl
	CreatedAt                time.Time
	UpdatedAt                time.Time
	LatestCheckpoint         Checkpoint
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

type HumanReply struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	ReceivedAt time.Time `json:"received_at"`
	Payload    string    `json:"payload"`
}

type Stats struct {
	TotalRuns     int64
	ResumableRuns int64
}

type ExecutorState struct {
	Transport        string
	ThreadID         string
	ThreadPath       string
	TurnID           string
	TurnStatus       string
	LastSuccess      *bool
	LastFailureStage string
	LastError        string
	LastMessage      string
	Approval         *ExecutorApproval
	LastControl      *ExecutorControl
}

type ExecutorApproval struct {
	State      string `json:"state,omitempty"`
	Kind       string `json:"kind,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	ItemID     string `json:"item_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Command    string `json:"command,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	GrantRoot  string `json:"grant_root,omitempty"`
	RawParams  string `json:"raw_params,omitempty"`
}

type ExecutorControl struct {
	Action  string    `json:"action,omitempty"`
	Payload string    `json:"payload,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

type CollectedContextState struct {
	ArtifactPath    string                   `json:"artifact_path,omitempty"`
	ArtifactPreview string                   `json:"artifact_preview,omitempty"`
	Focus           string                   `json:"focus"`
	Questions       []string                 `json:"questions,omitempty"`
	Results         []CollectedContextResult `json:"results,omitempty"`
	ToolResults     []PluginToolResult       `json:"tool_results,omitempty"`
	WorkerResults   []WorkerActionResult     `json:"worker_results,omitempty"`
	WorkerPlan      *WorkerPlanResult        `json:"worker_plan,omitempty"`
}

type CollectedContextResult struct {
	RequestedPath string   `json:"requested_path"`
	ResolvedPath  string   `json:"resolved_path,omitempty"`
	Kind          string   `json:"kind"`
	Detail        string   `json:"detail,omitempty"`
	Preview       string   `json:"preview,omitempty"`
	Entries       []string `json:"entries,omitempty"`
	Truncated     bool     `json:"truncated,omitempty"`
}

type PluginToolResult struct {
	Tool            string         `json:"tool"`
	Success         bool           `json:"success"`
	Message         string         `json:"message,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
	ArtifactPath    string         `json:"artifact_path,omitempty"`
	ArtifactPreview string         `json:"artifact_preview,omitempty"`
}

type WorkerActionResult struct {
	Action          string                   `json:"action"`
	Success         bool                     `json:"success"`
	Message         string                   `json:"message,omitempty"`
	Worker          *WorkerResultSummary     `json:"worker,omitempty"`
	ListedWorkers   []WorkerResultSummary    `json:"listed_workers,omitempty"`
	Removed         bool                     `json:"removed,omitempty"`
	ArtifactPath    string                   `json:"artifact_path,omitempty"`
	ArtifactPreview string                   `json:"artifact_preview,omitempty"`
	Integration     *IntegrationSummary      `json:"integration,omitempty"`
	Apply           *IntegrationApplySummary `json:"apply,omitempty"`
}

type WorkerResultSummary struct {
	WorkerID                   string    `json:"worker_id,omitempty"`
	WorkerName                 string    `json:"worker_name,omitempty"`
	WorkerStatus               string    `json:"worker_status,omitempty"`
	AssignedScope              string    `json:"assigned_scope,omitempty"`
	WorktreePath               string    `json:"worktree_path,omitempty"`
	WorkerTaskSummary          string    `json:"worker_task_summary,omitempty"`
	ExecutorPromptSummary      string    `json:"worker_executor_prompt_summary,omitempty"`
	WorkerResultSummary        string    `json:"worker_result_summary,omitempty"`
	WorkerErrorSummary         string    `json:"worker_error_summary,omitempty"`
	ExecutorThreadID           string    `json:"executor_thread_id,omitempty"`
	ExecutorTurnID             string    `json:"executor_turn_id,omitempty"`
	ExecutorTurnStatus         string    `json:"executor_turn_status,omitempty"`
	ExecutorApprovalState      string    `json:"executor_approval_state,omitempty"`
	ExecutorApprovalKind       string    `json:"executor_approval_kind,omitempty"`
	ExecutorApprovalPreview    string    `json:"executor_approval_preview,omitempty"`
	ExecutorInterruptible      bool      `json:"executor_interruptible,omitempty"`
	ExecutorSteerable          bool      `json:"executor_steerable,omitempty"`
	ExecutorFailureStage       string    `json:"executor_failure_stage,omitempty"`
	ExecutorLastControlAction  string    `json:"executor_last_control_action,omitempty"`
	ExecutorLastControlPayload string    `json:"executor_last_control_payload,omitempty"`
	StartedAt                  time.Time `json:"started_at,omitempty"`
	CompletedAt                time.Time `json:"completed_at,omitempty"`
}

type IntegrationSummary struct {
	WorkerIDs          []string                   `json:"worker_ids,omitempty"`
	Workers            []IntegrationWorkerSummary `json:"workers,omitempty"`
	ConflictCandidates []ConflictCandidate        `json:"conflict_candidates,omitempty"`
	IntegrationPreview string                     `json:"integration_preview,omitempty"`
}

type IntegrationWorkerSummary struct {
	WorkerID            string   `json:"worker_id,omitempty"`
	WorkerName          string   `json:"worker_name,omitempty"`
	WorktreePath        string   `json:"worktree_path,omitempty"`
	WorkerResultSummary string   `json:"worker_result_summary,omitempty"`
	FileList            []string `json:"file_list,omitempty"`
	DiffSummary         []string `json:"diff_summary,omitempty"`
}

type ConflictCandidate struct {
	Path        string   `json:"path,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	WorkerIDs   []string `json:"worker_ids,omitempty"`
	WorkerNames []string `json:"worker_names,omitempty"`
}

type WorkerPlanResult struct {
	Status                  string                   `json:"status,omitempty"`
	WorkerIDs               []string                 `json:"worker_ids,omitempty"`
	Workers                 []WorkerResultSummary    `json:"workers,omitempty"`
	ConcurrencyLimit        int                      `json:"concurrency_limit,omitempty"`
	IntegrationRequested    bool                     `json:"integration_requested,omitempty"`
	IntegrationArtifactPath string                   `json:"integration_artifact_path,omitempty"`
	IntegrationPreview      string                   `json:"integration_preview,omitempty"`
	ApplyMode               string                   `json:"apply_mode,omitempty"`
	ApplyArtifactPath       string                   `json:"apply_artifact_path,omitempty"`
	Apply                   *IntegrationApplySummary `json:"apply,omitempty"`
	Message                 string                   `json:"message,omitempty"`
}

type IntegrationApplySummary struct {
	Status             string                   `json:"status,omitempty"`
	SourceArtifactPath string                   `json:"source_artifact_path,omitempty"`
	ApplyMode          string                   `json:"apply_mode,omitempty"`
	FilesApplied       []IntegrationAppliedFile `json:"files_applied,omitempty"`
	FilesSkipped       []IntegrationSkippedFile `json:"files_skipped,omitempty"`
	ConflictCandidates []ConflictCandidate      `json:"conflict_candidates,omitempty"`
	BeforeSummary      string                   `json:"before_summary,omitempty"`
	AfterSummary       string                   `json:"after_summary,omitempty"`
}

type IntegrationAppliedFile struct {
	WorkerID   string `json:"worker_id,omitempty"`
	WorkerName string `json:"worker_name,omitempty"`
	Path       string `json:"path,omitempty"`
	ChangeKind string `json:"change_kind,omitempty"`
}

type IntegrationSkippedFile struct {
	WorkerID   string `json:"worker_id,omitempty"`
	WorkerName string `json:"worker_name,omitempty"`
	Path       string `json:"path,omitempty"`
	ChangeKind string `json:"change_kind,omitempty"`
	Reason     string `json:"reason,omitempty"`
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

	for _, statement := range []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
	} {
		if _, err := store.db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return store, nil
}

func IsBusyError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "sqlite_busy") ||
		strings.Contains(text, "sqlite_locked") ||
		strings.Contains(text, "database is locked") ||
		strings.Contains(text, "database table is locked") ||
		strings.Contains(text, "database schema is locked")
}

func WithBusyRetry(ctx context.Context, fn func() error) error {
	if fn == nil {
		return nil
	}
	delays := []time.Duration{
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
		350 * time.Millisecond,
		500 * time.Millisecond,
	}
	var err error
	for attempt := 0; attempt <= len(delays); attempt++ {
		err = fn()
		if err == nil || !IsBusyError(err) {
			return err
		}
		if attempt == len(delays) {
			break
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
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
			latest_stop_reason TEXT NOT NULL DEFAULT '',
			runtime_issue_reason TEXT NOT NULL DEFAULT '',
			runtime_issue_message TEXT NOT NULL DEFAULT '',
			previous_response_id TEXT NOT NULL DEFAULT '',
			human_replies_json TEXT NOT NULL DEFAULT '',
			collected_context_json TEXT NOT NULL DEFAULT '',
			planner_operator_status_json TEXT NOT NULL DEFAULT '',
			executor_transport TEXT NOT NULL DEFAULT '',
			executor_thread_id TEXT NOT NULL DEFAULT '',
			executor_thread_path TEXT NOT NULL DEFAULT '',
			executor_turn_id TEXT NOT NULL DEFAULT '',
			executor_turn_status TEXT NOT NULL DEFAULT '',
			executor_last_success INTEGER,
			executor_last_failure_stage TEXT NOT NULL DEFAULT '',
			executor_last_error TEXT NOT NULL DEFAULT '',
			executor_last_message TEXT NOT NULL DEFAULT '',
			executor_approval_json TEXT NOT NULL DEFAULT '',
			executor_last_control_json TEXT NOT NULL DEFAULT ''
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
		`CREATE TABLE IF NOT EXISTS workers (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			worker_name TEXT NOT NULL,
			worker_status TEXT NOT NULL,
			assigned_scope TEXT NOT NULL DEFAULT '',
			worktree_path TEXT NOT NULL,
			worker_task_summary TEXT NOT NULL DEFAULT '',
			worker_executor_prompt_summary TEXT NOT NULL DEFAULT '',
			worker_result_summary TEXT NOT NULL DEFAULT '',
			worker_error_summary TEXT NOT NULL DEFAULT '',
			executor_thread_id TEXT NOT NULL DEFAULT '',
			executor_turn_id TEXT NOT NULL DEFAULT '',
			executor_turn_status TEXT NOT NULL DEFAULT '',
			executor_approval_state TEXT NOT NULL DEFAULT '',
			executor_approval_kind TEXT NOT NULL DEFAULT '',
			executor_approval_preview TEXT NOT NULL DEFAULT '',
			executor_interruptible INTEGER NOT NULL DEFAULT 0,
			executor_steerable INTEGER NOT NULL DEFAULT 0,
			executor_failure_stage TEXT NOT NULL DEFAULT '',
			executor_last_control_action TEXT NOT NULL DEFAULT '',
			executor_approval_json TEXT NOT NULL DEFAULT '',
			executor_last_control_json TEXT NOT NULL DEFAULT '',
			assigned_at TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_workers_worktree_path
			ON workers(worktree_path);`,
		`CREATE INDEX IF NOT EXISTS idx_workers_run_updated_at
			ON workers(run_id, updated_at DESC);`,
		`CREATE TABLE IF NOT EXISTS pending_actions (
			run_id TEXT PRIMARY KEY,
			action_json TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS control_messages (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			target_binding TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			raw_text TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			consumed_at TEXT NOT NULL DEFAULT '',
			cancelled_at TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_control_messages_run_status_created_at
			ON control_messages(run_id, status, created_at ASC);`,
		`CREATE TABLE IF NOT EXISTS side_chat_messages (
			id TEXT PRIMARY KEY,
			repo_path TEXT NOT NULL,
			run_id TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL,
			context_policy TEXT NOT NULL DEFAULT '',
			raw_text TEXT NOT NULL,
			status TEXT NOT NULL,
			backend_state TEXT NOT NULL DEFAULT '',
			response_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_side_chat_messages_repo_created_at
			ON side_chat_messages(repo_path, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS side_chat_actions (
			id TEXT PRIMARY KEY,
			repo_path TEXT NOT NULL,
			run_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			request_text TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			result_message TEXT NOT NULL DEFAULT '',
			control_message_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_side_chat_actions_repo_created_at
			ON side_chat_actions(repo_path, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS dogfood_issues (
			id TEXT PRIMARY KEY,
			repo_path TEXT NOT NULL,
			run_id TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL,
			title TEXT NOT NULL,
			note TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_dogfood_issues_repo_created_at
			ON dogfood_issues(repo_path, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS build_time (
			repo_path TEXT PRIMARY KEY,
			total_build_time_ms INTEGER NOT NULL DEFAULT 0,
			current_active_session_started_at TEXT NOT NULL DEFAULT '',
			last_active_session_ended_at TEXT NOT NULL DEFAULT '',
			current_step_started_at TEXT NOT NULL DEFAULT '',
			current_run_started_at TEXT NOT NULL DEFAULT '',
			current_step_label TEXT NOT NULL DEFAULT '',
			planner_active_duration_ms INTEGER NOT NULL DEFAULT 0,
			executor_active_duration_ms INTEGER NOT NULL DEFAULT 0,
			executor_thinking_duration_ms INTEGER NOT NULL DEFAULT 0,
			command_active_duration_ms INTEGER NOT NULL DEFAULT 0,
			install_active_duration_ms INTEGER NOT NULL DEFAULT 0,
			test_active_duration_ms INTEGER NOT NULL DEFAULT 0,
			human_wait_duration_ms INTEGER NOT NULL DEFAULT 0,
			blocked_duration_ms INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	if err := s.ensureRunsColumn(ctx, "previous_response_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "latest_stop_reason", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "runtime_issue_reason", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "runtime_issue_message", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "human_replies_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "collected_context_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "planner_operator_status_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
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
	if err := s.ensureRunsColumn(ctx, "executor_last_success", "INTEGER"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_last_failure_stage", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_last_error", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_last_message", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_approval_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureRunsColumn(ctx, "executor_last_control_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "worker_task_summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "worker_executor_prompt_summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "worker_result_summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "worker_error_summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_turn_status", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_approval_state", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_approval_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_approval_preview", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_interruptible", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_steerable", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_failure_stage", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_last_control_action", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_approval_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "executor_last_control_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "assigned_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "started_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureWorkersColumn(ctx, "completed_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, "PRAGMA user_version = 20;"); err != nil {
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
			id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, latest_stop_reason, runtime_issue_reason, runtime_issue_message, previous_response_id, human_replies_json, collected_context_json,
			executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_success, executor_last_failure_stage, executor_last_error, executor_last_message, executor_approval_json, executor_last_control_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		"",
		"",
		"",
		"",
		nil,
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
		ID:                  runID,
		RepoPath:            params.RepoPath,
		Goal:                params.Goal,
		Status:              status,
		LatestStopReason:    "",
		RuntimeIssueReason:  "",
		RuntimeIssueMessage: "",
		PreviousResponseID:  "",
		HumanReplies:        nil,
		CollectedContext:    nil,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
		LatestCheckpoint:    checkpoint,
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

func (s *Store) SavePlannerCompletion(ctx context.Context, runID string, previousResponseID string, checkpoint Checkpoint) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}
	if strings.TrimSpace(previousResponseID) == "" {
		return errors.New("previous response id is required")
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
		     latest_stop_reason = '',
		     runtime_issue_reason = '',
		     runtime_issue_message = '',
		     previous_response_id = ?,
		     status = ?
		 WHERE id = ?`,
		formatTime(checkpoint.CreatedAt),
		string(checkpointJSON),
		previousResponseID,
		string(StatusCompleted),
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

func (s *Store) RecordHumanReply(ctx context.Context, runID string, source string, payload string, receivedAt time.Time) (HumanReply, error) {
	if strings.TrimSpace(runID) == "" {
		return HumanReply{}, errors.New("run id is required")
	}
	if strings.TrimSpace(source) == "" {
		return HumanReply{}, errors.New("human reply source is required")
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	} else {
		receivedAt = receivedAt.UTC()
	}

	replyID, err := newHumanReplyID()
	if err != nil {
		return HumanReply{}, err
	}

	reply := HumanReply{
		ID:         replyID,
		Source:     strings.TrimSpace(source),
		ReceivedAt: receivedAt,
		Payload:    payload,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HumanReply{}, err
	}
	defer tx.Rollback()

	var existingJSON string
	if err := tx.QueryRowContext(ctx, `SELECT human_replies_json FROM runs WHERE id = ? LIMIT 1`, runID).Scan(&existingJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HumanReply{}, fmt.Errorf("run %s not found", runID)
		}
		return HumanReply{}, err
	}

	replies, err := unmarshalHumanReplies(existingJSON)
	if err != nil {
		return HumanReply{}, err
	}
	replies = append(replies, reply)

	encoded, err := marshalHumanReplies(replies)
	if err != nil {
		return HumanReply{}, err
	}

	result, err := tx.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     latest_stop_reason = '',
		     runtime_issue_reason = '',
		     runtime_issue_message = '',
		     human_replies_json = ?
		 WHERE id = ?`,
		formatTime(receivedAt),
		encoded,
		runID,
	)
	if err != nil {
		return HumanReply{}, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return HumanReply{}, err
	}
	if rows == 0 {
		return HumanReply{}, fmt.Errorf("run %s not found", runID)
	}

	if err := tx.Commit(); err != nil {
		return HumanReply{}, err
	}

	return reply, nil
}

func (s *Store) SaveCollectedContext(ctx context.Context, runID string, collectedContext *CollectedContextState) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}

	encoded, err := marshalCollectedContext(collectedContext)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     latest_stop_reason = '',
		     runtime_issue_reason = '',
		     runtime_issue_message = '',
		     collected_context_json = ?
		 WHERE id = ?`,
		formatTime(time.Now().UTC()),
		encoded,
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

func (s *Store) SaveExecutorState(ctx context.Context, runID string, executorState ExecutorState) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}

	approvalJSON, err := marshalExecutorApproval(executorState.Approval)
	if err != nil {
		return err
	}
	lastControlJSON, err := marshalExecutorControl(executorState.LastControl)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     latest_stop_reason = '',
		     runtime_issue_reason = '',
		     runtime_issue_message = '',
		     executor_transport = ?,
		     executor_thread_id = ?,
		     executor_thread_path = ?,
		     executor_turn_id = ?,
		     executor_turn_status = ?,
		     executor_last_success = ?,
		     executor_last_failure_stage = ?,
		     executor_last_error = ?,
		     executor_last_message = ?,
		     executor_approval_json = ?,
		     executor_last_control_json = ?
		 WHERE id = ?`,
		formatTime(time.Now().UTC()),
		strings.TrimSpace(executorState.Transport),
		strings.TrimSpace(executorState.ThreadID),
		strings.TrimSpace(executorState.ThreadPath),
		strings.TrimSpace(executorState.TurnID),
		strings.TrimSpace(executorState.TurnStatus),
		optionalBoolToSQL(executorState.LastSuccess),
		strings.TrimSpace(executorState.LastFailureStage),
		strings.TrimSpace(executorState.LastError),
		strings.TrimSpace(executorState.LastMessage),
		approvalJSON,
		lastControlJSON,
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
	approvalJSON, err := marshalExecutorApproval(executorState.Approval)
	if err != nil {
		return err
	}
	lastControlJSON, err := marshalExecutorControl(executorState.LastControl)
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
		     latest_stop_reason = '',
		     runtime_issue_reason = '',
		     runtime_issue_message = '',
		     executor_transport = ?,
		     executor_thread_id = ?,
		     executor_thread_path = ?,
		     executor_turn_id = ?,
		     executor_turn_status = ?,
		     executor_last_success = ?,
		     executor_last_failure_stage = ?,
		     executor_last_error = ?,
		     executor_last_message = ?,
		     executor_approval_json = ?,
		     executor_last_control_json = ?
		 WHERE id = ?`,
		formatTime(checkpoint.CreatedAt),
		string(checkpointJSON),
		strings.TrimSpace(executorState.Transport),
		strings.TrimSpace(executorState.ThreadID),
		strings.TrimSpace(executorState.ThreadPath),
		strings.TrimSpace(executorState.TurnID),
		strings.TrimSpace(executorState.TurnStatus),
		optionalBoolToSQL(executorState.LastSuccess),
		strings.TrimSpace(executorState.LastFailureStage),
		strings.TrimSpace(executorState.LastError),
		strings.TrimSpace(executorState.LastMessage),
		approvalJSON,
		lastControlJSON,
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
		`SELECT id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, latest_stop_reason, runtime_issue_reason, runtime_issue_message, previous_response_id, human_replies_json, collected_context_json, planner_operator_status_json,
		        executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_success, executor_last_failure_stage, executor_last_error, executor_last_message, executor_approval_json, executor_last_control_json
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
		`SELECT id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, latest_stop_reason, runtime_issue_reason, runtime_issue_message, previous_response_id, human_replies_json, collected_context_json, planner_operator_status_json,
		        executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_success, executor_last_failure_stage, executor_last_error, executor_last_message, executor_approval_json, executor_last_control_json
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
		`SELECT id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, latest_stop_reason, runtime_issue_reason, runtime_issue_message, previous_response_id, human_replies_json, collected_context_json, planner_operator_status_json,
		        executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_success, executor_last_failure_stage, executor_last_error, executor_last_message, executor_approval_json, executor_last_control_json
		 FROM runs
		 ORDER BY updated_at DESC, created_at DESC
		 LIMIT 1`,
	)
}

func (s *Store) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 {
		return []Run{}, nil
	}

	return s.queryRuns(ctx,
		`SELECT id, repo_path, goal, status, created_at, updated_at, latest_checkpoint_json, latest_stop_reason, runtime_issue_reason, runtime_issue_message, previous_response_id, human_replies_json, collected_context_json, planner_operator_status_json,
		        executor_transport, executor_thread_id, executor_thread_path, executor_turn_id, executor_turn_status, executor_last_success, executor_last_failure_stage, executor_last_error, executor_last_message, executor_approval_json, executor_last_control_json
		 FROM runs
		 ORDER BY updated_at DESC, created_at DESC
		 LIMIT ?`,
		limit,
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

func (s *Store) SaveRuntimeIssue(ctx context.Context, runID string, reason string, message string) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("runtime issue reason is required")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("runtime issue message is required")
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     latest_stop_reason = ?,
		     runtime_issue_reason = ?,
		     runtime_issue_message = ?
		 WHERE id = ?`,
		formatTime(time.Now().UTC()),
		strings.TrimSpace(reason),
		strings.TrimSpace(reason),
		strings.TrimSpace(message),
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

func (s *Store) MarkRunAbandoned(ctx context.Context, runID string, reason string, message string) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("abandon reason is required")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("abandon message is required")
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     status = ?,
		     latest_stop_reason = ?,
		     runtime_issue_reason = ?,
		     runtime_issue_message = ?
		 WHERE id = ?`,
		formatTime(time.Now().UTC()),
		string(StatusCancelled),
		strings.TrimSpace(reason),
		strings.TrimSpace(reason),
		strings.TrimSpace(message),
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

func (s *Store) SaveLatestStopReason(ctx context.Context, runID string, reason string) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("latest stop reason is required")
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     latest_stop_reason = ?
		 WHERE id = ?`,
		formatTime(time.Now().UTC()),
		strings.TrimSpace(reason),
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

func (s *Store) ClearLatestStopReason(ctx context.Context, runID string) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     latest_stop_reason = ''
		 WHERE id = ?`,
		formatTime(time.Now().UTC()),
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

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(scanner rowScanner) (Run, error) {
	var (
		run                       Run
		status                    string
		createdAtRaw              string
		updatedAtRaw              string
		checkpointJSON            string
		latestStopReason          string
		runtimeIssueReason        string
		runtimeIssueMessage       string
		previousResponseID        string
		humanRepliesJSON          string
		collectedContextJSON      string
		plannerOperatorStatusJSON string
		executorTransport         string
		executorThreadID          string
		executorThreadPath        string
		executorTurnID            string
		executorTurnStatus        string
		executorLastSuccess       sql.NullInt64
		executorLastFailureStage  string
		executorLastError         string
		executorLastMessage       string
		executorApprovalJSON      string
		executorLastControlJSON   string
	)

	if err := scanner.Scan(
		&run.ID,
		&run.RepoPath,
		&run.Goal,
		&status,
		&createdAtRaw,
		&updatedAtRaw,
		&checkpointJSON,
		&latestStopReason,
		&runtimeIssueReason,
		&runtimeIssueMessage,
		&previousResponseID,
		&humanRepliesJSON,
		&collectedContextJSON,
		&plannerOperatorStatusJSON,
		&executorTransport,
		&executorThreadID,
		&executorThreadPath,
		&executorTurnID,
		&executorTurnStatus,
		&executorLastSuccess,
		&executorLastFailureStage,
		&executorLastError,
		&executorLastMessage,
		&executorApprovalJSON,
		&executorLastControlJSON,
	); err != nil {
		return Run{}, err
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return Run{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return Run{}, err
	}

	var checkpoint Checkpoint
	if err := json.Unmarshal([]byte(checkpointJSON), &checkpoint); err != nil {
		return Run{}, err
	}

	run.Status = RunStatus(status)
	run.LatestStopReason = latestStopReason
	run.RuntimeIssueReason = runtimeIssueReason
	run.RuntimeIssueMessage = runtimeIssueMessage
	run.PreviousResponseID = previousResponseID
	humanReplies, err := unmarshalHumanReplies(humanRepliesJSON)
	if err != nil {
		return Run{}, err
	}
	run.HumanReplies = humanReplies
	collectedContext, err := unmarshalCollectedContext(collectedContextJSON)
	if err != nil {
		return Run{}, err
	}
	run.CollectedContext = collectedContext
	plannerOperatorStatus, err := unmarshalPlannerOperatorStatus(plannerOperatorStatusJSON)
	if err != nil {
		return Run{}, err
	}
	run.PlannerOperatorStatus = plannerOperatorStatus
	run.ExecutorTransport = executorTransport
	run.ExecutorThreadID = executorThreadID
	run.ExecutorThreadPath = executorThreadPath
	run.ExecutorTurnID = executorTurnID
	run.ExecutorTurnStatus = executorTurnStatus
	run.ExecutorLastSuccess = sqlToOptionalBool(executorLastSuccess)
	run.ExecutorLastFailureStage = executorLastFailureStage
	run.ExecutorLastError = executorLastError
	run.ExecutorLastMessage = executorLastMessage
	executorApproval, err := unmarshalExecutorApproval(executorApprovalJSON)
	if err != nil {
		return Run{}, err
	}
	run.ExecutorApproval = executorApproval
	executorLastControl, err := unmarshalExecutorControl(executorLastControlJSON)
	if err != nil {
		return Run{}, err
	}
	run.ExecutorLastControl = executorLastControl
	run.CreatedAt = createdAt
	run.UpdatedAt = updatedAt
	run.LatestCheckpoint = checkpoint
	return run, nil
}

func (s *Store) querySingleRun(ctx context.Context, query string, args ...any) (Run, bool, error) {
	row := s.db.QueryRowContext(ctx, query, args...)

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, false, nil
		}
		return Run{}, false, err
	}

	return run, true, nil
}

func (s *Store) queryRuns(ctx context.Context, query string, args ...any) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]Run, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return runs, nil
}

func (s *Store) ensureRunsColumn(ctx context.Context, columnName string, definition string) error {
	return s.ensureTableColumn(ctx, "runs", columnName, definition)
}

func (s *Store) ensureWorkersColumn(ctx context.Context, columnName string, definition string) error {
	return s.ensureTableColumn(ctx, "workers", columnName, definition)
}

func (s *Store) ensureTableColumn(ctx context.Context, tableName string, columnName string, definition string) error {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", tableName))
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

	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", tableName, columnName, definition))
	return err
}

func updateRunStatement(persistResponseID bool) string {
	if persistResponseID {
		return `UPDATE runs
		 SET updated_at = ?, latest_checkpoint_json = ?, latest_stop_reason = '', runtime_issue_reason = '', runtime_issue_message = '', previous_response_id = ?
		 WHERE id = ?`
	}

	return `UPDATE runs
		SET updated_at = ?, latest_checkpoint_json = ?, latest_stop_reason = '', runtime_issue_reason = '', runtime_issue_message = ''
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

func marshalHumanReplies(replies []HumanReply) (string, error) {
	if len(replies) == 0 {
		return "", nil
	}

	encoded, err := json.Marshal(replies)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func unmarshalHumanReplies(value string) ([]HumanReply, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	var replies []HumanReply
	if err := json.Unmarshal([]byte(value), &replies); err != nil {
		return nil, err
	}
	return replies, nil
}

func marshalCollectedContext(collectedContext *CollectedContextState) (string, error) {
	if collectedContext == nil {
		return "", nil
	}

	encoded, err := json.Marshal(collectedContext)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func unmarshalCollectedContext(value string) (*CollectedContextState, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	var collectedContext CollectedContextState
	if err := json.Unmarshal([]byte(value), &collectedContext); err != nil {
		return nil, err
	}
	return &collectedContext, nil
}

func marshalExecutorApproval(approval *ExecutorApproval) (string, error) {
	if approval == nil {
		return "", nil
	}

	encoded, err := json.Marshal(approval)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func unmarshalExecutorApproval(value string) (*ExecutorApproval, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	var approval ExecutorApproval
	if err := json.Unmarshal([]byte(value), &approval); err != nil {
		return nil, err
	}
	return &approval, nil
}

func marshalExecutorControl(control *ExecutorControl) (string, error) {
	if control == nil {
		return "", nil
	}

	encoded, err := json.Marshal(control)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func unmarshalExecutorControl(value string) (*ExecutorControl, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	var control ExecutorControl
	if err := json.Unmarshal([]byte(value), &control); err != nil {
		return nil, err
	}
	return &control, nil
}

func newRunID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "run_" + hex.EncodeToString(bytes), nil
}

func newHumanReplyID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "human_" + hex.EncodeToString(bytes), nil
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

func optionalBoolToSQL(value *bool) any {
	if value == nil {
		return nil
	}
	return boolToInt(*value)
}

func sqlToOptionalBool(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}
	result := value.Int64 != 0
	return &result
}
