package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"orchestrator/internal/executor"
)

const (
	initializeRequestID = "init"
	threadRequestID     = "thread"
	turnRequestID       = "turn"

	defaultClientName  = "orchestrator"
	defaultClientTitle = "Orchestrator CLI"

	defaultProbeTimeout   = 2 * time.Minute
	defaultExecuteTimeout = 20 * time.Minute
	probeInstructions     = "You are an executor probe. Operate read-only. Do not modify files, apply patches, create commits, or run mutating commands. Inspect and report only."
	executeInstructions   = "You are the primary executor for the orchestrator. Perform only the bounded task provided in the current turn. Do not choose a new task and do not decide whether the overall run is complete."
)

type LaunchPlan struct {
	Command string
	Args    []string
}

type Client struct {
	LaunchPlan    LaunchPlan
	Timeout       time.Duration
	ClientName    string
	ClientTitle   string
	ClientVersion string
}

type wireMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Params json.RawMessage `json:"params"`
	Error  *wireError      `json:"error"`
}

type streamEvent struct {
	Message wireMessage
	Err     error
}

type wireError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

type initResponse struct {
	UserAgent      string `json:"userAgent"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type threadStartResponse struct {
	Thread        threadRef `json:"thread"`
	Model         string    `json:"model"`
	ModelProvider string    `json:"modelProvider"`
}

type threadResumeResponse struct {
	Thread        threadState `json:"thread"`
	Model         string      `json:"model"`
	ModelProvider string      `json:"modelProvider"`
}

type threadRef struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type threadState struct {
	ID     string       `json:"id"`
	Path   string       `json:"path"`
	Status threadStatus `json:"status"`
	Turns  []turnRef    `json:"turns"`
}

type threadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

type turnStartResponse struct {
	Turn turnRef `json:"turn"`
}

type turnCompletedParams struct {
	ThreadID string  `json:"threadId"`
	Turn     turnRef `json:"turn"`
}

type turnStartedParams struct {
	ThreadID string  `json:"threadId"`
	Turn     turnRef `json:"turn"`
}

type turnRef struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Error  *turnError `json:"error"`
}

type turnError struct {
	Message           string `json:"message"`
	AdditionalDetails string `json:"additionalDetails"`
}

type threadStartedParams struct {
	Thread threadRef `json:"thread"`
}

type agentMessageDeltaParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type itemCompletedParams struct {
	ThreadID string        `json:"threadId"`
	TurnID   string        `json:"turnId"`
	Item     completedItem `json:"item"`
}

type completedItem struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Text  string `json:"text"`
	Phase string `json:"phase"`
}

type errorNotificationParams struct {
	ThreadID  string    `json:"threadId"`
	TurnID    string    `json:"turnId"`
	Error     turnError `json:"error"`
	WillRetry bool      `json:"willRetry"`
}

type threadStatusChangedParams struct {
	ThreadID string       `json:"threadId"`
	Status   threadStatus `json:"status"`
}

type commandExecutionApprovalParams struct {
	ThreadID   string `json:"threadId"`
	TurnID     string `json:"turnId"`
	ItemID     string `json:"itemId"`
	ApprovalID string `json:"approvalId"`
	Command    string `json:"command"`
	CWD        string `json:"cwd"`
	Reason     string `json:"reason"`
}

type fileChangeApprovalParams struct {
	ThreadID  string `json:"threadId"`
	TurnID    string `json:"turnId"`
	ItemID    string `json:"itemId"`
	GrantRoot string `json:"grantRoot"`
	Reason    string `json:"reason"`
}

type permissionsApprovalParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Reason   string `json:"reason"`
}

type probeAccumulator struct {
	result    executor.ProbeResult
	deltaText strings.Builder
}

type turnMode struct {
	threadSandbox      string
	threadInstructions string
	turnSandboxPolicy  map[string]any
	approvalPolicy     string
	waitTimeout        time.Duration
}

func NewClient(version string) (Client, error) {
	plan, err := ResolveLaunchPlan()
	if err != nil {
		return Client{}, err
	}

	return Client{
		LaunchPlan:    plan,
		Timeout:       0,
		ClientName:    defaultClientName,
		ClientTitle:   defaultClientTitle,
		ClientVersion: version,
	}, nil
}

func ResolveLaunchPlan() (LaunchPlan, error) {
	if runtime.GOOS == "windows" {
		codexPath, err := exec.LookPath("codex")
		if err != nil {
			return LaunchPlan{}, fmt.Errorf("codex not found on PATH: %w", err)
		}

		nodePath, err := exec.LookPath("node")
		if err != nil {
			return LaunchPlan{}, fmt.Errorf("node not found on PATH: %w", err)
		}

		codexJS := deriveCodexJSPath(codexPath)
		if _, err := os.Stat(codexJS); err != nil {
			return LaunchPlan{}, fmt.Errorf("codex app-server entrypoint not found at %s: %w", codexJS, err)
		}

		return LaunchPlan{
			Command: nodePath,
			Args:    []string{codexJS, "app-server", "--listen", "stdio://"},
		}, nil
	}

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return LaunchPlan{}, fmt.Errorf("codex not found on PATH: %w", err)
	}

	return LaunchPlan{
		Command: codexPath,
		Args:    []string{"app-server", "--listen", "stdio://"},
	}, nil
}

func deriveCodexJSPath(codexPath string) string {
	return filepath.Join(filepath.Dir(codexPath), "node_modules", "@openai", "codex", "bin", "codex.js")
}

func (c Client) Probe(ctx context.Context, req executor.ProbeRequest) (executor.ProbeResult, error) {
	return c.runTurn(ctx, req, turnMode{
		threadSandbox:      "read-only",
		threadInstructions: probeInstructions,
		turnSandboxPolicy:  readOnlySandboxPolicy(),
		approvalPolicy:     "never",
		waitTimeout:        defaultProbeTimeout,
	})
}

func (c Client) Execute(ctx context.Context, req executor.TurnRequest) (executor.TurnResult, error) {
	return c.runTurn(ctx, req, turnMode{
		threadSandbox:      "workspace-write",
		threadInstructions: executeInstructions,
		turnSandboxPolicy:  workspaceWriteSandboxPolicy(),
		approvalPolicy:     "on-request",
		waitTimeout:        defaultExecuteTimeout,
	})
}

func (c Client) runTurn(ctx context.Context, req executor.TurnRequest, mode turnMode) (executor.TurnResult, error) {
	continueTurn := req.Continue && strings.TrimSpace(req.TurnID) != ""

	if strings.TrimSpace(req.RunID) == "" {
		return executor.TurnResult{}, errors.New("run id is required")
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return executor.TurnResult{}, errors.New("repo path is required")
	}
	if !continueTurn && strings.TrimSpace(req.Prompt) == "" {
		return executor.TurnResult{}, errors.New("prompt is required")
	}
	if strings.TrimSpace(c.LaunchPlan.Command) == "" {
		return executor.TurnResult{}, errors.New("app-server launch plan is required")
	}

	timeout := c.turnTimeout(mode)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.LaunchPlan.Command, c.LaunchPlan.Args...)
	cmd.Dir = req.RepoPath

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return executor.TurnResult{}, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return executor.TurnResult{}, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return executor.TurnResult{}, err
	}

	if err := cmd.Start(); err != nil {
		return executor.TurnResult{}, err
	}

	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	stream := streamMessages(stdout)
	stderrBuf := newLimitedBuffer(8192)
	go captureStderr(stderr, stderrBuf)

	acc := probeAccumulator{
		result: executor.ProbeResult{
			Transport:  executor.TransportAppServer,
			RunID:      req.RunID,
			ThreadID:   strings.TrimSpace(req.ThreadID),
			ThreadPath: strings.TrimSpace(req.ThreadPath),
			TurnID:     strings.TrimSpace(req.TurnID),
		},
	}

	sender := json.NewEncoder(stdin)
	sender.SetEscapeHTML(false)

	if err := sendMessage(sender, map[string]any{
		"method": "initialize",
		"id":     initializeRequestID,
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    c.clientName(),
				"title":   c.clientTitle(),
				"version": c.clientVersion(),
			},
		},
	}); err != nil {
		return acc.fail("initialize_send", err.Error(), stderrBuf.String())
	}

	initMsg, err := waitForResponse(ctx, stream, initializeRequestID, &acc, stderrBuf, nil)
	if err != nil {
		return acc.failWaitError("initialize_wait", err, stderrBuf.String(), timeout)
	}

	if err := parseResponse(initMsg, &initResponse{}); err != nil {
		return acc.fail("initialize_parse", err.Error(), stderrBuf.String())
	}

	if err := sendMessage(sender, map[string]any{
		"method": "initialized",
		"params": map[string]any{},
	}); err != nil {
		return acc.fail("initialized_send", err.Error(), stderrBuf.String())
	}

	threadMethod := "thread/start"
	threadParams := buildThreadStartParams(req.RepoPath, mode.threadSandbox, mode.threadInstructions, mode.approvalPolicy)
	if acc.result.ThreadID != "" {
		threadMethod = "thread/resume"
		threadParams = buildThreadResumeParams(acc.result.ThreadID, req.RepoPath, mode.threadSandbox, mode.threadInstructions, mode.approvalPolicy)
		acc.result.ResumedThread = true
	}

	if err := sendMessage(sender, map[string]any{
		"method": threadMethod,
		"id":     threadRequestID,
		"params": threadParams,
	}); err != nil {
		return acc.fail("thread_send", err.Error(), stderrBuf.String())
	}

	threadMsg, err := waitForResponse(ctx, stream, threadRequestID, &acc, stderrBuf, acc.handleServerRequest)
	if err != nil {
		return acc.failWaitError("thread_wait", err, stderrBuf.String(), timeout)
	}

	if threadMethod == "thread/start" {
		var response threadStartResponse
		if err := parseResponse(threadMsg, &response); err != nil {
			return acc.fail("thread_parse", err.Error(), stderrBuf.String())
		}
		acc.result.ThreadID = response.Thread.ID
		acc.result.ThreadPath = response.Thread.Path
		acc.result.Model = response.Model
		acc.result.ModelProvider = response.ModelProvider
	} else {
		var response threadResumeResponse
		if err := parseResponse(threadMsg, &response); err != nil {
			return acc.fail("thread_parse", err.Error(), stderrBuf.String())
		}
		acc.result.ThreadID = response.Thread.ID
		acc.result.ThreadPath = response.Thread.Path
		acc.result.Model = response.Model
		acc.result.ModelProvider = response.ModelProvider
		if continueTurn {
			acc.resumeExistingTurn(response.Thread, strings.TrimSpace(req.TurnID))
		}
	}

	if acc.result.ThreadID == "" {
		return acc.fail("thread_parse", "app-server did not return a thread id", stderrBuf.String())
	}
	if continueTurn && acc.result.TurnID == "" {
		acc.result.TurnID = strings.TrimSpace(req.TurnID)
	}
	if continueTurn && acc.result.TurnStatus == "" {
		acc.result.TurnStatus = executor.TurnStatusInProgress
	}
	if continueTurn {
		acc.result.Interruptible = true
		acc.result.Steerable = true
	}

	if !continueTurn {
		if err := sendMessage(sender, map[string]any{
			"method": "turn/start",
			"id":     turnRequestID,
			"params": buildTurnStartParams(acc.result.ThreadID, req.RepoPath, req.Prompt, mode.turnSandboxPolicy, mode.approvalPolicy),
		}); err != nil {
			return acc.fail("turn_send", err.Error(), stderrBuf.String())
		}

		turnMsg, err := waitForResponse(ctx, stream, turnRequestID, &acc, stderrBuf, acc.handleServerRequest)
		if err != nil {
			return acc.failWaitError("turn_wait", err, stderrBuf.String(), timeout)
		}

		var turnResponse turnStartResponse
		if err := parseResponse(turnMsg, &turnResponse); err != nil {
			return acc.fail("turn_parse", err.Error(), stderrBuf.String())
		}
		acc.result.TurnID = turnResponse.Turn.ID
		acc.result.TurnStatus = normalizeTurnStatus(turnResponse.Turn.Status)
		acc.result.Interruptible = true
		acc.result.Steerable = true
		if acc.result.StartedAt.IsZero() {
			acc.result.StartedAt = time.Now().UTC()
		}
	}

	for !acc.turnFinished() && !acc.approvalRequired() {
		msg, err := nextMessage(ctx, stream)
		if err != nil {
			return acc.failWaitError("turn_stream", err, stderrBuf.String(), timeout)
		}

		if msg.isServerRequest() {
			if err := acc.handleServerRequest(msg); err != nil {
				return acc.fail("turn_stream", err.Error(), stderrBuf.String())
			}
			continue
		}

		if err := acc.observe(msg); err != nil {
			return acc.fail("turn_stream", err.Error(), stderrBuf.String())
		}
	}

	if acc.approvalRequired() {
		if acc.result.FinalMessage == "" {
			acc.result.FinalMessage = strings.TrimSpace(acc.deltaText.String())
		}
		return acc.result, nil
	}

	if acc.result.FinalMessage == "" {
		acc.result.FinalMessage = strings.TrimSpace(acc.deltaText.String())
	}

	if acc.result.CompletedAt.IsZero() {
		acc.result.CompletedAt = time.Now().UTC()
	}

	switch acc.result.TurnStatus {
	case executor.TurnStatusCompleted:
		return acc.result, nil
	case executor.TurnStatusFailed, executor.TurnStatusInterrupted:
		if acc.result.Error == nil {
			acc.result.Error = &executor.Failure{
				Stage:   "turn_completed",
				Message: fmt.Sprintf("executor turn ended with status %s", acc.result.TurnStatus),
				Detail:  stderrBuf.String(),
			}
		}
		return acc.result, errors.New(acc.result.Error.Message)
	default:
		return acc.fail("turn_completed", fmt.Sprintf("unexpected executor turn status %s", acc.result.TurnStatus), stderrBuf.String())
	}
}

func (c Client) turnTimeout(mode turnMode) time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	if mode.waitTimeout > 0 {
		return mode.waitTimeout
	}
	return defaultProbeTimeout
}

func buildThreadStartParams(repoPath string, sandbox string, developerInstructions string, approvalPolicy string) map[string]any {
	return map[string]any{
		"cwd":                   repoPath,
		"approvalPolicy":        approvalPolicy,
		"sandbox":               sandbox,
		"developerInstructions": developerInstructions,
	}
}

func buildThreadResumeParams(threadID string, repoPath string, sandbox string, developerInstructions string, approvalPolicy string) map[string]any {
	return map[string]any{
		"threadId":              threadID,
		"cwd":                   repoPath,
		"approvalPolicy":        approvalPolicy,
		"sandbox":               sandbox,
		"developerInstructions": developerInstructions,
	}
}

func buildTurnStartParams(threadID string, repoPath string, prompt string, sandboxPolicy map[string]any, approvalPolicy string) map[string]any {
	return map[string]any{
		"threadId":       threadID,
		"cwd":            repoPath,
		"approvalPolicy": approvalPolicy,
		"input": []map[string]any{
			{
				"type": "text",
				"text": strings.TrimSpace(prompt),
			},
		},
		"sandboxPolicy": sandboxPolicy,
	}
}

func readOnlySandboxPolicy() map[string]any {
	return map[string]any{
		"type":          "readOnly",
		"access":        map[string]any{"type": "fullAccess"},
		"networkAccess": false,
	}
}

func workspaceWriteSandboxPolicy() map[string]any {
	return map[string]any{
		"type":           "workspaceWrite",
		"networkAccess":  false,
		"readOnlyAccess": map[string]any{"type": "fullAccess"},
	}
}

func sendMessage(encoder *json.Encoder, payload map[string]any) error {
	return encoder.Encode(payload)
}

func waitForResponse(
	ctx context.Context,
	stream <-chan streamEvent,
	responseID string,
	acc *probeAccumulator,
	stderrBuf *limitedBuffer,
	serverRequestHandler func(wireMessage) error,
) (wireMessage, error) {
	for {
		msg, err := nextMessage(ctx, stream)
		if err != nil {
			return wireMessage{}, err
		}

		if msg.isResponseFor(responseID) {
			if msg.Error != nil {
				detail := strings.TrimSpace(stderrBuf.String())
				if detail == "" {
					detail = msg.Error.Message
				}
				return wireMessage{}, fmt.Errorf("app-server %s request failed: %s", responseID, detail)
			}
			return msg, nil
		}

		if msg.isServerRequest() {
			if serverRequestHandler != nil {
				if err := serverRequestHandler(msg); err != nil {
					return wireMessage{}, err
				}
				continue
			}
			return wireMessage{}, fmt.Errorf("unsupported app-server request %q during probe", msg.Method)
		}

		if err := acc.observe(msg); err != nil {
			return wireMessage{}, err
		}
	}
}

func nextMessage(ctx context.Context, stream <-chan streamEvent) (wireMessage, error) {
	select {
	case <-ctx.Done():
		return wireMessage{}, ctx.Err()
	case event, ok := <-stream:
		if !ok {
			return wireMessage{}, io.EOF
		}
		if event.Err != nil {
			return wireMessage{}, event.Err
		}
		return event.Message, nil
	}
}

func streamMessages(stdout io.Reader) <-chan streamEvent {
	stream := make(chan streamEvent, 32)

	go func() {
		defer close(stream)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var msg wireMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				stream <- streamEvent{Err: err}
				return
			}

			stream <- streamEvent{Message: msg}
		}

		if err := scanner.Err(); err != nil {
			stream <- streamEvent{Err: err}
		}
	}()

	return stream
}

func captureStderr(stderr io.Reader, dst *limitedBuffer) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for scanner.Scan() {
		dst.WriteString(scanner.Text())
		dst.WriteString("\n")
	}
}

func parseResponse(msg wireMessage, target any) error {
	if len(msg.Result) == 0 {
		return errors.New("missing response result")
	}
	return json.Unmarshal(msg.Result, target)
}

func (m wireMessage) responseID() string {
	if len(m.ID) == 0 {
		return ""
	}

	var value any
	if err := json.Unmarshal(m.ID, &value); err != nil {
		return ""
	}
	return fmt.Sprint(value)
}

func (m wireMessage) isResponseFor(id string) bool {
	return m.responseID() == id && (len(m.Result) > 0 || m.Error != nil)
}

func (m wireMessage) isServerRequest() bool {
	return m.responseID() != "" && m.Method != "" && len(m.Result) == 0 && m.Error == nil
}

func (a *probeAccumulator) observe(msg wireMessage) error {
	if msg.Method == "" {
		return nil
	}

	a.result.EventsSeen++

	switch msg.Method {
	case "thread/started":
		var params threadStartedParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		if a.result.ThreadID == "" {
			a.result.ThreadID = params.Thread.ID
		}
		if a.result.ThreadPath == "" {
			a.result.ThreadPath = params.Thread.Path
		}
	case "turn/started":
		var params turnStartedParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		if a.result.ThreadID == "" {
			a.result.ThreadID = params.ThreadID
		}
		if a.result.TurnID == "" {
			a.result.TurnID = params.Turn.ID
		}
		a.result.TurnStatus = normalizeTurnStatus(params.Turn.Status)
		a.result.Interruptible = true
		a.result.Steerable = true
		if a.result.StartedAt.IsZero() {
			a.result.StartedAt = time.Now().UTC()
		}
	case "thread/status/changed":
		var params threadStatusChangedParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		if hasActiveFlag(params.Status.ActiveFlags, "waitingOnApproval") {
			a.result.TurnStatus = executor.TurnStatusApprovalRequired
			a.result.Interruptible = true
			a.result.Steerable = false
			if a.result.ApprovalState == executor.ApprovalStateNone {
				a.result.ApprovalState = executor.ApprovalStateRequired
			}
		}
	case "item/agentMessage/delta":
		var params agentMessageDeltaParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		a.deltaText.WriteString(params.Delta)
	case "item/completed":
		var params itemCompletedParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		if params.Item.Type == "agentMessage" && params.Item.Phase == "final_answer" {
			a.result.FinalMessage = strings.TrimSpace(params.Item.Text)
		}
	case "turn/completed":
		var params turnCompletedParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		a.result.ThreadID = params.ThreadID
		a.result.TurnID = params.Turn.ID
		a.result.TurnStatus = normalizeTurnStatus(params.Turn.Status)
		a.result.Interruptible = false
		a.result.Steerable = false
		a.result.CompletedAt = time.Now().UTC()
		if params.Turn.Error != nil {
			a.result.Error = &executor.Failure{
				Stage:   "turn_completed",
				Message: params.Turn.Error.Message,
				Detail:  strings.TrimSpace(params.Turn.Error.AdditionalDetails),
			}
		}
	case "error":
		var params errorNotificationParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		if !params.WillRetry {
			a.result.Error = &executor.Failure{
				Stage:   "transport_notification",
				Message: params.Error.Message,
				Detail:  strings.TrimSpace(params.Error.AdditionalDetails),
			}
		}
	}

	return nil
}

func (a *probeAccumulator) resumeExistingTurn(thread threadState, turnID string) {
	if len(thread.Turns) == 0 {
		return
	}

	for idx := len(thread.Turns) - 1; idx >= 0; idx-- {
		turn := thread.Turns[idx]
		if strings.TrimSpace(turnID) != "" && turn.ID != strings.TrimSpace(turnID) {
			continue
		}
		a.result.TurnID = turn.ID
		a.result.TurnStatus = normalizeTurnStatus(turn.Status)
		break
	}

	if a.result.TurnID == "" && strings.TrimSpace(turnID) != "" {
		a.result.TurnID = strings.TrimSpace(turnID)
	}

	if hasActiveFlag(thread.Status.ActiveFlags, "waitingOnApproval") {
		a.result.ApprovalState = executor.ApprovalStateRequired
		a.result.TurnStatus = executor.TurnStatusApprovalRequired
		a.result.Interruptible = true
		a.result.Steerable = false
	}
}

func (a *probeAccumulator) handleServerRequest(msg wireMessage) error {
	switch msg.Method {
	case "item/commandExecution/requestApproval":
		var params commandExecutionApprovalParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		a.result.ThreadID = firstNonEmpty(a.result.ThreadID, params.ThreadID)
		a.result.TurnID = firstNonEmpty(a.result.TurnID, params.TurnID)
		a.result.TurnStatus = executor.TurnStatusApprovalRequired
		a.result.ApprovalState = executor.ApprovalStateRequired
		a.result.Interruptible = true
		a.result.Steerable = false
		a.result.Approval = &executor.ApprovalRequest{
			RequestID:  msg.responseID(),
			ApprovalID: strings.TrimSpace(params.ApprovalID),
			ItemID:     strings.TrimSpace(params.ItemID),
			State:      executor.ApprovalStateRequired,
			Kind:       executor.ApprovalKindCommandExecution,
			Reason:     strings.TrimSpace(params.Reason),
			Command:    strings.TrimSpace(params.Command),
			CWD:        strings.TrimSpace(params.CWD),
			RawParams:  strings.TrimSpace(string(msg.Params)),
		}
		return nil
	case "item/fileChange/requestApproval":
		var params fileChangeApprovalParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		a.result.ThreadID = firstNonEmpty(a.result.ThreadID, params.ThreadID)
		a.result.TurnID = firstNonEmpty(a.result.TurnID, params.TurnID)
		a.result.TurnStatus = executor.TurnStatusApprovalRequired
		a.result.ApprovalState = executor.ApprovalStateRequired
		a.result.Interruptible = true
		a.result.Steerable = false
		a.result.Approval = &executor.ApprovalRequest{
			RequestID: msg.responseID(),
			ItemID:    strings.TrimSpace(params.ItemID),
			State:     executor.ApprovalStateRequired,
			Kind:      executor.ApprovalKindFileChange,
			Reason:    strings.TrimSpace(params.Reason),
			GrantRoot: strings.TrimSpace(params.GrantRoot),
			RawParams: strings.TrimSpace(string(msg.Params)),
		}
		return nil
	case "item/permissions/requestApproval":
		var params permissionsApprovalParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return err
		}
		a.result.ThreadID = firstNonEmpty(a.result.ThreadID, params.ThreadID)
		a.result.TurnID = firstNonEmpty(a.result.TurnID, params.TurnID)
		a.result.TurnStatus = executor.TurnStatusApprovalRequired
		a.result.ApprovalState = executor.ApprovalStateRequired
		a.result.Interruptible = true
		a.result.Steerable = false
		a.result.Approval = &executor.ApprovalRequest{
			RequestID: msg.responseID(),
			ItemID:    strings.TrimSpace(params.ItemID),
			State:     executor.ApprovalStateRequired,
			Kind:      executor.ApprovalKindPermissions,
			Reason:    strings.TrimSpace(params.Reason),
			RawParams: strings.TrimSpace(string(msg.Params)),
		}
		return nil
	default:
		return fmt.Errorf("unsupported app-server request %q during executor turn", msg.Method)
	}
}

func (a *probeAccumulator) approvalRequired() bool {
	return a.result.TurnStatus == executor.TurnStatusApprovalRequired || a.result.ApprovalState == executor.ApprovalStateRequired
}

func (a *probeAccumulator) turnFinished() bool {
	switch a.result.TurnStatus {
	case executor.TurnStatusCompleted, executor.TurnStatusFailed, executor.TurnStatusInterrupted:
		return !a.result.CompletedAt.IsZero()
	default:
		return false
	}
}

func (a *probeAccumulator) fail(stage string, message string, detail string) (executor.ProbeResult, error) {
	a.result.Error = &executor.Failure{
		Stage:   stage,
		Message: message,
		Detail:  strings.TrimSpace(detail),
	}
	if a.result.TurnStatus == "" {
		a.result.TurnStatus = executor.TurnStatusPending
	} else if a.result.TurnStatus == executor.TurnStatusInProgress {
		a.result.TurnStatus = executor.TurnStatusFailed
	}
	return a.result, errors.New(message)
}

func (a *probeAccumulator) failWaitError(stage string, err error, detail string, timeout time.Duration) (executor.ProbeResult, error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return a.failWithStatus(
			timeoutFailureStage(stage),
			timeoutFailureMessage(stage, timeout, a.result.TurnID != ""),
			mergeFailureDetail(detail, err),
			executor.TurnStatusFailed,
		)
	case errors.Is(err, context.Canceled):
		return a.failWithStatus(
			cancelFailureStage(stage),
			"executor app-server wait was canceled",
			mergeFailureDetail(detail, err),
			executor.TurnStatusInterrupted,
		)
	default:
		return a.fail(stage, err.Error(), detail)
	}
}

func (a *probeAccumulator) failWithStatus(stage string, message string, detail string, status executor.TurnStatus) (executor.ProbeResult, error) {
	a.result.Error = &executor.Failure{
		Stage:   stage,
		Message: message,
		Detail:  strings.TrimSpace(detail),
	}

	switch a.result.TurnStatus {
	case executor.TurnStatusCompleted, executor.TurnStatusFailed, executor.TurnStatusInterrupted:
	default:
		a.result.TurnStatus = status
	}

	return a.result, errors.New(message)
}

func normalizeTurnStatus(status string) executor.TurnStatus {
	switch strings.TrimSpace(status) {
	case "completed":
		return executor.TurnStatusCompleted
	case "failed":
		return executor.TurnStatusFailed
	case "interrupted":
		return executor.TurnStatusInterrupted
	case "inProgress":
		return executor.TurnStatusInProgress
	default:
		return executor.TurnStatusPending
	}
}

func timeoutFailureStage(stage string) string {
	if strings.HasPrefix(stage, "turn_") {
		return "turn_timeout"
	}
	return stage + "_timeout"
}

func cancelFailureStage(stage string) string {
	if strings.HasPrefix(stage, "turn_") {
		return "turn_canceled"
	}
	return stage + "_canceled"
}

func timeoutFailureMessage(stage string, timeout time.Duration, turnStarted bool) string {
	if turnStarted || strings.HasPrefix(stage, "turn_") {
		return fmt.Sprintf("executor turn exceeded app-server wait deadline (%s)", timeout)
	}
	return fmt.Sprintf("executor app-server request exceeded wait deadline (%s)", timeout)
}

func mergeFailureDetail(detail string, err error) string {
	pieces := make([]string, 0, 2)
	if trimmed := strings.TrimSpace(detail); trimmed != "" {
		pieces = append(pieces, trimmed)
	}
	if err != nil {
		pieces = append(pieces, err.Error())
	}
	return strings.Join(pieces, " | ")
}

func hasActiveFlag(flags []string, want string) bool {
	for _, flag := range flags {
		if strings.TrimSpace(flag) == want {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (c Client) clientName() string {
	if strings.TrimSpace(c.ClientName) != "" {
		return strings.TrimSpace(c.ClientName)
	}
	return defaultClientName
}

func (c Client) clientTitle() string {
	if strings.TrimSpace(c.ClientTitle) != "" {
		return strings.TrimSpace(c.ClientTitle)
	}
	return defaultClientTitle
}

func (c Client) clientVersion() string {
	if strings.TrimSpace(c.ClientVersion) != "" {
		return strings.TrimSpace(c.ClientVersion)
	}
	return "dev"
}

type limitedBuffer struct {
	mu       sync.Mutex
	capacity int
	data     string
}

func newLimitedBuffer(capacity int) *limitedBuffer {
	return &limitedBuffer{capacity: capacity}
}

func (b *limitedBuffer) WriteString(value string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.data += value
	if len(b.data) > b.capacity {
		b.data = b.data[len(b.data)-b.capacity:]
	}
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data
}
