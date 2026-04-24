package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"orchestrator/internal/config"
)

const (
	latestGPT5CacheTTL      = time.Hour
	latestGPT5LookupTimeout = 5 * time.Second
)

var (
	latestGPT5ModelsEndpoint = "https://api.openai.com/v1/models"
	latestGPT5HTTPClient     = &http.Client{Timeout: latestGPT5LookupTimeout}
	latestGPT5Lookup         = lookupLatestGPT5Model
	latestGPT5Cache          = struct {
		sync.Mutex
		model   string
		expires time.Time
	}{}
)

func resolvePlannerModel(inv Invocation) string {
	if model := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); model != "" {
		return model
	}
	if model := strings.TrimSpace(currentConfig(inv).PlannerModel); model != "" {
		return model
	}
	return config.PlannerModelLatestGPT5
}

func resolvePlannerAPIModel(ctx context.Context, inv Invocation) string {
	model := strings.TrimSpace(resolvePlannerModel(inv))
	if !isLatestGPT5Alias(model) {
		return model
	}
	return latestGPT5Model(ctx, plannerAPIKey())
}

func plannerAPIKey() string {
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
}

func isLatestGPT5Alias(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case config.PlannerModelLatestGPT5, "latest", "gpt5-latest":
		return true
	default:
		return false
	}
}

func latestGPT5Model(ctx context.Context, apiKey string) string {
	if strings.TrimSpace(apiKey) == "" {
		return config.PlannerModelLatestGPT5
	}

	now := time.Now().UTC()
	latestGPT5Cache.Lock()
	if latestGPT5Cache.model != "" && now.Before(latestGPT5Cache.expires) {
		model := latestGPT5Cache.model
		latestGPT5Cache.Unlock()
		return model
	}
	latestGPT5Cache.Unlock()

	model, err := latestGPT5Lookup(ctx, apiKey)
	if err != nil || strings.TrimSpace(model) == "" {
		return config.PlannerModelLatestGPT5
	}

	latestGPT5Cache.Lock()
	latestGPT5Cache.model = model
	latestGPT5Cache.expires = now.Add(latestGPT5CacheTTL)
	latestGPT5Cache.Unlock()
	return model
}

func lookupOpenAIModel(ctx context.Context, apiKey string, model string) error {
	apiKey = strings.TrimSpace(apiKey)
	model = strings.TrimSpace(model)
	if apiKey == "" {
		return errors.New("openai api key is required for model verification")
	}
	if model == "" {
		return errors.New("model is required for verification")
	}

	ctx, cancel := context.WithTimeout(ctx, latestGPT5LookupTimeout)
	defer cancel()

	endpoint := strings.TrimRight(latestGPT5ModelsEndpoint, "/") + "/" + url.PathEscape(model)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := latestGPT5HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("models api returned HTTP %d for model %s", resp.StatusCode, model)
	}
	return nil
}

func lookupLatestGPT5Model(ctx context.Context, apiKey string) (string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return "", errors.New("openai api key is required for model discovery")
	}

	ctx, cancel := context.WithTimeout(ctx, latestGPT5LookupTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestGPT5ModelsEndpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))

	resp, err := latestGPT5HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("models api returned HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", err
	}

	ids := make([]string, 0, len(envelope.Data))
	for _, item := range envelope.Data {
		ids = append(ids, item.ID)
	}
	if model := selectLatestMainlineGPT5Model(ids); model != "" {
		return model, nil
	}
	return "", errors.New("no versioned mainline gpt-5 model was returned by the models api")
}

func selectLatestMainlineGPT5Model(ids []string) string {
	bestModel := ""
	bestMinor := -1
	for _, id := range ids {
		minor, ok := mainlineGPT5MinorVersion(id)
		if !ok || minor <= bestMinor {
			continue
		}
		bestMinor = minor
		bestModel = strings.TrimSpace(id)
	}
	return bestModel
}

func mainlineGPT5MinorVersion(id string) (int, bool) {
	normalized := strings.ToLower(strings.TrimSpace(id))
	if !strings.HasPrefix(normalized, "gpt-5.") {
		return 0, false
	}

	suffix := strings.TrimPrefix(normalized, "gpt-5.")
	if suffix == "" {
		return 0, false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return 0, false
		}
	}

	minor, err := strconv.Atoi(suffix)
	if err != nil {
		return 0, false
	}
	return minor, true
}

func resetLatestGPT5CacheForTest() {
	latestGPT5Cache.Lock()
	latestGPT5Cache.model = ""
	latestGPT5Cache.expires = time.Time{}
	latestGPT5Cache.Unlock()
}
