package appserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"orchestrator/internal/config"
)

const defaultCodexProbeTimeout = 2 * time.Minute

type CodexEnvironment struct {
	CodexPath        string   `json:"codex_executable_path,omitempty"`
	CodexVersion     string   `json:"codex_version,omitempty"`
	NodePath         string   `json:"node_path,omitempty"`
	CodexJSPath      string   `json:"codex_js_path,omitempty"`
	AppServerCommand string   `json:"app_server_command,omitempty"`
	AppServerArgs    []string `json:"app_server_args,omitempty"`
	ConfigSource     string   `json:"codex_config_source,omitempty"`
	CodexHome        string   `json:"codex_home,omitempty"`
	PathEnv          string   `json:"path_env,omitempty"`
}

type CodexExecProbeResult struct {
	Environment     CodexEnvironment `json:"environment"`
	Command         []string         `json:"command"`
	RepoPath        string           `json:"repo_path"`
	Model           string           `json:"model"`
	ApprovalPolicy  string           `json:"approval_policy"`
	Sandbox         string           `json:"sandbox"`
	ReasoningEffort string           `json:"reasoning_effort"`
	StartedAt       string           `json:"started_at,omitempty"`
	CompletedAt     string           `json:"completed_at,omitempty"`
	DurationMillis  int64            `json:"duration_millis,omitempty"`
	Success         bool             `json:"success"`
	Output          string           `json:"output,omitempty"`
	Error           string           `json:"error,omitempty"`
}

func InspectCodexEnvironment(ctx context.Context) (CodexEnvironment, error) {
	plan, err := ResolveLaunchPlan()
	if err != nil {
		return CodexEnvironment{PathEnv: os.Getenv("PATH"), ConfigSource: codexConfigSource()}, err
	}

	env := CodexEnvironment{
		CodexPath:        strings.TrimSpace(plan.CodexPath),
		NodePath:         strings.TrimSpace(plan.NodePath),
		CodexJSPath:      strings.TrimSpace(plan.CodexJSPath),
		AppServerCommand: strings.TrimSpace(plan.Command),
		AppServerArgs:    append([]string(nil), plan.Args...),
		ConfigSource:     codexConfigSource(),
		CodexHome:        strings.TrimSpace(os.Getenv("CODEX_HOME")),
		PathEnv:          os.Getenv("PATH"),
	}
	if env.CodexPath == "" {
		env.CodexPath = "codex"
	}

	versionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, versionErr := exec.CommandContext(versionCtx, env.CodexPath, "--version").CombinedOutput()
	if versionErr == nil {
		env.CodexVersion = parseCodexVersion(string(output))
	}
	if env.CodexVersion == "" && versionErr != nil {
		env.CodexVersion = "unavailable: " + strings.TrimSpace(versionErr.Error())
	}

	return env, nil
}

func RunRequiredCodexExecProbe(ctx context.Context, repoPath string) (CodexExecProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultCodexProbeTimeout)
	defer cancel()

	env, err := InspectCodexEnvironment(ctx)
	result := CodexExecProbeResult{
		Environment:     env,
		RepoPath:        strings.TrimSpace(repoPath),
		Model:           config.RequiredCodexExecutorModel,
		ApprovalPolicy:  config.RequiredCodexApprovalPolicy,
		Sandbox:         config.RequiredCodexSandboxMode,
		ReasoningEffort: config.RequiredCodexReasoningEffort,
	}
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if strings.TrimSpace(repoPath) == "" {
		err := errors.New("repo path is required for Codex executor probe")
		result.Error = err.Error()
		return result, err
	}

	args := buildRequiredCodexExecProbeArgs(repoPath)
	result.Command = append([]string{env.CodexPath}, args...)
	started := time.Now().UTC()
	result.StartedAt = started.Format(time.RFC3339)

	cmd := exec.CommandContext(ctx, env.CodexPath, args...)
	cmd.Dir = repoPath
	cmd.Stdin = strings.NewReader("")
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	runErr := cmd.Run()
	completed := time.Now().UTC()
	result.CompletedAt = completed.Format(time.RFC3339)
	result.DurationMillis = completed.Sub(started).Milliseconds()
	result.Output = trimProbeOutput(combined.String())

	if runErr != nil {
		result.Error = strings.TrimSpace(runErr.Error())
		if result.Output != "" {
			result.Error = result.Error + ": " + result.Output
		}
		return result, runErr
	}
	if !codexProbeOutputOK(combined.String()) {
		err := fmt.Errorf("Codex probe completed but did not return the expected OK response")
		result.Error = err.Error()
		return result, err
	}

	result.Success = true
	return result, nil
}

func buildRequiredCodexExecProbeArgs(repoPath string) []string {
	return []string{
		"exec",
		"--model", config.RequiredCodexExecutorModel,
		"--sandbox", config.RequiredCodexSandboxMode,
		"-c", `approval_policy="never"`,
		"-c", `model_reasoning_effort="xhigh"`,
		"--cd", strings.TrimSpace(repoPath),
		"Reply with only OK.",
	}
}

func parseCodexVersion(output string) string {
	return strings.TrimSpace(output)
}

var okLinePattern = regexp.MustCompile(`(?m)^\s*OK\s*$`)

func codexProbeOutputOK(output string) bool {
	return okLinePattern.MatchString(output)
}

func codexConfigSource() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".codex", "config.toml")
	}
	return ""
}

func trimProbeOutput(output string) string {
	const limit = 4096
	trimmed := strings.TrimSpace(output)
	if len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "... [truncated]"
}
