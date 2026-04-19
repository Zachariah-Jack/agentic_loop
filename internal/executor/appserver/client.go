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

	defaultProbeTimeout = 2 * time.Minute
	probeInstructions   = "You are an executor probe. Operate read-only. Do not modify files, apply patches, create commits, or run mutating commands. Inspect and report only."
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
	Thread        threadRef `json:"thread"`
	Model         string    `json:"model"`
	ModelProvider string    `json:"modelProvider"`
}

type threadRef struct {
	ID   string `json:"id"`
	Path string `json:"path"`
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

type probeAccumulator struct {
	result    executor.ProbeResult
	deltaText strings.Builder
}

func NewClient(version string) (Client, error) {
	plan, err := ResolveLaunchPlan()
	if err != nil {
		return Client{}, err
	}

	return Client{
		LaunchPlan:    plan,
		Timeout:       defaultProbeTimeout,
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
	if strings.TrimSpace(req.RunID) == "" {
		return executor.ProbeResult{}, errors.New("run id is required")
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return executor.ProbeResult{}, errors.New("repo path is required")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return executor.ProbeResult{}, errors.New("prompt is required")
	}
	if strings.TrimSpace(c.LaunchPlan.Command) == "" {
		return executor.ProbeResult{}, errors.New("app-server launch plan is required")
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.LaunchPlan.Command, c.LaunchPlan.Args...)
	cmd.Dir = req.RepoPath

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return executor.ProbeResult{}, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return executor.ProbeResult{}, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return executor.ProbeResult{}, err
	}

	if err := cmd.Start(); err != nil {
		return executor.ProbeResult{}, err
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

	initMsg, err := waitForResponse(ctx, stream, initializeRequestID, &acc, stderrBuf)
	if err != nil {
		return acc.fail("initialize_wait", err.Error(), stderrBuf.String())
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
	threadParams := buildThreadStartParams(req.RepoPath)
	if acc.result.ThreadID != "" {
		threadMethod = "thread/resume"
		threadParams = map[string]any{
			"threadId":       acc.result.ThreadID,
			"cwd":            req.RepoPath,
			"approvalPolicy": "never",
			"sandbox":        "read-only",
		}
		acc.result.ResumedThread = true
	}

	if err := sendMessage(sender, map[string]any{
		"method": threadMethod,
		"id":     threadRequestID,
		"params": threadParams,
	}); err != nil {
		return acc.fail("thread_send", err.Error(), stderrBuf.String())
	}

	threadMsg, err := waitForResponse(ctx, stream, threadRequestID, &acc, stderrBuf)
	if err != nil {
		return acc.fail("thread_wait", err.Error(), stderrBuf.String())
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
	}

	if acc.result.ThreadID == "" {
		return acc.fail("thread_parse", "app-server did not return a thread id", stderrBuf.String())
	}

	if err := sendMessage(sender, map[string]any{
		"method": "turn/start",
		"id":     turnRequestID,
		"params": buildTurnStartParams(acc.result.ThreadID, req.RepoPath, req.Prompt),
	}); err != nil {
		return acc.fail("turn_send", err.Error(), stderrBuf.String())
	}

	turnMsg, err := waitForResponse(ctx, stream, turnRequestID, &acc, stderrBuf)
	if err != nil {
		return acc.fail("turn_wait", err.Error(), stderrBuf.String())
	}

	var turnResponse turnStartResponse
	if err := parseResponse(turnMsg, &turnResponse); err != nil {
		return acc.fail("turn_parse", err.Error(), stderrBuf.String())
	}
	acc.result.TurnID = turnResponse.Turn.ID
	acc.result.TurnStatus = normalizeTurnStatus(turnResponse.Turn.Status)
	if acc.result.StartedAt.IsZero() {
		acc.result.StartedAt = time.Now().UTC()
	}

	for !acc.turnFinished() {
		msg, err := nextMessage(ctx, stream)
		if err != nil {
			return acc.fail("turn_stream", err.Error(), stderrBuf.String())
		}

		if err := acc.observe(msg); err != nil {
			return acc.fail("turn_stream", err.Error(), stderrBuf.String())
		}
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

func buildThreadStartParams(repoPath string) map[string]any {
	return map[string]any{
		"cwd":                   repoPath,
		"approvalPolicy":        "never",
		"sandbox":               "read-only",
		"developerInstructions": probeInstructions,
	}
}

func buildTurnStartParams(threadID string, repoPath string, prompt string) map[string]any {
	return map[string]any{
		"threadId":       threadID,
		"cwd":            repoPath,
		"approvalPolicy": "never",
		"input": []map[string]any{
			{
				"type": "text",
				"text": strings.TrimSpace(prompt),
			},
		},
		"sandboxPolicy": map[string]any{
			"type":          "readOnly",
			"access":        map[string]any{"type": "fullAccess"},
			"networkAccess": false,
		},
	}
}

func sendMessage(encoder *json.Encoder, payload map[string]any) error {
	return encoder.Encode(payload)
}

func waitForResponse(ctx context.Context, stream <-chan streamEvent, responseID string, acc *probeAccumulator, stderrBuf *limitedBuffer) (wireMessage, error) {
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
		if a.result.StartedAt.IsZero() {
			a.result.StartedAt = time.Now().UTC()
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
