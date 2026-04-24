package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"orchestrator/internal/config"
)

func TestSelectLatestMainlineGPT5Model(t *testing.T) {
	got := selectLatestMainlineGPT5Model([]string{
		"gpt-5.4-mini",
		"gpt-5.4",
		"gpt-5.5-pro",
		"gpt-5.5-2026-04-30",
		"gpt-5.10",
		"gpt-5-chat-latest",
		"gpt-5.5",
	})
	if got != "gpt-5.10" {
		t.Fatalf("selectLatestMainlineGPT5Model() = %q, want gpt-5.10", got)
	}
}

func TestResolvePlannerAPIModelDiscoversLatestGPT5Alias(t *testing.T) {
	resetLatestGPT5CacheForTest()
	restoreLookup := latestGPT5Lookup
	latestGPT5Lookup = func(_ context.Context, apiKey string) (string, error) {
		if apiKey != "sk-test" {
			t.Fatalf("apiKey = %q, want sk-test", apiKey)
		}
		return "gpt-5.5", nil
	}
	defer func() {
		latestGPT5Lookup = restoreLookup
		resetLatestGPT5CacheForTest()
	}()

	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_MODEL", "")
	inv := Invocation{Config: config.Config{PlannerModel: config.PlannerModelLatestGPT5}}

	if got := resolvePlannerAPIModel(context.Background(), inv); got != "gpt-5.5" {
		t.Fatalf("resolvePlannerAPIModel() = %q, want gpt-5.5", got)
	}
}

func TestLookupLatestGPT5ModelUsesModelsAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "gpt-5.4"},
				{"id": "gpt-5.5-mini"},
				{"id": "gpt-5.5"},
				{"id": "gpt-5.5-pro"}
			]
		}`))
	}))
	defer server.Close()

	restoreEndpoint := latestGPT5ModelsEndpoint
	restoreClient := latestGPT5HTTPClient
	latestGPT5ModelsEndpoint = server.URL
	latestGPT5HTTPClient = server.Client()
	defer func() {
		latestGPT5ModelsEndpoint = restoreEndpoint
		latestGPT5HTTPClient = restoreClient
	}()

	got, err := lookupLatestGPT5Model(context.Background(), "sk-test")
	if err != nil {
		t.Fatalf("lookupLatestGPT5Model() error = %v", err)
	}
	if got != "gpt-5.5" {
		t.Fatalf("lookupLatestGPT5Model() = %q, want gpt-5.5", got)
	}
}

func TestResolvePlannerAPIModelDoesNotSilentlyFallbackWithoutAPIKey(t *testing.T) {
	resetLatestGPT5CacheForTest()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	inv := Invocation{Config: config.Config{PlannerModel: config.PlannerModelLatestGPT5}}

	if got := resolvePlannerAPIModel(context.Background(), inv); got != config.PlannerModelLatestGPT5 {
		t.Fatalf("resolvePlannerAPIModel() = %q, want unresolved alias %s", got, config.PlannerModelLatestGPT5)
	}
}

func TestResolvePlannerAPIModelDoesNotSilentlyFallbackWhenDiscoveryFails(t *testing.T) {
	resetLatestGPT5CacheForTest()
	restoreLookup := latestGPT5Lookup
	latestGPT5Lookup = func(context.Context, string) (string, error) {
		return "", errors.New("models api unavailable")
	}
	defer func() {
		latestGPT5Lookup = restoreLookup
		resetLatestGPT5CacheForTest()
	}()

	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_MODEL", "")
	inv := Invocation{Config: config.Config{PlannerModel: config.PlannerModelLatestGPT5}}

	if got := resolvePlannerAPIModel(context.Background(), inv); got != config.PlannerModelLatestGPT5 {
		t.Fatalf("resolvePlannerAPIModel() = %q, want unresolved alias %s", got, config.PlannerModelLatestGPT5)
	}
}

func TestResolvePlannerAPIModelKeepsPinnedModel(t *testing.T) {
	resetLatestGPT5CacheForTest()
	calledLookup := false
	restoreLookup := latestGPT5Lookup
	latestGPT5Lookup = func(context.Context, string) (string, error) {
		calledLookup = true
		return "gpt-5.5", nil
	}
	defer func() {
		latestGPT5Lookup = restoreLookup
		resetLatestGPT5CacheForTest()
	}()

	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_MODEL", "")
	inv := Invocation{Config: config.Config{PlannerModel: "gpt-5.4"}}

	if got := resolvePlannerAPIModel(context.Background(), inv); got != "gpt-5.4" {
		t.Fatalf("resolvePlannerAPIModel() = %q, want gpt-5.4", got)
	}
	if calledLookup {
		t.Fatal("latest model lookup was called for a pinned model")
	}
}

func TestOpenAIModelEnvironmentOverrideCanUseLatestAlias(t *testing.T) {
	resetLatestGPT5CacheForTest()
	restoreLookup := latestGPT5Lookup
	latestGPT5Lookup = func(context.Context, string) (string, error) {
		return "gpt-5.6", nil
	}
	defer func() {
		latestGPT5Lookup = restoreLookup
		resetLatestGPT5CacheForTest()
	}()

	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_MODEL", "gpt-5-latest")
	inv := Invocation{Config: config.Config{PlannerModel: "gpt-5.4"}}

	if configured := resolvePlannerModel(inv); configured != config.PlannerModelLatestGPT5 {
		t.Fatalf("resolvePlannerModel() = %q, want %s", configured, config.PlannerModelLatestGPT5)
	}
	if got := resolvePlannerAPIModel(context.Background(), inv); got != "gpt-5.6" {
		t.Fatalf("resolvePlannerAPIModel() = %q, want gpt-5.6", got)
	}
}
