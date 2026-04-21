package executor

import "time"

type Transport string

const (
	TransportAppServer Transport = "codex_app_server"
	TransportExecJSON  Transport = "codex_exec_json"
)

type TurnStatus string

const (
	TurnStatusPending          TurnStatus = "pending"
	TurnStatusInProgress       TurnStatus = "in_progress"
	TurnStatusApprovalRequired TurnStatus = "approval_required"
	TurnStatusCompleted        TurnStatus = "completed"
	TurnStatusFailed           TurnStatus = "failed"
	TurnStatusInterrupted      TurnStatus = "interrupted"
)

type ApprovalState string

const (
	ApprovalStateNone     ApprovalState = ""
	ApprovalStateRequired ApprovalState = "required"
	ApprovalStateGranted  ApprovalState = "granted"
	ApprovalStateDenied   ApprovalState = "denied"
)

type ApprovalKind string

const (
	ApprovalKindCommandExecution ApprovalKind = "command_execution"
	ApprovalKindFileChange       ApprovalKind = "file_change"
	ApprovalKindPermissions      ApprovalKind = "permissions"
)

type ApprovalDecision string

const (
	ApprovalDecisionAccept  ApprovalDecision = "accept"
	ApprovalDecisionDecline ApprovalDecision = "decline"
)

type ControlAction string

const (
	ControlActionApprove   ControlAction = "approved"
	ControlActionDeny      ControlAction = "denied"
	ControlActionInterrupt ControlAction = "interrupted"
	ControlActionKill      ControlAction = "kill_unsupported"
	ControlActionSteer     ControlAction = "steered"
)

type ApprovalRequest struct {
	RequestID  string        `json:"request_id,omitempty"`
	ApprovalID string        `json:"approval_id,omitempty"`
	ItemID     string        `json:"item_id,omitempty"`
	State      ApprovalState `json:"state,omitempty"`
	Kind       ApprovalKind  `json:"kind,omitempty"`
	Reason     string        `json:"reason,omitempty"`
	Command    string        `json:"command,omitempty"`
	CWD        string        `json:"cwd,omitempty"`
	GrantRoot  string        `json:"grant_root,omitempty"`
	RawParams  string        `json:"raw_params,omitempty"`
}

type ControlRecord struct {
	Action  ControlAction `json:"action"`
	Payload string        `json:"payload,omitempty"`
	At      time.Time     `json:"at"`
}

type TurnRequest struct {
	RunID      string
	RepoPath   string
	Prompt     string
	ThreadID   string
	ThreadPath string
	TurnID     string
	Continue   bool
}

type Failure struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type TurnResult struct {
	Transport     Transport        `json:"transport"`
	RunID         string           `json:"run_id"`
	ThreadID      string           `json:"thread_id,omitempty"`
	ThreadPath    string           `json:"thread_path,omitempty"`
	ResumedThread bool             `json:"resumed_thread"`
	TurnID        string           `json:"turn_id,omitempty"`
	TurnStatus    TurnStatus       `json:"turn_status,omitempty"`
	Model         string           `json:"model,omitempty"`
	ModelProvider string           `json:"model_provider,omitempty"`
	EventsSeen    int              `json:"events_seen"`
	StartedAt     time.Time        `json:"started_at,omitempty"`
	CompletedAt   time.Time        `json:"completed_at,omitempty"`
	FinalMessage  string           `json:"final_message,omitempty"`
	ApprovalState ApprovalState    `json:"approval_state,omitempty"`
	Approval      *ApprovalRequest `json:"approval,omitempty"`
	Steerable     bool             `json:"steerable,omitempty"`
	Interruptible bool             `json:"interruptible,omitempty"`
	Error         *Failure         `json:"error,omitempty"`
}

type ProbeRequest = TurnRequest

type ProbeResult = TurnResult
