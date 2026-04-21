package planner

import "time"

const ContractVersionV1 = "planner.v1"

type OutcomeKind string

const (
	OutcomeExecute        OutcomeKind = "execute"
	OutcomeAskHuman       OutcomeKind = "ask_human"
	OutcomeCollectContext OutcomeKind = "collect_context"
	OutcomePause          OutcomeKind = "pause"
	OutcomeComplete       OutcomeKind = "complete"
)

type CapabilityStatus string

const (
	CapabilityContractOnly CapabilityStatus = "contract_only"
	CapabilityDeferred     CapabilityStatus = "deferred"
	CapabilityAvailable    CapabilityStatus = "available"
	CapabilityUnavailable  CapabilityStatus = "unavailable"
)

type InputEnvelope struct {
	ContractVersion  string                   `json:"contract_version"`
	RunID            string                   `json:"run_id"`
	RepoPath         string                   `json:"repo_path"`
	Goal             string                   `json:"goal"`
	RunStatus        string                   `json:"run_status"`
	LatestCheckpoint Checkpoint               `json:"latest_checkpoint"`
	RecentEvents     []EventPreview           `json:"recent_events,omitempty"`
	ExecutorResult   *ExecutorResultSummary   `json:"executor_result,omitempty"`
	CollectedContext *CollectedContextSummary `json:"collected_context,omitempty"`
	DriftReview      *DriftReviewSummary      `json:"drift_review,omitempty"`
	PluginTools      []PluginToolDescriptor   `json:"plugin_tools,omitempty"`
	RepoContracts    RepoContractAvailability `json:"repo_contracts"`
	RawHumanReplies  []RawHumanReply          `json:"raw_human_replies,omitempty"`
	Capabilities     CapabilityMarkers        `json:"capabilities"`
}

type Checkpoint struct {
	Sequence     int64     `json:"sequence"`
	Stage        string    `json:"stage"`
	Label        string    `json:"label"`
	SafePause    bool      `json:"safe_pause"`
	PlannerTurn  int64     `json:"planner_turn"`
	ExecutorTurn int64     `json:"executor_turn"`
	CreatedAt    time.Time `json:"created_at"`
}

type EventPreview struct {
	At      time.Time `json:"at"`
	Type    string    `json:"type"`
	Summary string    `json:"summary"`
}

type RepoContractAvailability struct {
	HasAgentsMD         bool   `json:"has_agents_md"`
	AgentsMDPath        string `json:"agents_md_path"`
	HasUpdatedSpec      bool   `json:"has_updated_spec"`
	UpdatedSpecPath     string `json:"updated_spec_path"`
	HasNonNegotiables   bool   `json:"has_non_negotiables"`
	NonNegotiablesPath  string `json:"non_negotiables_path"`
	HasExecPlan         bool   `json:"has_exec_plan"`
	ExecPlanPath        string `json:"exec_plan_path"`
	OrchestratorDirPath string `json:"orchestrator_dir_path"`
	RoadmapPath         string `json:"roadmap_path"`
	DecisionsPath       string `json:"decisions_path"`
}

type RawHumanReply struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	ReceivedAt time.Time `json:"received_at"`
	Payload    string    `json:"payload"`
}

type CapabilityMarkers struct {
	Planner  CapabilityStatus `json:"planner"`
	Executor CapabilityStatus `json:"executor"`
	NTFY     CapabilityStatus `json:"ntfy"`
}

type ExecutorResultSummary struct {
	FinalMessage string `json:"final_message"`
	Success      bool   `json:"success"`
	ThreadID     string `json:"thread_id,omitempty"`
}

type CollectedContextSummary struct {
	Focus         string                   `json:"focus"`
	Questions     []string                 `json:"questions,omitempty"`
	Results       []CollectedContextResult `json:"results,omitempty"`
	ToolResults   []PluginToolResult       `json:"tool_results,omitempty"`
	WorkerResults []WorkerActionResult     `json:"worker_results,omitempty"`
	WorkerPlan    *WorkerPlanResult        `json:"worker_plan,omitempty"`
}

type CollectedContextResult struct {
	RequestedPath string   `json:"requested_path"`
	ResolvedPath  string   `json:"resolved_path,omitempty"`
	Kind          string   `json:"kind"`
	Detail        string   `json:"detail,omitempty"`
	Preview       string   `json:"preview,omitempty"`
	Entries       []string `json:"entries,omitempty"`
	Truncated     bool     `json:"truncated,omitempty"`
}

type DriftReviewSummary struct {
	Reviewer                      string   `json:"reviewer"`
	Aligned                       bool     `json:"aligned"`
	Concerns                      []string `json:"concerns,omitempty"`
	MissingContext                []string `json:"missing_context,omitempty"`
	RecommendedPlannerAdjustments []string `json:"recommended_planner_adjustments,omitempty"`
	EvidencePaths                 []string `json:"evidence_paths,omitempty"`
}

type PluginToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type PluginToolCall struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type PluginToolResult struct {
	Tool            string         `json:"tool"`
	Success         bool           `json:"success"`
	Message         string         `json:"message,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
	ArtifactPath    string         `json:"artifact_path,omitempty"`
	ArtifactPreview string         `json:"artifact_preview,omitempty"`
}

type WorkerActionKind string

const (
	WorkerActionCreate    WorkerActionKind = "create"
	WorkerActionDispatch  WorkerActionKind = "dispatch"
	WorkerActionList      WorkerActionKind = "list"
	WorkerActionRemove    WorkerActionKind = "remove"
	WorkerActionIntegrate WorkerActionKind = "integrate"
	WorkerActionApply     WorkerActionKind = "apply"
)

type WorkerApplyMode string

const (
	WorkerApplyModeAbortIfConflicts WorkerApplyMode = "abort_if_conflicts"
	WorkerApplyModeNonConflicting   WorkerApplyMode = "apply_non_conflicting"
	WorkerApplyModeUnavailable      WorkerApplyMode = "unavailable"
)

type WorkerPlan struct {
	Workers              []PlannedWorker `json:"workers,omitempty"`
	IntegrationRequested bool            `json:"integration_requested"`
	ApplyMode            string          `json:"apply_mode,omitempty"`
}

type PlannedWorker struct {
	Name           string `json:"name,omitempty"`
	Scope          string `json:"scope,omitempty"`
	TaskSummary    string `json:"task_summary,omitempty"`
	ExecutorPrompt string `json:"executor_prompt,omitempty"`
}

type WorkerAction struct {
	Action         WorkerActionKind `json:"action"`
	WorkerID       string           `json:"worker_id,omitempty"`
	WorkerIDs      []string         `json:"worker_ids,omitempty"`
	WorkerName     string           `json:"worker_name,omitempty"`
	Scope          string           `json:"scope,omitempty"`
	TaskSummary    string           `json:"task_summary,omitempty"`
	ExecutorPrompt string           `json:"executor_prompt,omitempty"`
	ArtifactPath   string           `json:"artifact_path,omitempty"`
	ApplyMode      string           `json:"apply_mode,omitempty"`
}

type WorkerResultSummary struct {
	WorkerID                   string    `json:"worker_id,omitempty"`
	WorkerName                 string    `json:"worker_name,omitempty"`
	WorkerStatus               string    `json:"worker_status,omitempty"`
	AssignedScope              string    `json:"assigned_scope,omitempty"`
	WorktreePath               string    `json:"worktree_path,omitempty"`
	WorkerTaskSummary          string    `json:"worker_task_summary,omitempty"`
	ExecutorPromptSummary      string    `json:"worker_executor_prompt_summary,omitempty"`
	WorkerResultSummary        string    `json:"worker_result_summary,omitempty"`
	WorkerErrorSummary         string    `json:"worker_error_summary,omitempty"`
	ExecutorThreadID           string    `json:"executor_thread_id,omitempty"`
	ExecutorTurnID             string    `json:"executor_turn_id,omitempty"`
	ExecutorTurnStatus         string    `json:"executor_turn_status,omitempty"`
	ExecutorApprovalState      string    `json:"executor_approval_state,omitempty"`
	ExecutorApprovalKind       string    `json:"executor_approval_kind,omitempty"`
	ExecutorApprovalPreview    string    `json:"executor_approval_preview,omitempty"`
	ExecutorInterruptible      bool      `json:"executor_interruptible,omitempty"`
	ExecutorSteerable          bool      `json:"executor_steerable,omitempty"`
	ExecutorFailureStage       string    `json:"executor_failure_stage,omitempty"`
	ExecutorLastControlAction  string    `json:"executor_last_control_action,omitempty"`
	ExecutorLastControlPayload string    `json:"executor_last_control_payload,omitempty"`
	StartedAt                  time.Time `json:"started_at,omitempty"`
	CompletedAt                time.Time `json:"completed_at,omitempty"`
}

type WorkerActionResult struct {
	Action          WorkerActionKind         `json:"action"`
	Success         bool                     `json:"success"`
	Message         string                   `json:"message,omitempty"`
	Worker          *WorkerResultSummary     `json:"worker,omitempty"`
	ListedWorkers   []WorkerResultSummary    `json:"listed_workers,omitempty"`
	Removed         bool                     `json:"removed,omitempty"`
	ArtifactPath    string                   `json:"artifact_path,omitempty"`
	ArtifactPreview string                   `json:"artifact_preview,omitempty"`
	Integration     *IntegrationSummary      `json:"integration,omitempty"`
	Apply           *IntegrationApplySummary `json:"apply,omitempty"`
}

type IntegrationSummary struct {
	WorkerIDs          []string                   `json:"worker_ids,omitempty"`
	Workers            []IntegrationWorkerSummary `json:"workers,omitempty"`
	ConflictCandidates []ConflictCandidate        `json:"conflict_candidates,omitempty"`
	IntegrationPreview string                     `json:"integration_preview,omitempty"`
}

type IntegrationWorkerSummary struct {
	WorkerID            string   `json:"worker_id,omitempty"`
	WorkerName          string   `json:"worker_name,omitempty"`
	WorktreePath        string   `json:"worktree_path,omitempty"`
	WorkerResultSummary string   `json:"worker_result_summary,omitempty"`
	FileList            []string `json:"file_list,omitempty"`
	DiffSummary         []string `json:"diff_summary,omitempty"`
}

type ConflictCandidate struct {
	Path        string   `json:"path,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	WorkerIDs   []string `json:"worker_ids,omitempty"`
	WorkerNames []string `json:"worker_names,omitempty"`
}

type WorkerPlanResult struct {
	Status                  string                   `json:"status,omitempty"`
	WorkerIDs               []string                 `json:"worker_ids,omitempty"`
	Workers                 []WorkerResultSummary    `json:"workers,omitempty"`
	ConcurrencyLimit        int                      `json:"concurrency_limit,omitempty"`
	IntegrationRequested    bool                     `json:"integration_requested,omitempty"`
	IntegrationArtifactPath string                   `json:"integration_artifact_path,omitempty"`
	IntegrationPreview      string                   `json:"integration_preview,omitempty"`
	ApplyMode               string                   `json:"apply_mode,omitempty"`
	ApplyArtifactPath       string                   `json:"apply_artifact_path,omitempty"`
	Apply                   *IntegrationApplySummary `json:"apply,omitempty"`
	Message                 string                   `json:"message,omitempty"`
}

type IntegrationApplySummary struct {
	Status             string                   `json:"status,omitempty"`
	SourceArtifactPath string                   `json:"source_artifact_path,omitempty"`
	ApplyMode          string                   `json:"apply_mode,omitempty"`
	FilesApplied       []IntegrationAppliedFile `json:"files_applied,omitempty"`
	FilesSkipped       []IntegrationSkippedFile `json:"files_skipped,omitempty"`
	ConflictCandidates []ConflictCandidate      `json:"conflict_candidates,omitempty"`
	BeforeSummary      string                   `json:"before_summary,omitempty"`
	AfterSummary       string                   `json:"after_summary,omitempty"`
}

type IntegrationAppliedFile struct {
	WorkerID   string `json:"worker_id,omitempty"`
	WorkerName string `json:"worker_name,omitempty"`
	Path       string `json:"path,omitempty"`
	ChangeKind string `json:"change_kind,omitempty"`
}

type IntegrationSkippedFile struct {
	WorkerID   string `json:"worker_id,omitempty"`
	WorkerName string `json:"worker_name,omitempty"`
	Path       string `json:"path,omitempty"`
	ChangeKind string `json:"change_kind,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type OutputEnvelope struct {
	ContractVersion string                 `json:"contract_version"`
	Outcome         OutcomeKind            `json:"outcome"`
	Execute         *ExecuteOutcome        `json:"execute,omitempty"`
	AskHuman        *AskHumanOutcome       `json:"ask_human,omitempty"`
	CollectContext  *CollectContextOutcome `json:"collect_context,omitempty"`
	Pause           *PauseOutcome          `json:"pause,omitempty"`
	Complete        *CompleteOutcome       `json:"complete,omitempty"`
}

type ExecuteOutcome struct {
	Task               string   `json:"task"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	WriteScope         []string `json:"write_scope,omitempty"`
}

type AskHumanOutcome struct {
	Question string `json:"question"`
	Context  string `json:"context,omitempty"`
}

type CollectContextOutcome struct {
	Focus         string           `json:"focus"`
	Questions     []string         `json:"questions,omitempty"`
	Paths         []string         `json:"paths,omitempty"`
	ToolCalls     []PluginToolCall `json:"tool_calls,omitempty"`
	WorkerActions []WorkerAction   `json:"worker_actions,omitempty"`
	WorkerPlan    *WorkerPlan      `json:"worker_plan,omitempty"`
}

type PauseOutcome struct {
	Reason string `json:"reason"`
}

type CompleteOutcome struct {
	Summary string `json:"summary"`
}
