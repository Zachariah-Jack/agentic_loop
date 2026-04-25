package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"orchestrator/internal/control"
	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

type controlRunLaunchSnapshot struct {
	Accepted  bool   `json:"accepted"`
	Async     bool   `json:"async"`
	Action    string `json:"action"`
	RunID     string `json:"run_id,omitempty"`
	Status    string `json:"status"`
	RepoPath  string `json:"repo_path,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	Message   string `json:"message,omitempty"`
}

type controlRunManager struct {
	mu       sync.Mutex
	active   bool
	activeID string
	action   string
	started  time.Time
}

func newControlRunManager() *controlRunManager {
	return &controlRunManager{}
}

func (m *controlRunManager) StartRun(ctx context.Context, inv Invocation, request control.StartRunRequest) (controlRunLaunchSnapshot, error) {
	if strings.TrimSpace(request.Goal) == "" {
		return controlRunLaunchSnapshot{}, errors.New("start_run requires goal")
	}
	if err := validateControlRunRepoPath(inv, request.RepoPath); err != nil {
		return controlRunLaunchSnapshot{}, err
	}
	if contract := inspectTargetRepoContract(inv.RepoRoot); !contract.Ready {
		return controlRunLaunchSnapshot{}, fmt.Errorf("target repo contract is not ready for start_run: %s", strings.Join(contract.Missing, ", "))
	}

	reservation, err := m.reserve("start_run", "")
	if err != nil {
		return controlRunLaunchSnapshot{}, err
	}

	store, journalWriter, run, err := createAutoRun(ctx, controlRunInvocation(inv), strings.TrimSpace(request.Goal))
	if err != nil {
		m.finish(reservation)
		return controlRunLaunchSnapshot{}, err
	}
	m.attachRunID(reservation, run.ID)

	go m.runForeground(reservation, controlRunInvocation(inv), store, journalWriter, run, foregroundLoopMode{
		Command:         "control start_run",
		RunAction:       "created_new_run",
		EventPrefix:     "run",
		InvocationLabel: "control start_run",
		StopFlagKey:     "run.stop_flag_path",
	})

	return controlRunLaunchSnapshot{
		Accepted:  true,
		Async:     true,
		Action:    "start_run",
		RunID:     run.ID,
		Status:    "started",
		RepoPath:  run.RepoPath,
		StartedAt: formatSnapshotTime(reservation.started),
		Message:   "start_run accepted; foreground loop is running asynchronously inside the control server process",
	}, nil
}

func (m *controlRunManager) ContinueRun(ctx context.Context, inv Invocation, request control.ContinueRunRequest) (controlRunLaunchSnapshot, error) {
	if err := validateControlRunRepoPath(inv, request.RepoPath); err != nil {
		return controlRunLaunchSnapshot{}, err
	}
	if contract := inspectTargetRepoContract(inv.RepoRoot); !contract.Ready {
		return controlRunLaunchSnapshot{}, fmt.Errorf("target repo contract is not ready for continue_run: %s", strings.Join(contract.Missing, ", "))
	}
	if !pathExists(inv.Layout.DBPath) {
		return controlRunLaunchSnapshot{}, errors.New("no unfinished run is available for continue_run")
	}

	reservation, err := m.reserve("continue_run", "")
	if err != nil {
		return controlRunLaunchSnapshot{}, err
	}

	store, journalWriter, err := ensureRuntime(ctx, inv.Layout)
	if err != nil {
		m.finish(reservation)
		return controlRunLaunchSnapshot{}, err
	}

	run, found, err := resolveControlContinueRun(ctx, store, request.RunID)
	if err != nil {
		_ = store.Close()
		m.finish(reservation)
		return controlRunLaunchSnapshot{}, err
	}
	if !found {
		_ = store.Close()
		m.finish(reservation)
		return controlRunLaunchSnapshot{}, errors.New("no unfinished run is available for continue_run")
	}
	if !isRunResumable(run) {
		_ = store.Close()
		m.finish(reservation)
		return controlRunLaunchSnapshot{}, fmt.Errorf("run %s is not resumable", run.ID)
	}
	m.attachRunID(reservation, run.ID)

	controlInv := controlRunInvocation(inv)
	emitEngineEvent(controlInv, "run_started", eventPayloadForRun(run, map[string]any{
		"action":  "continue_run",
		"resumed": true,
	}))

	go m.runForeground(reservation, controlInv, store, journalWriter, run, foregroundLoopMode{
		Command:         "control continue_run",
		RunAction:       "continued_existing_run",
		EventPrefix:     "continue",
		InvocationLabel: "control continue_run",
		StopFlagKey:     "continue.stop_flag_path",
	})

	return controlRunLaunchSnapshot{
		Accepted:  true,
		Async:     true,
		Action:    "continue_run",
		RunID:     run.ID,
		Status:    "started",
		RepoPath:  run.RepoPath,
		StartedAt: formatSnapshotTime(reservation.started),
		Message:   "continue_run accepted; foreground loop is running asynchronously inside the control server process",
	}, nil
}

type controlRunReservation struct {
	runID     string
	action    string
	started   time.Time
	sessionID string
}

func (m *controlRunManager) reserve(action string, runID string) (controlRunReservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active {
		return controlRunReservation{}, fmt.Errorf("run already active through control server: action=%s run_id=%s started_at=%s", m.action, m.activeID, formatSnapshotTime(m.started))
	}

	reservation := controlRunReservation{
		runID:     strings.TrimSpace(runID),
		action:    strings.TrimSpace(action),
		started:   time.Now().UTC(),
		sessionID: newControlSessionID(),
	}
	m.active = true
	m.activeID = reservation.runID
	m.action = reservation.action
	m.started = reservation.started
	return reservation, nil
}

func (m *controlRunManager) attachRunID(reservation controlRunReservation, runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.active || m.action != reservation.action || !m.started.Equal(reservation.started) {
		return
	}
	m.activeID = strings.TrimSpace(runID)
}

func (m *controlRunManager) finish(reservation controlRunReservation) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.active || m.action != reservation.action || !m.started.Equal(reservation.started) {
		return
	}
	m.active = false
	m.activeID = ""
	m.action = ""
	m.started = time.Time{}
}

func (m *controlRunManager) runForeground(
	reservation controlRunReservation,
	inv Invocation,
	store *state.Store,
	journalWriter *journal.Journal,
	run state.Run,
	mode foregroundLoopMode,
) {
	_ = writeActiveRunGuard(inv, reservation, run, "active")
	_ = store.StartBuildSession(context.Background(), run.RepoPath, run.ID, buildStepLabelForAction(reservation.action), reservation.started)
	defer m.finish(reservation)
	defer removeActiveRunGuard(inv, reservation)
	defer func() {
		_ = store.EndBuildSession(context.Background(), run.RepoPath, "loop stopped", time.Now().UTC())
	}()
	defer store.Close()

	if err := executeForegroundLoop(context.Background(), inv, store, journalWriter, run, mode); err != nil {
		emitEngineEvent(inv, "fault_recorded", eventPayloadForRun(run, map[string]any{
			"action": reservation.action,
			"error":  err.Error(),
		}))
	}
}

func buildStepLabelForAction(action string) string {
	switch strings.TrimSpace(action) {
	case "start_run":
		return "Starting build loop"
	case "continue_run":
		return "Continuing build loop"
	default:
		return "Orchestrator loop active"
	}
}

func (m *controlRunManager) applyActiveStatus(snapshot controlStatusSnapshot) controlStatusSnapshot {
	m.mu.Lock()
	active := m.active
	activeID := m.activeID
	action := m.action
	started := m.started
	m.mu.Unlock()

	if !active || snapshot.Run == nil || strings.TrimSpace(snapshot.Run.ID) != strings.TrimSpace(activeID) {
		return snapshot
	}

	snapshot.ActiveRunGuard.Present = true
	snapshot.ActiveRunGuard.RunID = activeID
	snapshot.ActiveRunGuard.Action = action
	snapshot.ActiveRunGuard.Status = "active"
	snapshot.ActiveRunGuard.BackendPID = os.Getpid()
	snapshot.ActiveRunGuard.BackendStartedAt = formatSnapshotTime(controlBackendStartedAt)
	snapshot.ActiveRunGuard.StartedAt = formatSnapshotTime(started)
	snapshot.ActiveRunGuard.CurrentlyProcessing = true
	snapshot.ActiveRunGuard.WaitingAtSafePoint = false
	snapshot.ActiveRunGuard.LastProgressAt = formatSnapshotTime(started)
	snapshot.ActiveRunGuard.CurrentBackend = true
	snapshot.ActiveRunGuard.Stale = false
	snapshot.ActiveRunGuard.StaleReason = ""
	snapshot.ActiveRunGuard.Message = "a control-server-launched run loop is currently active"
	snapshot.Run.Status = "active"
	snapshot.Run.StopReason = ""
	snapshot.Run.StartedAt = formatSnapshotTime(started)
	snapshot.Run.StoppedAt = ""
	snapshot.Run.ElapsedSeconds = int64(time.Since(started).Seconds())
	snapshot.Run.ElapsedLabel = "running for " + formatHumanDuration(time.Since(started))
	snapshot.Run.NextOperatorAction = "watch_progress"
	snapshot.Run.ActivityState = "running"
	activeStep := strings.TrimSpace(snapshot.BuildTime.CurrentStepLabel)
	if activeStep == "" || activeStep == "No active build step" || activeStep == "loop stopped" {
		activeStep = buildStepLabelForAction(action)
	}
	snapshot.Run.ActivityMessage = "Current step: " + activeStep + "."
	snapshot.Run.ActivelyProcessing = true
	snapshot.Run.WaitingAtSafePoint = false
	snapshot.Run.ExecuteReady = false
	snapshot.Run.Completed = false
	snapshot.Run.Resumable = false
	snapshot.PendingAction.Message = strings.TrimSpace(snapshot.PendingAction.Message)
	if snapshot.PendingAction.Message == "" {
		snapshot.PendingAction.Message = fmt.Sprintf("%s launched at %s is currently active", action, formatSnapshotTime(started))
	}
	elapsed := time.Since(started)
	if elapsed < 0 {
		elapsed = 0
	}
	stepStarted := started
	if strings.TrimSpace(snapshot.BuildTime.CurrentStepStartedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(snapshot.BuildTime.CurrentStepStartedAt)); err == nil {
			stepStarted = parsed
		}
	}
	stepElapsed := time.Since(stepStarted)
	if stepElapsed < 0 {
		stepElapsed = 0
	}
	total := time.Duration(snapshot.BuildTime.TotalBuildTimeMS)*time.Millisecond + elapsed
	snapshot.BuildTime.TotalBuildTimeMS = int64(total / time.Millisecond)
	snapshot.BuildTime.TotalBuildTimeLabel = formatHumanDuration(total)
	snapshot.BuildTime.CurrentRunTimeMS = int64(elapsed / time.Millisecond)
	snapshot.BuildTime.CurrentRunTimeLabel = formatHumanDuration(elapsed)
	snapshot.BuildTime.CurrentStepStartedAt = formatSnapshotTime(stepStarted)
	snapshot.BuildTime.CurrentStepTimeMS = int64(stepElapsed / time.Millisecond)
	snapshot.BuildTime.CurrentStepTimeLabel = formatHumanDuration(stepElapsed)
	snapshot.BuildTime.CurrentStepLabel = activeStep
	snapshot.BuildTime.CurrentActiveSessionStart = formatSnapshotTime(started)
	return snapshot
}

func controlRunInvocation(inv Invocation) Invocation {
	inv.Args = nil
	inv.Stdout = io.Discard
	inv.Stderr = io.Discard
	return inv
}

func validateControlRunRepoPath(inv Invocation, requestedRepoPath string) error {
	requested := strings.TrimSpace(requestedRepoPath)
	if requested == "" {
		return nil
	}

	serverRoot, err := filepath.Abs(inv.RepoRoot)
	if err != nil {
		return err
	}
	requestedRoot, err := filepath.Abs(requested)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(serverRoot), filepath.Clean(requestedRoot)) {
		return fmt.Errorf("repo_path must match control server repo root: requested=%s server=%s", requestedRoot, serverRoot)
	}
	return nil
}

func resolveControlContinueRun(ctx context.Context, store *state.Store, requestedRunID string) (state.Run, bool, error) {
	if strings.TrimSpace(requestedRunID) != "" {
		return store.GetRun(ctx, strings.TrimSpace(requestedRunID))
	}
	return store.LatestResumableRun(ctx)
}
