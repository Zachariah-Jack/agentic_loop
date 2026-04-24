package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/control"
	"orchestrator/internal/journal"
	"orchestrator/internal/runtimecfg"
	"orchestrator/internal/state"
)

type controlRuntimeSnapshot struct {
	EngineMode             string `json:"engine_mode"`
	RepoRoot               string `json:"repo_root"`
	RepoReady              bool   `json:"repo_ready"`
	PlannerReady           bool   `json:"planner_ready"`
	ExecutorReady          bool   `json:"executor_ready"`
	NTFYReady              bool   `json:"ntfy_ready"`
	Verbosity              string `json:"verbosity"`
	WorkerConcurrencyLimit int    `json:"worker_concurrency_limit"`
}

type controlCheckpointSnapshot struct {
	Sequence  int64  `json:"sequence"`
	Stage     string `json:"stage"`
	Label     string `json:"label"`
	SafePause bool   `json:"safe_pause"`
}

type controlRunSnapshot struct {
	ID                   string                    `json:"id"`
	Goal                 string                    `json:"goal"`
	Status               string                    `json:"status"`
	StopReason           string                    `json:"stop_reason,omitempty"`
	StartedAt            string                    `json:"started_at,omitempty"`
	StoppedAt            string                    `json:"stopped_at,omitempty"`
	ElapsedSeconds       int64                     `json:"elapsed_seconds,omitempty"`
	ElapsedLabel         string                    `json:"elapsed_label,omitempty"`
	ExecutorLastError    string                    `json:"executor_last_error,omitempty"`
	ExecutorFailureStage string                    `json:"executor_failure_stage,omitempty"`
	NextOperatorAction   string                    `json:"next_operator_action"`
	LatestCheckpoint     controlCheckpointSnapshot `json:"latest_checkpoint"`
	LatestPlannerEvent   string                    `json:"latest_planner_outcome,omitempty"`
	LatestArtifactPath   string                    `json:"latest_artifact_path,omitempty"`
	Completed            bool                      `json:"completed"`
	Resumable            bool                      `json:"resumable"`
}

type controlPlannerStatusSnapshot struct {
	Present            bool   `json:"present"`
	ContractVersion    string `json:"contract_version"`
	MigrationState     string `json:"migration_state,omitempty"`
	OperatorMessage    string `json:"operator_message,omitempty"`
	CurrentFocus       string `json:"current_focus,omitempty"`
	NextIntendedStep   string `json:"next_intended_step,omitempty"`
	WhyThisStep        string `json:"why_this_step,omitempty"`
	ProgressPercent    int    `json:"progress_percent,omitempty"`
	ProgressConfidence string `json:"progress_confidence,omitempty"`
	ProgressBasis      string `json:"progress_basis,omitempty"`
}

type controlRoadmapSnapshot struct {
	Present       bool   `json:"present"`
	Path          string `json:"path,omitempty"`
	Preview       string `json:"preview,omitempty"`
	AlignmentText string `json:"alignment_text,omitempty"`
	ModifiedAt    string `json:"modified_at,omitempty"`
	Message       string `json:"message,omitempty"`
}

type controlPendingActionSnapshot struct {
	Available              bool                                  `json:"available"`
	Present                bool                                  `json:"present"`
	TurnType               string                                `json:"turn_type,omitempty"`
	PlannerOutcome         string                                `json:"planner_outcome,omitempty"`
	PlannerResponseID      string                                `json:"planner_response_id,omitempty"`
	PendingActionSummary   string                                `json:"pending_action_summary,omitempty"`
	PendingExecutorPrompt  string                                `json:"pending_executor_prompt,omitempty"`
	PendingExecutorSummary string                                `json:"pending_executor_prompt_summary,omitempty"`
	PendingDispatchTarget  *controlPendingDispatchTargetSnapshot `json:"pending_dispatch_target,omitempty"`
	PendingReason          string                                `json:"pending_reason,omitempty"`
	Held                   bool                                  `json:"held,omitempty"`
	HoldReason             string                                `json:"hold_reason,omitempty"`
	UpdatedAt              string                                `json:"updated_at,omitempty"`
	Message                string                                `json:"message,omitempty"`
}

type controlPendingDispatchTargetSnapshot struct {
	Kind         string `json:"kind,omitempty"`
	WorkerID     string `json:"worker_id,omitempty"`
	WorkerName   string `json:"worker_name,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
}

type controlWorkerSnapshot struct {
	WorkerID          string `json:"worker_id"`
	WorkerName        string `json:"worker_name"`
	Status            string `json:"status"`
	Scope             string `json:"scope,omitempty"`
	WorktreePath      string `json:"worktree_path,omitempty"`
	ApprovalRequired  bool   `json:"approval_required"`
	ApprovalKind      string `json:"approval_kind,omitempty"`
	ApprovalPreview   string `json:"approval_preview,omitempty"`
	ExecutorThreadID  string `json:"executor_thread_id,omitempty"`
	ExecutorTurnID    string `json:"executor_turn_id,omitempty"`
	Interruptible     bool   `json:"interruptible"`
	Steerable         bool   `json:"steerable"`
	LastControlAction string `json:"last_control_action,omitempty"`
	WorkerTaskSummary string `json:"worker_task_summary,omitempty"`
	WorkerResult      string `json:"worker_result_summary,omitempty"`
	WorkerError       string `json:"worker_error_summary,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

type controlWorkersSnapshot struct {
	Total            int                     `json:"total"`
	Active           int                     `json:"active"`
	ApprovalRequired int                     `json:"approval_required"`
	LatestRunCount   int                     `json:"latest_run_count"`
	Items            []controlWorkerSnapshot `json:"items,omitempty"`
}

type controlWorkerListSnapshot struct {
	Count          int                     `json:"count"`
	CountsByStatus map[string]int          `json:"counts_by_status,omitempty"`
	Items          []controlWorkerSnapshot `json:"items,omitempty"`
	Message        string                  `json:"message,omitempty"`
}

type controlSideChatMessageSnapshot struct {
	ID              string `json:"id"`
	RepoPath        string `json:"repo_path"`
	RunID           string `json:"run_id,omitempty"`
	Source          string `json:"source"`
	ContextPolicy   string `json:"context_policy,omitempty"`
	RawText         string `json:"raw_text"`
	Status          string `json:"status"`
	BackendState    string `json:"backend_state,omitempty"`
	ResponseMessage string `json:"response_message,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type controlSideChatListSnapshot struct {
	Available bool                             `json:"available"`
	Count     int                              `json:"count"`
	Items     []controlSideChatMessageSnapshot `json:"items,omitempty"`
	Message   string                           `json:"message,omitempty"`
}

type controlSideChatSendSnapshot struct {
	Available bool                            `json:"available"`
	Stored    bool                            `json:"stored"`
	Message   string                          `json:"message,omitempty"`
	Entry     *controlSideChatMessageSnapshot `json:"entry,omitempty"`
}

type controlDogfoodIssueSnapshot struct {
	ID        string `json:"id"`
	RepoPath  string `json:"repo_path"`
	RunID     string `json:"run_id,omitempty"`
	Source    string `json:"source"`
	Title     string `json:"title"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type controlDogfoodIssueListSnapshot struct {
	Available bool                          `json:"available"`
	Count     int                           `json:"count"`
	Items     []controlDogfoodIssueSnapshot `json:"items,omitempty"`
	Message   string                        `json:"message,omitempty"`
}

type controlDogfoodIssueCaptureSnapshot struct {
	Available bool                         `json:"available"`
	Stored    bool                         `json:"stored"`
	Message   string                       `json:"message,omitempty"`
	Entry     *controlDogfoodIssueSnapshot `json:"entry,omitempty"`
}

type controlApprovalSnapshot struct {
	Present                bool     `json:"present"`
	State                  string   `json:"state,omitempty"`
	Kind                   string   `json:"kind,omitempty"`
	Summary                string   `json:"summary,omitempty"`
	RunID                  string   `json:"run_id,omitempty"`
	ExecutorThreadID       string   `json:"executor_thread_id,omitempty"`
	ExecutorTurnID         string   `json:"executor_turn_id,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
	Command                string   `json:"command,omitempty"`
	CWD                    string   `json:"cwd,omitempty"`
	GrantRoot              string   `json:"grant_root,omitempty"`
	LastControlAction      string   `json:"last_control_action,omitempty"`
	WorkerApprovalRequired int      `json:"worker_approval_required,omitempty"`
	AvailableActions       []string `json:"available_actions,omitempty"`
	Message                string   `json:"message,omitempty"`
}

type controlStatusSnapshot struct {
	Runtime       controlRuntimeSnapshot       `json:"runtime"`
	Run           *controlRunSnapshot          `json:"run,omitempty"`
	ModelHealth   controlModelHealthSnapshot   `json:"model_health"`
	PlannerStatus controlPlannerStatusSnapshot `json:"planner_status"`
	Roadmap       controlRoadmapSnapshot       `json:"roadmap"`
	PendingAction controlPendingActionSnapshot `json:"pending_action"`
	Artifacts     controlArtifactsSnapshot     `json:"artifacts"`
	Workers       controlWorkersSnapshot       `json:"workers"`
	Approval      controlApprovalSnapshot      `json:"approval"`
}

type controlRuntimeConfigSnapshot struct {
	ConfigPath             string   `json:"config_path"`
	Verbosity              string   `json:"verbosity"`
	PlannerModel           string   `json:"planner_model"`
	WorkerConcurrencyLimit int      `json:"worker_concurrency_limit"`
	DriftWatcherEnabled    bool     `json:"drift_watcher_enabled"`
	MutableFields          []string `json:"mutable_fields"`
}

type controlMessageSnapshot struct {
	ID            string `json:"id"`
	RunID         string `json:"run_id"`
	TargetBinding string `json:"target_binding,omitempty"`
	Source        string `json:"source"`
	Reason        string `json:"reason,omitempty"`
	RawText       string `json:"raw_text"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	ConsumedAt    string `json:"consumed_at,omitempty"`
	CancelledAt   string `json:"cancelled_at,omitempty"`
}

type controlMessageListSnapshot struct {
	Count    int                      `json:"count"`
	Messages []controlMessageSnapshot `json:"messages"`
}

type controlStopFlagState struct {
	Present   bool   `json:"present"`
	Path      string `json:"path"`
	AppliesAt string `json:"applies_at"`
	Reason    string `json:"reason,omitempty"`
}

func currentConfig(inv Invocation) config.Config {
	cfg := inv.Config
	if inv.RuntimeCfg != nil {
		cfg = inv.RuntimeCfg.Snapshot()
	}
	if normalized, err := config.NormalizeVerbosity(cfg.Verbosity); err == nil {
		cfg.Verbosity = normalized
	} else {
		cfg.Verbosity = config.VerbosityNormal
	}
	return config.WithDefaults(cfg)
}

func applyRuntimeConfigAtSafePoint(ctx context.Context, inv *Invocation, journalWriter *journal.Journal, run state.Run) error {
	if inv == nil || inv.RuntimeCfg == nil {
		return nil
	}

	before := currentConfig(*inv)
	after, changed, err := inv.RuntimeCfg.ReloadFromDisk()
	if err != nil {
		return err
	}
	inv.Config = after
	if !changed {
		return nil
	}

	after = currentConfig(*inv)
	if before.Verbosity == after.Verbosity {
		return nil
	}

	if journalWriter != nil && strings.TrimSpace(run.ID) != "" {
		_ = journalWriter.Append(journal.Event{
			Type:       "runtime.verbosity.changed",
			RunID:      run.ID,
			RepoPath:   run.RepoPath,
			Goal:       run.Goal,
			Status:     string(run.Status),
			Message:    fmt.Sprintf("runtime verbosity changed to %s at a safe point", after.Verbosity),
			Checkpoint: journalCheckpointRef(run.LatestCheckpoint),
		})
	}

	emitEngineEvent(*inv, "verbosity_changed", eventPayloadForRun(run, map[string]any{
		"previous_verbosity": before.Verbosity,
		"verbosity":          after.Verbosity,
		"applies_at":         "current_safe_point",
	}))

	return nil
}

func emitEngineEvent(inv Invocation, event string, payload map[string]any) {
	if inv.Events == nil {
		return
	}
	inv.Events.Publish(strings.TrimSpace(event), payload)
}

func eventPayloadForRun(run state.Run, extra map[string]any) map[string]any {
	payload := map[string]any{}
	if strings.TrimSpace(run.ID) != "" {
		payload["run_id"] = run.ID
	}
	if strings.TrimSpace(run.Goal) != "" {
		payload["goal"] = run.Goal
	}
	if strings.TrimSpace(run.RepoPath) != "" {
		payload["repo_path"] = run.RepoPath
	}
	if strings.TrimSpace(string(run.Status)) != "" {
		payload["status"] = string(run.Status)
	}
	if run.LatestCheckpoint.Sequence > 0 {
		payload["checkpoint"] = map[string]any{
			"sequence":   run.LatestCheckpoint.Sequence,
			"stage":      run.LatestCheckpoint.Stage,
			"label":      run.LatestCheckpoint.Label,
			"safe_pause": run.LatestCheckpoint.SafePause,
		}
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func newLocalControlServer(inv Invocation) control.Server {
	current := &inv
	runManager := newControlRunManager()
	return control.Server{
		Broker: inv.Events,
		Actions: control.ActionSet{
			StartRun: func(ctx context.Context, request control.StartRunRequest) (any, error) {
				return runManager.StartRun(ctx, *current, request)
			},
			ContinueRun: func(ctx context.Context, request control.ContinueRunRequest) (any, error) {
				return runManager.ContinueRun(ctx, *current, request)
			},
			TestPlannerModel: func(ctx context.Context, request control.ModelTestRequest) (any, error) {
				return testPlannerModelHealth(ctx, *current, request)
			},
			TestExecutorModel: func(ctx context.Context, request control.ModelTestRequest) (any, error) {
				return testExecutorModelHealth(ctx, *current, request)
			},
			GetStatusSnapshot: func(ctx context.Context, runID string) (any, error) {
				snapshot, err := buildControlStatusSnapshot(ctx, *current, runID)
				if err != nil {
					return nil, err
				}
				return runManager.applyActiveStatus(snapshot), nil
			},
			SetVerbosity: func(ctx context.Context, verbosity string) (any, error) {
				return applyRuntimeConfigPatch(ctx, current, runtimecfg.Patch{Verbosity: &verbosity})
			},
			SetStopFlag: func(_ context.Context, _ string, reason string) (any, error) {
				return setControlStopFlag(*current, reason)
			},
			ClearStopFlag: func(_ context.Context, _ string) (any, error) {
				return clearControlStopFlag(*current)
			},
			GetPendingAction: func(ctx context.Context, runID string) (any, error) {
				return getPendingActionSnapshot(ctx, *current, runID)
			},
			ApproveExecutor: func(ctx context.Context, request control.ExecutorApprovalActionRequest) (any, error) {
				return approveExecutorRequest(ctx, *current, request)
			},
			DenyExecutor: func(ctx context.Context, request control.ExecutorApprovalActionRequest) (any, error) {
				return denyExecutorRequest(ctx, *current, request)
			},
			GetArtifact: func(ctx context.Context, request control.ArtifactRequest) (any, error) {
				return getArtifact(ctx, *current, request)
			},
			ListRecentArtifacts: func(ctx context.Context, request control.ListArtifactsRequest) (any, error) {
				return listRecentArtifacts(ctx, *current, request)
			},
			ListContractFiles: func(ctx context.Context, request control.ListContractFilesRequest) (any, error) {
				return listContractFiles(ctx, *current, request)
			},
			OpenContractFile: func(ctx context.Context, request control.ContractFileRequest) (any, error) {
				return openContractFile(ctx, *current, request)
			},
			SaveContractFile: func(ctx context.Context, request control.SaveContractFileRequest) (any, error) {
				return saveContractFile(ctx, *current, request)
			},
			RunAIAutofill: func(ctx context.Context, request control.RunAIAutofillRequest) (any, error) {
				return runAIAutofill(ctx, *current, request)
			},
			ListRepoTree: func(ctx context.Context, request control.RepoTreeRequest) (any, error) {
				return listRepoTree(ctx, *current, request)
			},
			OpenRepoFile: func(ctx context.Context, request control.RepoFileRequest) (any, error) {
				return openRepoFile(ctx, *current, request)
			},
			InjectControlMessage: func(ctx context.Context, request control.InjectControlMessageRequest) (any, error) {
				return injectControlMessage(ctx, *current, request)
			},
			ListControlMessages: func(ctx context.Context, request control.ListControlMessagesRequest) (any, error) {
				return listControlMessages(ctx, *current, request)
			},
			SendSideChatMessage: func(ctx context.Context, request control.SideChatRequest) (any, error) {
				return sendSideChatMessage(ctx, *current, request)
			},
			ListSideChatMessages: func(ctx context.Context, request control.ListSideChatMessagesRequest) (any, error) {
				return listSideChatMessages(ctx, *current, request)
			},
			CaptureDogfoodIssue: func(ctx context.Context, request control.CaptureDogfoodIssueRequest) (any, error) {
				return captureDogfoodIssue(ctx, *current, request)
			},
			ListDogfoodIssues: func(ctx context.Context, request control.ListDogfoodIssuesRequest) (any, error) {
				return listDogfoodIssues(ctx, *current, request)
			},
			ListWorkers: func(ctx context.Context, request control.ListWorkersRequest) (any, error) {
				return listWorkersForControl(ctx, *current, request)
			},
			CreateWorker: func(ctx context.Context, request control.CreateWorkerRequest) (any, error) {
				return createWorkerForControl(ctx, *current, request)
			},
			DispatchWorker: func(ctx context.Context, request control.DispatchWorkerRequest) (any, error) {
				return dispatchWorkerForControl(ctx, *current, request)
			},
			RemoveWorker: func(ctx context.Context, request control.RemoveWorkerRequest) (any, error) {
				return removeWorkerForControl(ctx, *current, request)
			},
			IntegrateWorkers: func(ctx context.Context, request control.IntegrateWorkersRequest) (any, error) {
				return integrateWorkersForControl(ctx, *current, request)
			},
			GetRuntimeConfig: func(_ context.Context) (any, error) {
				return buildRuntimeConfigSnapshot(*current), nil
			},
			SetRuntimeConfig: func(ctx context.Context, patch runtimecfg.Patch) (any, error) {
				return applyRuntimeConfigPatch(ctx, current, patch)
			},
		},
	}
}

func buildControlStatusSnapshot(ctx context.Context, inv Invocation, requestedRunID string) (controlStatusSnapshot, error) {
	cfg := currentConfig(inv)
	snapshot := controlStatusSnapshot{
		Runtime: controlRuntimeSnapshot{
			EngineMode:             "headless_cli",
			RepoRoot:               inv.RepoRoot,
			RepoReady:              inspectTargetRepoContract(inv.RepoRoot).Ready,
			PlannerReady:           plannerAPIKeyStatus() == "present",
			ExecutorReady:          executorAppServerState() == "ready",
			NTFYReady:              ntfyBridgeState(cfg) == "ready",
			Verbosity:              cfg.Verbosity,
			WorkerConcurrencyLimit: cfg.WorkerConcurrencyLimit,
		},
		PlannerStatus: controlPlannerStatusSnapshot{
			Present:         false,
			ContractVersion: "planner.v1",
			MigrationState:  "live runtime uses planner.v1 with additive optional operator_status; planner.v2 remains the stricter required-status contract",
		},
		Roadmap: buildControlRoadmapSnapshot(inv.RepoRoot),
		PendingAction: controlPendingActionSnapshot{
			Available: false,
			Present:   false,
			Message:   "no pending action is currently recorded for this run",
		},
		Artifacts: controlArtifactsSnapshot{
			Count:   0,
			Message: "no artifacts are currently recorded for this run",
		},
		Approval: controlApprovalSnapshot{
			Present: false,
			Message: "no approval is currently required",
		},
	}

	if !pathExists(inv.Layout.DBPath) {
		snapshot.ModelHealth = buildModelHealthSnapshot(ctx, inv, nil)
		return snapshot, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return snapshot, nil
		}
		return controlStatusSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlStatusSnapshot{}, err
	}

	workerStats, err := store.WorkerStats(ctx, "")
	if err != nil {
		return controlStatusSnapshot{}, err
	}
	snapshot.Workers.Total = int(workerStats.Total)
	snapshot.Workers.Active = int(workerStats.Active)

	var run state.Run
	var found bool
	if strings.TrimSpace(requestedRunID) != "" {
		run, found, err = store.GetRun(ctx, strings.TrimSpace(requestedRunID))
	} else {
		run, found, err = store.LatestRun(ctx)
	}
	if err != nil {
		return controlStatusSnapshot{}, err
	}
	if !found {
		snapshot.ModelHealth = buildModelHealthSnapshot(ctx, inv, nil)
		return snapshot, nil
	}

	pendingAction, pendingFound, err := store.GetPendingAction(ctx, run.ID)
	if err != nil {
		return controlStatusSnapshot{}, err
	}
	snapshot.PendingAction = pendingActionSnapshot(pendingAction, pendingFound)

	runWorkers, err := store.ListWorkers(ctx, run.ID)
	if err != nil {
		return controlStatusSnapshot{}, err
	}
	events, err := latestRunEvents(inv.Layout, run.ID, 64)
	if err != nil {
		return controlStatusSnapshot{}, err
	}

	snapshot.Run = &controlRunSnapshot{
		ID:                   run.ID,
		Goal:                 run.Goal,
		Status:               string(run.Status),
		StopReason:           run.LatestStopReason,
		StartedAt:            formatSnapshotTime(run.CreatedAt),
		StoppedAt:            formatSnapshotTime(run.UpdatedAt),
		ElapsedSeconds:       runElapsedSeconds(run, time.Now().UTC()),
		ElapsedLabel:         runElapsedLabel(run, time.Now().UTC()),
		ExecutorLastError:    strings.TrimSpace(run.ExecutorLastError),
		ExecutorFailureStage: strings.TrimSpace(run.ExecutorLastFailureStage),
		NextOperatorAction:   nextOperatorActionForExistingRun(run),
		LatestCheckpoint: controlCheckpointSnapshot{
			Sequence:  run.LatestCheckpoint.Sequence,
			Stage:     run.LatestCheckpoint.Stage,
			Label:     run.LatestCheckpoint.Label,
			SafePause: run.LatestCheckpoint.SafePause,
		},
		LatestPlannerEvent: latestPlannerOutcomeFromEvents(events),
		LatestArtifactPath: latestArtifactPath(run, events),
		Completed:          run.Status == state.StatusCompleted,
		Resumable:          isRunResumable(run),
	}
	snapshot.ModelHealth = buildModelHealthSnapshot(ctx, inv, &run)
	snapshot.PlannerStatus = plannerStatusSnapshotFromRun(run)
	snapshot.Artifacts = buildControlArtifactsSnapshot(run, events, 12)

	snapshot.Workers.LatestRunCount = len(runWorkers)
	snapshot.Workers.ApprovalRequired = workerStatusCount(runWorkers, state.WorkerStatusApprovalRequired)
	for _, worker := range workerSummary(runWorkers, 5) {
		snapshot.Workers.Items = append(snapshot.Workers.Items, controlWorkerSnapshot{
			WorkerID:          worker.ID,
			WorkerName:        worker.WorkerName,
			Status:            string(worker.WorkerStatus),
			Scope:             worker.AssignedScope,
			WorktreePath:      strings.TrimSpace(worker.WorktreePath),
			ApprovalRequired:  workerApprovalRequired(worker),
			ApprovalKind:      strings.TrimSpace(worker.ExecutorApprovalKind),
			ApprovalPreview:   previewString(strings.TrimSpace(worker.ExecutorApprovalPreview), 240),
			ExecutorThreadID:  strings.TrimSpace(worker.ExecutorThreadID),
			ExecutorTurnID:    strings.TrimSpace(worker.ExecutorTurnID),
			Interruptible:     workerTurnInterruptibleState(worker),
			Steerable:         workerTurnSteerableState(worker),
			LastControlAction: strings.TrimSpace(worker.ExecutorLastControlAction),
			WorkerTaskSummary: previewString(strings.TrimSpace(worker.WorkerTaskSummary), 240),
			WorkerResult:      previewString(strings.TrimSpace(worker.WorkerResultSummary), 240),
			WorkerError:       previewString(strings.TrimSpace(worker.WorkerErrorSummary), 240),
			UpdatedAt:         formatSnapshotTime(worker.UpdatedAt),
		})
	}
	snapshot.Approval = approvalSnapshotFromRun(run, runWorkers)

	return snapshot, nil
}

func buildRuntimeConfigSnapshot(inv Invocation) controlRuntimeConfigSnapshot {
	cfg := currentConfig(inv)
	return controlRuntimeConfigSnapshot{
		ConfigPath:             inv.ConfigPath,
		Verbosity:              cfg.Verbosity,
		PlannerModel:           resolvePlannerModel(inv),
		WorkerConcurrencyLimit: cfg.WorkerConcurrencyLimit,
		DriftWatcherEnabled:    cfg.DriftWatcherEnabled,
		MutableFields:          []string{"verbosity"},
	}
}

func plannerStatusSnapshotFromRun(run state.Run) controlPlannerStatusSnapshot {
	snapshot := controlPlannerStatusSnapshot{
		Present:         false,
		ContractVersion: "planner.v1",
		MigrationState:  "live runtime uses planner.v1 with additive optional operator_status; planner.v2 remains the stricter required-status contract",
	}
	if run.PlannerOperatorStatus == nil {
		return snapshot
	}

	status := run.PlannerOperatorStatus
	snapshot.Present = true
	if strings.TrimSpace(status.ContractVersion) != "" {
		snapshot.ContractVersion = strings.TrimSpace(status.ContractVersion)
	}
	snapshot.OperatorMessage = strings.TrimSpace(status.OperatorMessage)
	snapshot.CurrentFocus = strings.TrimSpace(status.CurrentFocus)
	snapshot.NextIntendedStep = strings.TrimSpace(status.NextIntendedStep)
	snapshot.WhyThisStep = strings.TrimSpace(status.WhyThisStep)
	snapshot.ProgressPercent = status.ProgressPercent
	snapshot.ProgressConfidence = strings.TrimSpace(status.ProgressConfidence)
	snapshot.ProgressBasis = strings.TrimSpace(status.ProgressBasis)
	return snapshot
}

func applyRuntimeConfigPatch(ctx context.Context, inv *Invocation, patch runtimecfg.Patch) (controlRuntimeConfigSnapshot, error) {
	if inv == nil {
		return controlRuntimeConfigSnapshot{}, errors.New("invocation is required")
	}

	if inv.RuntimeCfg != nil {
		cfg, _, err := inv.RuntimeCfg.ApplyPatch(patch)
		if err != nil {
			return controlRuntimeConfigSnapshot{}, err
		}
		inv.Config = cfg
		return buildRuntimeConfigSnapshot(*inv), nil
	}

	if patch.Verbosity != nil {
		normalized, err := config.NormalizeVerbosity(*patch.Verbosity)
		if err != nil {
			return controlRuntimeConfigSnapshot{}, err
		}
		inv.Config.Verbosity = normalized
	}

	_ = ctx
	return buildRuntimeConfigSnapshot(*inv), nil
}

func getPendingActionSnapshot(ctx context.Context, inv Invocation, requestedRunID string) (controlPendingActionSnapshot, error) {
	if !pathExists(inv.Layout.DBPath) {
		return controlPendingActionSnapshot{
			Available: false,
			Present:   false,
			Message:   "pending action buffer is unavailable because runtime state has not been initialized yet",
		}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlPendingActionSnapshot{
				Available: false,
				Present:   false,
				Message:   "pending action buffer is unavailable because runtime state has not been initialized yet",
			}, nil
		}
		return controlPendingActionSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlPendingActionSnapshot{}, err
	}

	run, found, err := resolveControlRun(ctx, store, strings.TrimSpace(requestedRunID))
	if err != nil {
		return controlPendingActionSnapshot{}, err
	}
	if !found {
		return controlPendingActionSnapshot{
			Available: false,
			Present:   false,
			Message:   "no run is available for pending action inspection",
		}, nil
	}

	pending, found, err := store.GetPendingAction(ctx, run.ID)
	if err != nil {
		return controlPendingActionSnapshot{}, err
	}
	return pendingActionSnapshot(pending, found), nil
}

func injectControlMessage(ctx context.Context, inv Invocation, request control.InjectControlMessageRequest) (controlMessageSnapshot, error) {
	if !pathExists(inv.Layout.DBPath) {
		return controlMessageSnapshot{}, errors.New("runtime state is not initialized for control-message injection")
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		return controlMessageSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlMessageSnapshot{}, err
	}

	run, binding, found, err := resolveControlMessageRun(ctx, store, strings.TrimSpace(request.RunID))
	if err != nil {
		return controlMessageSnapshot{}, err
	}
	if !found {
		return controlMessageSnapshot{}, errors.New("no unfinished run is available for control-message injection")
	}

	source := strings.TrimSpace(request.Source)
	if source == "" {
		source = "control_chat"
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "operator_intervention"
	}

	message, err := store.RecordControlMessage(ctx, state.CreateControlMessageParams{
		RunID:         run.ID,
		TargetBinding: binding,
		Source:        source,
		Reason:        reason,
		RawText:       request.Message,
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		return controlMessageSnapshot{}, err
	}

	emitEngineEvent(inv, "control_message_queued", eventPayloadForRun(run, map[string]any{
		"control_message_id": message.ID,
		"source":             message.Source,
		"reason":             message.Reason,
		"target_binding":     message.TargetBinding,
		"message_preview":    previewString(message.RawText, 240),
	}))

	return controlMessageSnapshotFromState(message), nil
}

func listControlMessages(ctx context.Context, inv Invocation, request control.ListControlMessagesRequest) (controlMessageListSnapshot, error) {
	if !pathExists(inv.Layout.DBPath) {
		return controlMessageListSnapshot{Count: 0, Messages: []controlMessageSnapshot{}}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlMessageListSnapshot{Count: 0, Messages: []controlMessageSnapshot{}}, nil
		}
		return controlMessageListSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlMessageListSnapshot{}, err
	}

	status := state.ControlMessageStatus(strings.TrimSpace(request.Status))
	switch status {
	case "", state.ControlMessageQueued, state.ControlMessageConsumed, state.ControlMessageCanceled:
	default:
		return controlMessageListSnapshot{}, fmt.Errorf("unsupported control message status %q", request.Status)
	}

	messages, err := store.ListControlMessages(ctx, strings.TrimSpace(request.RunID), status, request.Limit)
	if err != nil {
		return controlMessageListSnapshot{}, err
	}

	items := make([]controlMessageSnapshot, 0, len(messages))
	for _, message := range messages {
		items = append(items, controlMessageSnapshotFromState(message))
	}
	return controlMessageListSnapshot{
		Count:    len(items),
		Messages: items,
	}, nil
}

func setControlStopFlag(inv Invocation, reason string) (controlStopFlagState, error) {
	path := autoStopFlagPath(inv.Layout)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return controlStopFlagState{}, err
	}
	if strings.TrimSpace(reason) == "" {
		reason = "operator_requested_safe_stop"
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(reason)+"\n"), 0o600); err != nil {
		return controlStopFlagState{}, err
	}
	return controlStopFlagState{
		Present:   true,
		Path:      path,
		AppliesAt: "next_safe_point",
		Reason:    strings.TrimSpace(reason),
	}, nil
}

func clearControlStopFlag(inv Invocation) (controlStopFlagState, error) {
	path := autoStopFlagPath(inv.Layout)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return controlStopFlagState{}, err
	}
	return controlStopFlagState{
		Present:   false,
		Path:      path,
		AppliesAt: "next_safe_point",
	}, nil
}

func resolveControlRun(ctx context.Context, store *state.Store, requestedRunID string) (state.Run, bool, error) {
	if strings.TrimSpace(requestedRunID) != "" {
		return store.GetRun(ctx, strings.TrimSpace(requestedRunID))
	}
	return store.LatestRun(ctx)
}

func resolveControlMessageRun(ctx context.Context, store *state.Store, requestedRunID string) (state.Run, string, bool, error) {
	if strings.TrimSpace(requestedRunID) != "" {
		run, found, err := store.GetRun(ctx, strings.TrimSpace(requestedRunID))
		return run, "explicit_run", found, err
	}
	run, found, err := store.LatestResumableRun(ctx)
	return run, "latest_unfinished_run", found, err
}

func pendingActionSnapshot(pending state.PendingAction, found bool) controlPendingActionSnapshot {
	if !found {
		return controlPendingActionSnapshot{
			Available: true,
			Present:   false,
			Message:   "no pending action is currently recorded for this run",
		}
	}

	snapshot := controlPendingActionSnapshot{
		Available:              true,
		Present:                true,
		TurnType:               strings.TrimSpace(pending.TurnType),
		PlannerOutcome:         strings.TrimSpace(pending.PlannerOutcome),
		PlannerResponseID:      strings.TrimSpace(pending.PlannerResponseID),
		PendingActionSummary:   strings.TrimSpace(pending.PendingActionSummary),
		PendingExecutorPrompt:  strings.TrimSpace(pending.PendingExecutorPrompt),
		PendingExecutorSummary: strings.TrimSpace(pending.PendingExecutorSummary),
		PendingReason:          strings.TrimSpace(pending.PendingReason),
		Held:                   pending.Held,
		HoldReason:             strings.TrimSpace(pending.HoldReason),
		UpdatedAt:              formatSnapshotTime(pending.UpdatedAt),
	}
	if pending.PendingDispatchTarget != nil {
		snapshot.PendingDispatchTarget = &controlPendingDispatchTargetSnapshot{
			Kind:         strings.TrimSpace(pending.PendingDispatchTarget.Kind),
			WorkerID:     strings.TrimSpace(pending.PendingDispatchTarget.WorkerID),
			WorkerName:   strings.TrimSpace(pending.PendingDispatchTarget.WorkerName),
			WorktreePath: strings.TrimSpace(pending.PendingDispatchTarget.WorktreePath),
		}
	}
	return snapshot
}

func controlMessageSnapshotFromState(message state.ControlMessage) controlMessageSnapshot {
	return controlMessageSnapshot{
		ID:            message.ID,
		RunID:         message.RunID,
		TargetBinding: message.TargetBinding,
		Source:        message.Source,
		Reason:        message.Reason,
		RawText:       message.RawText,
		Status:        string(message.Status),
		CreatedAt:     formatSnapshotTime(message.CreatedAt),
		ConsumedAt:    formatSnapshotTime(message.ConsumedAt),
		CancelledAt:   formatSnapshotTime(message.CancelledAt),
	}
}

func formatSnapshotTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func runElapsedSeconds(run state.Run, now time.Time) int64 {
	if run.CreatedAt.IsZero() {
		return 0
	}
	end := run.UpdatedAt
	if end.IsZero() || run.Status == state.StatusInitialized && strings.TrimSpace(run.LatestStopReason) == "" {
		end = now
	}
	if end.Before(run.CreatedAt) {
		return 0
	}
	return int64(end.Sub(run.CreatedAt).Seconds())
}

func runElapsedLabel(run state.Run, now time.Time) string {
	seconds := runElapsedSeconds(run, now)
	if seconds <= 0 {
		return "unavailable"
	}
	duration := formatHumanDuration(time.Duration(seconds) * time.Second)
	if run.Status == state.StatusCompleted || run.Status == state.StatusFailed || run.Status == state.StatusCancelled || strings.TrimSpace(run.LatestStopReason) != "" {
		return "stopped after " + duration
	}
	return "running for " + duration
}

func formatHumanDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	total := int64(duration.Seconds())
	hours := total / 3600
	minutes := (total % 3600) / 60
	seconds := total % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
