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
	Focus     string                   `json:"focus"`
	Questions []string                 `json:"questions,omitempty"`
	Results   []CollectedContextResult `json:"results,omitempty"`
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
	Focus     string   `json:"focus"`
	Questions []string `json:"questions,omitempty"`
	Paths     []string `json:"paths,omitempty"`
}

type PauseOutcome struct {
	Reason string `json:"reason"`
}

type CompleteOutcome struct {
	Summary string `json:"summary"`
}
