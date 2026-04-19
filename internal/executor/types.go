package executor

import "time"

type Transport string

const (
	TransportAppServer Transport = "codex_app_server"
	TransportExecJSON  Transport = "codex_exec_json"
)

type TurnStatus string

const (
	TurnStatusPending     TurnStatus = "pending"
	TurnStatusInProgress  TurnStatus = "in_progress"
	TurnStatusCompleted   TurnStatus = "completed"
	TurnStatusFailed      TurnStatus = "failed"
	TurnStatusInterrupted TurnStatus = "interrupted"
)

type TurnRequest struct {
	RunID      string
	RepoPath   string
	Prompt     string
	ThreadID   string
	ThreadPath string
}

type Failure struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type TurnResult struct {
	Transport     Transport  `json:"transport"`
	RunID         string     `json:"run_id"`
	ThreadID      string     `json:"thread_id,omitempty"`
	ThreadPath    string     `json:"thread_path,omitempty"`
	ResumedThread bool       `json:"resumed_thread"`
	TurnID        string     `json:"turn_id,omitempty"`
	TurnStatus    TurnStatus `json:"turn_status,omitempty"`
	Model         string     `json:"model,omitempty"`
	ModelProvider string     `json:"model_provider,omitempty"`
	EventsSeen    int        `json:"events_seen"`
	StartedAt     time.Time  `json:"started_at,omitempty"`
	CompletedAt   time.Time  `json:"completed_at,omitempty"`
	FinalMessage  string     `json:"final_message,omitempty"`
	Error         *Failure   `json:"error,omitempty"`
}

type ProbeRequest = TurnRequest

type ProbeResult = TurnResult
