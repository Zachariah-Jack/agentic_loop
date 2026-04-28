package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/control"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

type controlSetupHealthSnapshot struct {
	RepoPath     string              `json:"repo_path"`
	GeneratedAt  string              `json:"generated_at"`
	AutoRepaired []string            `json:"auto_repaired,omitempty"`
	Checks       []controlSetupCheck `json:"checks"`
	Message      string              `json:"message,omitempty"`
}

type controlSetupCheck struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Action      string `json:"action,omitempty"`
	ActionLabel string `json:"action_label,omitempty"`
	Manual      bool   `json:"manual,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

type controlSetupActionSnapshot struct {
	Action    string `json:"action"`
	Succeeded bool   `json:"succeeded"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Manual    bool   `json:"manual,omitempty"`
	RepoPath  string `json:"repo_path,omitempty"`
}

type controlSnapshotCapture struct {
	RunID        string `json:"run_id,omitempty"`
	RepoPath     string `json:"repo_path"`
	ArtifactPath string `json:"artifact_path"`
	AbsolutePath string `json:"absolute_path"`
	CapturedAt   string `json:"captured_at"`
	Message      string `json:"message"`
}

func getSetupHealth(ctx context.Context, inv Invocation) (controlSetupHealthSnapshot, error) {
	repoRoot := inv.RepoRoot
	repairResult, err := repairSafeRepoContractDirs(inv.Layout)
	if err != nil {
		return controlSetupHealthSnapshot{}, err
	}
	gitReady := pathExists(filepath.Join(repoRoot, ".git"))
	contract := inspectTargetRepoContract(repoRoot)
	orchestratorRoot := pathExists(inv.Layout.RootDir)
	stateReady := pathExists(inv.Layout.StateDir) && pathExists(inv.Layout.LogsDir) && pathExists(filepath.Join(inv.Layout.RootDir, "artifacts"))
	plannerKeyPresent := plannerAPIKeyStatus() == "present"
	ntfyReady := ntfyBridgeState(currentConfig(inv)) == "ready"

	codexDetail := "Codex command was not detected from the control-server environment."
	codexStatus := "action_required"
	if env, err := appserver.InspectCodexEnvironment(ctx); err == nil {
		codexStatus = "ok"
		codexDetail = fmt.Sprintf("%s (%s)", valueOrUnavailable(env.CodexPath), valueOrUnavailable(env.CodexVersion))
	}

	gitTrustStatus, gitTrustDetail := inspectGitSafeDirectory(ctx, repoRoot)
	checks := []controlSetupCheck{
		{
			ID:          "git_initialized",
			Label:       "Git initialized",
			Status:      statusFromBool(gitReady),
			Detail:      filepath.Join(repoRoot, ".git"),
			Action:      actionIfMissing(gitReady, "git_init"),
			ActionLabel: actionLabelIfMissing(gitReady, "Initialize Git"),
		},
		{
			ID:          "git_safe_directory",
			Label:       "Repository trusted for Git",
			Status:      gitTrustStatus,
			Detail:      gitTrustDetail,
			Action:      actionIfMissing(gitTrustStatus == "ok", "git_safe_directory"),
			ActionLabel: actionLabelIfMissing(gitTrustStatus == "ok", "Trust Repo for Git"),
		},
		{
			ID:          "orchestrator_initialized",
			Label:       "Orchestrator initialized",
			Status:      statusFromBool(orchestratorRoot),
			Detail:      inv.Layout.RootDir,
			Action:      actionIfMissing(orchestratorRoot, "orchestrator_init"),
			ActionLabel: actionLabelIfMissing(orchestratorRoot, "Initialize Orchestrator"),
		},
		{
			ID:          "required_project_files",
			Label:       "Required project files exist",
			Status:      statusFromBool(contract.Ready),
			Detail:      missingOrReady(contract.Missing),
			Action:      actionIfMissing(contract.Ready, "repair_project_setup"),
			ActionLabel: actionLabelIfMissing(contract.Ready, "Repair Project Setup"),
		},
		{
			ID:          "codex_available",
			Label:       "Codex available",
			Status:      codexStatus,
			Detail:      codexDetail,
			Action:      "check_codex",
			ActionLabel: "Check Codex",
		},
		{
			ID:     "codex_repo_trust",
			Label:  "Repository trusted for Codex",
			Status: "manual_required",
			Detail: "No reliable current Codex repo-trust write action is exposed by the installed CLI. Use Test Codex Config or run a Codex command with --cd for this repo and confirm any Codex prompt manually.",
			Manual: true,
		},
		{
			ID:          "planner_config",
			Label:       "Planner key/config available",
			Status:      statusFromBool(plannerKeyPresent),
			Detail:      "OPENAI_API_KEY " + plannerAPIKeyStatus(),
			Action:      actionIfMissing(plannerKeyPresent, "verify_planner_config"),
			ActionLabel: actionLabelIfMissing(plannerKeyPresent, "Verify Planner Config"),
		},
		{
			ID:       "ntfy_configured",
			Label:    "ntfy configured",
			Status:   optionalStatus(ntfyReady),
			Detail:   ntfyBridgeState(currentConfig(inv)),
			Optional: true,
		},
		{
			ID:          "state_logs_writable",
			Label:       "State/log folders writable",
			Status:      statusFromBool(stateReady),
			Detail:      fmt.Sprintf("state=%s logs=%s artifacts=%s", inv.Layout.StateDir, inv.Layout.LogsDir, filepath.Join(inv.Layout.RootDir, "artifacts")),
			Action:      actionIfMissing(stateReady, "orchestrator_init"),
			ActionLabel: actionLabelIfMissing(stateReady, "Initialize Orchestrator"),
		},
	}

	return controlSetupHealthSnapshot{
		RepoPath:     repoRoot,
		GeneratedAt:  formatSnapshotTime(time.Now().UTC()),
		AutoRepaired: repairResult.Created,
		Checks:       checks,
		Message:      "Setup checks are mechanical readiness checks. They do not decide whether project work is complete.",
	}, nil
}

func runSetupAction(ctx context.Context, inv Invocation, request control.SetupActionRequest) (controlSetupActionSnapshot, error) {
	action := strings.TrimSpace(request.Action)
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlSetupActionSnapshot{}, err
	}

	result := controlSetupActionSnapshot{
		Action:   action,
		RepoPath: repoRoot,
	}
	switch action {
	case "git_init":
		err = runGitCommand(ctx, repoRoot, "init")
		result.Detail = "git init"
	case "git_safe_directory":
		err = runGitCommand(ctx, repoRoot, "config", "--global", "--add", "safe.directory", repoRoot)
		result.Detail = fmt.Sprintf("git config --global --add safe.directory %q", repoRoot)
	case "orchestrator_init", "create_templates", "repair_project_setup":
		layout := state.ResolveLayout(repoRoot)
		if err = ensureTargetRepoContractDirs(layout); err == nil {
			_, err = scaffoldTargetRepoContract(repoRoot, layout)
		}
		if err == nil {
			var storeClose func() error
			store, journalWriter, runtimeErr := ensureRuntime(ctx, layout)
			if runtimeErr != nil {
				err = runtimeErr
			} else {
				storeClose = store.Close
				err = journalWriter.Append(journal.Event{
					Type:     "runtime.initialized",
					RepoPath: repoRoot,
					Message:  "setup action initialized target repo scaffold and runtime surfaces",
				})
			}
			if storeClose != nil {
				_ = storeClose()
			}
		}
		result.Detail = "created missing .orchestrator files/folders and runtime surfaces without overwriting existing files"
	case "check_codex":
		env, inspectErr := appserver.InspectCodexEnvironment(ctx)
		if inspectErr != nil {
			err = inspectErr
			result.Detail = "Codex command was not detected."
		} else {
			result.Detail = fmt.Sprintf("%s (%s)", valueOrUnavailable(env.CodexPath), valueOrUnavailable(env.CodexVersion))
		}
	case "verify_planner_config":
		if plannerAPIKeyStatus() != "present" {
			result.Manual = true
			result.Status = "manual_required"
			result.Detail = "OPENAI_API_KEY is missing. Set it in the environment, then restart or reconnect the control server."
			return result, nil
		}
		result.Detail = "OPENAI_API_KEY is present; use Test Planner Model for a live model probe."
	case "codex_repo_trust":
		result.Manual = true
		result.Status = "manual_required"
		result.Detail = fmt.Sprintf("Codex trust cannot be automated reliably. Run Codex once with --cd %q and confirm any Codex prompt manually.", repoRoot)
		return result, nil
	default:
		return controlSetupActionSnapshot{}, fmt.Errorf("unsupported setup action %q", action)
	}
	if err != nil {
		result.Succeeded = false
		result.Status = "failed"
		result.Detail = strings.TrimSpace(result.Detail + ": " + err.Error())
		return result, err
	}

	result.Succeeded = true
	result.Status = "ok"
	emitEngineEvent(inv, "setup_action_completed", map[string]any{
		"repo_path": repoRoot,
		"action":    action,
		"detail":    result.Detail,
	})
	return result, nil
}

func captureControlSnapshot(ctx context.Context, inv Invocation, request control.CaptureSnapshotRequest) (controlSnapshotCapture, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlSnapshotCapture{}, err
	}
	status, err := buildControlStatusSnapshot(ctx, inv, strings.TrimSpace(request.RunID))
	if err != nil {
		return controlSnapshotCapture{}, err
	}
	var events []journal.Event
	if status.Run != nil && strings.TrimSpace(status.Run.ID) != "" {
		events, _ = latestRunEvents(inv.Layout, status.Run.ID, 128)
	}

	capturedAt := time.Now().UTC()
	report := map[string]any{
		"captured_at": capturedAt.Format(time.RFC3339Nano),
		"repo_path":   repoRoot,
		"status":      status,
		"events":      events,
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return controlSnapshotCapture{}, err
	}
	relativePath := fmt.Sprintf(".orchestrator/artifacts/reports/snapshots/snapshot-%s.json", capturedAt.Format("20060102-150405"))
	absolutePath, err := resolveRepoRelativePath(repoRoot, relativePath)
	if err != nil {
		return controlSnapshotCapture{}, err
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return controlSnapshotCapture{}, err
	}
	if err := os.WriteFile(absolutePath, encoded, 0o644); err != nil {
		return controlSnapshotCapture{}, err
	}

	runID := strings.TrimSpace(request.RunID)
	if status.Run != nil && strings.TrimSpace(status.Run.ID) != "" {
		runID = strings.TrimSpace(status.Run.ID)
	}
	if journalWriter, err := journal.Open(inv.Layout.JournalPath); err == nil {
		_ = journalWriter.Append(journal.Event{
			Type:            "snapshot_captured",
			RunID:           runID,
			RepoPath:        repoRoot,
			Message:         "operator snapshot captured",
			ArtifactPath:    relativePath,
			ArtifactPreview: "Control status snapshot and recent events captured for later inspection.",
		})
	}
	emitEngineEvent(inv, "snapshot_captured", map[string]any{
		"repo_path":     repoRoot,
		"run_id":        runID,
		"artifact_path": relativePath,
		"captured_at":   formatSnapshotTime(capturedAt),
	})
	return controlSnapshotCapture{
		RunID:        runID,
		RepoPath:     repoRoot,
		ArtifactPath: relativePath,
		AbsolutePath: absolutePath,
		CapturedAt:   formatSnapshotTime(capturedAt),
		Message:      "Snapshot captured as a durable report artifact.",
	}, nil
}

func inspectGitSafeDirectory(ctx context.Context, repoRoot string) (string, string) {
	output, err := exec.CommandContext(ctx, "git", "config", "--global", "--get-all", "safe.directory").Output()
	if err != nil {
		return "action_required", "Git safe.directory could not be inspected; use Trust Repo for Git if Git reports ownership warnings."
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.EqualFold(filepath.Clean(strings.TrimSpace(line)), filepath.Clean(repoRoot)) {
			return "ok", "repo path is present in global Git safe.directory"
		}
	}
	return "action_required", "repo path is not present in global Git safe.directory"
}

func runGitCommand(ctx context.Context, repoRoot string, args ...string) error {
	if strings.TrimSpace(repoRoot) == "" {
		return errors.New("repo root is unavailable")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return errors.New(detail)
	}
	return nil
}

func statusFromBool(ok bool) string {
	if ok {
		return "ok"
	}
	return "action_required"
}

func optionalStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "optional"
}

func actionIfMissing(ok bool, action string) string {
	if ok {
		return ""
	}
	return action
}

func actionLabelIfMissing(ok bool, label string) string {
	if ok {
		return ""
	}
	return label
}

func missingOrReady(missing []string) string {
	if len(missing) == 0 {
		return "all required project files and runtime folders are present"
	}
	return "missing " + strings.Join(missing, ", ")
}
