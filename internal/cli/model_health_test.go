package cli

import (
	"context"
	"errors"
	"testing"

	"orchestrator/internal/config"
	"orchestrator/internal/executor/appserver"
)

func TestPlannerModelBelowMinimumInvalid(t *testing.T) {
	t.Parallel()

	inv := Invocation{Config: config.Config{PlannerModel: "gpt-5.1"}}
	snapshot := plannerModelHealthSnapshot(context.Background(), inv, false, "")
	if snapshot.VerificationState != "invalid" {
		t.Fatalf("VerificationState = %q, want invalid", snapshot.VerificationState)
	}
	if !plannerModelBelowMinimum("gpt-5.1") {
		t.Fatal("plannerModelBelowMinimum(gpt-5.1) = false, want true")
	}
	if plannerModelBelowMinimum("gpt-5.4") {
		t.Fatal("plannerModelBelowMinimum(gpt-5.4) = true, want false")
	}
	if plannerModelBelowMinimum("gpt-5.10") {
		t.Fatal("plannerModelBelowMinimum(gpt-5.10) = true, want false")
	}
}

func TestPlannerModelHealthUsesResponsesProbe(t *testing.T) {
	restoreProbe := runPlannerModelProbe
	t.Cleanup(func() {
		runPlannerModelProbe = restoreProbe
	})
	t.Setenv("OPENAI_API_KEY", "sk-test")

	var called bool
	runPlannerModelProbe = func(_ context.Context, apiKey string, model string) (plannerModelProbeResult, error) {
		called = true
		if apiKey != "sk-test" {
			t.Fatalf("apiKey = %q, want sk-test", apiKey)
		}
		if model != "gpt-5.4" {
			t.Fatalf("model = %q, want gpt-5.4", model)
		}
		return plannerModelProbeResult{
			RequestedModel: model,
			VerifiedModel:  model,
			ResponseID:     "resp_model_probe",
		}, nil
	}

	snapshot := plannerModelHealthSnapshot(context.Background(), Invocation{
		Config: config.Config{PlannerModel: "gpt-5.4"},
	}, true, "")
	if !called {
		t.Fatal("planner model probe was not called")
	}
	if snapshot.VerificationState != "verified" {
		t.Fatalf("VerificationState = %q, want verified", snapshot.VerificationState)
	}
	if snapshot.VerifiedModel != "gpt-5.4" {
		t.Fatalf("VerifiedModel = %q, want gpt-5.4", snapshot.VerifiedModel)
	}
	if !snapshot.TestPerformed {
		t.Fatal("TestPerformed = false, want true")
	}
}

func TestPlannerLatestAliasResolvesThenUsesResponsesProbe(t *testing.T) {
	restoreLookup := latestGPT5Lookup
	restoreProbe := runPlannerModelProbe
	t.Cleanup(func() {
		latestGPT5Lookup = restoreLookup
		runPlannerModelProbe = restoreProbe
		resetLatestGPT5CacheForTest()
	})
	t.Setenv("OPENAI_API_KEY", "sk-test")

	latestGPT5Lookup = func(_ context.Context, apiKey string) (string, error) {
		if apiKey != "sk-test" {
			t.Fatalf("apiKey = %q, want sk-test", apiKey)
		}
		return "gpt-5.5", nil
	}
	runPlannerModelProbe = func(_ context.Context, apiKey string, model string) (plannerModelProbeResult, error) {
		if apiKey != "sk-test" {
			t.Fatalf("apiKey = %q, want sk-test", apiKey)
		}
		if model != "gpt-5.5" {
			t.Fatalf("model = %q, want resolved gpt-5.5", model)
		}
		return plannerModelProbeResult{RequestedModel: model, VerifiedModel: model}, nil
	}

	snapshot := plannerModelHealthSnapshot(context.Background(), Invocation{
		Config: config.Config{PlannerModel: config.PlannerModelLatestGPT5},
	}, true, "")
	if snapshot.VerificationState != "verified" {
		t.Fatalf("VerificationState = %q, want verified", snapshot.VerificationState)
	}
	if snapshot.ConfiguredModel != config.PlannerModelLatestGPT5 {
		t.Fatalf("ConfiguredModel = %q, want latest alias", snapshot.ConfiguredModel)
	}
	if snapshot.RequestedModel != "gpt-5.5" || snapshot.ResolvedModel != "gpt-5.5" || snapshot.VerifiedModel != "gpt-5.5" {
		t.Fatalf("model fields = requested %q resolved %q verified %q, want gpt-5.5", snapshot.RequestedModel, snapshot.ResolvedModel, snapshot.VerifiedModel)
	}
}

func TestExecutorModelHealthProbeSuccess(t *testing.T) {
	restoreInspect := inspectCodexEnvironment
	restoreProbe := runRequiredCodexExecProbe
	t.Cleanup(func() {
		inspectCodexEnvironment = restoreInspect
		runRequiredCodexExecProbe = restoreProbe
	})

	env := appserver.CodexEnvironment{
		CodexPath:        `C:\Users\me\AppData\Roaming\npm\codex.cmd`,
		CodexVersion:     "codex-cli 0.124.0",
		AppServerCommand: `C:\Program Files\nodejs\node.exe`,
		AppServerArgs:    []string{`C:\Users\me\AppData\Roaming\npm\node_modules\@openai\codex\bin\codex.js`, "app-server", "--listen", "stdio://"},
		ConfigSource:     `C:\Users\me\.codex\config.toml`,
	}
	inspectCodexEnvironment = func(context.Context) (appserver.CodexEnvironment, error) {
		return env, nil
	}
	runRequiredCodexExecProbe = func(context.Context, string) (appserver.CodexExecProbeResult, error) {
		return appserver.CodexExecProbeResult{Environment: env, Success: true}, nil
	}

	snapshot := executorModelHealthSnapshot(context.Background(), Invocation{RepoRoot: `D:\repo`}, nil, true)
	if snapshot.VerificationState != "verified" {
		t.Fatalf("VerificationState = %q, want verified", snapshot.VerificationState)
	}
	if snapshot.VerifiedModel != config.RequiredCodexExecutorModel {
		t.Fatalf("VerifiedModel = %q, want %s", snapshot.VerifiedModel, config.RequiredCodexExecutorModel)
	}
	if !snapshot.CodexModelVerified {
		t.Fatal("CodexModelVerified = false, want true")
	}
	if !snapshot.CodexPermissionModeVerified {
		t.Fatal("CodexPermissionModeVerified = false, want true")
	}
	if snapshot.CodexExecutablePath == "" || snapshot.CodexVersion == "" {
		t.Fatalf("Codex env fields missing: %#v", snapshot)
	}
}

func TestExecutorModelHealthProbeFailure(t *testing.T) {
	restoreInspect := inspectCodexEnvironment
	restoreProbe := runRequiredCodexExecProbe
	t.Cleanup(func() {
		inspectCodexEnvironment = restoreInspect
		runRequiredCodexExecProbe = restoreProbe
	})

	env := appserver.CodexEnvironment{CodexPath: `C:\codex.cmd`, CodexVersion: "codex-cli 0.124.0"}
	inspectCodexEnvironment = func(context.Context) (appserver.CodexEnvironment, error) {
		return env, nil
	}
	runRequiredCodexExecProbe = func(context.Context, string) (appserver.CodexExecProbeResult, error) {
		return appserver.CodexExecProbeResult{
			Environment: env,
			Error:       "The model `gpt-5.5` does not exist or you do not have access to it.",
		}, errors.New("probe failed")
	}

	snapshot := executorModelHealthSnapshot(context.Background(), Invocation{RepoRoot: `D:\repo`}, nil, true)
	if snapshot.VerificationState != "invalid" {
		t.Fatalf("VerificationState = %q, want invalid", snapshot.VerificationState)
	}
	if !snapshot.ModelUnavailable {
		t.Fatal("ModelUnavailable = false, want true")
	}
	if snapshot.CodexLastProbeError == "" {
		t.Fatal("CodexLastProbeError is empty")
	}
}
