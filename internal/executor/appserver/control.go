package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"orchestrator/internal/config"
	"orchestrator/internal/executor"
)

const (
	threadResumeControlRequestID = "thread_resume_control"
	turnInterruptRequestID       = "turn_interrupt"
	turnSteerRequestID           = "turn_steer"
)

type controlSession struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stream     <-chan streamEvent
	stderrBuf  *limitedBuffer
	sender     *json.Encoder
	pendingReq []wireMessage
}

func (c Client) Approve(ctx context.Context, req executor.TurnRequest, approval executor.ApprovalRequest) error {
	return c.resolveApproval(ctx, req, approval, executor.ApprovalDecisionAccept)
}

func (c Client) Deny(ctx context.Context, req executor.TurnRequest, approval executor.ApprovalRequest) error {
	return c.resolveApproval(ctx, req, approval, executor.ApprovalDecisionDecline)
}

func (c Client) InterruptTurn(ctx context.Context, req executor.TurnRequest) error {
	if strings.TrimSpace(req.RepoPath) == "" {
		return errors.New("repo path is required")
	}
	if strings.TrimSpace(req.ThreadID) == "" || strings.TrimSpace(req.TurnID) == "" {
		return errors.New("thread id and turn id are required")
	}

	session, err := c.openControlSession(ctx, req.RepoPath)
	if err != nil {
		return err
	}
	defer session.close()

	if err := session.resumeThread(ctx, req); err != nil {
		return err
	}
	if err := sendMessage(session.sender, map[string]any{
		"method": "turn/interrupt",
		"id":     turnInterruptRequestID,
		"params": map[string]any{
			"threadId": req.ThreadID,
			"turnId":   req.TurnID,
		},
	}); err != nil {
		return err
	}

	_, err = waitForResponse(ctx, session.stream, turnInterruptRequestID, &probeAccumulator{}, session.stderrBuf, session.captureServerRequest)
	return err
}

func (c Client) SteerTurn(ctx context.Context, req executor.TurnRequest, note string) error {
	if strings.TrimSpace(req.RepoPath) == "" {
		return errors.New("repo path is required")
	}
	if strings.TrimSpace(req.ThreadID) == "" || strings.TrimSpace(req.TurnID) == "" {
		return errors.New("thread id and turn id are required")
	}
	if strings.TrimSpace(note) == "" {
		return errors.New("steer note is required")
	}

	session, err := c.openControlSession(ctx, req.RepoPath)
	if err != nil {
		return err
	}
	defer session.close()

	if err := session.resumeThread(ctx, req); err != nil {
		return err
	}
	if err := sendMessage(session.sender, map[string]any{
		"method": "turn/steer",
		"id":     turnSteerRequestID,
		"params": map[string]any{
			"threadId":       req.ThreadID,
			"expectedTurnId": req.TurnID,
			"input": []map[string]any{
				{
					"type": "text",
					"text": strings.TrimSpace(note),
				},
			},
		},
	}); err != nil {
		return err
	}

	_, err = waitForResponse(ctx, session.stream, turnSteerRequestID, &probeAccumulator{}, session.stderrBuf, session.captureServerRequest)
	return err
}

func (c Client) resolveApproval(
	ctx context.Context,
	req executor.TurnRequest,
	approval executor.ApprovalRequest,
	decision executor.ApprovalDecision,
) error {
	if strings.TrimSpace(req.RepoPath) == "" {
		return errors.New("repo path is required")
	}
	if strings.TrimSpace(req.ThreadID) == "" || strings.TrimSpace(req.TurnID) == "" {
		return errors.New("thread id and turn id are required")
	}
	if strings.TrimSpace(string(approval.State)) == "" || approval.Kind == "" {
		return errors.New("approval state is required")
	}

	session, err := c.openControlSession(ctx, req.RepoPath)
	if err != nil {
		return err
	}
	defer session.close()

	if err := session.resumeThread(ctx, req); err != nil {
		return err
	}

	requestMsg, err := session.waitForMatchingApproval(ctx, approval)
	if err != nil {
		return err
	}

	result, err := approvalResponsePayload(approval, decision)
	if err != nil {
		return err
	}

	return sendMessage(session.sender, map[string]any{
		"id":     responseIDForMessage(requestMsg),
		"result": result,
	})
}

func (c Client) openControlSession(ctx context.Context, repoPath string) (*controlSession, error) {
	if strings.TrimSpace(repoPath) == "" {
		return nil, errors.New("repo path is required")
	}
	if strings.TrimSpace(c.LaunchPlan.Command) == "" {
		return nil, errors.New("app-server launch plan is required")
	}

	cmd := exec.CommandContext(ctx, c.LaunchPlan.Command, c.LaunchPlan.Args...)
	cmd.Dir = repoPath

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	session := &controlSession{
		cmd:       cmd,
		stdin:     stdin,
		stream:    streamMessages(stdout),
		stderrBuf: newLimitedBuffer(8192),
		sender:    json.NewEncoder(stdin),
	}
	session.sender.SetEscapeHTML(false)
	go captureStderr(stderr, session.stderrBuf)

	if err := sendMessage(session.sender, map[string]any{
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
		session.close()
		return nil, err
	}
	if _, err := waitForResponse(ctx, session.stream, initializeRequestID, &probeAccumulator{}, session.stderrBuf, session.captureServerRequest); err != nil {
		session.close()
		return nil, err
	}
	if err := sendMessage(session.sender, map[string]any{
		"method": "initialized",
		"params": map[string]any{},
	}); err != nil {
		session.close()
		return nil, err
	}

	return session, nil
}

func (s *controlSession) close() {
	if s == nil {
		return
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil {
		_ = s.cmd.Wait()
	}
}

func (s *controlSession) resumeThread(ctx context.Context, req executor.TurnRequest) error {
	if err := sendMessage(s.sender, map[string]any{
		"method": "thread/resume",
		"id":     threadResumeControlRequestID,
		"params": buildThreadResumeParams(req.ThreadID, req.RepoPath, config.RequiredCodexSandboxMode, executeInstructions, config.RequiredCodexApprovalPolicy, config.RequiredCodexExecutorModel),
	}); err != nil {
		return err
	}

	_, err := waitForResponse(ctx, s.stream, threadResumeControlRequestID, &probeAccumulator{}, s.stderrBuf, s.captureServerRequest)
	return err
}

func (s *controlSession) captureServerRequest(msg wireMessage) error {
	s.pendingReq = append(s.pendingReq, msg)
	return nil
}

func (s *controlSession) waitForMatchingApproval(ctx context.Context, approval executor.ApprovalRequest) (wireMessage, error) {
	for len(s.pendingReq) > 0 {
		msg := s.pendingReq[0]
		s.pendingReq = s.pendingReq[1:]
		if approvalMatches(msg, approval) {
			return msg, nil
		}
	}

	for {
		msg, err := nextMessage(ctx, s.stream)
		if err != nil {
			return wireMessage{}, err
		}
		if !msg.isServerRequest() {
			continue
		}
		if approvalMatches(msg, approval) {
			return msg, nil
		}
	}
}

func approvalMatches(msg wireMessage, approval executor.ApprovalRequest) bool {
	switch msg.Method {
	case "item/commandExecution/requestApproval":
		if approval.Kind != executor.ApprovalKindCommandExecution {
			return false
		}
		var params commandExecutionApprovalParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return false
		}
		if strings.TrimSpace(approval.ApprovalID) != "" && strings.TrimSpace(params.ApprovalID) != strings.TrimSpace(approval.ApprovalID) {
			return false
		}
		return strings.TrimSpace(params.ItemID) == strings.TrimSpace(approval.ItemID)
	case "item/fileChange/requestApproval":
		if approval.Kind != executor.ApprovalKindFileChange {
			return false
		}
		var params fileChangeApprovalParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return false
		}
		return strings.TrimSpace(params.ItemID) == strings.TrimSpace(approval.ItemID)
	case "item/permissions/requestApproval":
		if approval.Kind != executor.ApprovalKindPermissions {
			return false
		}
		var params permissionsApprovalParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return false
		}
		return strings.TrimSpace(params.ItemID) == strings.TrimSpace(approval.ItemID)
	default:
		return false
	}
}

func approvalResponsePayload(approval executor.ApprovalRequest, decision executor.ApprovalDecision) (map[string]any, error) {
	switch approval.Kind {
	case executor.ApprovalKindCommandExecution:
		value := "accept"
		if decision == executor.ApprovalDecisionDecline {
			value = "decline"
		}
		return map[string]any{"decision": value}, nil
	case executor.ApprovalKindFileChange:
		value := "accept"
		if decision == executor.ApprovalDecisionDecline {
			value = "decline"
		}
		return map[string]any{"decision": value}, nil
	case executor.ApprovalKindPermissions:
		if decision == executor.ApprovalDecisionDecline {
			return map[string]any{"permissions": map[string]any{}, "scope": "turn"}, nil
		}

		var params struct {
			Permissions json.RawMessage `json:"permissions"`
		}
		if err := json.Unmarshal([]byte(approval.RawParams), &params); err != nil {
			return nil, err
		}
		if len(params.Permissions) == 0 {
			return nil, errors.New("permissions approval is missing raw permissions payload")
		}

		var permissions any
		if err := json.Unmarshal(params.Permissions, &permissions); err != nil {
			return nil, err
		}
		return map[string]any{"permissions": permissions, "scope": "turn"}, nil
	default:
		return nil, fmt.Errorf("unsupported approval kind %q", approval.Kind)
	}
}

func responseIDForMessage(msg wireMessage) any {
	if len(msg.ID) == 0 {
		return nil
	}

	var value any
	if err := json.Unmarshal(msg.ID, &value); err != nil {
		return msg.responseID()
	}
	return value
}
