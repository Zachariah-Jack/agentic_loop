package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Journal struct {
	path string
	mu   sync.Mutex
}

type Event struct {
	At                    time.Time      `json:"at"`
	Type                  string         `json:"type"`
	RunID                 string         `json:"run_id,omitempty"`
	RepoPath              string         `json:"repo_path,omitempty"`
	Goal                  string         `json:"goal,omitempty"`
	Status                string         `json:"status,omitempty"`
	Message               string         `json:"message,omitempty"`
	ResponseID            string         `json:"response_id,omitempty"`
	PreviousResponseID    string         `json:"previous_response_id,omitempty"`
	PlannerOutcome        string         `json:"planner_outcome,omitempty"`
	ExecutorTransport     string         `json:"executor_transport,omitempty"`
	ExecutorThreadID      string         `json:"executor_thread_id,omitempty"`
	ExecutorThreadPath    string         `json:"executor_thread_path,omitempty"`
	ExecutorTurnID        string         `json:"executor_turn_id,omitempty"`
	ExecutorTurnStatus    string         `json:"executor_turn_status,omitempty"`
	ExecutorOutputPreview string         `json:"executor_output_preview,omitempty"`
	Checkpoint            *CheckpointRef `json:"checkpoint,omitempty"`
}

type CheckpointRef struct {
	Sequence  int64  `json:"sequence"`
	Stage     string `json:"stage"`
	Label     string `json:"label"`
	SafePause bool   `json:"safe_pause"`
}

func Open(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}

	return &Journal{path: path}, nil
}

func (j *Journal) Append(event Event) error {
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	} else {
		event.At = event.At.UTC()
	}

	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	j.mu.Lock()
	defer j.mu.Unlock()

	file, err := os.OpenFile(j.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(line)
	return err
}

func (j *Journal) ReadRecent(runID string, limit int) ([]Event, error) {
	if limit <= 0 {
		return []Event{}, nil
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	file, err := os.Open(j.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	events := make([]Event, 0, limit)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		if runID != "" && event.RunID != "" && event.RunID != runID {
			continue
		}
		if runID != "" && event.RunID == "" {
			continue
		}

		events = append(events, event)
		if len(events) > limit {
			events = events[len(events)-limit:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return events, nil
}
