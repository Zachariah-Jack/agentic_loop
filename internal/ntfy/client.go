package ntfy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"orchestrator/internal/config"
)

const (
	defaultQuestionTitle = "orchestrator ask_human"
	defaultQuestionTag   = "orchestrator"
	defaultAskHumanTag   = "ask-human"
)

type Client struct {
	baseURL    string
	topic      string
	authToken  string
	httpClient *http.Client
}

type Question struct {
	Question string
	Context  string
}

type PublishedMessage struct {
	ID string
}

type Reply struct {
	ID      string
	Payload string
}

type HealthStatus struct {
	Healthy bool `json:"healthy"`
}

type publishRequest struct {
	Topic   string   `json:"topic"`
	Title   string   `json:"title,omitempty"`
	Message string   `json:"message"`
	Tags    []string `json:"tags,omitempty"`
}

type messageEnvelope struct {
	ID      string `json:"id"`
	Event   string `json:"event"`
	Topic   string `json:"topic"`
	Message string `json:"message"`
}

func IsConfigured(cfg config.NTFYConfig) bool {
	return strings.TrimSpace(cfg.ServerURL) != "" && strings.TrimSpace(cfg.Topic) != ""
}

func NewClient(cfg config.NTFYConfig) (*Client, error) {
	serverURL := strings.TrimSpace(cfg.ServerURL)
	topic := strings.TrimSpace(cfg.Topic)

	if serverURL == "" {
		return nil, errors.New("ntfy server URL is required")
	}
	if topic == "" {
		return nil, errors.New("ntfy topic is required")
	}

	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("invalid ntfy server URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("ntfy server URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("ntfy server URL must include a host")
	}
	if strings.ContainsAny(topic, "/?#") {
		return nil, errors.New("ntfy topic must not contain /, ?, or #")
	}

	return &Client{
		baseURL:   strings.TrimRight(parsed.String(), "/"),
		topic:     topic,
		authToken: strings.TrimSpace(cfg.AuthToken),
		httpClient: &http.Client{
			Transport: http.DefaultTransport,
		},
	}, nil
}

func (c *Client) ServerURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

func (c *Client) Topic() string {
	if c == nil {
		return ""
	}
	return c.topic
}

func (c *Client) PublishQuestion(ctx context.Context, question Question) (PublishedMessage, error) {
	return c.PublishMessage(ctx, defaultQuestionTitle, renderQuestionMessage(question), []string{defaultQuestionTag, defaultAskHumanTag})
}

func (c *Client) PublishMessage(ctx context.Context, title string, message string, tags []string) (PublishedMessage, error) {
	if c == nil {
		return PublishedMessage{}, errors.New("ntfy client is required")
	}

	payload, err := json.Marshal(publishRequest{
		Topic:   c.topic,
		Title:   strings.TrimSpace(title),
		Message: strings.TrimSpace(message),
		Tags:    tags,
	})
	if err != nil {
		return PublishedMessage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return PublishedMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return PublishedMessage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PublishedMessage{}, responseError(resp)
	}

	var envelope messageEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return PublishedMessage{}, err
	}
	if strings.TrimSpace(envelope.ID) == "" {
		return PublishedMessage{}, errors.New("ntfy publish response did not include a message id")
	}

	return PublishedMessage{ID: envelope.ID}, nil
}

func (c *Client) WaitForReply(ctx context.Context, afterID string) (Reply, error) {
	if c == nil {
		return Reply{}, errors.New("ntfy client is required")
	}
	if strings.TrimSpace(afterID) == "" {
		return Reply{}, errors.New("ntfy after message id is required")
	}

	subscribeURL, err := c.subscribeURL(afterID)
	if err != nil {
		return Reply{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscribeURL, nil)
	if err != nil {
		return Reply{}, err
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Reply{}, responseError(resp)
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var envelope messageEnvelope
			if decodeErr := json.Unmarshal(bytes.TrimSpace(line), &envelope); decodeErr != nil {
				return Reply{}, decodeErr
			}
			if envelope.Event == "message" && envelope.ID != "" && envelope.ID != afterID {
				return Reply{
					ID:      envelope.ID,
					Payload: envelope.Message,
				}, nil
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return Reply{}, errors.New("ntfy wait stream ended before a reply was received")
			}
			return Reply{}, err
		}
	}
}

func (c *Client) HealthCheck(ctx context.Context) (HealthStatus, error) {
	if c == nil {
		return HealthStatus{}, errors.New("ntfy client is required")
	}

	healthURL, err := c.healthURL()
	if err != nil {
		return HealthStatus{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return HealthStatus{}, err
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return HealthStatus{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HealthStatus{}, responseError(resp)
	}

	var health HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return HealthStatus{}, err
	}
	if !health.Healthy {
		return health, errors.New("ntfy health endpoint reported unhealthy=false")
	}

	return health, nil
}

func (c *Client) subscribeURL(afterID string) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = path.Join(parsed.Path, c.topic, "json")

	query := parsed.Query()
	query.Set("since", afterID)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (c *Client) healthURL() (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = path.Join(parsed.Path, "v1", "health")
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func (c *Client) applyAuth(req *http.Request) {
	if c == nil || req == nil {
		return
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
}

func renderQuestionMessage(question Question) string {
	var builder strings.Builder
	builder.WriteString("planner_question:\n")
	builder.WriteString(question.Question)
	if question.Context != "" {
		builder.WriteString("\n\nplanner_question_context:\n")
		builder.WriteString(question.Context)
	}
	builder.WriteString("\n\nreply_instruction:\nPublish one raw reply message to this same topic.")
	return builder.String()
}

func responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("ntfy request failed with status %s", resp.Status)
	}
	return fmt.Errorf("ntfy request failed with status %s: %s", resp.Status, detail)
}
