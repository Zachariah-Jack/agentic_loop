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

const (
	SideChatMessageRecorded SideChatMessageStatus = "recorded"
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

func newSideChatMessageID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "sidechat_" + hex.EncodeToString(bytes), nil
}
