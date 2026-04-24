package cli

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/control"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/state"
)

type controlModelComponentSnapshot struct {
	Component         string `json:"component"`
	ConfiguredModel   string `json:"configured_model,omitempty"`
	RequestedModel    string `json:"requested_model,omitempty"`
	ResolvedModel     string `json:"resolved_model,omitempty"`
	VerifiedModel     string `json:"verified_model,omitempty"`
	VerificationState string `json:"verification_state"`
	AccessMode        string `json:"access_mode,omitempty"`
	Effort            string `json:"effort,omitempty"`
	TestPerformed     bool   `json:"test_performed"`
	LastTestedAt      string `json:"last_tested_at,omitempty"`
	LastError         string `json:"last_error,omitempty"`
	ModelUnavailable  bool   `json:"model_unavailable"`
	PlainEnglish      string `json:"plain_english"`
	RecommendedAction string `json:"recommended_action"`
}

type controlModelHealthSnapshot struct {
	Planner        controlModelComponentSnapshot `json:"planner"`
	Executor       controlModelComponentSnapshot `json:"executor"`
	NeedsAttention bool                          `json:"needs_attention"`
	Blocking       bool                          `json:"blocking"`
	Message        string                        `json:"message"`
}

func buildModelHealthSnapshot(ctx context.Context, inv Invocation, latestRun *state.Run) controlModelHealthSnapshot {
	planner := plannerModelHealthSnapshot(ctx, inv, false, "")
	executor := executorModelHealthSnapshot(latestRun, false)
	return combineModelHealth(planner, executor)
}

func testPlannerModelHealth(ctx context.Context, inv Invocation, request control.ModelTestRequest) (controlModelHealthSnapshot, error) {
	planner := plannerModelHealthSnapshot(ctx, inv, true, request.Model)
	var latestRun *state.Run
	if run, found := latestRunForModelHealth(ctx, inv); found {
		latestRun = &run
	}
	return combineModelHealth(planner, executorModelHealthSnapshot(latestRun, false)), nil
}

func testExecutorModelHealth(ctx context.Context, inv Invocation, _ control.ModelTestRequest) (controlModelHealthSnapshot, error) {
	var latestRun *state.Run
	if run, found := latestRunForModelHealth(ctx, inv); found {
		latestRun = &run
	}
	executor := executorModelHealthSnapshot(latestRun, true)
	return combineModelHealth(plannerModelHealthSnapshot(ctx, inv, false, ""), executor), nil
}

func plannerModelHealthSnapshot(ctx context.Context, inv Invocation, performTest bool, overrideModel string) controlModelComponentSnapshot {
	configured := strings.TrimSpace(resolvePlannerModel(inv))
	if strings.TrimSpace(overrideModel) != "" {
		configured = strings.TrimSpace(overrideModel)
	}
	if configured == "" {
		configured = config.PlannerModelLatestGPT5
	}

	snapshot := controlModelComponentSnapshot{
		Component:         "planner",
		ConfiguredModel:   configured,
		RequestedModel:    configured,
		VerificationState: "not_verified",
		TestPerformed:     performTest,
		PlainEnglish:      "Planner model availability has not been tested in this session.",
		RecommendedAction: "Use Test Planner Model before a long unattended run if model access is uncertain.",
	}
	if performTest {
		snapshot.LastTestedAt = formatSnapshotTime(time.Now().UTC())
	}

	apiKey := plannerAPIKey()
	if strings.TrimSpace(apiKey) == "" {
		snapshot.VerificationState = "missing_api_key"
		snapshot.LastError = "OPENAI_API_KEY is not set."
		snapshot.PlainEnglish = "Planner model availability cannot be tested because OPENAI_API_KEY is missing."
		snapshot.RecommendedAction = "Set OPENAI_API_KEY in the environment, then test the planner model again."
		return snapshot
	}

	if !performTest {
		if isLatestGPT5Alias(configured) {
			snapshot.PlainEnglish = "Planner model uses the latest GPT-5 alias. The actual model is resolved only during a model test or planner call."
		}
		return snapshot
	}

	if isLatestGPT5Alias(configured) {
		resolved, err := latestGPT5Lookup(ctx, apiKey)
		if err != nil || strings.TrimSpace(resolved) == "" {
			snapshot.VerificationState = "discovery_failed"
			snapshot.LastError = valueOrUnavailable(errorString(err))
			snapshot.PlainEnglish = "The planner latest-model alias could not be resolved through the Models API."
			snapshot.RecommendedAction = "Check OpenAI API access or configure an explicit verified planner model."
			return snapshot
		}
		snapshot.ResolvedModel = strings.TrimSpace(resolved)
		snapshot.VerifiedModel = strings.TrimSpace(resolved)
		snapshot.VerificationState = "verified"
		snapshot.PlainEnglish = fmt.Sprintf("Planner latest-model alias resolved and verified as %s.", snapshot.VerifiedModel)
		snapshot.RecommendedAction = "Planner model access is verified for this account."
		return snapshot
	}

	if err := lookupOpenAIModel(ctx, apiKey, configured); err != nil {
		snapshot.VerificationState = modelVerificationStateFromError(err)
		snapshot.LastError = err.Error()
		snapshot.ModelUnavailable = modelUnavailableFromText(err.Error())
		snapshot.PlainEnglish = fmt.Sprintf("Planner model %s could not be verified for this account.", configured)
		snapshot.RecommendedAction = "Choose a model this account can access, then test again. No silent fallback will be used."
		return snapshot
	}

	snapshot.ResolvedModel = configured
	snapshot.VerifiedModel = configured
	snapshot.VerificationState = "verified"
	snapshot.PlainEnglish = fmt.Sprintf("Planner model %s is available to this account.", configured)
	snapshot.RecommendedAction = "Planner model access is verified for this account."
	return snapshot
}

func executorModelHealthSnapshot(latestRun *state.Run, performTest bool) controlModelComponentSnapshot {
	snapshot := controlModelComponentSnapshot{
		Component:         "executor",
		ConfiguredModel:   "external Codex configuration",
		RequestedModel:    "not reported yet",
		VerificationState: "not_verified",
		AccessMode:        "workspace-write sandbox, approval on-request, network access disabled by current executor policy",
		Effort:            "not reported by Codex app-server until Codex provides it",
		TestPerformed:     performTest,
		PlainEnglish:      "Codex model and effort are controlled by the external Codex configuration and have not been verified by a turn in this session.",
		RecommendedAction: "Use Test Codex Config and inspect the latest executor error before a long unattended run.",
	}
	if performTest {
		snapshot.LastTestedAt = formatSnapshotTime(time.Now().UTC())
	}

	if latestRun != nil {
		errorText := strings.TrimSpace(latestRun.ExecutorLastError)
		if errorText == "" {
			errorText = strings.TrimSpace(latestRun.RuntimeIssueMessage)
		}
		if model := extractUnavailableModel(errorText); model != "" {
			snapshot.RequestedModel = model
		}
		if modelUnavailableFromText(errorText) {
			snapshot.VerificationState = "invalid"
			snapshot.LastError = errorText
			snapshot.ModelUnavailable = true
			snapshot.PlainEnglish = fmt.Sprintf("Codex could not start because the configured model %s is not available to this account. No code changes were made by that executor turn.", valueOrUnavailable(snapshot.RequestedModel))
			snapshot.RecommendedAction = "Change the Codex model in Codex/OpenAI configuration, test it, then continue the run. No silent fallback will be used."
			return snapshot
		}
		if errorText != "" && strings.TrimSpace(latestRun.LatestStopReason) == "executor_failed" {
			snapshot.VerificationState = "executor_failed"
			snapshot.LastError = errorText
			snapshot.PlainEnglish = "The latest Codex executor turn failed before completing."
			snapshot.RecommendedAction = "Review Live Output and Codex configuration, then continue only after the failure is understood."
			return snapshot
		}
	}

	if !performTest {
		return snapshot
	}

	plan, err := appserver.ResolveLaunchPlan()
	if err != nil {
		snapshot.VerificationState = "unavailable"
		snapshot.LastError = err.Error()
		snapshot.PlainEnglish = "Codex app-server could not be resolved from PATH."
		snapshot.RecommendedAction = "Install/sign in to Codex and ensure the codex command is on PATH, then test again."
		return snapshot
	}

	snapshot.VerificationState = "launch_ready_model_not_verified"
	snapshot.ResolvedModel = snapshot.RequestedModel
	snapshot.PlainEnglish = "Codex app-server launch path is available, but the exact configured model is not exposed until Codex starts a turn."
	snapshot.RecommendedAction = fmt.Sprintf("Launch path is ready (%s). If a run fails with a model error, change the external Codex model and test again.", plan.Command)
	return snapshot
}

func combineModelHealth(planner, executor controlModelComponentSnapshot) controlModelHealthSnapshot {
	needsAttention := modelComponentNeedsAttention(planner) || modelComponentNeedsAttention(executor)
	blocking := executor.ModelUnavailable || executor.VerificationState == "invalid"
	message := "Model health has not been fully verified. Use the test actions before long unattended runs."
	if blocking {
		message = "Configured executor/Codex model is unavailable. Change or test the configured Codex model before continuing."
	} else if needsAttention {
		message = "Model or Codex configuration needs attention before unattended operation."
	} else if planner.VerificationState == "verified" && strings.HasPrefix(executor.VerificationState, "launch_ready") {
		message = "Planner model is verified and Codex launch path is available; Codex model identity remains externally managed."
	}
	return controlModelHealthSnapshot{
		Planner:        planner,
		Executor:       executor,
		NeedsAttention: needsAttention,
		Blocking:       blocking,
		Message:        message,
	}
}

func modelComponentNeedsAttention(component controlModelComponentSnapshot) bool {
	switch component.VerificationState {
	case "verified", "launch_ready_model_not_verified", "not_verified":
		return false
	default:
		return true
	}
}

func latestRunForModelHealth(ctx context.Context, inv Invocation) (state.Run, bool) {
	if !pathExists(inv.Layout.DBPath) {
		return state.Run{}, false
	}
	store, err := openExistingStore(inv.Layout)
	if err != nil {
		return state.Run{}, false
	}
	defer store.Close()
	if err := store.EnsureSchema(ctx); err != nil {
		return state.Run{}, false
	}
	run, found, err := store.LatestRun(ctx)
	if err != nil {
		return state.Run{}, false
	}
	return run, found
}

func modelVerificationStateFromError(err error) string {
	if err == nil {
		return "verified"
	}
	text := err.Error()
	if modelUnavailableFromText(text) || strings.Contains(text, "HTTP 404") {
		return "invalid"
	}
	return "unavailable"
}

func modelUnavailableFromText(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" || !strings.Contains(normalized, "model") {
		return false
	}
	return strings.Contains(normalized, "does not exist") ||
		strings.Contains(normalized, "do not have access") ||
		strings.Contains(normalized, "don't have access") ||
		strings.Contains(normalized, "lacks access") ||
		strings.Contains(normalized, "lack access")
}

var unavailableModelPattern = regexp.MustCompile("(?i)model [`'\"]?([a-z0-9._:-]+)[`'\"]?")

func extractUnavailableModel(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if parts := strings.Split(text, "`"); len(parts) >= 3 {
		return strings.TrimSpace(parts[1])
	}
	match := unavailableModelPattern.FindStringSubmatch(text)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}
