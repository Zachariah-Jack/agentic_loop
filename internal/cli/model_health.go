package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/control"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/state"
)

type controlModelComponentSnapshot struct {
	Component                   string `json:"component"`
	ConfiguredModel             string `json:"configured_model,omitempty"`
	RequestedModel              string `json:"requested_model,omitempty"`
	ResolvedModel               string `json:"resolved_model,omitempty"`
	VerifiedModel               string `json:"verified_model,omitempty"`
	VerificationState           string `json:"verification_state"`
	AccessMode                  string `json:"access_mode,omitempty"`
	Effort                      string `json:"effort,omitempty"`
	TestPerformed               bool   `json:"test_performed"`
	LastTestedAt                string `json:"last_tested_at,omitempty"`
	LastError                   string `json:"last_error,omitempty"`
	ModelUnavailable            bool   `json:"model_unavailable"`
	PlainEnglish                string `json:"plain_english"`
	RecommendedAction           string `json:"recommended_action"`
	CodexExecutablePath         string `json:"codex_executable_path,omitempty"`
	CodexVersion                string `json:"codex_version,omitempty"`
	CodexConfigSource           string `json:"codex_config_source,omitempty"`
	CodexAppServerCommand       string `json:"codex_app_server_command,omitempty"`
	CodexAppServerArgs          string `json:"codex_app_server_args,omitempty"`
	CodexModelConfigured        string `json:"codex_model_configured,omitempty"`
	CodexModelVerified          bool   `json:"codex_model_verified"`
	CodexPermissionModeVerified bool   `json:"codex_permission_mode_verified"`
	CodexLastProbeError         string `json:"codex_last_probe_error,omitempty"`
}

type controlModelHealthSnapshot struct {
	Planner        controlModelComponentSnapshot `json:"planner"`
	Executor       controlModelComponentSnapshot `json:"executor"`
	NeedsAttention bool                          `json:"needs_attention"`
	Blocking       bool                          `json:"blocking"`
	Message        string                        `json:"message"`
}

var (
	inspectCodexEnvironment   = appserver.InspectCodexEnvironment
	runRequiredCodexExecProbe = appserver.RunRequiredCodexExecProbe
)

func buildModelHealthSnapshot(ctx context.Context, inv Invocation, latestRun *state.Run) controlModelHealthSnapshot {
	planner := plannerModelHealthSnapshot(ctx, inv, false, "")
	executor := executorModelHealthSnapshot(ctx, inv, latestRun, false)
	return combineModelHealth(planner, executor)
}

func testPlannerModelHealth(ctx context.Context, inv Invocation, request control.ModelTestRequest) (controlModelHealthSnapshot, error) {
	planner := plannerModelHealthSnapshot(ctx, inv, true, request.Model)
	var latestRun *state.Run
	if run, found := latestRunForModelHealth(ctx, inv); found {
		latestRun = &run
	}
	return combineModelHealth(planner, executorModelHealthSnapshot(ctx, inv, latestRun, false)), nil
}

func testExecutorModelHealth(ctx context.Context, inv Invocation, _ control.ModelTestRequest) (controlModelHealthSnapshot, error) {
	var latestRun *state.Run
	if run, found := latestRunForModelHealth(ctx, inv); found {
		latestRun = &run
	}
	executor := executorModelHealthSnapshot(ctx, inv, latestRun, true)
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

	if plannerModelBelowMinimum(configured) {
		snapshot.VerificationState = "invalid"
		snapshot.LastError = fmt.Sprintf("planner model %s is below required minimum %s", configured, config.PlannerModelMinimumGPT5)
		snapshot.PlainEnglish = fmt.Sprintf("Planner model %s is below the required minimum %s.", configured, config.PlannerModelMinimumGPT5)
		snapshot.RecommendedAction = "Configure gpt-5-latest or an explicit planner model at least gpt-5.4, then test again."
		return snapshot
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
		if plannerModelBelowMinimum(snapshot.ResolvedModel) {
			snapshot.VerificationState = "invalid"
			snapshot.LastError = fmt.Sprintf("planner latest-model alias resolved to %s, below required minimum %s", snapshot.ResolvedModel, config.PlannerModelMinimumGPT5)
			snapshot.PlainEnglish = fmt.Sprintf("Planner latest-model alias resolved to %s, which is below the required minimum %s.", snapshot.ResolvedModel, config.PlannerModelMinimumGPT5)
			snapshot.RecommendedAction = "Configure an explicit planner model at least gpt-5.4 and test again."
			return snapshot
		}
		probe, err := runPlannerModelProbe(ctx, apiKey, snapshot.ResolvedModel)
		if err != nil {
			snapshot.VerificationState = modelVerificationStateFromError(err)
			snapshot.LastError = err.Error()
			snapshot.ModelUnavailable = modelUnavailableFromText(err.Error())
			snapshot.PlainEnglish = fmt.Sprintf("Planner latest-model alias resolved to %s, but the Responses API probe failed.", snapshot.ResolvedModel)
			snapshot.RecommendedAction = "Check OpenAI API access or configure an explicit verified planner model. No silent fallback will be used."
			return snapshot
		}
		snapshot.RequestedModel = probe.RequestedModel
		snapshot.VerifiedModel = valueOrDefault(probe.VerifiedModel, snapshot.ResolvedModel)
		snapshot.VerificationState = "verified"
		snapshot.PlainEnglish = fmt.Sprintf("Planner latest-model alias resolved to %s and completed a tiny Responses API probe.", snapshot.VerifiedModel)
		snapshot.RecommendedAction = "Planner model access is verified for this account."
		return snapshot
	}

	probe, err := runPlannerModelProbe(ctx, apiKey, configured)
	if err != nil {
		snapshot.VerificationState = modelVerificationStateFromError(err)
		snapshot.LastError = err.Error()
		snapshot.ModelUnavailable = modelUnavailableFromText(err.Error())
		snapshot.PlainEnglish = fmt.Sprintf("Planner model %s could not complete the tiny Responses API verification call.", configured)
		snapshot.RecommendedAction = "Choose a model this account can access, then test again. No silent fallback will be used."
		return snapshot
	}

	if plannerModelBelowMinimum(configured) {
		snapshot.VerificationState = "invalid"
		snapshot.LastError = fmt.Sprintf("planner model %s is below required minimum %s", configured, config.PlannerModelMinimumGPT5)
		snapshot.PlainEnglish = fmt.Sprintf("Planner model %s is below the required minimum %s.", configured, config.PlannerModelMinimumGPT5)
		snapshot.RecommendedAction = "Configure gpt-5-latest or an explicit planner model at least gpt-5.4, then test again."
		return snapshot
	}

	snapshot.ResolvedModel = configured
	snapshot.VerifiedModel = valueOrDefault(probe.VerifiedModel, configured)
	snapshot.VerificationState = "verified"
	snapshot.PlainEnglish = fmt.Sprintf("Planner model %s completed a tiny Responses API probe.", snapshot.VerifiedModel)
	snapshot.RecommendedAction = "Planner model access is verified for this account."
	return snapshot
}

func executorModelHealthSnapshot(ctx context.Context, inv Invocation, latestRun *state.Run, performTest bool) controlModelComponentSnapshot {
	snapshot := controlModelComponentSnapshot{
		Component:            "executor",
		ConfiguredModel:      config.RequiredCodexExecutorModel,
		RequestedModel:       config.RequiredCodexExecutorModel,
		VerificationState:    "not_verified",
		AccessMode:           fmt.Sprintf("%s sandbox, approval %s", config.RequiredCodexSandboxMode, config.RequiredCodexApprovalPolicy),
		Effort:               config.RequiredCodexReasoningEffort,
		TestPerformed:        performTest,
		CodexModelConfigured: config.RequiredCodexExecutorModel,
		PlainEnglish:         "Codex is required to run gpt-5.5 with full autonomous access, but this has not been verified in this session.",
		RecommendedAction:    "Use Test Codex Config before a long unattended run. The check uses the same Codex command path and full-access settings required by the executor.",
	}
	if performTest {
		snapshot.LastTestedAt = formatSnapshotTime(time.Now().UTC())
	}

	var envErr error
	if env, err := inspectCodexEnvironment(ctx); err == nil {
		applyCodexEnvironment(&snapshot, env)
	} else {
		envErr = err
		snapshot.VerificationState = "unavailable"
		snapshot.LastError = err.Error()
		snapshot.CodexLastProbeError = err.Error()
		snapshot.PlainEnglish = "Codex could not be resolved from the orchestrator process environment."
		snapshot.RecommendedAction = "Install/update Codex, ensure the intended codex command is on PATH, restart the control server, then test again."
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
			if !performTest {
				snapshot.VerificationState = "invalid"
				snapshot.LastError = errorText
				snapshot.ModelUnavailable = true
				snapshot.PlainEnglish = fmt.Sprintf("Codex could not start because the configured model %s is not available to this account. No code changes were made by that executor turn.", valueOrUnavailable(snapshot.RequestedModel))
				snapshot.RecommendedAction = "Change the Codex model in Codex/OpenAI configuration, test it, then continue the run. No silent fallback will be used."
				return snapshot
			}
			snapshot.LastError = errorText
		}
		if errorText != "" && strings.TrimSpace(latestRun.LatestStopReason) == "executor_failed" {
			if !performTest {
				snapshot.VerificationState = "executor_failed"
				snapshot.LastError = errorText
				snapshot.PlainEnglish = "The latest Codex executor turn failed before completing."
				snapshot.RecommendedAction = "Review Live Output and Codex configuration, then continue only after the failure is understood."
				return snapshot
			}
			snapshot.LastError = errorText
		}
	}

	if envErr != nil {
		return snapshot
	}

	if !performTest {
		if snapshot.VerificationState == "not_verified" && snapshot.CodexExecutablePath != "" {
			snapshot.VerificationState = "launch_ready_model_not_verified"
			snapshot.ResolvedModel = config.RequiredCodexExecutorModel
			snapshot.PlainEnglish = fmt.Sprintf("Codex launch path is available at %s, but gpt-5.5/full-access has not been probed in this control-server session.", snapshot.CodexExecutablePath)
			snapshot.RecommendedAction = "Use Test Codex Config after Codex updates or before serious autonomous work; restart the control server if the binary was updated."
		}
		return snapshot
	}

	probe, err := runRequiredCodexExecProbe(ctx, inv.RepoRoot)
	applyCodexEnvironment(&snapshot, probe.Environment)
	if err != nil || !probe.Success {
		errorText := strings.TrimSpace(probe.Error)
		if errorText == "" && err != nil {
			errorText = err.Error()
		}
		if errorText == "" {
			errorText = "Codex probe failed"
		}
		snapshot.VerificationState = modelVerificationStateFromError(errors.New(errorText))
		if snapshot.VerificationState == "verified" {
			snapshot.VerificationState = "unavailable"
		}
		snapshot.LastError = errorText
		snapshot.CodexLastProbeError = errorText
		snapshot.ModelUnavailable = modelUnavailableFromText(errorText)
		snapshot.PlainEnglish = fmt.Sprintf("Codex probe failed using %s with model %s and full autonomous access.", valueOrUnavailable(snapshot.CodexExecutablePath), config.RequiredCodexExecutorModel)
		snapshot.RecommendedAction = "Verify this same Codex path works in the target repo, restart any old control server/app-server process after Codex updates, then test again. No fallback model will be used."
		return snapshot
	}

	snapshot.VerificationState = "verified"
	snapshot.ResolvedModel = config.RequiredCodexExecutorModel
	snapshot.VerifiedModel = config.RequiredCodexExecutorModel
	snapshot.CodexModelVerified = true
	snapshot.CodexPermissionModeVerified = true
	snapshot.ModelUnavailable = false
	snapshot.LastError = ""
	snapshot.CodexLastProbeError = ""
	snapshot.PlainEnglish = fmt.Sprintf("Codex %s verified %s with %s, approval %s, and reasoning effort %s in %s.", valueOrUnavailable(snapshot.CodexVersion), config.RequiredCodexExecutorModel, config.RequiredCodexSandboxMode, config.RequiredCodexApprovalPolicy, config.RequiredCodexReasoningEffort, inv.RepoRoot)
	snapshot.RecommendedAction = "Codex executor model and full autonomous access are verified for this control-server environment."
	return snapshot
}

func combineModelHealth(planner, executor controlModelComponentSnapshot) controlModelHealthSnapshot {
	plannerVerified := plannerModelComponentVerified(planner)
	executorVerified := executorModelComponentVerified(executor)
	needsAttention := !plannerVerified || !executorVerified
	blocking := planner.VerificationState == "invalid" ||
		executor.ModelUnavailable ||
		executor.VerificationState == "invalid" ||
		executor.VerificationState == "unavailable"
	message := "Model health has not been fully verified. Use the test actions before long unattended runs."
	if blocking {
		message = "Configured planner/Codex model requirements are not satisfied. Fix and test before continuing."
	} else if needsAttention {
		message = "Model or Codex configuration needs attention before unattended operation."
	} else if plannerVerified && executorVerified {
		message = "Planner model and Codex executor model/full-access mode are verified."
	} else if planner.VerificationState == "verified" && strings.HasPrefix(executor.VerificationState, "launch_ready") {
		message = "Planner model is verified and Codex launch path is available; Codex full-access probe has not been run in this session."
	}
	return controlModelHealthSnapshot{
		Planner:        planner,
		Executor:       executor,
		NeedsAttention: needsAttention,
		Blocking:       blocking,
		Message:        message,
	}
}

func plannerModelComponentVerified(component controlModelComponentSnapshot) bool {
	if component.VerificationState != "verified" {
		return false
	}
	for _, model := range []string{component.VerifiedModel, component.ResolvedModel, component.RequestedModel, component.ConfiguredModel} {
		model = strings.TrimSpace(model)
		if model == "" || isLatestGPT5Alias(model) {
			continue
		}
		return !plannerModelBelowMinimum(model)
	}
	return false
}

func executorModelComponentVerified(component controlModelComponentSnapshot) bool {
	if component.VerificationState != "verified" {
		return false
	}
	model := strings.TrimSpace(firstNonEmpty(component.VerifiedModel, component.ResolvedModel, component.RequestedModel, component.ConfiguredModel, component.CodexModelConfigured))
	if model != config.RequiredCodexExecutorModel {
		return false
	}
	access := strings.ToLower(component.AccessMode)
	effort := strings.ToLower(strings.TrimSpace(component.Effort))
	return component.CodexModelVerified &&
		component.CodexPermissionModeVerified &&
		strings.Contains(access, config.RequiredCodexSandboxMode) &&
		strings.Contains(access, "approval "+config.RequiredCodexApprovalPolicy) &&
		(effort == "" || effort == config.RequiredCodexReasoningEffort)
}

func applyCodexEnvironment(snapshot *controlModelComponentSnapshot, env appserver.CodexEnvironment) {
	if snapshot == nil {
		return
	}
	snapshot.CodexExecutablePath = strings.TrimSpace(env.CodexPath)
	snapshot.CodexVersion = strings.TrimSpace(env.CodexVersion)
	snapshot.CodexConfigSource = strings.TrimSpace(env.ConfigSource)
	snapshot.CodexAppServerCommand = strings.TrimSpace(env.AppServerCommand)
	snapshot.CodexAppServerArgs = strings.Join(env.AppServerArgs, " ")
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

func valueOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fallback)
}

var gpt5VersionPattern = regexp.MustCompile(`(?i)^gpt-5(?:\.([0-9]+))?(?:-|$)`)

func plannerModelBelowMinimum(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" || isLatestGPT5Alias(normalized) {
		return false
	}
	match := gpt5VersionPattern.FindStringSubmatch(normalized)
	if len(match) == 0 {
		return false
	}
	if len(match) < 2 || strings.TrimSpace(match[1]) == "" {
		return true
	}
	minor, err := strconv.Atoi(strings.TrimSpace(match[1]))
	if err != nil {
		return false
	}
	return minor < 4
}
