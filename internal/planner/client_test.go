package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientPlanBuildsExpectedResponsesRequest(t *testing.T) {
	var captured createRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %s", got)
		}

		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "id":"resp_123",
		  "output":[
		    {
		      "type":"message",
		      "role":"assistant",
		      "content":[
		        {
		          "type":"output_text",
		          "text":"{\"contract_version\":\"planner.v1\",\"outcome\":\"pause\",\"pause\":{\"reason\":\"Waiting for a human decision before continuing.\"}}"
		        }
		      ]
		    }
		  ]
		}`))
	}))
	defer server.Close()

	client := Client{
		APIKey:     "test-key",
		Model:      "gpt-5.1",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	result, err := client.Plan(context.Background(), validInputEnvelope(), "resp_prev")
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	if result.ResponseID != "resp_123" {
		t.Fatalf("unexpected response id: %s", result.ResponseID)
	}
	if result.Output.Outcome != OutcomePause {
		t.Fatalf("unexpected outcome: %s", result.Output.Outcome)
	}
	if captured.Model != "gpt-5.1" {
		t.Fatalf("unexpected model: %s", captured.Model)
	}
	if captured.PreviousResponseID != "resp_prev" {
		t.Fatalf("unexpected previous_response_id: %s", captured.PreviousResponseID)
	}
	if !captured.Store {
		t.Fatal("expected store=true")
	}
	if !strings.Contains(captured.Instructions, "You are the planner for the orchestrator.") {
		t.Fatalf("instructions missing planner contract: %s", captured.Instructions)
	}
	if len(captured.Input) != 1 || captured.Input[0].Role != "user" {
		t.Fatalf("unexpected input payload: %#v", captured.Input)
	}
	if !strings.Contains(captured.Input[0].Content, `"run_id": "run_123"`) {
		t.Fatalf("input packet missing run id: %s", captured.Input[0].Content)
	}
	if captured.Text.Format.Type != "json_schema" || !captured.Text.Format.Strict {
		t.Fatalf("unexpected text.format: %#v", captured.Text.Format)
	}
	if captured.Text.Format.Name != OutputSchemaName {
		t.Fatalf("unexpected schema name: %s", captured.Text.Format.Name)
	}
}

func TestClientPlanRejectsInvalidPlannerOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "id":"resp_invalid",
		  "output":[
		    {
		      "type":"message",
		      "role":"assistant",
		      "content":[
		        {
		          "type":"output_text",
		          "text":"{\"contract_version\":\"planner.v1\",\"outcome\":\"complete\"}"
		        }
		      ]
		    }
		  ]
		}`))
	}))
	defer server.Close()

	client := Client{
		APIKey:     "test-key",
		Model:      "gpt-5.1",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	_, err := client.Plan(context.Background(), validInputEnvelope(), "")
	if err == nil {
		t.Fatal("Plan unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "planner output failed planner.v1 validation") {
		t.Fatalf("expected planner validation error, got: %v", err)
	}
}

func TestClientPlanFailsClearlyWithoutAPIKey(t *testing.T) {
	client := Client{
		Model: "gpt-5.1",
	}

	_, err := client.Plan(context.Background(), validInputEnvelope(), "")
	if err == nil {
		t.Fatal("Plan unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY is required") {
		t.Fatalf("expected missing api key error, got: %v", err)
	}
}
