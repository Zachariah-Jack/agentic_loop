package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultResponsesBaseURL = "https://api.openai.com/v1"
const defaultHTTPTimeout = 2 * time.Minute

type Client struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

type Result struct {
	ResponseID string
	Output     OutputEnvelope
	RawOutput  string
}

type createRequest struct {
	Model              string         `json:"model"`
	Instructions       string         `json:"instructions"`
	PreviousResponseID string         `json:"previous_response_id,omitempty"`
	Input              []inputMessage `json:"input"`
	Text               textConfig     `json:"text"`
	Store              bool           `json:"store"`
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
	Status            string               `json:"status,omitempty"`
	Output            []responseOutputItem `json:"output"`
	IncompleteDetails map[string]any       `json:"incomplete_details,omitempty"`
}

type responseOutputItem struct {
	Type    string                `json:"type"`
	Role    string                `json:"role,omitempty"`
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
	Type    string `json:"type,omitempty"`
}

func (c *Client) Plan(ctx context.Context, input InputEnvelope, previousResponseID string) (Result, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return Result{}, ErrMissingAPIKey
	}
	if strings.TrimSpace(c.Model) == "" {
		return Result{}, ErrMissingModel
	}

	body, err := c.buildCreateRequest(input, previousResponseID)
	if err != nil {
		return Result{}, err
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL(), "/")+"/responses", bytes.NewReader(payload))
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
	rawResponse := string(responseBody)

	var parsed createResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return Result{}, NewValidationError(err, rawResponse, "", "")
	}
	if strings.TrimSpace(parsed.ID) == "" {
		return Result{}, NewValidationError(errors.New("responses api returned an empty response id"), rawResponse, "", "")
	}

	rawOutput, err := extractOutputText(parsed)
	if err != nil {
		return Result{}, NewValidationError(err, rawResponse, "", parsed.ID)
	}

	var output OutputEnvelope
	decoder := json.NewDecoder(strings.NewReader(rawOutput))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return Result{}, NewValidationError(fmt.Errorf("planner output was not valid JSON: %w", err), rawResponse, rawOutput, parsed.ID)
	}
	if err := ValidateOutput(output); err != nil {
		return Result{}, NewValidationError(fmt.Errorf("planner output failed planner.v1 validation: %w", err), rawResponse, rawOutput, parsed.ID)
	}

	return Result{
		ResponseID: parsed.ID,
		Output:     output,
		RawOutput:  rawOutput,
	}, nil
}

func (c *Client) buildCreateRequest(input InputEnvelope, previousResponseID string) (createRequest, error) {
	if err := ValidateInput(input); err != nil {
		return createRequest{}, err
	}

	instructions, err := RenderInstructions()
	if err != nil {
		return createRequest{}, err
	}
	packet, err := MarshalInputPacket(input)
	if err != nil {
		return createRequest{}, err
	}

	return createRequest{
		Model:              c.Model,
		Instructions:       instructions,
		PreviousResponseID: strings.TrimSpace(previousResponseID),
		Input: []inputMessage{
			{
				Role:    "user",
				Content: packet,
			},
		},
		Text: textConfig{
			Format: textFormat{
				Type:   "json_schema",
				Name:   OutputSchemaName,
				Schema: OutputJSONSchema(),
				Strict: true,
			},
		},
		Store: true,
	}, nil
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
				if trimmed := strings.TrimSpace(content.Refusal); trimmed != "" {
					refusals = append(refusals, trimmed)
				}
			}
		}
	}

	if len(texts) > 0 {
		return strings.Join(texts, ""), nil
	}
	if len(refusals) > 0 {
		return "", fmt.Errorf("planner refused structured output: %s", strings.Join(refusals, " | "))
	}
	if len(response.IncompleteDetails) > 0 {
		return "", fmt.Errorf("planner response was incomplete: %v", response.IncompleteDetails)
	}

	return "", errors.New("responses api returned no planner output text")
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
		return c.BaseURL
	}
	return defaultResponsesBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}
