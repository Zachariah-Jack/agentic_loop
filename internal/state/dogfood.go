package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type DogfoodIssue struct {
	ID        string
	RepoPath  string
	RunID     string
	Source    string
	Title     string
	Note      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateDogfoodIssueParams struct {
	RepoPath  string
	RunID     string
	Source    string
	Title     string
	Note      string
	CreatedAt time.Time
}

func (s *Store) RecordDogfoodIssue(ctx context.Context, params CreateDogfoodIssueParams) (DogfoodIssue, error) {
	if strings.TrimSpace(params.RepoPath) == "" {
		return DogfoodIssue{}, errors.New("dogfood issue repo path is required")
	}
	if strings.TrimSpace(params.Source) == "" {
		return DogfoodIssue{}, errors.New("dogfood issue source is required")
	}
	if strings.TrimSpace(params.Title) == "" {
		return DogfoodIssue{}, errors.New("dogfood issue title is required")
	}
	if strings.TrimSpace(params.Note) == "" {
		return DogfoodIssue{}, errors.New("dogfood issue note is required")
	}

	issueID, err := newDogfoodIssueID()
	if err != nil {
		return DogfoodIssue{}, err
	}

	createdAt := params.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}

	issue := DogfoodIssue{
		ID:        issueID,
		RepoPath:  strings.TrimSpace(params.RepoPath),
		RunID:     strings.TrimSpace(params.RunID),
		Source:    strings.TrimSpace(params.Source),
		Title:     strings.TrimSpace(params.Title),
		Note:      params.Note,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO dogfood_issues (
			id, repo_path, run_id, source, title, note, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		issue.ID,
		issue.RepoPath,
		issue.RunID,
		issue.Source,
		issue.Title,
		issue.Note,
		formatTime(issue.CreatedAt),
		formatTime(issue.UpdatedAt),
	)
	if err != nil {
		return DogfoodIssue{}, err
	}

	return issue, nil
}

func (s *Store) ListDogfoodIssues(ctx context.Context, repoPath string, limit int) ([]DogfoodIssue, error) {
	if strings.TrimSpace(repoPath) == "" {
		return nil, errors.New("dogfood issue repo path is required")
	}
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_path, run_id, source, title, note, created_at, updated_at
		   FROM dogfood_issues
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

	items := make([]DogfoodIssue, 0)
	for rows.Next() {
		item, err := scanDogfoodIssue(rows)
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

func scanDogfoodIssue(scanner rowScanner) (DogfoodIssue, error) {
	var (
		issue        DogfoodIssue
		createdAtRaw string
		updatedAtRaw string
	)

	if err := scanner.Scan(
		&issue.ID,
		&issue.RepoPath,
		&issue.RunID,
		&issue.Source,
		&issue.Title,
		&issue.Note,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return DogfoodIssue{}, err
	}

	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return DogfoodIssue{}, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtRaw)
	if err != nil {
		return DogfoodIssue{}, err
	}

	issue.CreatedAt = createdAt
	issue.UpdatedAt = updatedAt
	return issue, nil
}

func newDogfoodIssueID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return "dogfood_" + hex.EncodeToString(bytes), nil
}
