package autofill

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const defaultResponsesBaseURL = "https://api.openai.com/v1"
const defaultHTTPTimeout = 2 * time.Minute

var ErrMissingAPIKey = errors.New("missing OPENAI_API_KEY")
var ErrMissingModel = errors.New("autofill model is required")

type Client struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

type Request struct {
	RepoPath       string         `json:"repo_path"`
	Targets        []string       `json:"targets"`
	Answers        Answers        `json:"answers"`
	ExistingFiles  []ExistingFile `json:"existing_files,omitempty"`
	ReferenceFiles []ExistingFile `json:"reference_files,omitempty"`
	RepoTopLevel   []string       `json:"repo_top_level,omitempty"`
}

type Answers struct {
	ProjectSummary string `json:"project_summary,omitempty"`
	DesiredOutcome string `json:"desired_outcome,omitempty"`
	UsersPlatform  string `json:"users_platform,omitempty"`
	Constraints    string `json:"constraints,omitempty"`
	Milestones     string `json:"milestones,omitempty"`
	Decisions      string `json:"decisions,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

type ExistingFile struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

type DraftFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Summary string `json:"summary,omitempty"`
}

type Result struct {
	ResponseID string
	Message    string
	Files      []DraftFile
	RawOutput  string
}

type createRequest struct {
	Model        string         `json:"model"`
	Instructions string         `json:"instructions"`
	Input        []inputMessage `json:"input"`
	Text         textConfig     `json:"text"`
	Store        bool           `json:"store"`
}

type inputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type textConfig struct {
	Format textFormat `json:"format"`
}

type textFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

type createResponse struct {
	ID                string               `json:"id"`
	Output            []responseOutputItem `json:"output"`
	IncompleteDetails map[string]any       `json:"incomplete_details,omitempty"`
}

type responseOutputItem struct {
	Type    string                `json:"type"`
	Content []responseContentItem `json:"content,omitempty"`
}

type responseContentItem struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

type apiErrorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
}

type outputEnvelope struct {
	Message string      `json:"message"`
	Files   []DraftFile `json:"files"`
}

func (c *Client) Draft(ctx context.Context, request Request) (Result, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return Result{}, ErrMissingAPIKey
	}
	if strings.TrimSpace(c.Model) == "" {
		return Result{}, ErrMissingModel
	}
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}

	packet, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return Result{}, err
	}

	body, err := json.Marshal(createRequest{
		Model:        c.Model,
		Instructions: renderInstructions(),
		Input: []inputMessage{
			{
				Role:    "user",
				Content: string(packet),
			},
		},
		Text: textConfig{
			Format: textFormat{
				Type:   "json_schema",
				Name:   "contract_autofill_v1",
				Schema: outputSchema(request.Targets),
				Strict: true,
			},
		},
		Store: true,
	})
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL(), "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, decodeAPIError(resp)
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}

	var parsed createResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return Result{}, err
	}

	rawOutput, err := extractOutputText(parsed)
	if err != nil {
		return Result{}, err
	}

	var envelope outputEnvelope
	decoder := json.NewDecoder(strings.NewReader(rawOutput))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Result{}, err
	}

	if err := validateResponse(envelope, request.Targets); err != nil {
		return Result{}, err
	}

	return Result{
		ResponseID: strings.TrimSpace(parsed.ID),
		Message:    strings.TrimSpace(envelope.Message),
		Files:      envelope.Files,
		RawOutput:  rawOutput,
	}, nil
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.RepoPath) == "" {
		return errors.New("repo path is required")
	}
	if len(request.Targets) == 0 {
		return errors.New("at least one autofill target is required")
	}
	seen := map[string]bool{}
	for _, target := range request.Targets {
		normalized := strings.TrimSpace(target)
		if normalized == "" {
			return errors.New("autofill target path is required")
		}
		if seen[normalized] {
			return fmt.Errorf("duplicate autofill target %q", normalized)
		}
		seen[normalized] = true
	}
	return nil
}

func validateResponse(envelope outputEnvelope, targets []string) error {
	if strings.TrimSpace(envelope.Message) == "" {
		return errors.New("autofill response message is required")
	}
	if len(envelope.Files) == 0 {
		return errors.New("autofill response must contain at least one file")
	}

	remaining := make(map[string]bool, len(targets))
	for _, target := range targets {
		remaining[strings.TrimSpace(target)] = true
	}

	for _, file := range envelope.Files {
		path := strings.TrimSpace(file.Path)
		if !remaining[path] {
			return fmt.Errorf("autofill returned unexpected path %q", file.Path)
		}
		if strings.TrimSpace(file.Content) == "" {
			return fmt.Errorf("autofill returned empty content for %s", file.Path)
		}
		delete(remaining, path)
	}

	if len(remaining) > 0 {
		missing := make([]string, 0, len(remaining))
		for path := range remaining {
			missing = append(missing, path)
		}
		slices.Sort(missing)
		return fmt.Errorf("autofill response omitted targets: %s", strings.Join(missing, ", "))
	}

	return nil
}

func renderInstructions() string {
	return strings.TrimSpace(`
You are drafting canonical orchestrator contract files for a software repository.

Rules:
- Return only the requested files.
- Use concise, operator-facing markdown.
- Do not expose chain-of-thought or hidden reasoning.
- Preserve useful existing intent when current file contents are provided.
- Be practical and specific rather than generic.
- brief.md should capture what is being built, why it matters, user outcomes, scope, and constraints.
- roadmap.md should give concrete milestones, sequencing, and near-term next steps.
- decisions.md should record current locked decisions, constraints, and unresolved questions in a crisp format.
- human-notes.md should preserve operator-useful notes, assumptions, and loose context in a readable way.
- If AGENTS.md is requested, keep it as repository instructions and guardrails only; do not include secrets.
- Do not mention missing backend systems, hidden thoughts, or meta commentary about the API.
`)
}

func outputSchema(targets []string) map[string]any {
	pathEnum := make([]any, 0, len(targets))
	for _, target := range targets {
		pathEnum = append(pathEnum, strings.TrimSpace(target))
	}

	return strictObject(map[string]any{
		"message": map[string]any{"type": "string"},
		"files": map[string]any{
			"type": "array",
			"items": strictObject(map[string]any{
				"path": map[string]any{
					"type": "string",
					"enum": pathEnum,
				},
				"content": map[string]any{"type": "string"},
				"summary": map[string]any{"type": "string"},
			}, "path", "content", "summary"),
		},
	}, "message", "files")
}

func strictObject(properties map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func extractOutputText(response createResponse) (string, error) {
	texts := make([]string, 0, 1)
	refusals := make([]string, 0, 1)

	for _, item := range response.Output {
		for _, content := range item.Content {
			switch content.Type {
			case "output_text", "text":
				if strings.TrimSpace(content.Text) != "" {
					texts = append(texts, content.Text)
				}
			case "refusal":
				if strings.TrimSpace(content.Refusal) != "" {
					refusals = append(refusals, strings.TrimSpace(content.Refusal))
				}
			}
		}
	}

	if len(texts) > 0 {
		return strings.Join(texts, ""), nil
	}
	if len(refusals) > 0 {
		return "", fmt.Errorf("autofill request refused: %s", strings.Join(refusals, " | "))
	}
	if len(response.IncompleteDetails) > 0 {
		return "", fmt.Errorf("autofill response was incomplete: %v", response.IncompleteDetails)
	}
	return "", errors.New("responses api returned no autofill output text")
}

func decodeAPIError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("responses api returned HTTP %d", resp.StatusCode)
	}

	var apiErr apiErrorEnvelope
	if err := json.Unmarshal(body, &apiErr); err == nil && strings.TrimSpace(apiErr.Error.Message) != "" {
		return fmt.Errorf("responses api returned HTTP %d: %s", resp.StatusCode, apiErr.Error.Message)
	}
	return fmt.Errorf("responses api returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func (c *Client) baseURL() string {
	if strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimSpace(c.BaseURL)
	}
	return defaultResponsesBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}
