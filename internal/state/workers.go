package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type WorkerStatus string

const (
	WorkerStatusCreating         WorkerStatus = "creating"
	WorkerStatusPending          WorkerStatus = "pending"
	WorkerStatusAssigned         WorkerStatus = "assigned"
	WorkerStatusIdle             WorkerStatus = "idle"
	WorkerStatusExecutorActive   WorkerStatus = "executor_active"
	WorkerStatusApprovalRequired WorkerStatus = "approval_required"
	WorkerStatusCompleted        WorkerStatus = "completed"
	WorkerStatusFailed           WorkerStatus = "failed"
)

type Worker struct {
	ID                          string
	RunID                       string
	WorkerName                  string
	WorkerStatus                WorkerStatus
	AssignedScope               string
	WorktreePath                string
	WorkerTaskSummary           string
	WorkerExecutorPromptSummary string
	WorkerResultSummary         string
	WorkerErrorSummary          string
	ExecutorThreadID            string
	ExecutorTurnID              string
	ExecutorTurnStatus          string
	ExecutorApprovalState       string
	ExecutorApprovalKind        string
	ExecutorApprovalPreview     string
	ExecutorInterruptible       bool
	ExecutorSteerable           bool
	ExecutorFailureStage        string
	ExecutorLastControlAction   string
	ExecutorApproval            *ExecutorApproval
	ExecutorLastControl         *ExecutorControl
	AssignedAt                  time.Time
	StartedAt                   time.Time
	CompletedAt                 time.Time
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

type CreateWorkerParams struct {
	RunID         string
	WorkerName    string
	WorkerStatus  WorkerStatus
	AssignedScope string
	WorktreePath  string
	CreatedAt     time.Time
}

type WorkerStats struct {
	Total  int64
	Active int64
}

func IsWorkerActive(status WorkerStatus) bool {
	switch status {
	case WorkerStatusCreating, WorkerStatusPending, WorkerStatusAssigned, WorkerStatusExecutorActive, WorkerStatusApprovalRequired:
		return true
	default:
		return false
	}
}

func (s *Store) CreateWorker(ctx context.Context, params CreateWorkerParams) (Worker, error) {
	if strings.TrimSpace(params.RunID) == "" {
		return Worker{}, errors.New("worker run id is required")
	}
	if strings.TrimSpace(params.WorkerName) == "" {
		return Worker{}, errors.New("worker name is required")
	}
	if strings.TrimSpace(params.WorktreePath) == "" {
		return Worker{}, errors.New("worker worktree path is required")
	}

	status := params.WorkerStatus
	if status == "" {
		status = WorkerStatusCreating
	}

	createdAt := params.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}

	workerID, err := newWorkerID()
	if err != nil {
		return Worker{}, err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workers (
			id, run_id, worker_name, worker_status, assigned_scope, worktree_path,
			worker_task_summary, worker_executor_prompt_summary, worker_result_summary,
			worker_error_summary, executor_thread_id, executor_turn_id, executor_turn_status,
			executor_approval_state, executor_approval_kind, executor_approval_preview,
			executor_interruptible, executor_steerable, executor_failure_stage, executor_last_control_action,
			executor_approval_json, executor_last_control_json,
			assigned_at, started_at, completed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		workerID,
		strings.TrimSpace(params.RunID),
		strings.TrimSpace(params.WorkerName),
		string(status),
		strings.TrimSpace(params.AssignedScope),
		strings.TrimSpace(params.WorktreePath),
		"", // worker_task_summary
		"", // worker_executor_prompt_summary
		"", // worker_result_summary
		"", // worker_error_summary
		"", // executor_thread_id
		"", // executor_turn_id
		"", // executor_turn_status
		"", // executor_approval_state
		"", // executor_approval_kind
		"", // executor_approval_preview
		0,  // executor_interruptible
		0,  // executor_steerable
		"", // executor_failure_stage
		"", // executor_last_control_action
		"", // executor_approval_json
		"", // executor_last_control_json
		"", // assigned_at
		"", // started_at
		"", // completed_at
		formatTime(createdAt),
		formatTime(createdAt),
	)
	if err != nil {
		return Worker{}, err
	}

	return Worker{
		ID:            workerID,
		RunID:         strings.TrimSpace(params.RunID),
		WorkerName:    strings.TrimSpace(params.WorkerName),
		WorkerStatus:  status,
		AssignedScope: strings.TrimSpace(params.AssignedScope),
		WorktreePath:  strings.TrimSpace(params.WorktreePath),
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}, nil
}

func (s *Store) GetWorker(ctx context.Context, workerID string) (Worker, bool, error) {
	return s.querySingleWorker(ctx,
		`SELECT id, run_id, worker_name, worker_status, assigned_scope, worktree_path,
		        worker_task_summary, worker_executor_prompt_summary, worker_result_summary,
		        worker_error_summary, executor_thread_id, executor_turn_id, executor_turn_status,
		        executor_approval_state, executor_approval_kind, executor_approval_preview,
		        executor_interruptible, executor_steerable, executor_failure_stage, executor_last_control_action,
		        executor_approval_json, executor_last_control_json,
		        assigned_at, started_at, completed_at, created_at, updated_at
		 FROM workers
		 WHERE id = ?
		 LIMIT 1`,
		strings.TrimSpace(workerID),
	)
}

func (s *Store) GetWorkerByPath(ctx context.Context, worktreePath string) (Worker, bool, error) {
	return s.querySingleWorker(ctx,
		`SELECT id, run_id, worker_name, worker_status, assigned_scope, worktree_path,
		        worker_task_summary, worker_executor_prompt_summary, worker_result_summary,
		        worker_error_summary, executor_thread_id, executor_turn_id, executor_turn_status,
		        executor_approval_state, executor_approval_kind, executor_approval_preview,
		        executor_interruptible, executor_steerable, executor_failure_stage, executor_last_control_action,
		        executor_approval_json, executor_last_control_json,
		        assigned_at, started_at, completed_at, created_at, updated_at
		 FROM workers
		 WHERE worktree_path = ?
		 LIMIT 1`,
		strings.TrimSpace(worktreePath),
	)
}

func (s *Store) ListWorkers(ctx context.Context, runID string) ([]Worker, error) {
	if strings.TrimSpace(runID) == "" {
		return s.queryWorkers(ctx,
			`SELECT id, run_id, worker_name, worker_status, assigned_scope, worktree_path,
			        worker_task_summary, worker_executor_prompt_summary, worker_result_summary,
			        worker_error_summary, executor_thread_id, executor_turn_id, executor_turn_status,
			        executor_approval_state, executor_approval_kind, executor_approval_preview,
			        executor_interruptible, executor_steerable, executor_failure_stage, executor_last_control_action,
			        executor_approval_json, executor_last_control_json,
			        assigned_at, started_at, completed_at, created_at, updated_at
			 FROM workers
			 ORDER BY updated_at DESC, created_at DESC`,
		)
	}

	return s.queryWorkers(ctx,
		`SELECT id, run_id, worker_name, worker_status, assigned_scope, worktree_path,
		        worker_task_summary, worker_executor_prompt_summary, worker_result_summary,
		        worker_error_summary, executor_thread_id, executor_turn_id, executor_turn_status,
		        executor_approval_state, executor_approval_kind, executor_approval_preview,
		        executor_interruptible, executor_steerable, executor_failure_stage, executor_last_control_action,
		        executor_approval_json, executor_last_control_json,
		        assigned_at, started_at, completed_at, created_at, updated_at
		 FROM workers
		 WHERE run_id = ?
		 ORDER BY updated_at DESC, created_at DESC`,
		strings.TrimSpace(runID),
	)
}

func (s *Store) WorkerStats(ctx context.Context, runID string) (WorkerStats, error) {
	activeStatuses := []string{
		string(WorkerStatusCreating),
		string(WorkerStatusPending),
		string(WorkerStatusAssigned),
		string(WorkerStatusExecutorActive),
		string(WorkerStatusApprovalRequired),
	}

	var row *sql.Row
	if strings.TrimSpace(runID) == "" {
		row = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*),
			        COALESCE(SUM(CASE WHEN worker_status IN (?, ?, ?, ?, ?) THEN 1 ELSE 0 END), 0)
			   FROM workers`,
			activeStatuses[0],
			activeStatuses[1],
			activeStatuses[2],
			activeStatuses[3],
			activeStatuses[4],
		)
	} else {
		row = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*),
			        COALESCE(SUM(CASE WHEN worker_status IN (?, ?, ?, ?, ?) THEN 1 ELSE 0 END), 0)
			   FROM workers
			  WHERE run_id = ?`,
			activeStatuses[0],
			activeStatuses[1],
			activeStatuses[2],
			activeStatuses[3],
			activeStatuses[4],
			strings.TrimSpace(runID),
		)
	}

	var stats WorkerStats
	if err := row.Scan(&stats.Total, &stats.Active); err != nil {
		return WorkerStats{}, err
	}
	return stats, nil
}

func (s *Store) SaveWorker(ctx context.Context, worker Worker) error {
	if strings.TrimSpace(worker.ID) == "" {
		return errors.New("worker id is required")
	}
	if strings.TrimSpace(worker.RunID) == "" {
		return errors.New("worker run id is required")
	}
	if strings.TrimSpace(worker.WorkerName) == "" {
		return errors.New("worker name is required")
	}
	if strings.TrimSpace(worker.WorktreePath) == "" {
		return errors.New("worker worktree path is required")
	}
	if worker.WorkerStatus == "" {
		return errors.New("worker status is required")
	}

	updatedAt := worker.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	} else {
		updatedAt = updatedAt.UTC()
	}
	approvalJSON, err := marshalExecutorApproval(worker.ExecutorApproval)
	if err != nil {
		return err
	}
	lastControlJSON, err := marshalExecutorControl(worker.ExecutorLastControl)
	if err != nil {
		return err
	}

	approvalState := strings.TrimSpace(worker.ExecutorApprovalState)
	approvalKind := strings.TrimSpace(worker.ExecutorApprovalKind)
	if worker.ExecutorApproval != nil {
		if approvalState == "" {
			approvalState = strings.TrimSpace(worker.ExecutorApproval.State)
		}
		if approvalKind == "" {
			approvalKind = strings.TrimSpace(worker.ExecutorApproval.Kind)
		}
	}
	lastControlAction := strings.TrimSpace(worker.ExecutorLastControlAction)
	if worker.ExecutorLastControl != nil && lastControlAction == "" {
		lastControlAction = strings.TrimSpace(worker.ExecutorLastControl.Action)
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE workers
		 SET run_id = ?,
		     worker_name = ?,
		     worker_status = ?,
		     assigned_scope = ?,
		     worktree_path = ?,
		     worker_task_summary = ?,
		     worker_executor_prompt_summary = ?,
		     worker_result_summary = ?,
		     worker_error_summary = ?,
		     executor_thread_id = ?,
		     executor_turn_id = ?,
		     executor_turn_status = ?,
		     executor_approval_state = ?,
		     executor_approval_kind = ?,
		     executor_approval_preview = ?,
		     executor_interruptible = ?,
		     executor_steerable = ?,
		     executor_failure_stage = ?,
		     executor_last_control_action = ?,
		     executor_approval_json = ?,
		     executor_last_control_json = ?,
		     assigned_at = ?,
		     started_at = ?,
		     completed_at = ?,
		     updated_at = ?
		 WHERE id = ?`,
		strings.TrimSpace(worker.RunID),
		strings.TrimSpace(worker.WorkerName),
		string(worker.WorkerStatus),
		strings.TrimSpace(worker.AssignedScope),
		strings.TrimSpace(worker.WorktreePath),
		strings.TrimSpace(worker.WorkerTaskSummary),
		strings.TrimSpace(worker.WorkerExecutorPromptSummary),
		strings.TrimSpace(worker.WorkerResultSummary),
		strings.TrimSpace(worker.WorkerErrorSummary),
		strings.TrimSpace(worker.ExecutorThreadID),
		strings.TrimSpace(worker.ExecutorTurnID),
		strings.TrimSpace(worker.ExecutorTurnStatus),
		approvalState,
		approvalKind,
		strings.TrimSpace(worker.ExecutorApprovalPreview),
		boolToInt(worker.ExecutorInterruptible),
		boolToInt(worker.ExecutorSteerable),
		strings.TrimSpace(worker.ExecutorFailureStage),
		lastControlAction,
		approvalJSON,
		lastControlJSON,
		formatOptionalTime(worker.AssignedAt),
		formatOptionalTime(worker.StartedAt),
		formatOptionalTime(worker.CompletedAt),
		formatTime(updatedAt),
		strings.TrimSpace(worker.ID),
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("worker %s not found", worker.ID)
	}

	return nil
}

func (s *Store) DeleteWorker(ctx context.Context, workerID string) error {
	if strings.TrimSpace(workerID) == "" {
		return errors.New("worker id is required")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM workers WHERE id = ?`, strings.TrimSpace(workerID))
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("worker %s not found", workerID)
	}
	return nil
}

func (s *Store) querySingleWorker(ctx context.Context, query string, args ...any) (Worker, bool, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	worker, err := scanWorker(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Worker{}, false, nil
		}
		return Worker{}, false, err
	}
	return worker, true, nil
}

func (s *Store) queryWorkers(ctx context.Context, query string, args ...any) ([]Worker, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workers := make([]Worker, 0)
	for rows.Next() {
		worker, err := scanWorker(rows)
		if err != nil {
			return nil, err
		}
		workers = append(workers, worker)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return workers, nil
}

func scanWorker(scanner rowScanner) (Worker, error) {
	var (
		worker            Worker
		status            string
		executorApproval  string
		executorControl   string
		executorInterrupt int64
		executorSteer     int64
		assignedAtRaw     string
		startedAtRaw      string
		completedAtRaw    string
		createdAtRaw      string
		updatedAtRaw      string
	)

	if err := scanner.Scan(
		&worker.ID,
		&worker.RunID,
		&worker.WorkerName,
		&status,
		&worker.AssignedScope,
		&worker.WorktreePath,
		&worker.WorkerTaskSummary,
		&worker.WorkerExecutorPromptSummary,
		&worker.WorkerResultSummary,
		&worker.WorkerErrorSummary,
		&worker.ExecutorThreadID,
		&worker.ExecutorTurnID,
		&worker.ExecutorTurnStatus,
		&worker.ExecutorApprovalState,
		&worker.ExecutorApprovalKind,
		&worker.ExecutorApprovalPreview,
		&executorInterrupt,
		&executorSteer,
		&worker.ExecutorFailureStage,
		&worker.ExecutorLastControlAction,
		&executorApproval,
		&executorControl,
		&assignedAtRaw,
		&startedAtRaw,
		&completedAtRaw,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return Worker{}, err
	}

	assignedAt, err := parseOptionalTime(assignedAtRaw)
	if err != nil {
		return Worker{}, err
	}
	startedAt, err := parseOptionalTime(startedAtRaw)
	if err != nil {
		return Worker{}, err
	}
	completedAt, err := parseOptionalTime(completedAtRaw)
	if err != nil {
		return Worker{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return Worker{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return Worker{}, err
	}
	approval, err := unmarshalExecutorApproval(executorApproval)
	if err != nil {
		return Worker{}, err
	}
	lastControl, err := unmarshalExecutorControl(executorControl)
	if err != nil {
		return Worker{}, err
	}

	worker.WorkerStatus = WorkerStatus(status)
	worker.ExecutorInterruptible = executorInterrupt != 0
	worker.ExecutorSteerable = executorSteer != 0
	worker.ExecutorApproval = approval
	worker.ExecutorLastControl = lastControl
	worker.AssignedAt = assignedAt
	worker.StartedAt = startedAt
	worker.CompletedAt = completedAt
	worker.CreatedAt = createdAt
	worker.UpdatedAt = updatedAt
	return worker, nil
}

func parseOptionalTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, raw)
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTime(value)
}

func newWorkerID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "worker_" + hex.EncodeToString(bytes), nil
}
