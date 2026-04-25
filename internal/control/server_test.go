package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"orchestrator/internal/activity"
	"orchestrator/internal/runtimecfg"
)

func TestServerHandlesGetStatusSnapshot(t *testing.T) {
	t.Parallel()

	server := Server{
		Actions: ActionSet{
			GetStatusSnapshot: func(_ context.Context, runID string) (any, error) {
				return map[string]any{
					"run": map[string]any{
						"id":     runID,
						"status": "initialized",
					},
				}, nil
			},
		},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v2/control", bytes.NewBufferString(`{
		"id":"req_1",
		"type":"request",
		"action":"get_status_snapshot",
		"payload":{"run_id":"run_123"}
	}`))

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200\n%s", recorder.Code, recorder.Body.String())
	}

	var response ResponseEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !response.OK {
		t.Fatalf("response.OK = false, want true: %#v", response.Error)
	}
	payload, ok := response.Payload.(map[string]any)
	if !ok {
		t.Fatalf("response.Payload = %#v, want object", response.Payload)
	}
	run, ok := payload["run"].(map[string]any)
	if !ok {
		t.Fatalf("payload[run] = %#v, want object", payload["run"])
	}
	if run["id"] != "run_123" {
		t.Fatalf("run[id] = %#v, want run_123", run["id"])
	}
}

func TestServerEventStreamReplaysPublishedEvents(t *testing.T) {
	t.Parallel()

	broker := activity.NewBroker(8)
	broker.Publish("run_started", map[string]any{"run_id": "run_123"})

	server := Server{Broker: broker}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v2/events?from_sequence=0", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do() error = %v", err)
	}
	defer resp.Body.Close()

	done := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(resp.Body).ReadString('\n')
		done <- line
	}()

	select {
	case output := <-done:
		if !strings.Contains(output, `"event":"run_started"`) {
			t.Fatalf("event stream missing run_started\n%s", output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event stream output")
	}
}

func TestServerSetRuntimeConfigRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	server := Server{
		Actions: ActionSet{
			SetRuntimeConfig: func(_ context.Context, patch runtimecfg.Patch) (any, error) {
				return map[string]any{"verbosity": patch.Verbosity}, nil
			},
		},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v2/control", bytes.NewBufferString(`{
		"id":"req_2",
		"type":"request",
		"action":"set_runtime_config",
		"payload":{"verbosity":"trace","planner_model":"gpt-5-latest"}
	}`))

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want 400\n%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"invalid_payload"`) {
		t.Fatalf("response missing invalid_payload\n%s", recorder.Body.String())
	}
}

func TestServerSetRuntimeConfigAcceptsTimeoutPatch(t *testing.T) {
	t.Parallel()

	called := false
	server := Server{
		Actions: ActionSet{
			SetRuntimeConfig: func(_ context.Context, patch runtimecfg.Patch) (any, error) {
				called = true
				if !patch.Timeouts.ExecutorTurnTimeout.Set {
					t.Fatal("ExecutorTurnTimeout.Set = false, want true")
				}
				return map[string]any{"ok": true}, nil
			},
		},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v2/control", bytes.NewBufferString(`{
		"id":"req_timeout",
		"type":"request",
		"action":"set_runtime_config",
		"payload":{"timeouts":{"executor_turn_timeout":null,"human_wait_timeout":"unlimited"}}
	}`))

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200\n%s", recorder.Code, recorder.Body.String())
	}
	if !called {
		t.Fatal("SetRuntimeConfig handler was not called")
	}
}

func TestServerRoutesUpdateActions(t *testing.T) {
	t.Parallel()

	server := Server{
		Actions: ActionSet{
			CheckForUpdates: func(_ context.Context, _ UpdateRequest) (any, error) {
				return map[string]any{"checked": true}, nil
			},
			GetUpdateStatus: func(_ context.Context) (any, error) {
				return map[string]any{"current_version": "v1.1.0-dev"}, nil
			},
			GetUpdateChangelog: func(_ context.Context, _ UpdateRequest) (any, error) {
				return map[string]any{"changelog": "changes"}, nil
			},
		},
	}
	for _, action := range []string{"check_for_updates", "get_update_status", "get_update_changelog"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v2/control", bytes.NewBufferString(`{"type":"request","action":"`+action+`","payload":{}}`))
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status code = %d, want 200\n%s", action, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "unsupported_action") {
			t.Fatalf("%s returned unsupported_action\n%s", action, recorder.Body.String())
		}
	}
}
