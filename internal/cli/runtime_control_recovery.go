package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/control"
	"orchestrator/internal/state"
)

const (
	activeRunGuardFileName = "active-run-guard.json"
	activeRunGuardOwner    = "orchestrator-control-server"
	staleRunRecoveredCode  = "stale_run_recovered"
)

type activeRunGuardFile struct {
	Owner            string `json:"owner"`
	SessionID        string `json:"session_id"`
	RunID            string `json:"run_id"`
	Action           string `json:"action"`
	Status           string `json:"status"`
	RepoPath         string `json:"repo_path"`
	BackendPID       int    `json:"backend_pid"`
	BackendStartedAt string `json:"backend_started_at"`
	StartedAt        string `json:"started_at"`
	UpdatedAt        string `json:"updated_at"`
}

func activeRunGuardPath(layout state.Layout) string {
	if strings.TrimSpace(layout.StateDir) == "" {
		return ""
	}
	return filepath.Join(layout.StateDir, activeRunGuardFileName)
}

func newControlSessionID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("session_%d_%d", os.Getpid(), time.Now().UnixNano())
	}
	return "session_" + hex.EncodeToString(bytes[:])
}

func writeActiveRunGuard(inv Invocation, reservation controlRunReservation, run state.Run, status string) error {
	path := activeRunGuardPath(inv.Layout)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	now := time.Now().UTC()
	guard := activeRunGuardFile{
		Owner:            activeRunGuardOwner,
		SessionID:        reservation.sessionID,
		RunID:            strings.TrimSpace(run.ID),
		Action:           reservation.action,
		Status:           strings.TrimSpace(status),
		RepoPath:         strings.TrimSpace(run.RepoPath),
		BackendPID:       os.Getpid(),
		BackendStartedAt: formatSnapshotTime(controlBackendStartedAt),
		StartedAt:        formatSnapshotTime(reservation.started),
		UpdatedAt:        formatSnapshotTime(now),
	}
	encoded, err := json.MarshalIndent(guard, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func removeActiveRunGuard(inv Invocation, reservation controlRunReservation) {
	path := activeRunGuardPath(inv.Layout)
	if path == "" {
		return
	}
	guard, found, err := readActiveRunGuardFile(inv.Layout)
	if err != nil || !found {
		return
	}
	if strings.TrimSpace(guard.SessionID) != strings.TrimSpace(reservation.sessionID) {
		return
	}
	_ = os.Remove(path)
}

func readActiveRunGuardFile(layout state.Layout) (activeRunGuardFile, bool, error) {
	path := activeRunGuardPath(layout)
	if path == "" {
		return activeRunGuardFile{}, false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return activeRunGuardFile{}, false, nil
		}
		return activeRunGuardFile{}, false, err
	}
	var guard activeRunGuardFile
	if err := json.Unmarshal(raw, &guard); err != nil {
		return activeRunGuardFile{}, false, err
	}
	return guard, true, nil
}

func buildActiveRunGuardSnapshot(inv Invocation) controlActiveRunGuardSnapshot {
	guard, found, err := readActiveRunGuardFile(inv.Layout)
	path := activeRunGuardPath(inv.Layout)
	if err != nil {
		return controlActiveRunGuardSnapshot{
			Present:     true,
			Path:        path,
			Stale:       true,
			StaleReason: "active run guard could not be read: " + err.Error(),
			Message:     "The active run guard is unreadable. Use Recover Backend / Unlock Repo to clear it mechanically if no run is actually progressing.",
		}
	}
	if !found {
		return controlActiveRunGuardSnapshot{
			Present:        false,
			Path:           path,
			CurrentBackend: true,
			Message:        "no active run guard is currently recorded",
		}
	}
	currentBackend := guard.BackendPID == os.Getpid() && strings.TrimSpace(guard.BackendStartedAt) == formatSnapshotTime(controlBackendStartedAt)
	stale := !currentBackend
	staleReason := ""
	message := "active run is owned by the current backend process"
	if stale {
		staleReason = "active run guard belongs to a previous backend process/session"
		message = "This run was active under a previous backend process and may be stale. Recover Backend / Unlock Repo can mechanically clear the stale active-run guard without deleting history or artifacts."
	}
	return controlActiveRunGuardSnapshot{
		Present:          true,
		Path:             path,
		RunID:            strings.TrimSpace(guard.RunID),
		Action:           strings.TrimSpace(guard.Action),
		Status:           strings.TrimSpace(guard.Status),
		RepoPath:         strings.TrimSpace(guard.RepoPath),
		BackendPID:       guard.BackendPID,
		BackendStartedAt: strings.TrimSpace(guard.BackendStartedAt),
		SessionID:        strings.TrimSpace(guard.SessionID),
		StartedAt:        strings.TrimSpace(guard.StartedAt),
		UpdatedAt:        strings.TrimSpace(guard.UpdatedAt),
		CurrentBackend:   currentBackend,
		Stale:            stale,
		StaleReason:      staleReason,
		Message:          message,
	}
}

func (m *controlRunManager) activeRunGuardSnapshot(inv Invocation) controlActiveRunGuardSnapshot {
	snapshot := buildActiveRunGuardSnapshot(inv)
	m.mu.Lock()
	active := m.active
	activeID := m.activeID
	action := m.action
	started := m.started
	m.mu.Unlock()
	if active {
		snapshot.Present = true
		snapshot.RunID = firstNonEmpty(snapshot.RunID, activeID)
		snapshot.Action = firstNonEmpty(snapshot.Action, action)
		snapshot.Status = "active"
		snapshot.BackendPID = os.Getpid()
		snapshot.BackendStartedAt = formatSnapshotTime(controlBackendStartedAt)
		snapshot.StartedAt = firstNonEmpty(snapshot.StartedAt, formatSnapshotTime(started))
		snapshot.CurrentlyProcessing = true
		snapshot.WaitingAtSafePoint = false
		snapshot.LastProgressAt = formatSnapshotTime(started)
		snapshot.CurrentBackend = true
		snapshot.Stale = false
		snapshot.StaleReason = ""
		snapshot.Message = "a control-server-launched run loop is currently active"
	}
	return snapshot
}

func (m *controlRunManager) RecoverStaleRun(ctx context.Context, inv Invocation, request control.RecoverStaleRunRequest) (controlStaleRunRecoverySnapshot, error) {
	m.mu.Lock()
	currentActive := m.active
	currentRunID := m.activeID
	m.mu.Unlock()
	if currentActive {
		return controlStaleRunRecoverySnapshot{}, fmt.Errorf("run %s is actively owned by this backend; request Safe Stop before recovering it", currentRunID)
	}

	guard := buildActiveRunGuardSnapshot(inv)
	runID := strings.TrimSpace(request.RunID)
	if runID == "" {
		runID = strings.TrimSpace(guard.RunID)
	}

	if !pathExists(inv.Layout.DBPath) {
		if guard.Present && (guard.Stale || request.Force) {
			_ = os.Remove(guard.Path)
			cleared := buildActiveRunGuardSnapshot(inv)
			return controlStaleRunRecoverySnapshot{
				Recovered:          false,
				Reason:             staleRunRecoveredCode,
				ActiveGuardCleared: true,
				Guard:              &cleared,
				Message:            "runtime state is not initialized; stale active-run guard was cleared but no run row was changed",
			}, nil
		}
		return controlStaleRunRecoverySnapshot{}, errors.New("runtime state is not initialized for stale run recovery")
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		return controlStaleRunRecoverySnapshot{}, err
	}
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		return controlStaleRunRecoverySnapshot{}, err
	}

	var run state.Run
	var found bool
	if runID != "" {
		run, found, err = store.GetRun(ctx, runID)
	} else {
		run, found, err = store.LatestResumableRun(ctx)
	}
	if err != nil {
		return controlStaleRunRecoverySnapshot{}, err
	}
	if !found {
		if guard.Present && (guard.Stale || request.Force) {
			_ = os.Remove(guard.Path)
			cleared := buildActiveRunGuardSnapshot(inv)
			return controlStaleRunRecoverySnapshot{
				Recovered:          false,
				Reason:             staleRunRecoveredCode,
				ActiveGuardCleared: true,
				Guard:              &cleared,
				Message:            "no matching unfinished run was found; stale active-run guard was cleared",
			}, nil
		}
		return controlStaleRunRecoverySnapshot{}, errors.New("no unfinished run is available for stale run recovery")
	}

	if run.Status == state.StatusCompleted || run.Status == state.StatusFailed || run.Status == state.StatusCancelled {
		if guard.Present && (guard.Stale || request.Force) && (strings.TrimSpace(guard.RunID) == "" || strings.TrimSpace(guard.RunID) == strings.TrimSpace(run.ID) || request.Force) {
			_ = os.Remove(guard.Path)
			cleared := buildActiveRunGuardSnapshot(inv)
			return controlStaleRunRecoverySnapshot{
				Recovered:          false,
				RunID:              run.ID,
				Reason:             staleRunRecoveredCode,
				Status:             string(run.Status),
				ActiveGuardCleared: !cleared.Present,
				NextOperatorAction: nextOperatorActionForExistingRun(run),
				Guard:              &cleared,
				Message:            "selected run was already terminal; stale active-run guard was cleared and no run history was changed",
			}, nil
		}
		return controlStaleRunRecoverySnapshot{
			Recovered:          false,
			RunID:              run.ID,
			Reason:             staleRunRecoveredCode,
			Status:             string(run.Status),
			NextOperatorAction: nextOperatorActionForExistingRun(run),
			Guard:              &guard,
			Message:            "selected run is already terminal; no recovery change was needed",
		}, nil
	}
	if !guard.Stale && !request.Force && strings.TrimSpace(guard.RunID) == strings.TrimSpace(run.ID) {
		return controlStaleRunRecoverySnapshot{}, errors.New("active run guard is owned by the current backend; use Safe Stop before stale recovery")
	}

	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = staleRunRecoveredCode
	}
	if guard.Present && (strings.TrimSpace(guard.RunID) == "" || strings.TrimSpace(guard.RunID) == strings.TrimSpace(run.ID) || request.Force) {
		_ = os.Remove(guard.Path)
	}
	updated, found, err := store.GetRun(ctx, run.ID)
	if err != nil {
		return controlStaleRunRecoverySnapshot{}, err
	}
	if !found {
		updated = run
	}
	cleared := buildActiveRunGuardSnapshot(inv)
	message := "stale active-run guard was mechanically cleared; run history, status, checkpoint, and artifacts were preserved"
	emitEngineEvent(inv, "stale_run_recovered", eventPayloadForRun(updated, map[string]any{
		"reason":               reason,
		"message":              message,
		"run_status_unchanged": true,
	}))
	nextAction := nextOperatorActionForExistingRun(updated)
	resultMessage := "Recovered stale active run guard. Continue Build is now available when the run is otherwise resumable."
	if nextAction != "continue_existing_run" {
		resultMessage = "Recovered stale active run guard. Run history was preserved; follow the refreshed next operator action."
	}
	return controlStaleRunRecoverySnapshot{
		Recovered:          true,
		RunID:              updated.ID,
		Reason:             reason,
		Status:             string(updated.Status),
		ActiveGuardCleared: !cleared.Present,
		NextOperatorAction: nextAction,
		Guard:              &cleared,
		Message:            resultMessage,
	}, nil
}
