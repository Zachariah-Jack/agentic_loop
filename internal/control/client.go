package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"orchestrator/internal/activity"
)

const defaultClientTimeout = 30 * time.Second

var ErrStopStream = errors.New("stop event stream")

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

type StreamEventsParams struct {
	FromSequence int64
	RunID        string
}

func (c Client) Call(ctx context.Context, action string, payload any) (ResponseEnvelope, error) {
	rawPayload, err := marshalPayload(payload)
	if err != nil {
		return ResponseEnvelope{}, err
	}

	body, err := json.Marshal(RequestEnvelope{
		ID:      fmt.Sprintf("req_%d", time.Now().UTC().UnixNano()),
		Type:    "request",
		Action:  strings.TrimSpace(action),
		Payload: rawPayload,
	})
	if err != nil {
		return ResponseEnvelope{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL(), "/")+"/v2/control", strings.NewReader(string(body)))
	if err != nil {
		return ResponseEnvelope{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return ResponseEnvelope{}, err
	}
	defer resp.Body.Close()

	var envelope ResponseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return ResponseEnvelope{}, err
	}
	if !envelope.OK {
		if envelope.Error != nil && strings.TrimSpace(envelope.Error.Message) != "" {
			return envelope, errors.New(strings.TrimSpace(envelope.Error.Message))
		}
		return envelope, fmt.Errorf("control action %s failed", strings.TrimSpace(action))
	}
	return envelope, nil
}

func (c Client) StreamEvents(ctx context.Context, params StreamEventsParams, handle func(activity.Event) error) error {
	if handle == nil {
		return errors.New("stream handler is required")
	}

	query := url.Values{}
	if params.FromSequence > 0 {
		query.Set("from_sequence", fmt.Sprintf("%d", params.FromSequence))
	}
	if strings.TrimSpace(params.RunID) != "" {
		query.Set("run_id", strings.TrimSpace(params.RunID))
	}

	endpoint := strings.TrimRight(c.baseURL(), "/") + "/v2/events"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := c.streamHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("event stream returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var event activity.Event
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := handle(event); err != nil {
			if errors.Is(err, ErrStopStream) {
				return nil
			}
			return err
		}
	}
}

func (c Client) baseURL() string {
	baseURL := strings.TrimSpace(c.BaseURL)
	if baseURL == "" {
		return "http://127.0.0.1:44777"
	}
	if strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://") {
		return baseURL
	}
	return "http://" + baseURL
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultClientTimeout}
}

func (c Client) streamHTTPClient() *http.Client {
	if c.HTTPClient == nil {
		return &http.Client{}
	}

	clone := *c.HTTPClient
	clone.Timeout = 0
	return &clone
}

func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return json.RawMessage(`{}`), nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}
