package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"orchestrator/internal/buildinfo"
	"orchestrator/internal/config"
	"orchestrator/internal/control"
	"orchestrator/internal/journal"
	"orchestrator/internal/ntfy"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/runtimecfg"
	"orchestrator/internal/state"
)

var controlBackendStartedAt = time.Now().UTC()

const controlProtocolVersion = "v2.2"

type controlProtocolSupportSnapshot struct {
	RuntimeConfig          bool `json:"runtime_config"`
	NTFYRuntimeConfig      bool `json:"ntfy_runtime_config"`
	TestNTFY               bool `json:"test_ntfy"`
	BackendCompatibility   bool `json:"backend_compatibility"`
	RuntimeConfigFieldList bool `json:"runtime_config_field_list"`
}

type controlProtocolSnapshot struct {
	Version  string                         `json:"version"`
	Supports controlProtocolSupportSnapshot `json:"supports"`
}

type controlBackendSnapshot struct {
	PID                       int                            `json:"pid"`
	StartedAt                 string                         `json:"started_at"`
	BinaryPath                string                         `json:"binary_path,omitempty"`
	BinaryModifiedAt          string                         `json:"binary_modified_at,omitempty"`
	BinaryVersion             string                         `json:"binary_version"`
	BinaryRevision            string                         `json:"binary_revision"`
	BinaryBuildTime           string                         `json:"binary_build_time"`
	ProtocolVersion           string                         `json:"protocol_version"`
	RepoRoot                  string                         `json:"repo_root"`
	ControlAddress            string                         `json:"control_address,omitempty"`
	Owner                     string                         `json:"owner,omitempty"`
	OwnerSessionID            string                         `json:"owner_session_id,omitempty"`
	OwnerMetadata             string                         `json:"owner_metadata_path,omitempty"`
	Supports                  controlProtocolSupportSnapshot `json:"supports"`
	SupportsNTFYRuntimeConfig bool                           `json:"supports_ntfy_runtime_config"`
	Stale                     bool                           `json:"stale"`
	StaleReason               string                         `json:"stale_reason,omitempty"`
}

type controlRuntimeSnapshot struct {
	EngineMode                string   `json:"engine_mode"`
	RepoRoot                  string   `json:"repo_root"`
	ProtocolVersion           string   `json:"protocol_version"`
	SupportsNTFYRuntimeConfig bool     `json:"supports_ntfy_runtime_config"`
	RepoReady                 bool     `json:"repo_ready"`
	RepoContractMissing       []string `json:"repo_contract_missing,omitempty"`
	PlannerReady              bool     `json:"planner_ready"`
	ExecutorReady             bool     `json:"executor_ready"`
	NTFYReady                 bool     `json:"ntfy_ready"`
	Verbosity                 string   `json:"verbosity"`
	WorkerConcurrencyLimit    int      `json:"worker_concurrency_limit"`
	PermissionProfile         string   `json:"permission_profile"`
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
	ExecutorThreadID     string                    `json:"executor_thread_id,omitempty"`
	ExecutorTurnID       string                    `json:"executor_turn_id,omitempty"`
	ExecutorTurnStatus   string                    `json:"executor_turn_status,omitempty"`
	NextOperatorAction   string                    `json:"next_operator_action"`
	LatestCheckpoint     controlCheckpointSnapshot `json:"latest_checkpoint"`
	LatestPlannerEvent   string                    `json:"latest_planner_outcome,omitempty"`
	LatestArtifactPath   string                    `json:"latest_artifact_path,omitempty"`
	ActivityState        string                    `json:"activity_state,omitempty"`
	ActivityMessage      string                    `json:"activity_message,omitempty"`
	ActivelyProcessing   bool                      `json:"actively_processing"`
	WaitingAtSafePoint   bool                      `json:"waiting_at_safe_point"`
	ExecuteReady         bool                      `json:"execute_ready"`
	Completed            bool                      `json:"completed"`
	Resumable            bool                      `json:"resumable"`
}

type controlTimeoutSettingsSnapshot struct {
	PlannerRequestTimeout controlTimeoutValueSnapshot `json:"planner_request_timeout"`
	ExecutorIdleTimeout   controlTimeoutValueSnapshot `json:"executor_idle_timeout"`
	ExecutorTurnTimeout   controlTimeoutValueSnapshot `json:"executor_turn_timeout"`
	SubagentTimeout       controlTimeoutValueSnapshot `json:"subagent_timeout"`
	ShellCommandTimeout   controlTimeoutValueSnapshot `json:"shell_command_timeout"`
	InstallTimeout        controlTimeoutValueSnapshot `json:"install_timeout"`
	HumanWaitTimeout      controlTimeoutValueSnapshot `json:"human_wait_timeout"`
	Message               string                      `json:"message,omitempty"`
}

type controlTimeoutValueSnapshot struct {
	Value       string `json:"value"`
	Unlimited   bool   `json:"unlimited"`
	AppliesAt   string `json:"applies_at"`
	Description string `json:"description,omitempty"`
}

type controlBuildTimeSnapshot struct {
	TotalBuildTimeMS           int64  `json:"total_build_time_ms"`
	TotalBuildTimeLabel        string `json:"total_build_time_label"`
	CurrentRunTimeMS           int64  `json:"current_run_time_ms,omitempty"`
	CurrentRunTimeLabel        string `json:"current_run_time_label,omitempty"`
	CurrentStepStartedAt       string `json:"current_step_started_at,omitempty"`
	CurrentStepTimeMS          int64  `json:"current_step_time_ms,omitempty"`
	CurrentStepTimeLabel       string `json:"current_step_time_label,omitempty"`
	CurrentStepLabel           string `json:"current_step_label,omitempty"`
	CurrentActiveSessionStart  string `json:"current_active_session_started_at,omitempty"`
	LastActiveSessionEndedAt   string `json:"last_active_session_ended_at,omitempty"`
	PlannerActiveDurationMS    int64  `json:"planner_active_duration_ms"`
	ExecutorActiveDurationMS   int64  `json:"executor_active_duration_ms"`
	ExecutorThinkingDurationMS int64  `json:"executor_thinking_duration_ms"`
	CommandActiveDurationMS    int64  `json:"command_active_duration_ms"`
	InstallActiveDurationMS    int64  `json:"install_active_duration_ms"`
	TestActiveDurationMS       int64  `json:"test_active_duration_ms"`
	HumanWaitDurationMS        int64  `json:"human_wait_duration_ms"`
	BlockedDurationMS          int64  `json:"blocked_duration_ms"`
	Message                    string `json:"message,omitempty"`
}

type controlPermissionSnapshot struct {
	Profile                         string `json:"profile"`
	AskBeforeInstallingPrograms     bool   `json:"ask_before_installing_programs"`
	AskBeforeInstallingDependencies bool   `json:"ask_before_installing_dependencies"`
	AskBeforeOutsideRepoChanges     bool   `json:"ask_before_modifying_files_outside_repo"`
	AskBeforeDeletingFiles          bool   `json:"ask_before_deleting_files"`
	AskBeforeRunningTests           bool   `json:"ask_before_running_tests"`
	AskBeforeEmulatorTesting        bool   `json:"ask_before_emulator_device_testing"`
	AskBeforeNetworkCalls           bool   `json:"ask_before_network_calls"`
	AskBeforeGitCommits             bool   `json:"ask_before_git_commits"`
	AskBeforeGitPushes              bool   `json:"ask_before_git_pushes"`
	AskBeforeUpdaterInstalls        bool   `json:"ask_before_updater_installs"`
	AskBeforeWorkerParallelism      bool   `json:"ask_before_worker_parallelism"`
	AskBeforeExecutorSteering       bool   `json:"ask_before_executor_steering"`
	AskBeforePlannerDirection       bool   `json:"ask_before_planner_direction_changes"`
	Message                         string `json:"message,omitempty"`
}

type controlNTFYConfigSnapshot struct {
	Configured     bool   `json:"configured"`
	ServerURL      string `json:"server_url,omitempty"`
	Topic          string `json:"topic,omitempty"`
	AuthTokenSaved bool   `json:"auth_token_saved"`
	Listening      string `json:"listening"`
	LastReplyTime  string `json:"last_reply_time,omitempty"`
	Message        string `json:"message,omitempty"`
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

type controlSideChatEventSnapshot struct {
	At                 string `json:"at,omitempty"`
	Type               string `json:"type"`
	Message            string `json:"message,omitempty"`
	PlannerOutcome     string `json:"planner_outcome,omitempty"`
	ExecutorTurnStatus string `json:"executor_turn_status,omitempty"`
	ArtifactPath       string `json:"artifact_path,omitempty"`
	StopReason         string `json:"stop_reason,omitempty"`
}

type controlSideChatContextSnapshot struct {
	Available      bool                             `json:"available"`
	RepoPath       string                           `json:"repo_path,omitempty"`
	RunID          string                           `json:"run_id,omitempty"`
	VisibleContext []string                         `json:"visible_context,omitempty"`
	Status         controlStatusSnapshot            `json:"status"`
	RecentMessages []controlSideChatMessageSnapshot `json:"recent_messages,omitempty"`
	RecentEvents   []controlSideChatEventSnapshot   `json:"recent_events,omitempty"`
	Message        string                           `json:"message,omitempty"`
}

type controlSideChatActionEntrySnapshot struct {
	ID               string `json:"id"`
	RepoPath         string `json:"repo_path"`
	RunID            string `json:"run_id,omitempty"`
	Action           string `json:"action"`
	RequestText      string `json:"request_text,omitempty"`
	Source           string `json:"source,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Status           string `json:"status"`
	ResultMessage    string `json:"result_message,omitempty"`
	ControlMessageID string `json:"control_message_id,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type controlSideChatActionSnapshot struct {
	Available        bool                                `json:"available"`
	Stored           bool                                `json:"stored"`
	RequiresApproval bool                                `json:"requires_approval"`
	Action           string                              `json:"action,omitempty"`
	Status           string                              `json:"status,omitempty"`
	Message          string                              `json:"message,omitempty"`
	Entry            *controlSideChatActionEntrySnapshot `json:"entry,omitempty"`
	ControlMessage   *controlMessageSnapshot             `json:"control_message,omitempty"`
	StopFlag         *controlStopFlagState               `json:"stop_flag,omitempty"`
	Context          *controlSideChatContextSnapshot     `json:"context,omitempty"`
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

type controlAskHumanSnapshot struct {
	Present        bool   `json:"present"`
	RunID          string `json:"run_id,omitempty"`
	Question       string `json:"question,omitempty"`
	Blocker        string `json:"blocker,omitempty"`
	ActionSummary  string `json:"action_summary,omitempty"`
	PlannerOutcome string `json:"planner_outcome,omitempty"`
	ResponseID     string `json:"response_id,omitempty"`
	Source         string `json:"source,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	Message        string `json:"message,omitempty"`
}

type controlStatusSnapshot struct {
	Protocol       controlProtocolSnapshot        `json:"protocol"`
	Runtime        controlRuntimeSnapshot         `json:"runtime"`
	Backend        controlBackendSnapshot         `json:"backend"`
	ActiveRunGuard controlActiveRunGuardSnapshot  `json:"active_run_guard"`
	StopFlag       controlStopFlagState           `json:"stop_flag"`
	Run            *controlRunSnapshot            `json:"run,omitempty"`
	ModelHealth    controlModelHealthSnapshot     `json:"model_health"`
	PlannerStatus  controlPlannerStatusSnapshot   `json:"planner_status"`
	Roadmap        controlRoadmapSnapshot         `json:"roadmap"`
	PendingAction  controlPendingActionSnapshot   `json:"pending_action"`
	Artifacts      controlArtifactsSnapshot       `json:"artifacts"`
	Workers        controlWorkersSnapshot         `json:"workers"`
	Approval       controlApprovalSnapshot        `json:"approval"`
	AskHuman       controlAskHumanSnapshot        `json:"ask_human"`
	Timeouts       controlTimeoutSettingsSnapshot `json:"timeouts"`
	BuildTime      controlBuildTimeSnapshot       `json:"build_time"`
	Permissions    controlPermissionSnapshot      `json:"permissions"`
	UpdateStatus   controlUpdateStatusSnapshot    `json:"update_status"`
}

type controlRuntimeConfigSnapshot struct {
	ConfigPath             string                         `json:"config_path"`
	Verbosity              string                         `json:"verbosity"`
	PlannerModel           string                         `json:"planner_model"`
	WorkerConcurrencyLimit int                            `json:"worker_concurrency_limit"`
	DriftWatcherEnabled    bool                           `json:"drift_watcher_enabled"`
	Timeouts               controlTimeoutSettingsSnapshot `json:"timeouts"`
	Permissions            controlPermissionSnapshot      `json:"permissions"`
	Updates                controlUpdateSettingsSnapshot  `json:"updates"`
	NTFY                   controlNTFYConfigSnapshot      `json:"ntfy"`
	MutableFields          []string                       `json:"mutable_fields"`
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

type controlActiveRunGuardSnapshot struct {
	Present             bool   `json:"present"`
	Path                string `json:"path"`
	RunID               string `json:"run_id,omitempty"`
	Action              string `json:"action,omitempty"`
	Status              string `json:"status,omitempty"`
	RepoPath            string `json:"repo_path,omitempty"`
	BackendPID          int    `json:"backend_pid,omitempty"`
	BackendStartedAt    string `json:"backend_started_at,omitempty"`
	SessionID           string `json:"session_id,omitempty"`
	StartedAt           string `json:"started_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
	CurrentlyProcessing bool   `json:"currently_processing"`
	WaitingAtSafePoint  bool   `json:"waiting_at_safe_point"`
	LastProgressAt      string `json:"last_progress_at,omitempty"`
	CurrentBackend      bool   `json:"current_backend"`
	Stale               bool   `json:"stale"`
	StaleReason         string `json:"stale_reason,omitempty"`
	Message             string `json:"message,omitempty"`
}

type controlStaleRunRecoverySnapshot struct {
	Recovered          bool                           `json:"recovered"`
	RunID              string                         `json:"run_id,omitempty"`
	Reason             string                         `json:"reason,omitempty"`
	Status             string                         `json:"status,omitempty"`
	ActiveGuardCleared bool                           `json:"active_guard_cleared"`
	NextOperatorAction string                         `json:"next_operator_action,omitempty"`
	Guard              *controlActiveRunGuardSnapshot `json:"active_run_guard,omitempty"`
	Message            string                         `json:"message,omitempty"`
}

type controlModelHealthCache struct {
	mu       sync.Mutex
	planner  *controlModelComponentSnapshot
	executor *controlModelComponentSnapshot
}

func (c *controlModelHealthCache) store(component controlModelComponentSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copy := component
	switch strings.TrimSpace(component.Component) {
	case "planner":
		c.planner = &copy
	case "executor":
		c.executor = &copy
	}
}

func (c *controlModelHealthCache) merge(base controlModelHealthSnapshot, latestRun *state.Run) controlModelHealthSnapshot {
	c.mu.Lock()
	var planner *controlModelComponentSnapshot
	var executor *controlModelComponentSnapshot
	if c.planner != nil {
		copy := *c.planner
		planner = &copy
	}
	if c.executor != nil {
		copy := *c.executor
		executor = &copy
	}
	c.mu.Unlock()

	if planner != nil && modelHealthComponentCompatible(*planner, base.Planner) {
		base.Planner = *planner
	}
	if executor != nil && modelHealthComponentCompatible(*executor, base.Executor) && !latestRunHasNewerExecutorFailure(latestRun, executor.LastTestedAt) {
		base.Executor = *executor
	}
	return combineModelHealth(base.Planner, base.Executor)
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

func runtimeVersion(inv Invocation) string {
	return firstNonEmpty(strings.TrimSpace(inv.Version), buildinfo.Current().Version)
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
	if journalWriter != nil && strings.TrimSpace(run.ID) != "" {
		_ = journalWriter.Append(journal.Event{
			Type:       "runtime.config.changed",
			RunID:      run.ID,
			RepoPath:   run.RepoPath,
			Goal:       run.Goal,
			Status:     string(run.Status),
			Message:    "runtime configuration changed at a safe point",
			Checkpoint: journalCheckpointRef(run.LatestCheckpoint),
		})
	}

	emitEngineEvent(*inv, "runtime_config_changed", eventPayloadForRun(run, map[string]any{
		"applies_at": "current_safe_point",
	}))
	if before.Verbosity == after.Verbosity {
		return nil
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

func withControlStateBusyRetry[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var out T
	err := state.WithBusyRetry(ctx, func() error {
		value, err := fn()
		if err != nil {
			return err
		}
		out = value
		return nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
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
	modelCache := &controlModelHealthCache{}
	return control.Server{
		Broker: inv.Events,
		Actions: control.ActionSet{
			StartRun: func(ctx context.Context, request control.StartRunRequest) (any, error) {
				return runManager.StartRun(ctx, *current, request)
			},
			ContinueRun: func(ctx context.Context, request control.ContinueRunRequest) (any, error) {
				return runManager.ContinueRun(ctx, *current, request)
			},
			GetActiveRunGuard: func(ctx context.Context) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlActiveRunGuardSnapshot, error) {
					return runManager.activeRunGuardSnapshot(*current), nil
				})
			},
			RecoverStaleRun: func(ctx context.Context, request control.RecoverStaleRunRequest) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlStaleRunRecoverySnapshot, error) {
					return runManager.RecoverStaleRun(ctx, *current, request)
				})
			},
			TestPlannerModel: func(ctx context.Context, request control.ModelTestRequest) (any, error) {
				snapshot, err := testPlannerModelHealth(ctx, *current, request)
				if err != nil {
					return nil, err
				}
				modelCache.store(snapshot.Planner)
				latestRun, _ := runForModelHealth(ctx, *current, "")
				return modelCache.merge(snapshot, latestRun), nil
			},
			TestExecutorModel: func(ctx context.Context, request control.ModelTestRequest) (any, error) {
				snapshot, err := testExecutorModelHealth(ctx, *current, request)
				if err != nil {
					return nil, err
				}
				modelCache.store(snapshot.Executor)
				latestRun, _ := runForModelHealth(ctx, *current, "")
				return modelCache.merge(snapshot, latestRun), nil
			},
			GetStatusSnapshot: func(ctx context.Context, runID string) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlStatusSnapshot, error) {
					snapshot, err := buildControlStatusSnapshot(ctx, *current, runID)
					if err != nil {
						return controlStatusSnapshot{}, err
					}
					latestRun, _ := runForModelHealth(ctx, *current, runID)
					snapshot.ModelHealth = modelCache.merge(snapshot.ModelHealth, latestRun)
					return runManager.applyActiveStatus(snapshot), nil
				})
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
				return withControlStateBusyRetry(ctx, func() (controlPendingActionSnapshot, error) {
					return getPendingActionSnapshot(ctx, *current, runID)
				})
			},
			ApproveExecutor: func(ctx context.Context, request control.ExecutorApprovalActionRequest) (any, error) {
				return approveExecutorRequest(ctx, *current, request)
			},
			DenyExecutor: func(ctx context.Context, request control.ExecutorApprovalActionRequest) (any, error) {
				return denyExecutorRequest(ctx, *current, request)
			},
			GetSetupHealth: func(ctx context.Context) (any, error) {
				return getSetupHealth(ctx, *current)
			},
			RunSetupAction: func(ctx context.Context, request control.SetupActionRequest) (any, error) {
				return runSetupAction(ctx, *current, request)
			},
			CaptureSnapshot: func(ctx context.Context, request control.CaptureSnapshotRequest) (any, error) {
				return captureControlSnapshot(ctx, *current, request)
			},
			GetArtifact: func(ctx context.Context, request control.ArtifactRequest) (any, error) {
				return getArtifact(ctx, *current, request)
			},
			ListRecentArtifacts: func(ctx context.Context, request control.ListArtifactsRequest) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlArtifactsSnapshot, error) {
					return listRecentArtifacts(ctx, *current, request)
				})
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
				return withControlStateBusyRetry(ctx, func() (controlMessageListSnapshot, error) {
					return listControlMessages(ctx, *current, request)
				})
			},
			SendSideChatMessage: func(ctx context.Context, request control.SideChatRequest) (any, error) {
				return sendSideChatMessage(ctx, *current, request)
			},
			ListSideChatMessages: func(ctx context.Context, request control.ListSideChatMessagesRequest) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlSideChatListSnapshot, error) {
					return listSideChatMessages(ctx, *current, request)
				})
			},
			SideChatContextSnapshot: func(ctx context.Context, request control.SideChatContextSnapshotRequest) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlSideChatContextSnapshot, error) {
					return sideChatContextSnapshot(ctx, *current, request)
				})
			},
			SideChatActionRequest: func(ctx context.Context, request control.SideChatActionRequest) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlSideChatActionSnapshot, error) {
					return requestSideChatAction(ctx, *current, request)
				})
			},
			CaptureDogfoodIssue: func(ctx context.Context, request control.CaptureDogfoodIssueRequest) (any, error) {
				return captureDogfoodIssue(ctx, *current, request)
			},
			ListDogfoodIssues: func(ctx context.Context, request control.ListDogfoodIssuesRequest) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlDogfoodIssueListSnapshot, error) {
					return listDogfoodIssues(ctx, *current, request)
				})
			},
			ListWorkers: func(ctx context.Context, request control.ListWorkersRequest) (any, error) {
				return withControlStateBusyRetry(ctx, func() (controlWorkerListSnapshot, error) {
					return listWorkersForControl(ctx, *current, request)
				})
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
			TestNTFY: func(ctx context.Context) (any, error) {
				return testNTFYForControl(ctx, *current)
			},
			CheckForUpdates: func(ctx context.Context, request control.UpdateRequest) (any, error) {
				return checkUpdateStatus(ctx, *current, updateActionRequest{
					IncludePrereleases: request.IncludePrereleases,
					Version:            request.Version,
				})
			},
			GetUpdateStatus: func(_ context.Context) (any, error) {
				return buildUpdateStatusSnapshot(*current, currentConfig(*current).Updates, nil), nil
			},
			InstallUpdate: func(ctx context.Context, request control.UpdateRequest) (any, error) {
				return installUpdate(ctx, *current, updateActionRequest{
					IncludePrereleases: request.IncludePrereleases,
					Version:            request.Version,
				})
			},
			SkipUpdate: func(ctx context.Context, request control.UpdateRequest) (any, error) {
				return skipUpdate(ctx, current, request)
			},
			GetUpdateChangelog: func(ctx context.Context, request control.UpdateRequest) (any, error) {
				status, err := checkUpdateStatus(ctx, *current, updateActionRequest{
					IncludePrereleases: request.IncludePrereleases,
					Version:            request.Version,
				})
				if err != nil {
					return status, err
				}
				return map[string]any{
					"latest_version": status.LatestVersion,
					"release_url":    status.ReleaseURL,
					"changelog":      status.Changelog,
				}, nil
			},
		},
	}
}

func buildControlStatusSnapshot(ctx context.Context, inv Invocation, requestedRunID string) (controlStatusSnapshot, error) {
	cfg := currentConfig(inv)
	if _, err := repairSafeRepoContractDirs(inv.Layout); err != nil {
		return controlStatusSnapshot{}, err
	}
	repoContract := inspectTargetRepoContract(inv.RepoRoot)
	snapshot := controlStatusSnapshot{
		Protocol:       buildControlProtocolSnapshot(),
		Backend:        buildControlBackendSnapshot(inv),
		ActiveRunGuard: buildActiveRunGuardSnapshot(inv),
		StopFlag:       buildControlStopFlagSnapshot(inv),
		Runtime: controlRuntimeSnapshot{
			EngineMode:                "headless_cli",
			RepoRoot:                  inv.RepoRoot,
			ProtocolVersion:           controlProtocolVersion,
			SupportsNTFYRuntimeConfig: true,
			RepoReady:                 repoContract.Ready,
			RepoContractMissing:       repoContract.Missing,
			PlannerReady:              plannerAPIKeyStatus() == "present",
			ExecutorReady:             executorAppServerState() == "ready",
			NTFYReady:                 ntfyBridgeState(cfg) == "ready",
			Verbosity:                 cfg.Verbosity,
			WorkerConcurrencyLimit:    cfg.WorkerConcurrencyLimit,
			PermissionProfile:         cfg.Permissions.Profile,
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
		AskHuman: controlAskHumanSnapshot{
			Present: false,
			Message: "no planner question is currently waiting for a human answer",
		},
		Timeouts:     buildTimeoutSettingsSnapshot(cfg),
		BuildTime:    buildBuildTimeSnapshot(state.BuildTime{RepoPath: inv.RepoRoot}, false, false, time.Now().UTC()),
		Permissions:  buildPermissionSnapshot(cfg.Permissions),
		UpdateStatus: buildUpdateStatusSnapshot(inv, cfg.Updates, nil),
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
	if buildTime, found, err := store.GetBuildTime(ctx, inv.RepoRoot); err == nil {
		snapshot.BuildTime = buildBuildTimeSnapshot(buildTime, found, false, time.Now().UTC())
	} else {
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
		ExecutorThreadID:     strings.TrimSpace(run.ExecutorThreadID),
		ExecutorTurnID:       strings.TrimSpace(run.ExecutorTurnID),
		ExecutorTurnStatus:   strings.TrimSpace(run.ExecutorTurnStatus),
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
	snapshot.AskHuman = askHumanSnapshotFromRun(run, events, snapshot.PendingAction, snapshot.PlannerStatus)

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
	annotateControlRunActivity(&snapshot, run)

	return snapshot, nil
}

func buildControlProtocolSnapshot() controlProtocolSnapshot {
	return controlProtocolSnapshot{
		Version:  controlProtocolVersion,
		Supports: buildControlProtocolSupportSnapshot(),
	}
}

func buildControlProtocolSupportSnapshot() controlProtocolSupportSnapshot {
	return controlProtocolSupportSnapshot{
		RuntimeConfig:          true,
		NTFYRuntimeConfig:      true,
		TestNTFY:               true,
		BackendCompatibility:   true,
		RuntimeConfigFieldList: true,
	}
}

func annotateControlRunActivity(snapshot *controlStatusSnapshot, run state.Run) {
	if snapshot == nil || snapshot.Run == nil {
		return
	}

	outcome := strings.TrimSpace(snapshot.Run.LatestPlannerEvent)
	nextAction := strings.TrimSpace(snapshot.Run.NextOperatorAction)
	executeReady := controlRunExecuteReady(run, outcome, nextAction)
	waitingAtSafePoint := controlRunWaitingAtSafePoint(run, outcome, nextAction)

	snapshot.Run.ExecuteReady = executeReady
	snapshot.Run.WaitingAtSafePoint = waitingAtSafePoint
	snapshot.Run.ActivelyProcessing = false
	switch {
	case executeReady:
		snapshot.Run.ActivityState = "ready_to_dispatch"
		snapshot.Run.ActivityMessage = "Planner selected the next code task. Executor has not started yet. Click Continue Build to dispatch it."
	case waitingAtSafePoint:
		snapshot.Run.ActivityState = "waiting_at_safe_point"
		snapshot.Run.ActivityMessage = "The run is paused at a safe checkpoint. Continue Build will advance the next planner-owned step."
	case run.Status == state.StatusCompleted:
		snapshot.Run.ActivityState = "completed"
		snapshot.Run.ActivityMessage = "The run is complete."
	case run.Status == state.StatusFailed || strings.TrimSpace(run.RuntimeIssueReason) != "":
		snapshot.Run.ActivityState = "error"
		snapshot.Run.ActivityMessage = "The run stopped with an error or runtime issue."
	default:
		snapshot.Run.ActivityState = "stopped"
		snapshot.Run.ActivityMessage = "The run is not currently being advanced by the control server."
	}

	if snapshot.ActiveRunGuard.Present && !snapshot.ActiveRunGuard.CurrentlyProcessing {
		snapshot.ActiveRunGuard.WaitingAtSafePoint = waitingAtSafePoint
		if strings.TrimSpace(snapshot.ActiveRunGuard.LastProgressAt) == "" {
			snapshot.ActiveRunGuard.LastProgressAt = formatSnapshotTime(run.UpdatedAt)
		}
	}
}

func controlRunExecuteReady(run state.Run, outcome string, nextAction string) bool {
	return run.LatestCheckpoint.SafePause &&
		strings.TrimSpace(run.LatestCheckpoint.Stage) == "planner" &&
		strings.TrimSpace(outcome) == "execute" &&
		strings.TrimSpace(nextAction) == "continue_existing_run" &&
		!controlExecutorTurnActive(run)
}

func controlRunWaitingAtSafePoint(run state.Run, outcome string, nextAction string) bool {
	if !run.LatestCheckpoint.SafePause || !isRunResumable(run) || controlExecutorTurnActive(run) {
		return false
	}
	stopReason := strings.TrimSpace(run.LatestStopReason)
	if stopReason == orchestration.StopReasonPlannerAskHuman || stopReason == orchestration.StopReasonExecutorApprovalReq {
		return false
	}
	if executorApprovalStateValue(run) == orchestrationApprovalStateRequired {
		return false
	}
	return strings.TrimSpace(nextAction) == "continue_existing_run" || strings.TrimSpace(outcome) != ""
}

func controlExecutorTurnActive(run state.Run) bool {
	switch strings.TrimSpace(run.ExecutorTurnStatus) {
	case "active", "running", "in_progress", "started", "executor_active":
		return true
	default:
		return false
	}
}

func askHumanSnapshotFromRun(
	run state.Run,
	events []journal.Event,
	pending controlPendingActionSnapshot,
	plannerStatus controlPlannerStatusSnapshot,
) controlAskHumanSnapshot {
	present := strings.TrimSpace(run.LatestStopReason) == "planner_ask_human" ||
		strings.TrimSpace(latestPlannerOutcomeFromEvents(events)) == "ask_human" ||
		strings.TrimSpace(nextOperatorActionForExistingRun(run)) == "answer_human_question" ||
		strings.TrimSpace(pending.TurnType) == "ask_human" ||
		strings.TrimSpace(pending.PlannerOutcome) == "ask_human"

	snapshot := controlAskHumanSnapshot{
		Present:        present,
		RunID:          strings.TrimSpace(run.ID),
		PlannerOutcome: "ask_human",
		Message:        "no planner question is currently waiting for a human answer",
	}
	if !present {
		return snapshot
	}

	snapshot.Message = "planner is waiting for a raw human answer"
	if strings.TrimSpace(pending.PendingActionSummary) != "" {
		snapshot.Question = strings.TrimSpace(pending.PendingActionSummary)
		snapshot.ActionSummary = strings.TrimSpace(pending.PendingActionSummary)
		snapshot.Source = "pending_action"
		snapshot.ResponseID = strings.TrimSpace(pending.PlannerResponseID)
		snapshot.UpdatedAt = strings.TrimSpace(pending.UpdatedAt)
	}

	for idx := len(events) - 1; idx >= 0; idx-- {
		event := events[idx]
		if strings.TrimSpace(event.HumanQuestion) == "" && strings.TrimSpace(event.PlannerOutcome) != "ask_human" {
			continue
		}
		if strings.TrimSpace(event.HumanQuestion) != "" {
			snapshot.Question = strings.TrimSpace(event.HumanQuestion)
			snapshot.Blocker = strings.TrimSpace(event.Message)
		}
		if strings.TrimSpace(snapshot.ActionSummary) == "" {
			snapshot.ActionSummary = askHumanEventSummary(event)
		}
		if strings.TrimSpace(event.ResponseID) != "" {
			snapshot.ResponseID = strings.TrimSpace(event.ResponseID)
		}
		if !event.At.IsZero() {
			snapshot.UpdatedAt = formatSnapshotTime(event.At)
		}
		if strings.TrimSpace(snapshot.Source) == "" {
			snapshot.Source = strings.TrimSpace(event.Type)
		}
		break
	}

	if strings.TrimSpace(snapshot.Question) == "" {
		snapshot.Question = firstNonEmpty(
			strings.TrimSpace(snapshot.ActionSummary),
			strings.TrimSpace(plannerStatus.OperatorMessage),
			strings.TrimSpace(plannerStatus.CurrentFocus),
			"The planner needs a raw human answer before it can continue.",
		)
	}
	if strings.TrimSpace(snapshot.Blocker) == "" {
		snapshot.Blocker = firstNonEmpty(
			strings.TrimSpace(plannerStatus.OperatorMessage),
			strings.TrimSpace(plannerStatus.CurrentFocus),
			strings.TrimSpace(snapshot.ActionSummary),
			"The planner stopped at ask_human and is waiting for your answer.",
		)
	}
	if strings.TrimSpace(snapshot.ActionSummary) == "" {
		snapshot.ActionSummary = snapshot.Question
	}
	if strings.TrimSpace(snapshot.Source) == "" {
		snapshot.Source = "run_status"
	}
	return snapshot
}

func askHumanEventSummary(event journal.Event) string {
	return firstNonEmpty(
		strings.TrimSpace(event.Message),
		strings.TrimSpace(event.HumanQuestion),
		strings.TrimSpace(event.PlannerOutcome),
		strings.TrimSpace(event.Type),
	)
}

func buildRuntimeConfigSnapshot(inv Invocation) controlRuntimeConfigSnapshot {
	cfg := currentConfig(inv)
	return controlRuntimeConfigSnapshot{
		ConfigPath:             inv.ConfigPath,
		Verbosity:              cfg.Verbosity,
		PlannerModel:           resolvePlannerModel(inv),
		WorkerConcurrencyLimit: cfg.WorkerConcurrencyLimit,
		DriftWatcherEnabled:    cfg.DriftWatcherEnabled,
		Timeouts:               buildTimeoutSettingsSnapshot(cfg),
		Permissions:            buildPermissionSnapshot(cfg.Permissions),
		Updates:                buildUpdateSettingsSnapshot(cfg.Updates),
		NTFY:                   buildNTFYConfigSnapshot(cfg.NTFY),
		MutableFields: []string{
			"verbosity",
			"timeouts.planner_request_timeout",
			"timeouts.executor_idle_timeout",
			"timeouts.executor_turn_timeout",
			"timeouts.subagent_timeout",
			"timeouts.shell_command_timeout",
			"timeouts.install_timeout",
			"timeouts.human_wait_timeout",
			"permission_profile",
			"permissions.ask_before_installing_programs",
			"permissions.ask_before_installing_dependencies",
			"permissions.ask_before_modifying_files_outside_repo",
			"permissions.ask_before_deleting_files",
			"permissions.ask_before_running_tests",
			"permissions.ask_before_emulator_device_testing",
			"permissions.ask_before_network_calls",
			"permissions.ask_before_git_commits",
			"permissions.ask_before_git_pushes",
			"permissions.ask_before_updater_installs",
			"permissions.ask_before_worker_parallelism",
			"permissions.ask_before_executor_steering",
			"permissions.ask_before_planner_direction_changes",
			"updates.update_channel",
			"updates.auto_check_updates",
			"updates.auto_download_updates",
			"updates.auto_install_updates",
			"updates.ask_before_update",
			"updates.include_prereleases",
			"updates.update_check_interval",
			"ntfy.server_url",
			"ntfy.topic",
			"ntfy.auth_token",
		},
	}
}

func buildNTFYConfigSnapshot(cfg config.NTFYConfig) controlNTFYConfigSnapshot {
	configured := ntfy.IsConfigured(cfg)
	return controlNTFYConfigSnapshot{
		Configured:     configured,
		ServerURL:      strings.TrimSpace(cfg.ServerURL),
		Topic:          strings.TrimSpace(cfg.Topic),
		AuthTokenSaved: strings.TrimSpace(cfg.AuthToken) != "",
		Listening: func() string {
			if configured {
				return "active during planner ask-human waits"
			}
			return "not listening"
		}(),
		Message: func() string {
			if configured {
				return "ntfy is configured. Replies are subscribed during planner ask-human waits and are forwarded raw."
			}
			return "ntfy is not configured for this repo/session."
		}(),
	}
}

func testNTFYForControl(ctx context.Context, inv Invocation) (map[string]any, error) {
	cfg := currentConfig(inv).NTFY
	if !ntfy.IsConfigured(cfg) {
		return nil, errors.New("ntfy is not configured; set ntfy.server_url and ntfy.topic first")
	}
	client, err := ntfy.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	published, err := client.PublishMessage(
		ctx,
		"Aurora Orchestrator ntfy test",
		fmt.Sprintf("Aurora Orchestrator test notification for %s at %s.", filepath.Base(inv.RepoRoot), time.Now().UTC().Format(time.RFC3339)),
		[]string{"orchestrator", "test"},
	)
	if err != nil {
		return nil, err
	}
	emitEngineEvent(inv, "ntfy_test_completed", map[string]any{
		"repo_path":  inv.RepoRoot,
		"topic":      strings.TrimSpace(cfg.Topic),
		"message_id": published.ID,
	})
	return map[string]any{
		"configured":       true,
		"test_sent":        true,
		"status":           "sent",
		"message_id":       published.ID,
		"server_url":       strings.TrimSpace(cfg.ServerURL),
		"topic":            strings.TrimSpace(cfg.Topic),
		"auth_token_saved": strings.TrimSpace(cfg.AuthToken) != "",
		"listening":        "active during planner ask-human waits",
		"message":          "ntfy config saved and test notification sent. Planner ask-human replies on this topic are forwarded raw.",
	}, nil
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
	if patch.Timeouts.HasChanges() || patch.PermissionProfile != nil || patch.Permissions.HasChanges() || patch.Updates.HasChanges() || patch.NTFY.HasChanges() {
		manager := runtimecfg.NewManager("", inv.Config)
		cfg, _, err := manager.ApplyPatch(patch)
		if err != nil {
			return controlRuntimeConfigSnapshot{}, err
		}
		inv.Config = cfg
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

func buildControlStopFlagSnapshot(inv Invocation) controlStopFlagState {
	path := autoStopFlagPath(inv.Layout)
	snapshot := controlStopFlagState{
		Present:   false,
		Path:      path,
		AppliesAt: "next_safe_point",
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return snapshot
	}
	snapshot.Present = true
	snapshot.Reason = strings.TrimSpace(string(raw))
	return snapshot
}

func buildControlBackendSnapshot(inv Invocation) controlBackendSnapshot {
	build := buildinfo.Current()
	snapshot := controlBackendSnapshot{
		PID:                       os.Getpid(),
		StartedAt:                 formatSnapshotTime(controlBackendStartedAt),
		BinaryVersion:             runtimeVersion(inv),
		BinaryRevision:            build.Revision,
		BinaryBuildTime:           build.BuildTime,
		ProtocolVersion:           controlProtocolVersion,
		RepoRoot:                  inv.RepoRoot,
		ControlAddress:            strings.TrimSpace(os.Getenv("ORCHESTRATOR_CONTROL_ADDR")),
		Supports:                  buildControlProtocolSupportSnapshot(),
		SupportsNTFYRuntimeConfig: true,
	}

	executable, err := os.Executable()
	if err == nil {
		snapshot.BinaryPath = executable
		if info, statErr := os.Stat(executable); statErr == nil {
			modified := info.ModTime().UTC()
			snapshot.BinaryModifiedAt = formatSnapshotTime(modified)
			if modified.After(controlBackendStartedAt.Add(time.Second)) {
				snapshot.Stale = true
				snapshot.StaleReason = "binary file was modified after this backend process started; restart the backend before trusting newly rebuilt code"
			}
		}
	}

	metadataPath := dogfoodBackendMetadataPath(inv)
	if metadataPath != "" {
		snapshot.OwnerMetadata = metadataPath
		if raw, err := os.ReadFile(metadataPath); err == nil {
			var metadata map[string]any
			if json.Unmarshal(raw, &metadata) == nil {
				snapshot.Owner = stringMapValue(metadata, "owner")
				snapshot.OwnerSessionID = firstNonEmpty(stringMapValue(metadata, "owner_session_id"), stringMapValue(metadata, "session_id"))
				if snapshot.ControlAddress == "" {
					snapshot.ControlAddress = stringMapValue(metadata, "control_addr")
				}
			}
		}
	}

	return snapshot
}

func dogfoodBackendMetadataPath(inv Invocation) string {
	if strings.TrimSpace(inv.Layout.StateDir) == "" {
		return ""
	}
	return filepath.Join(inv.Layout.StateDir, "dogfood-backend.json")
}

func stringMapValue(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func modelHealthComponentCompatible(fresh controlModelComponentSnapshot, current controlModelComponentSnapshot) bool {
	if strings.TrimSpace(fresh.Component) == "" || strings.TrimSpace(current.Component) == "" {
		return true
	}
	if strings.TrimSpace(fresh.Component) != strings.TrimSpace(current.Component) {
		return false
	}
	if strings.TrimSpace(fresh.Component) == "planner" {
		return strings.TrimSpace(current.ConfiguredModel) == "" ||
			strings.TrimSpace(fresh.ConfiguredModel) == "" ||
			strings.EqualFold(strings.TrimSpace(fresh.ConfiguredModel), strings.TrimSpace(current.ConfiguredModel))
	}
	if strings.TrimSpace(fresh.Component) == "executor" {
		return strings.TrimSpace(current.ConfiguredModel) == "" ||
			strings.TrimSpace(fresh.ConfiguredModel) == "" ||
			strings.EqualFold(strings.TrimSpace(fresh.ConfiguredModel), strings.TrimSpace(current.ConfiguredModel))
	}
	return true
}

func latestRunHasNewerExecutorFailure(run *state.Run, testedAt string) bool {
	if run == nil || strings.TrimSpace(testedAt) == "" {
		return false
	}
	if strings.TrimSpace(run.ExecutorLastError) == "" && strings.TrimSpace(run.RuntimeIssueMessage) == "" {
		return false
	}
	tested, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(testedAt))
	if err != nil {
		return false
	}
	return run.UpdatedAt.After(tested)
}

func runForModelHealth(ctx context.Context, inv Invocation, requestedRunID string) (*state.Run, bool) {
	if strings.TrimSpace(requestedRunID) == "" {
		run, found := latestRunForModelHealth(ctx, inv)
		if !found {
			return nil, false
		}
		return &run, true
	}
	if !pathExists(inv.Layout.DBPath) {
		return nil, false
	}
	store, err := openExistingStore(inv.Layout)
	if err != nil {
		return nil, false
	}
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		return nil, false
	}
	run, found, err := store.GetRun(ctx, strings.TrimSpace(requestedRunID))
	if err != nil || !found {
		return nil, false
	}
	return &run, true
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
