package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"orchestrator/internal/config"
	"orchestrator/internal/journal"
	"orchestrator/internal/orchestration"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

func TestNTFYHumanInteractorUsesNTFYReply(t *testing.T) {
	t.Parallel()

	var authHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_question","event":"message","topic":"orchestrator-reply","message":"published"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/orchestrator-reply/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"open_1","event":"open","topic":"orchestrator-reply"}`+"\n")
			_, _ = io.WriteString(w, `{"id":"reply_1","event":"message","topic":"orchestrator-reply","message":"raw ntfy reply"}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	journalWriter, err := journal.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatalf("journal.Open() error = %v", err)
	}

	var output bytes.Buffer
	interactor := newHumanInteractor(Invocation{
		Stdout: &output,
		Stdin:  strings.NewReader("terminal fallback should not run\n"),
		Config: config.Config{
			NTFY: config.NTFYConfig{
				ServerURL: server.URL,
				Topic:     "orchestrator-reply",
				AuthToken: "tk_testtoken",
			},
		},
	}, journalWriter)

	reply, err := interactor.Ask(context.Background(), state.Run{
		ID:       "run_123",
		RepoPath: root,
		Goal:     "wait for one ntfy reply",
		Status:   state.StatusInitialized,
	}, planner.AskHumanOutcome{
		Question: "Which file should we inspect next?",
		Context:  "Reply with one exact repo-relative path.",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}

	if reply.Source != "ntfy" {
		t.Fatalf("reply.Source = %q, want ntfy", reply.Source)
	}
	if reply.Payload != "raw ntfy reply" {
		t.Fatalf("reply.Payload = %q, want raw ntfy reply", reply.Payload)
	}
	if authHeader != "Bearer tk_testtoken" {
		t.Fatalf("Authorization header = %q, want bearer token", authHeader)
	}

	for _, want := range []string{
		"planner_question_delivery: ntfy",
		"ntfy.topic: orchestrator-reply",
		"ntfy.wait: waiting for one raw reply message",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q\n%s", want, output.String())
		}
	}

	events, err := journalWriter.ReadRecent("run_123", 10)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	for _, want := range []string{
		"ntfy.question.published",
		"ntfy.wait.started",
		"ntfy.reply.received",
	} {
		if countJournalEvents(events, want) == 0 {
			t.Fatalf("journal missing %q event", want)
		}
	}
}

func TestNTFYHumanInteractorFallsBackToTerminalOnWaitFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_question","event":"message","topic":"orchestrator-reply","message":"published"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/orchestrator-reply/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"open_1","event":"open","topic":"orchestrator-reply"}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	journalWriter, err := journal.Open(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatalf("journal.Open() error = %v", err)
	}

	var output bytes.Buffer
	interactor := newHumanInteractor(Invocation{
		Stdout: &output,
		Stdin:  strings.NewReader("  terminal fallback reply  \n"),
		Config: config.Config{
			NTFY: config.NTFYConfig{
				ServerURL: server.URL,
				Topic:     "orchestrator-reply",
			},
		},
	}, journalWriter)

	reply, err := interactor.Ask(context.Background(), state.Run{
		ID:       "run_456",
		RepoPath: root,
		Goal:     "fallback to terminal on ntfy wait failure",
		Status:   state.StatusInitialized,
	}, planner.AskHumanOutcome{
		Question: "What should we do next?",
	})
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}

	if reply.Source != "terminal" {
		t.Fatalf("reply.Source = %q, want terminal", reply.Source)
	}
	if reply.Payload != "  terminal fallback reply  \n" {
		t.Fatalf("reply.Payload = %q, want terminal fallback payload", reply.Payload)
	}

	for _, want := range []string{
		"planner_question_delivery: terminal_fallback",
		"ntfy.stop_reason: ntfy_failed_terminal_fallback_used",
		"planner_question:",
		"human_reply> ",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q\n%s", want, output.String())
		}
	}

	events, err := journalWriter.ReadRecent("run_456", 10)
	if err != nil {
		t.Fatalf("ReadRecent() error = %v", err)
	}
	if countJournalEvents(events, "ntfy.wait.failed") == 0 {
		t.Fatal("journal missing ntfy.wait.failed event")
	}
	if latestJournalEvent(events, "ntfy.wait.failed").StopReason != orchestration.StopReasonNTFYFallbackUsed {
		t.Fatalf("ntfy.wait.failed stop reason = %q, want %q", latestJournalEvent(events, "ntfy.wait.failed").StopReason, orchestration.StopReasonNTFYFallbackUsed)
	}
}

func TestNTFYBridgeState(t *testing.T) {
	t.Parallel()

	if got := ntfyBridgeState(config.Config{}); got != "terminal fallback only" {
		t.Fatalf("ntfyBridgeState() = %q, want terminal fallback only", got)
	}

	if got := ntfyBridgeState(config.Config{
		NTFY: config.NTFYConfig{
			ServerURL: "https://ntfy.example.com",
			Topic:     "orchestrator-reply",
		},
	}); got != "ready" {
		t.Fatalf("ntfyBridgeState() = %q, want ready", got)
	}
}
