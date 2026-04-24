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
)

type PendingAction struct {
	TurnType                 string                `json:"turn_type"`
	PlannerOutcome           string                `json:"planner_outcome,omitempty"`
	PlannerResponseID        string                `json:"planner_response_id,omitempty"`
	PendingActionSummary     string                `json:"pending_action_summary,omitempty"`
	PendingExecutorPrompt    string                `json:"pending_executor_prompt,omitempty"`
	PendingExecutorSummary   string                `json:"pending_executor_prompt_summary,omitempty"`
	PendingDispatchTarget    *PendingDispatchTarget `json:"pending_dispatch_target,omitempty"`
	PendingReason            string                `json:"pending_reason,omitempty"`
	Held                     bool                  `json:"held,omitempty"`
	HoldReason               string                `json:"hold_reason,omitempty"`
	UpdatedAt                time.Time             `json:"updated_at,omitempty"`
}

type PendingDispatchTarget struct {
	Kind        string `json:"kind,omitempty"`
	WorkerID    string `json:"worker_id,omitempty"`
	WorkerName  string `json:"worker_name,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
}

type ControlMessageStatus string

const (
	ControlMessageQueued   ControlMessageStatus = "queued"
	ControlMessageConsumed ControlMessageStatus = "consumed"
	ControlMessageCanceled ControlMessageStatus = "cancelled"
)

type ControlMessage struct {
	ID            string               `json:"id"`
	RunID         string               `json:"run_id"`
	TargetBinding string               `json:"target_binding,omitempty"`
	Source        string               `json:"source"`
	Reason        string               `json:"reason,omitempty"`
	RawText       string               `json:"raw_text"`
	Status        ControlMessageStatus `json:"status"`
	CreatedAt     time.Time            `json:"created_at"`
	ConsumedAt    time.Time            `json:"consumed_at,omitempty"`
	CancelledAt   time.Time            `json:"cancelled_at,omitempty"`
}

type CreateControlMessageParams struct {
	RunID         string
	TargetBinding string
	Source        string
	Reason        string
	RawText       string
	CreatedAt     time.Time
}

func (s *Store) SavePendingAction(ctx context.Context, runID string, pending *PendingAction) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}

	if pending == nil {
		_, err := s.db.ExecContext(ctx, `DELETE FROM pending_actions WHERE run_id = ?`, strings.TrimSpace(runID))
		return err
	}

	updatedAt := pending.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	} else {
		updatedAt = updatedAt.UTC()
	}
	pendingCopy := *pending
	pendingCopy.UpdatedAt = updatedAt

	encoded, err := json.Marshal(pendingCopy)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO pending_actions (run_id, action_json, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(run_id) DO UPDATE SET
		     action_json = excluded.action_json,
		     updated_at = excluded.updated_at`,
		strings.TrimSpace(runID),
		string(encoded),
		formatTime(updatedAt),
	)
	return err
}

func (s *Store) GetPendingAction(ctx context.Context, runID string) (PendingAction, bool, error) {
	if strings.TrimSpace(runID) == "" {
		return PendingAction{}, false, errors.New("run id is required")
	}

	row := s.db.QueryRowContext(ctx,
		`SELECT action_json
		   FROM pending_actions
		  WHERE run_id = ?
		  LIMIT 1`,
		strings.TrimSpace(runID),
	)

	var encoded string
	if err := row.Scan(&encoded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PendingAction{}, false, nil
		}
		return PendingAction{}, false, err
	}

	var pending PendingAction
	if err := json.Unmarshal([]byte(encoded), &pending); err != nil {
		return PendingAction{}, false, err
	}
	return pending, true, nil
}

func (s *Store) RecordControlMessage(ctx context.Context, params CreateControlMessageParams) (ControlMessage, error) {
	if strings.TrimSpace(params.RunID) == "" {
		return ControlMessage{}, errors.New("control message run id is required")
	}
	if strings.TrimSpace(params.Source) == "" {
		return ControlMessage{}, errors.New("control message source is required")
	}
	if params.RawText == "" {
		return ControlMessage{}, errors.New("control message raw text is required")
	}

	messageID, err := newControlMessageID()
	if err != nil {
		return ControlMessage{}, err
	}

	createdAt := params.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}

	message := ControlMessage{
		ID:            messageID,
		RunID:         strings.TrimSpace(params.RunID),
		TargetBinding: strings.TrimSpace(params.TargetBinding),
		Source:        strings.TrimSpace(params.Source),
		Reason:        strings.TrimSpace(params.Reason),
		RawText:       params.RawText,
		Status:        ControlMessageQueued,
		CreatedAt:     createdAt,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO control_messages (
			id, run_id, target_binding, source, reason, raw_text, status, created_at, consumed_at, cancelled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID,
		message.RunID,
		message.TargetBinding,
		message.Source,
		message.Reason,
		message.RawText,
		string(message.Status),
		formatTime(message.CreatedAt),
		"",
		"",
	)
	if err != nil {
		return ControlMessage{}, err
	}

	return message, nil
}

func (s *Store) ListControlMessages(ctx context.Context, runID string, status ControlMessageStatus, limit int) ([]ControlMessage, error) {
	if limit <= 0 {
		limit = 20
	}

	baseQuery := `SELECT id, run_id, target_binding, source, reason, raw_text, status, created_at, consumed_at, cancelled_at
		FROM control_messages`
	var clauses []string
	var args []any

	if strings.TrimSpace(runID) != "" {
		clauses = append(clauses, "run_id = ?")
		args = append(args, strings.TrimSpace(runID))
	}
	if strings.TrimSpace(string(status)) != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(status))
	}
	if len(clauses) > 0 {
		baseQuery += " WHERE " + strings.Join(clauses, " AND ")
	}
	baseQuery += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]ControlMessage, 0)
	for rows.Next() {
		message, err := scanControlMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *Store) NextQueuedControlMessage(ctx context.Context, runID string) (ControlMessage, bool, error) {
	if strings.TrimSpace(runID) == "" {
		return ControlMessage{}, false, errors.New("run id is required")
	}

	row := s.db.QueryRowContext(ctx,
		`SELECT id, run_id, target_binding, source, reason, raw_text, status, created_at, consumed_at, cancelled_at
		   FROM control_messages
		  WHERE run_id = ? AND status = ?
		  ORDER BY created_at ASC, id ASC
		  LIMIT 1`,
		strings.TrimSpace(runID),
		string(ControlMessageQueued),
	)
	message, err := scanControlMessage(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ControlMessage{}, false, nil
		}
		return ControlMessage{}, false, err
	}
	return message, true, nil
}

func (s *Store) ConsumeControlMessage(ctx context.Context, messageID string, consumedAt time.Time) error {
	return s.updateControlMessageStatus(ctx, messageID, ControlMessageConsumed, consumedAt)
}

func (s *Store) CancelControlMessage(ctx context.Context, messageID string, cancelledAt time.Time) error {
	return s.updateControlMessageStatus(ctx, messageID, ControlMessageCanceled, cancelledAt)
}

func (s *Store) updateControlMessageStatus(ctx context.Context, messageID string, status ControlMessageStatus, changedAt time.Time) error {
	if strings.TrimSpace(messageID) == "" {
		return errors.New("control message id is required")
	}
	if strings.TrimSpace(string(status)) == "" {
		return errors.New("control message status is required")
	}
	if changedAt.IsZero() {
		changedAt = time.Now().UTC()
	} else {
		changedAt = changedAt.UTC()
	}

	consumedAt := ""
	cancelledAt := ""
	switch status {
	case ControlMessageConsumed:
		consumedAt = formatTime(changedAt)
	case ControlMessageCanceled:
		cancelledAt = formatTime(changedAt)
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE control_messages
		    SET status = ?,
		        consumed_at = ?,
		        cancelled_at = ?
		  WHERE id = ?`,
		string(status),
		consumedAt,
		cancelledAt,
		strings.TrimSpace(messageID),
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("control message %s not found", messageID)
	}
	return nil
}

func scanControlMessage(scanner rowScanner) (ControlMessage, error) {
	var (
		message         ControlMessage
		status          string
		createdAtRaw    string
		consumedAtRaw   string
		cancelledAtRaw  string
	)

	if err := scanner.Scan(
		&message.ID,
		&message.RunID,
		&message.TargetBinding,
		&message.Source,
		&message.Reason,
		&message.RawText,
		&status,
		&createdAtRaw,
		&consumedAtRaw,
		&cancelledAtRaw,
	); err != nil {
		return ControlMessage{}, err
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return ControlMessage{}, err
	}
	consumedAt, err := parseOptionalTime(consumedAtRaw)
	if err != nil {
		return ControlMessage{}, err
	}
	cancelledAt, err := parseOptionalTime(cancelledAtRaw)
	if err != nil {
		return ControlMessage{}, err
	}

	message.Status = ControlMessageStatus(status)
	message.CreatedAt = createdAt
	message.ConsumedAt = consumedAt
	message.CancelledAt = cancelledAt
	return message, nil
}

func newControlMessageID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "control_" + hex.EncodeToString(bytes), nil
}
