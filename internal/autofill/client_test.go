package autofill

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientDraftReturnsStrictFileSet(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %s, want /responses", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_autofill",
			"output": []map[string]any{
				{
					"type": "message",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": `{
  "message": "Drafted the requested contract files.",
  "files": [
    {
      "path": ".orchestrator/brief.md",
      "content": "# Brief\nBuilt from autofill.\n",
      "summary": "Brief draft"
    },
    {
      "path": ".orchestrator/roadmap.md",
      "content": "# Roadmap\nPhase 1.\n",
      "summary": "Roadmap draft"
    }
  ]
}`,
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := &Client{
		APIKey:  "test-key",
		Model:   "gpt-test",
		BaseURL: server.URL,
	}
	result, err := client.Draft(context.Background(), Request{
		RepoPath: "D:/repo",
		Targets:  []string{".orchestrator/brief.md", ".orchestrator/roadmap.md"},
		Answers: Answers{
			ProjectSummary: "Build a better operator shell.",
			DesiredOutcome: "Helpful contract files.",
		},
	})
	if err != nil {
		t.Fatalf("Draft() error = %v", err)
	}

	if result.ResponseID != "resp_autofill" {
		t.Fatalf("ResponseID = %q, want resp_autofill", result.ResponseID)
	}
	if len(result.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(result.Files))
	}
	if captured["model"] != "gpt-test" {
		t.Fatalf("model = %#v, want gpt-test", captured["model"])
	}
	text := captured["text"].(map[string]any)
	format := text["format"].(map[string]any)
	schema := format["schema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Fatalf("schema.additionalProperties = %#v, want false", schema["additionalProperties"])
	}
}

func TestClientDraftRejectsUnexpectedTarget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_autofill_bad",
			"output": []map[string]any{
				{
					"type": "message",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": `{
  "message": "bad",
  "files": [
    {
      "path": ".orchestrator/decisions.md",
      "content": "# Decisions\n",
      "summary": "bad"
    }
  ]
}`,
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := &Client{
		APIKey:  "test-key",
		Model:   "gpt-test",
		BaseURL: server.URL,
	}
	_, err := client.Draft(context.Background(), Request{
		RepoPath: "D:/repo",
		Targets:  []string{".orchestrator/brief.md"},
		Answers: Answers{
			ProjectSummary: "Build a better operator shell.",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected path") {
		t.Fatalf("Draft() error = %v, want unexpected path", err)
	}
}
