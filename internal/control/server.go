package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"orchestrator/internal/activity"
	"orchestrator/internal/runtimecfg"
)

type RequestEnvelope struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type ResponseEnvelope struct {
	ID      string         `json:"id,omitempty"`
	Type    string         `json:"type"`
	OK      bool           `json:"ok"`
	Payload any            `json:"payload,omitempty"`
	Error   *ErrorEnvelope `json:"error,omitempty"`
}

type ErrorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type StatusSnapshotRequest struct {
	RunID string `json:"run_id,omitempty"`
}

type SetVerbosityRequest struct {
	Scope     string `json:"scope,omitempty"`
	Verbosity string `json:"verbosity"`
}

type StopFlagRequest struct {
	RunID  string `json:"run_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type StartRunRequest struct {
	RepoPath  string `json:"repo_path,omitempty"`
	Goal      string `json:"goal"`
	Mode      string `json:"mode,omitempty"`
	Verbosity string `json:"verbosity,omitempty"`
}

type ContinueRunRequest struct {
	RepoPath string `json:"repo_path,omitempty"`
	RunID    string `json:"run_id,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

type ModelTestRequest struct {
	Model string `json:"model,omitempty"`
}

type PendingActionRequest struct {
	RunID string `json:"run_id,omitempty"`
}

type ExecutorApprovalActionRequest struct {
	RunID string `json:"run_id,omitempty"`
}

type ArtifactRequest struct {
	ArtifactPath string `json:"artifact_path"`
}

type ListArtifactsRequest struct {
	RunID    string `json:"run_id,omitempty"`
	Category string `json:"category,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type ContractFileRequest struct {
	RepoPath string `json:"repo_path,omitempty"`
	Path     string `json:"path"`
}

type ListContractFilesRequest struct {
	RepoPath string `json:"repo_path,omitempty"`
}

type SaveContractFileRequest struct {
	RepoPath      string `json:"repo_path,omitempty"`
	Path          string `json:"path"`
	Content       string `json:"content"`
	ExpectedMTime string `json:"expected_mtime,omitempty"`
}

type RunAIAutofillRequest struct {
	RepoPath string         `json:"repo_path,omitempty"`
	Targets  []string       `json:"targets"`
	Answers  map[string]any `json:"answers"`
}

type RepoTreeRequest struct {
	RepoPath string `json:"repo_path,omitempty"`
	Path     string `json:"path,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type RepoFileRequest struct {
	RepoPath string `json:"repo_path,omitempty"`
	Path     string `json:"path"`
}

type InjectControlMessageRequest struct {
	RunID   string `json:"run_id,omitempty"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type ListControlMessagesRequest struct {
	RunID  string `json:"run_id,omitempty"`
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ListSideChatMessagesRequest struct {
	RepoPath string `json:"repo_path,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type SideChatRequest struct {
	RepoPath      string `json:"repo_path,omitempty"`
	Message       string `json:"message"`
	ContextPolicy string `json:"context_policy,omitempty"`
}

type CaptureDogfoodIssueRequest struct {
	RepoPath string `json:"repo_path,omitempty"`
	Title    string `json:"title"`
	Note     string `json:"note"`
	Source   string `json:"source,omitempty"`
	RunID    string `json:"run_id,omitempty"`
}

type ListDogfoodIssuesRequest struct {
	RepoPath string `json:"repo_path,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type ListWorkersRequest struct {
	RunID string `json:"run_id,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type CreateWorkerRequest struct {
	RunID string `json:"run_id,omitempty"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type DispatchWorkerRequest struct {
	WorkerID string `json:"worker_id"`
	Prompt   string `json:"prompt"`
}

type RemoveWorkerRequest struct {
	WorkerID string `json:"worker_id"`
}

type IntegrateWorkersRequest struct {
	WorkerIDs []string `json:"worker_ids"`
}

type ActionSet struct {
	StartRun             func(context.Context, StartRunRequest) (any, error)
	ContinueRun          func(context.Context, ContinueRunRequest) (any, error)
	TestPlannerModel     func(context.Context, ModelTestRequest) (any, error)
	TestExecutorModel    func(context.Context, ModelTestRequest) (any, error)
	GetStatusSnapshot    func(context.Context, string) (any, error)
	SetVerbosity         func(context.Context, string) (any, error)
	SetStopFlag          func(context.Context, string, string) (any, error)
	ClearStopFlag        func(context.Context, string) (any, error)
	GetPendingAction     func(context.Context, string) (any, error)
	ApproveExecutor      func(context.Context, ExecutorApprovalActionRequest) (any, error)
	DenyExecutor         func(context.Context, ExecutorApprovalActionRequest) (any, error)
	GetArtifact          func(context.Context, ArtifactRequest) (any, error)
	ListRecentArtifacts  func(context.Context, ListArtifactsRequest) (any, error)
	ListContractFiles    func(context.Context, ListContractFilesRequest) (any, error)
	OpenContractFile     func(context.Context, ContractFileRequest) (any, error)
	SaveContractFile     func(context.Context, SaveContractFileRequest) (any, error)
	RunAIAutofill        func(context.Context, RunAIAutofillRequest) (any, error)
	ListRepoTree         func(context.Context, RepoTreeRequest) (any, error)
	OpenRepoFile         func(context.Context, RepoFileRequest) (any, error)
	InjectControlMessage func(context.Context, InjectControlMessageRequest) (any, error)
	ListControlMessages  func(context.Context, ListControlMessagesRequest) (any, error)
	SendSideChatMessage  func(context.Context, SideChatRequest) (any, error)
	ListSideChatMessages func(context.Context, ListSideChatMessagesRequest) (any, error)
	CaptureDogfoodIssue  func(context.Context, CaptureDogfoodIssueRequest) (any, error)
	ListDogfoodIssues    func(context.Context, ListDogfoodIssuesRequest) (any, error)
	ListWorkers          func(context.Context, ListWorkersRequest) (any, error)
	CreateWorker         func(context.Context, CreateWorkerRequest) (any, error)
	DispatchWorker       func(context.Context, DispatchWorkerRequest) (any, error)
	RemoveWorker         func(context.Context, RemoveWorkerRequest) (any, error)
	IntegrateWorkers     func(context.Context, IntegrateWorkersRequest) (any, error)
	GetRuntimeConfig     func(context.Context) (any, error)
	SetRuntimeConfig     func(context.Context, runtimecfg.Patch) (any, error)
}

type Server struct {
	Broker  *activity.Broker
	Actions ActionSet
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/control", s.handleControl)
	mux.HandleFunc("/v2/events", s.handleEvents)
	return mux
}

func (s Server) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ResponseEnvelope{
			Type:  "response",
			OK:    false,
			Error: &ErrorEnvelope{Code: "method_not_allowed", Message: "use POST /v2/control"},
		})
		return
	}

	var req RequestEnvelope
	if err := decodeStrictJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ResponseEnvelope{
			Type:  "response",
			OK:    false,
			Error: &ErrorEnvelope{Code: "invalid_request", Message: err.Error()},
		})
		return
	}

	if strings.TrimSpace(req.Type) == "" {
		req.Type = "request"
	}
	if req.Type != "request" {
		writeJSON(w, http.StatusBadRequest, ResponseEnvelope{
			ID:    req.ID,
			Type:  "response",
			OK:    false,
			Error: &ErrorEnvelope{Code: "invalid_request_type", Message: `request.type must be "request"`},
		})
		return
	}

	response := s.dispatch(r.Context(), req)
	statusCode := http.StatusOK
	if !response.OK {
		statusCode = http.StatusBadRequest
	}
	writeJSON(w, statusCode, response)
}

func (s Server) dispatch(ctx context.Context, req RequestEnvelope) ResponseEnvelope {
	switch strings.TrimSpace(req.Action) {
	case "start_run":
		handler := s.Actions.StartRun
		if handler == nil {
			return unsupportedAction(req, "start_run")
		}
		var payload StartRunRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "start_run_failed", err)
		}
		return okResponse(req, response)
	case "continue_run":
		handler := s.Actions.ContinueRun
		if handler == nil {
			return unsupportedAction(req, "continue_run")
		}
		var payload ContinueRunRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "continue_run_failed", err)
		}
		return okResponse(req, response)
	case "test_planner_model":
		handler := s.Actions.TestPlannerModel
		if handler == nil {
			return unsupportedAction(req, "test_planner_model")
		}
		var payload ModelTestRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "test_planner_model_failed", err)
		}
		s.publish("model_health_tested", map[string]any{
			"component": "planner",
			"model":     strings.TrimSpace(payload.Model),
		})
		return okResponse(req, response)
	case "test_executor_model", "test_codex_config":
		handler := s.Actions.TestExecutorModel
		if handler == nil {
			return unsupportedAction(req, "test_executor_model")
		}
		var payload ModelTestRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "test_executor_model_failed", err)
		}
		s.publish("model_health_tested", map[string]any{
			"component": "executor",
			"model":     strings.TrimSpace(payload.Model),
		})
		return okResponse(req, response)
	case "get_status_snapshot":
		handler := s.Actions.GetStatusSnapshot
		if handler == nil {
			return unsupportedAction(req, "get_status_snapshot")
		}
		var payload StatusSnapshotRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		snapshot, err := handler(ctx, payload.RunID)
		if err != nil {
			return actionError(req, "status_snapshot_failed", err)
		}
		s.publish("status_snapshot_emitted", map[string]any{"run_id": strings.TrimSpace(payload.RunID)})
		return okResponse(req, snapshot)
	case "set_verbosity":
		handler := s.Actions.SetVerbosity
		if handler == nil {
			return unsupportedAction(req, "set_verbosity")
		}
		var payload SetVerbosityRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		updated, err := handler(ctx, payload.Verbosity)
		if err != nil {
			return actionError(req, "set_verbosity_failed", err)
		}
		s.publish("verbosity_changed", map[string]any{
			"verbosity":       strings.TrimSpace(payload.Verbosity),
			"applies_at":      "next_safe_point",
			"requested_scope": strings.TrimSpace(payload.Scope),
		})
		return okResponse(req, updated)
	case "set_stop_flag", "stop_safe":
		handler := s.Actions.SetStopFlag
		if handler == nil {
			return unsupportedAction(req, "stop_safe")
		}
		var payload StopFlagRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		state, err := handler(ctx, payload.RunID, payload.Reason)
		if err != nil {
			return actionError(req, "stop_safe_failed", err)
		}
		return okResponse(req, state)
	case "clear_stop_flag":
		handler := s.Actions.ClearStopFlag
		if handler == nil {
			return unsupportedAction(req, "clear_stop_flag")
		}
		var payload StopFlagRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		state, err := handler(ctx, payload.RunID)
		if err != nil {
			return actionError(req, "clear_stop_flag_failed", err)
		}
		return okResponse(req, state)
	case "get_pending_action":
		handler := s.Actions.GetPendingAction
		if handler == nil {
			return unsupportedAction(req, "get_pending_action")
		}
		var payload PendingActionRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		pending, err := handler(ctx, payload.RunID)
		if err != nil {
			return actionError(req, "get_pending_action_failed", err)
		}
		return okResponse(req, pending)
	case "approve_executor":
		handler := s.Actions.ApproveExecutor
		if handler == nil {
			return unsupportedAction(req, "approve_executor")
		}
		var payload ExecutorApprovalActionRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "approve_executor_failed", err)
		}
		return okResponse(req, response)
	case "deny_executor":
		handler := s.Actions.DenyExecutor
		if handler == nil {
			return unsupportedAction(req, "deny_executor")
		}
		var payload ExecutorApprovalActionRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "deny_executor_failed", err)
		}
		return okResponse(req, response)
	case "get_artifact":
		handler := s.Actions.GetArtifact
		if handler == nil {
			return unsupportedAction(req, "get_artifact")
		}
		var payload ArtifactRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		artifact, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "get_artifact_failed", err)
		}
		return okResponse(req, artifact)
	case "list_recent_artifacts":
		handler := s.Actions.ListRecentArtifacts
		if handler == nil {
			return unsupportedAction(req, "list_recent_artifacts")
		}
		var payload ListArtifactsRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		artifacts, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "list_recent_artifacts_failed", err)
		}
		return okResponse(req, artifacts)
	case "list_contract_files":
		handler := s.Actions.ListContractFiles
		if handler == nil {
			return unsupportedAction(req, "list_contract_files")
		}
		var payload ListContractFilesRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		files, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "list_contract_files_failed", err)
		}
		return okResponse(req, files)
	case "open_contract_file":
		handler := s.Actions.OpenContractFile
		if handler == nil {
			return unsupportedAction(req, "open_contract_file")
		}
		var payload ContractFileRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		file, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "open_contract_file_failed", err)
		}
		return okResponse(req, file)
	case "save_contract_file":
		handler := s.Actions.SaveContractFile
		if handler == nil {
			return unsupportedAction(req, "save_contract_file")
		}
		var payload SaveContractFileRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		file, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "save_contract_file_failed", err)
		}
		return okResponse(req, file)
	case "run_ai_autofill":
		handler := s.Actions.RunAIAutofill
		if handler == nil {
			return unsupportedAction(req, "run_ai_autofill")
		}
		var payload RunAIAutofillRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "run_ai_autofill_failed", err)
		}
		return okResponse(req, response)
	case "list_repo_tree":
		handler := s.Actions.ListRepoTree
		if handler == nil {
			return unsupportedAction(req, "list_repo_tree")
		}
		var payload RepoTreeRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "list_repo_tree_failed", err)
		}
		return okResponse(req, response)
	case "open_repo_file":
		handler := s.Actions.OpenRepoFile
		if handler == nil {
			return unsupportedAction(req, "open_repo_file")
		}
		var payload RepoFileRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "open_repo_file_failed", err)
		}
		return okResponse(req, response)
	case "inject_control_message":
		handler := s.Actions.InjectControlMessage
		if handler == nil {
			return unsupportedAction(req, "inject_control_message")
		}
		var payload InjectControlMessageRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		queued, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "inject_control_message_failed", err)
		}
		return okResponse(req, queued)
	case "list_control_messages":
		handler := s.Actions.ListControlMessages
		if handler == nil {
			return unsupportedAction(req, "list_control_messages")
		}
		var payload ListControlMessagesRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		messages, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "list_control_messages_failed", err)
		}
		return okResponse(req, messages)
	case "send_side_chat_message":
		handler := s.Actions.SendSideChatMessage
		if handler == nil {
			return unsupportedAction(req, "send_side_chat_message")
		}
		var payload SideChatRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "send_side_chat_message_failed", err)
		}
		return okResponse(req, response)
	case "list_side_chat_messages":
		handler := s.Actions.ListSideChatMessages
		if handler == nil {
			return unsupportedAction(req, "list_side_chat_messages")
		}
		var payload ListSideChatMessagesRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "list_side_chat_messages_failed", err)
		}
		return okResponse(req, response)
	case "capture_dogfood_issue":
		handler := s.Actions.CaptureDogfoodIssue
		if handler == nil {
			return unsupportedAction(req, "capture_dogfood_issue")
		}
		var payload CaptureDogfoodIssueRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "capture_dogfood_issue_failed", err)
		}
		return okResponse(req, response)
	case "list_dogfood_issues":
		handler := s.Actions.ListDogfoodIssues
		if handler == nil {
			return unsupportedAction(req, "list_dogfood_issues")
		}
		var payload ListDogfoodIssuesRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "list_dogfood_issues_failed", err)
		}
		return okResponse(req, response)
	case "list_workers":
		handler := s.Actions.ListWorkers
		if handler == nil {
			return unsupportedAction(req, "list_workers")
		}
		var payload ListWorkersRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "list_workers_failed", err)
		}
		return okResponse(req, response)
	case "create_worker":
		handler := s.Actions.CreateWorker
		if handler == nil {
			return unsupportedAction(req, "create_worker")
		}
		var payload CreateWorkerRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "create_worker_failed", err)
		}
		return okResponse(req, response)
	case "dispatch_worker":
		handler := s.Actions.DispatchWorker
		if handler == nil {
			return unsupportedAction(req, "dispatch_worker")
		}
		var payload DispatchWorkerRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "dispatch_worker_failed", err)
		}
		return okResponse(req, response)
	case "remove_worker":
		handler := s.Actions.RemoveWorker
		if handler == nil {
			return unsupportedAction(req, "remove_worker")
		}
		var payload RemoveWorkerRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "remove_worker_failed", err)
		}
		return okResponse(req, response)
	case "integrate_workers":
		handler := s.Actions.IntegrateWorkers
		if handler == nil {
			return unsupportedAction(req, "integrate_workers")
		}
		var payload IntegrateWorkersRequest
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		response, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "integrate_workers_failed", err)
		}
		return okResponse(req, response)
	case "get_runtime_config":
		handler := s.Actions.GetRuntimeConfig
		if handler == nil {
			return unsupportedAction(req, "get_runtime_config")
		}
		cfg, err := handler(ctx)
		if err != nil {
			return actionError(req, "get_runtime_config_failed", err)
		}
		return okResponse(req, cfg)
	case "set_runtime_config":
		handler := s.Actions.SetRuntimeConfig
		if handler == nil {
			return unsupportedAction(req, "set_runtime_config")
		}
		var payload runtimecfg.Patch
		if err := decodePayload(req.Payload, &payload); err != nil {
			return invalidPayload(req, err)
		}
		cfg, err := handler(ctx, payload)
		if err != nil {
			return actionError(req, "set_runtime_config_failed", err)
		}
		if payload.Verbosity != nil {
			s.publish("verbosity_changed", map[string]any{
				"verbosity":  strings.TrimSpace(*payload.Verbosity),
				"applies_at": "next_safe_point",
			})
		}
		return okResponse(req, cfg)
	default:
		return unsupportedAction(req, strings.TrimSpace(req.Action))
	}
}

func (s Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "use GET /v2/events", http.StatusMethodNotAllowed)
		return
	}
	if s.Broker == nil {
		http.Error(w, "event stream unavailable", http.StatusServiceUnavailable)
		return
	}

	fromSequence, err := parseFromSequence(r.URL.Query().Get("from_sequence"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filterRunID := strings.TrimSpace(r.URL.Query().Get("run_id"))

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	stream, cancel := s.Broker.Subscribe(fromSequence)
	defer cancel()

	encoder := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-stream:
			if !ok {
				return
			}
			if !eventMatchesRun(event, filterRunID) {
				continue
			}
			if err := encoder.Encode(event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func parseFromSequence(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0, errors.New("from_sequence must be a non-negative integer")
	}
	return value, nil
}

func eventMatchesRun(event activity.Event, runID string) bool {
	if strings.TrimSpace(runID) == "" {
		return true
	}
	if event.Payload == nil {
		return false
	}
	value, ok := event.Payload["run_id"]
	if !ok {
		return false
	}
	text, _ := value.(string)
	return strings.TrimSpace(text) == strings.TrimSpace(runID)
}

func decodeStrictJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func decodePayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload ResponseEnvelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func okResponse(req RequestEnvelope, payload any) ResponseEnvelope {
	return ResponseEnvelope{
		ID:      req.ID,
		Type:    "response",
		OK:      true,
		Payload: payload,
	}
}

func invalidPayload(req RequestEnvelope, err error) ResponseEnvelope {
	return ResponseEnvelope{
		ID:    req.ID,
		Type:  "response",
		OK:    false,
		Error: &ErrorEnvelope{Code: "invalid_payload", Message: err.Error()},
	}
}

func actionError(req RequestEnvelope, code string, err error) ResponseEnvelope {
	return ResponseEnvelope{
		ID:    req.ID,
		Type:  "response",
		OK:    false,
		Error: &ErrorEnvelope{Code: code, Message: err.Error()},
	}
}

func unsupportedAction(req RequestEnvelope, action string) ResponseEnvelope {
	return ResponseEnvelope{
		ID:   req.ID,
		Type: "response",
		OK:   false,
		Error: &ErrorEnvelope{
			Code:    "unsupported_action",
			Message: fmt.Sprintf("%s is not implemented in this slice", action),
		},
	}
}

func (s Server) publish(event string, payload map[string]any) {
	if s.Broker == nil {
		return
	}
	s.Broker.Publish(event, payload)
}
