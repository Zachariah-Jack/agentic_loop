package ntfy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"orchestrator/internal/config"
)

func TestNewClientRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	for _, tc := range []config.NTFYConfig{
		{},
		{ServerURL: "://bad", Topic: "reply"},
		{ServerURL: "https://ntfy.example.com", Topic: "bad/topic"},
	} {
		if _, err := NewClient(tc); err == nil {
			t.Fatalf("NewClient(%#v) error = nil, want validation failure", tc)
		}
	}
}

func TestPublishQuestionAndWaitForReply(t *testing.T) {
	t.Parallel()

	var authHeader string
	var published publishRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if err := json.Unmarshal(body, &published); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_question","event":"message","topic":"orchestrator-reply","message":"published"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/orchestrator-reply/json":
			if got := r.URL.Query().Get("since"); got != "msg_question" {
				t.Fatalf("since query = %q, want msg_question", got)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"open_1","event":"open","topic":"orchestrator-reply"}`+"\n")
			_, _ = io.WriteString(w, `{"id":"reply_1","event":"message","topic":"orchestrator-reply","message":"raw ntfy reply"}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(config.NTFYConfig{
		ServerURL: server.URL,
		Topic:     "orchestrator-reply",
		AuthToken: "tk_testtoken",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	publishedMessage, err := client.PublishQuestion(context.Background(), Question{
		Question: "Which file should we change next?",
		Context:  "Reply with one exact repo-relative path.",
	})
	if err != nil {
		t.Fatalf("PublishQuestion() error = %v", err)
	}
	if publishedMessage.ID != "msg_question" {
		t.Fatalf("publishedMessage.ID = %q, want msg_question", publishedMessage.ID)
	}
	if authHeader != "Bearer tk_testtoken" {
		t.Fatalf("Authorization header = %q, want bearer token", authHeader)
	}
	if published.Topic != "orchestrator-reply" {
		t.Fatalf("published.Topic = %q, want orchestrator-reply", published.Topic)
	}
	if published.Title != defaultQuestionTitle {
		t.Fatalf("published.Title = %q, want %q", published.Title, defaultQuestionTitle)
	}
	for _, want := range []string{
		"planner_question:",
		"Which file should we change next?",
		"planner_question_context:",
		"Reply with one exact repo-relative path.",
		"reply_instruction:",
	} {
		if !strings.Contains(published.Message, want) {
			t.Fatalf("published message missing %q\n%s", want, published.Message)
		}
	}

	reply, err := client.WaitForReply(context.Background(), publishedMessage.ID)
	if err != nil {
		t.Fatalf("WaitForReply() error = %v", err)
	}
	if reply.ID != "reply_1" {
		t.Fatalf("reply.ID = %q, want reply_1", reply.ID)
	}
	if reply.Payload != "raw ntfy reply" {
		t.Fatalf("reply.Payload = %q, want raw ntfy reply", reply.Payload)
	}
}

func TestHealthCheck(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"healthy":true}`)
	}))
	defer server.Close()

	client, err := NewClient(config.NTFYConfig{
		ServerURL: server.URL,
		Topic:     "orchestrator-reply",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	health, err := client.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if !health.Healthy {
		t.Fatal("health.Healthy = false, want true")
	}
}
