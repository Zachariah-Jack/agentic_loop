package appserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"orchestrator/internal/executor"
)

func TestDeriveCodexJSPath(t *testing.T) {
	t.Parallel()

	codexPath := filepath.FromSlash("C:/Users/me/AppData/Roaming/npm/codex.cmd")
	if runtime.GOOS != "windows" {
		codexPath = filepath.FromSlash("/Users/me/.npm-global/bin/codex")
	}

	got := deriveCodexJSPath(codexPath)
	want := filepath.Join(filepath.Dir(codexPath), "node_modules", "@openai", "codex", "bin", "codex.js")
	if got != want {
		t.Fatalf("deriveCodexJSPath() = %q, want %q", got, want)
	}
}

func TestBuildThreadStartParams(t *testing.T) {
	t.Parallel()

	params := buildThreadStartParams(`D:\repo`, "read-only", probeInstructions, "never")
	if params["cwd"] != `D:\repo` {
		t.Fatalf("cwd = %v, want repo path", params["cwd"])
	}
	if params["approvalPolicy"] != "never" {
		t.Fatalf("approvalPolicy = %v, want never", params["approvalPolicy"])
	}
	if params["sandbox"] != "read-only" {
		t.Fatalf("sandbox = %v, want read-only", params["sandbox"])
	}

	instructions, _ := params["developerInstructions"].(string)
	if !strings.Contains(strings.ToLower(instructions), "read-only") {
		t.Fatalf("developerInstructions should mention read-only behavior, got %q", instructions)
	}
}

func TestBuildThreadStartParamsWorkspaceWrite(t *testing.T) {
	t.Parallel()

	params := buildThreadStartParams(`D:\repo`, "workspace-write", executeInstructions, "on-request")
	if params["sandbox"] != "workspace-write" {
		t.Fatalf("sandbox = %v, want workspace-write", params["sandbox"])
	}

	instructions, _ := params["developerInstructions"].(string)
	if !strings.Contains(strings.ToLower(instructions), "primary executor") {
		t.Fatalf("developerInstructions should mention primary executor behavior, got %q", instructions)
	}
}

func TestBuildTurnStartParams(t *testing.T) {
	t.Parallel()

	params := buildTurnStartParams("thr_123", `D:\repo`, "Reply with ok.", readOnlySandboxPolicy(), "never")
	if params["threadId"] != "thr_123" {
		t.Fatalf("threadId = %v, want thr_123", params["threadId"])
	}

	input, ok := params["input"].([]map[string]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v, want one text input item", params["input"])
	}
	if input[0]["text"] != "Reply with ok." {
		t.Fatalf("input text = %v, want prompt text", input[0]["text"])
	}

	sandbox, ok := params["sandboxPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("sandboxPolicy = %#v, want map", params["sandboxPolicy"])
	}
	if sandbox["type"] != "readOnly" {
		t.Fatalf("sandboxPolicy.type = %v, want readOnly", sandbox["type"])
	}
}

func TestBuildTurnStartParamsWorkspaceWrite(t *testing.T) {
	t.Parallel()

	params := buildTurnStartParams("thr_123", `D:\repo`, "Implement the bounded slice.", workspaceWriteSandboxPolicy(), "on-request")
	sandbox, ok := params["sandboxPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("sandboxPolicy = %#v, want map", params["sandboxPolicy"])
	}
	if sandbox["type"] != "workspaceWrite" {
		t.Fatalf("sandboxPolicy.type = %v, want workspaceWrite", sandbox["type"])
	}
	if sandbox["networkAccess"] != false {
		t.Fatalf("sandboxPolicy.networkAccess = %v, want false", sandbox["networkAccess"])
	}
}

func TestProbeAccumulatorObserve(t *testing.T) {
	t.Parallel()

	acc := &probeAccumulator{
		result: executor.ProbeResult{
			Transport: executor.TransportAppServer,
			RunID:     "run_123",
		},
	}

	messages := []string{
		`{"method":"thread/started","params":{"thread":{"id":"thr_123","path":"C:\\Users\\me\\.codex\\sessions\\thr_123.jsonl"}}}`,
		`{"method":"turn/started","params":{"threadId":"thr_123","turn":{"id":"turn_123","status":"inProgress","error":null}}}`,
		`{"method":"item/agentMessage/delta","params":{"threadId":"thr_123","turnId":"turn_123","itemId":"msg_1","delta":"ok"}}`,
		`{"method":"item/completed","params":{"threadId":"thr_123","turnId":"turn_123","item":{"id":"msg_1","type":"agentMessage","text":"ok","phase":"final_answer"}}}`,
		`{"method":"turn/completed","params":{"threadId":"thr_123","turn":{"id":"turn_123","status":"completed","error":null}}}`,
	}

	for _, raw := range messages {
		var msg wireMessage
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if err := acc.observe(msg); err != nil {
			t.Fatalf("observe() error = %v", err)
		}
	}

	if acc.result.ThreadID != "thr_123" {
		t.Fatalf("ThreadID = %q, want thr_123", acc.result.ThreadID)
	}
	if acc.result.ThreadPath != `C:\Users\me\.codex\sessions\thr_123.jsonl` {
		t.Fatalf("ThreadPath = %q", acc.result.ThreadPath)
	}
	if acc.result.TurnID != "turn_123" {
		t.Fatalf("TurnID = %q, want turn_123", acc.result.TurnID)
	}
	if acc.result.TurnStatus != executor.TurnStatusCompleted {
		t.Fatalf("TurnStatus = %q, want completed", acc.result.TurnStatus)
	}
	if acc.result.FinalMessage != "ok" {
		t.Fatalf("FinalMessage = %q, want ok", acc.result.FinalMessage)
	}
	if acc.result.EventsSeen != len(messages) {
		t.Fatalf("EventsSeen = %d, want %d", acc.result.EventsSeen, len(messages))
	}
	if acc.result.CompletedAt.IsZero() {
		t.Fatal("CompletedAt should be set")
	}
}

func TestProbeAccumulatorHandleServerRequestApprovalRequired(t *testing.T) {
	t.Parallel()

	acc := &probeAccumulator{
		result: executor.ProbeResult{
			Transport: executor.TransportAppServer,
			RunID:     "run_123",
		},
	}

	var msg wireMessage
	raw := `{
		"id":"approval_req_1",
		"method":"item/commandExecution/requestApproval",
		"params":{
			"threadId":"thr_123",
			"turnId":"turn_123",
			"itemId":"item_123",
			"approvalId":"approval_123",
			"command":"go test ./...",
			"cwd":"D:\\repo",
			"reason":"Run the requested test command."
		}
	}`
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if err := acc.handleServerRequest(msg); err != nil {
		t.Fatalf("handleServerRequest() error = %v", err)
	}

	if acc.result.ThreadID != "thr_123" {
		t.Fatalf("ThreadID = %q, want thr_123", acc.result.ThreadID)
	}
	if acc.result.TurnID != "turn_123" {
		t.Fatalf("TurnID = %q, want turn_123", acc.result.TurnID)
	}
	if acc.result.TurnStatus != executor.TurnStatusApprovalRequired {
		t.Fatalf("TurnStatus = %q, want approval_required", acc.result.TurnStatus)
	}
	if acc.result.ApprovalState != executor.ApprovalStateRequired {
		t.Fatalf("ApprovalState = %q, want required", acc.result.ApprovalState)
	}
	if !acc.result.Interruptible {
		t.Fatal("Interruptible = false, want true")
	}
	if acc.result.Steerable {
		t.Fatal("Steerable = true, want false")
	}
	if acc.result.Approval == nil {
		t.Fatal("Approval = nil, want structured approval request")
	}
	if acc.result.Approval.Kind != executor.ApprovalKindCommandExecution {
		t.Fatalf("Approval.Kind = %q, want command_execution", acc.result.Approval.Kind)
	}
	if acc.result.Approval.RequestID != "approval_req_1" {
		t.Fatalf("Approval.RequestID = %q, want approval_req_1", acc.result.Approval.RequestID)
	}
	if acc.result.Approval.ApprovalID != "approval_123" {
		t.Fatalf("Approval.ApprovalID = %q, want approval_123", acc.result.Approval.ApprovalID)
	}
	if acc.result.Approval.Command != "go test ./..." {
		t.Fatalf("Approval.Command = %q, want go test ./...", acc.result.Approval.Command)
	}
	if !strings.Contains(acc.result.Approval.RawParams, `"approvalId":"approval_123"`) {
		t.Fatalf("Approval.RawParams = %q, want raw approval payload", acc.result.Approval.RawParams)
	}
}

func TestTurnTimeoutUsesExecuteDefault(t *testing.T) {
	t.Parallel()

	client := Client{}
	timeout := client.turnTimeout(turnMode{waitTimeout: defaultExecuteTimeout})
	if timeout != defaultExecuteTimeout {
		t.Fatalf("turnTimeout() = %s, want %s", timeout, defaultExecuteTimeout)
	}
}

func TestFailWaitErrorDeadlineExceededMarksTurnFailed(t *testing.T) {
	t.Parallel()

	acc := &probeAccumulator{
		result: executor.ProbeResult{
			Transport:  executor.TransportAppServer,
			RunID:      "run_123",
			TurnID:     "turn_123",
			TurnStatus: executor.TurnStatusInProgress,
		},
	}

	result, err := acc.failWaitError("turn_stream", context.DeadlineExceeded, "", defaultExecuteTimeout)
	if err == nil {
		t.Fatal("failWaitError() error = nil, want error")
	}
	if result.TurnStatus != executor.TurnStatusFailed {
		t.Fatalf("TurnStatus = %q, want failed", result.TurnStatus)
	}
	if result.Error == nil {
		t.Fatal("Error = nil, want structured failure")
	}
	if result.Error.Stage != "turn_timeout" {
		t.Fatalf("Error.Stage = %q, want turn_timeout", result.Error.Stage)
	}
	if !strings.Contains(result.Error.Message, "executor turn exceeded app-server wait deadline") {
		t.Fatalf("Error.Message = %q", result.Error.Message)
	}
	if !strings.Contains(result.Error.Detail, "context deadline exceeded") {
		t.Fatalf("Error.Detail = %q", result.Error.Detail)
	}
}
