package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type SideChatMessageStatus string
type SideChatActionStatus string

const (
	SideChatMessageRecorded SideChatMessageStatus = "recorded"

	SideChatActionQueued           SideChatActionStatus = "queued"
	SideChatActionCompleted        SideChatActionStatus = "completed"
	SideChatActionApprovalRequired SideChatActionStatus = "approval_required"
	SideChatActionUnsupported      SideChatActionStatus = "unsupported"
	SideChatActionFailed           SideChatActionStatus = "failed"
)

type SideChatMessage struct {
	ID              string
	RepoPath        string
	RunID           string
	Source          string
	ContextPolicy   string
	RawText         string
	Status          SideChatMessageStatus
	BackendState    string
	ResponseMessage string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateSideChatMessageParams struct {
	RepoPath        string
	RunID           string
	Source          string
	ContextPolicy   string
	RawText         string
	BackendState    string
	ResponseMessage string
	CreatedAt       time.Time
}

type SideChatAction struct {
	ID               string
	RepoPath         string
	RunID            string
	Action           string
	RequestText      string
	Source           string
	Reason           string
	Status           SideChatActionStatus
	ResultMessage    string
	ControlMessageID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateSideChatActionParams struct {
	RepoPath         string
	RunID            string
	Action           string
	RequestText      string
	Source           string
	Reason           string
	Status           SideChatActionStatus
	ResultMessage    string
	ControlMessageID string
	CreatedAt        time.Time
}

func (s *Store) RecordSideChatMessage(ctx context.Context, params CreateSideChatMessageParams) (SideChatMessage, error) {
	if strings.TrimSpace(params.RepoPath) == "" {
		return SideChatMessage{}, errors.New("side chat repo path is required")
	}
	if strings.TrimSpace(params.Source) == "" {
		return SideChatMessage{}, errors.New("side chat source is required")
	}
	if strings.TrimSpace(params.RawText) == "" {
		return SideChatMessage{}, errors.New("side chat message text is required")
	}

	messageID, err := newSideChatMessageID()
	if err != nil {
		return SideChatMessage{}, err
	}

	createdAt := params.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}

	message := SideChatMessage{
		ID:              messageID,
		RepoPath:        strings.TrimSpace(params.RepoPath),
		RunID:           strings.TrimSpace(params.RunID),
		Source:          strings.TrimSpace(params.Source),
		ContextPolicy:   strings.TrimSpace(params.ContextPolicy),
		RawText:         params.RawText,
		Status:          SideChatMessageRecorded,
		BackendState:    strings.TrimSpace(params.BackendState),
		ResponseMessage: strings.TrimSpace(params.ResponseMessage),
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO side_chat_messages (
			id, repo_path, run_id, source, context_policy, raw_text, status, backend_state, response_message, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID,
		message.RepoPath,
		message.RunID,
		message.Source,
		message.ContextPolicy,
		message.RawText,
		string(message.Status),
		message.BackendState,
		message.ResponseMessage,
		formatTime(message.CreatedAt),
		formatTime(message.UpdatedAt),
	)
	if err != nil {
		return SideChatMessage{}, err
	}

	return message, nil
}

func (s *Store) ListSideChatMessages(ctx context.Context, repoPath string, limit int) ([]SideChatMessage, error) {
	if strings.TrimSpace(repoPath) == "" {
		return nil, errors.New("side chat repo path is required")
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_path, run_id, source, context_policy, raw_text, status, backend_state, response_message, created_at, updated_at
		   FROM side_chat_messages
		  WHERE repo_path = ?
		  ORDER BY created_at DESC, id DESC
		  LIMIT ?`,
		strings.TrimSpace(repoPath),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]SideChatMessage, 0)
	for rows.Next() {
		item, err := scanSideChatMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) RecordSideChatAction(ctx context.Context, params CreateSideChatActionParams) (SideChatAction, error) {
	if strings.TrimSpace(params.RepoPath) == "" {
		return SideChatAction{}, errors.New("side chat action repo path is required")
	}
	if strings.TrimSpace(params.Action) == "" {
		return SideChatAction{}, errors.New("side chat action is required")
	}
	if strings.TrimSpace(string(params.Status)) == "" {
		params.Status = SideChatActionQueued
	}
	actionID, err := newSideChatActionID()
	if err != nil {
		return SideChatAction{}, err
	}
	createdAt := params.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	action := SideChatAction{
		ID:               actionID,
		RepoPath:         strings.TrimSpace(params.RepoPath),
		RunID:            strings.TrimSpace(params.RunID),
		Action:           strings.TrimSpace(params.Action),
		RequestText:      params.RequestText,
		Source:           strings.TrimSpace(params.Source),
		Reason:           strings.TrimSpace(params.Reason),
		Status:           params.Status,
		ResultMessage:    strings.TrimSpace(params.ResultMessage),
		ControlMessageID: strings.TrimSpace(params.ControlMessageID),
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO side_chat_actions (
			id, repo_path, run_id, action, request_text, source, reason, status,
			result_message, control_message_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		action.ID,
		action.RepoPath,
		action.RunID,
		action.Action,
		action.RequestText,
		action.Source,
		action.Reason,
		string(action.Status),
		action.ResultMessage,
		action.ControlMessageID,
		formatTime(action.CreatedAt),
		formatTime(action.UpdatedAt),
	)
	if err != nil {
		return SideChatAction{}, err
	}
	return action, nil
}

func (s *Store) ListSideChatActions(ctx context.Context, repoPath string, limit int) ([]SideChatAction, error) {
	if strings.TrimSpace(repoPath) == "" {
		return nil, errors.New("side chat action repo path is required")
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_path, run_id, action, request_text, source, reason, status,
		        result_message, control_message_id, created_at, updated_at
		   FROM side_chat_actions
		  WHERE repo_path = ?
		  ORDER BY created_at DESC, id DESC
		  LIMIT ?`,
		strings.TrimSpace(repoPath),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SideChatAction, 0)
	for rows.Next() {
		item, err := scanSideChatAction(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanSideChatMessage(scanner rowScanner) (SideChatMessage, error) {
	var (
		message      SideChatMessage
		status       string
		createdAtRaw string
		updatedAtRaw string
	)

	if err := scanner.Scan(
		&message.ID,
		&message.RepoPath,
		&message.RunID,
		&message.Source,
		&message.ContextPolicy,
		&message.RawText,
		&status,
		&message.BackendState,
		&message.ResponseMessage,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return SideChatMessage{}, err
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return SideChatMessage{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return SideChatMessage{}, err
	}

	message.Status = SideChatMessageStatus(status)
	message.CreatedAt = createdAt
	message.UpdatedAt = updatedAt
	return message, nil
}

func scanSideChatAction(scanner rowScanner) (SideChatAction, error) {
	var (
		action       SideChatAction
		status       string
		createdAtRaw string
		updatedAtRaw string
	)
	if err := scanner.Scan(
		&action.ID,
		&action.RepoPath,
		&action.RunID,
		&action.Action,
		&action.RequestText,
		&action.Source,
		&action.Reason,
		&status,
		&action.ResultMessage,
		&action.ControlMessageID,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return SideChatAction{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return SideChatAction{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return SideChatAction{}, err
	}
	action.Status = SideChatActionStatus(status)
	action.CreatedAt = createdAt
	action.UpdatedAt = updatedAt
	return action, nil
}

func newSideChatMessageID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "sidechat_" + hex.EncodeToString(bytes), nil
}

func newSideChatActionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "sidechat_action_" + hex.EncodeToString(bytes), nil
}
