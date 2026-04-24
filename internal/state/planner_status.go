package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PlannerOperatorStatus struct {
	ContractVersion    string `json:"contract_version,omitempty"`
	OperatorMessage    string `json:"operator_message,omitempty"`
	CurrentFocus       string `json:"current_focus,omitempty"`
	NextIntendedStep   string `json:"next_intended_step,omitempty"`
	WhyThisStep        string `json:"why_this_step,omitempty"`
	ProgressPercent    int    `json:"progress_percent,omitempty"`
	ProgressConfidence string `json:"progress_confidence,omitempty"`
	ProgressBasis      string `json:"progress_basis,omitempty"`
}

func (s *Store) SavePlannerOperatorStatus(ctx context.Context, runID string, status *PlannerOperatorStatus) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}

	encoded, err := marshalPlannerOperatorStatus(status)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE runs
		 SET updated_at = ?,
		     latest_stop_reason = '',
		     runtime_issue_reason = '',
		     runtime_issue_message = '',
		     planner_operator_status_json = ?
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

func marshalPlannerOperatorStatus(status *PlannerOperatorStatus) (string, error) {
	if status == nil {
		return "", nil
	}

	encoded, err := json.Marshal(status)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func unmarshalPlannerOperatorStatus(value string) (*PlannerOperatorStatus, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	var status PlannerOperatorStatus
	if err := json.Unmarshal([]byte(value), &status); err != nil {
		return nil, err
	}
	return &status, nil
}
