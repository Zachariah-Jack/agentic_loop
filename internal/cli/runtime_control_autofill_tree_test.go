package cli

import (
	"bytes"
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"orchestrator/internal/activity"
	"orchestrator/internal/autofill"
	"orchestrator/internal/config"
	"orchestrator/internal/runtimecfg"
	"orchestrator/internal/state"
)

type fakeAutofillClient struct {
	result  autofill.Result
	request autofill.Request
	err     error
}

func (f *fakeAutofillClient) Draft(_ context.Context, request autofill.Request) (autofill.Result, error) {
	f.request = request
	if f.err != nil {
		return autofill.Result{}, f.err
	}
	return f.result, nil
}

func TestLocalControlServerRunAIAutofillDraftsCanonicalFiles(t *testing.T) {
	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	mustWriteFile(t, filepath.Join(repoRoot, "README.md"), "# Demo App\n\nThis app needs a better setup brief.\n")
	mustWriteFile(t, filepath.Join(repoRoot, ".orchestrator", "brief.md"), "existing brief\n")

	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	cfg := config.Default()
	cfg.PlannerModel = "gpt-5.2"
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	fakeClient := &fakeAutofillClient{
		result: autofill.Result{
			ResponseID: "resp_autofill",
			Message:    "Drafted the requested contract files from the operator answers.",
			Files: []autofill.DraftFile{
				{
					Path:    ".orchestrator/brief.md",
					Summary: "Crisp app brief",
					Content: "# Brief\n\nBuild the first operator shell.\n",
				},
				{
					Path:    ".orchestrator/roadmap.md",
					Summary: "Sequenced roadmap",
					Content: "# Roadmap\n\n1. Ship the shell.\n",
				},
			},
		},
	}
	restore := newAIAutofillClient
	newAIAutofillClient = func(context.Context, Invocation) autofillDraftingClient { return fakeClient }
	defer func() { newAIAutofillClient = restore }()

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     cfg,
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, cfg),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	response := postControlAction(t, server.URL, `{
		"id":"req_autofill",
		"type":"request",
		"action":"run_ai_autofill",
		"payload":{
			"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`",
			"targets":[".orchestrator/brief.md",".orchestrator/roadmap.md"],
			"answers":{
				"project_summary":"Planner-led operator shell",
				"desired_outcome":"Reduce setup friction",
				"constraints":"Keep engine inert"
			}
		}
	}`)
	if !response.OK {
		t.Fatalf("response.OK = false, error = %#v", response.Error)
	}

	payload := response.Payload.(map[string]any)
	if payload["available"] != true {
		t.Fatalf("available = %#v, want true", payload["available"])
	}
	if payload["model"] != "gpt-5.2" {
		t.Fatalf("model = %#v, want gpt-5.2", payload["model"])
	}
	if payload["response_id"] != "resp_autofill" {
		t.Fatalf("response_id = %#v, want resp_autofill", payload["response_id"])
	}
	files := payload["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	first := files[0].(map[string]any)
	if first["path"] != ".orchestrator/brief.md" {
		t.Fatalf("first.path = %#v, want .orchestrator/brief.md", first["path"])
	}
	if first["existing"] != true {
		t.Fatalf("first.existing = %#v, want true", first["existing"])
	}
	if !strings.Contains(first["content"].(string), "Build the first operator shell") {
		t.Fatalf("first.content = %#v, want draft body", first["content"])
	}

	if fakeClient.request.RepoPath != repoRoot {
		t.Fatalf("request.RepoPath = %q, want %q", fakeClient.request.RepoPath, repoRoot)
	}
	if got := fakeClient.request.Answers.ProjectSummary; got != "Planner-led operator shell" {
		t.Fatalf("Answers.ProjectSummary = %q, want Planner-led operator shell", got)
	}
	if len(fakeClient.request.ExistingFiles) == 0 || fakeClient.request.ExistingFiles[0].Path != ".orchestrator/brief.md" {
		t.Fatalf("ExistingFiles = %#v, want seeded contract file", fakeClient.request.ExistingFiles)
	}
	if !slicesContains(fakeClient.request.RepoTopLevel, "README.md") {
		t.Fatalf("RepoTopLevel = %#v, want README.md", fakeClient.request.RepoTopLevel)
	}
}

func TestLocalControlServerListRepoTreeAndOpenRepoFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRepoMarkerFiles(t, repoRoot)
	mustWriteFile(t, filepath.Join(repoRoot, "cmd", "app.go"), "package main\n")
	mustWriteFile(t, filepath.Join(repoRoot, "notes.txt"), "repo note\n")

	layout := state.ResolveLayout(repoRoot)
	configPath := filepathJoin(t, repoRoot, "config.json")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("config.Save() error = %v", err)
	}

	inv := Invocation{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		RepoRoot:   repoRoot,
		Layout:     layout,
		Config:     config.Default(),
		ConfigPath: configPath,
		RuntimeCfg: runtimecfg.NewManager(configPath, config.Default()),
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Version:    "test",
	}
	server := httptest.NewServer(newLocalControlServer(inv).Handler())
	defer server.Close()

	treeResponse := postControlAction(t, server.URL, `{
		"id":"req_tree",
		"type":"request",
		"action":"list_repo_tree",
		"payload":{"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`","path":"","limit":50}
	}`)
	if !treeResponse.OK {
		t.Fatalf("treeResponse.OK = false, error = %#v", treeResponse.Error)
	}
	treePayload := treeResponse.Payload.(map[string]any)
	if treePayload["read_only"] != true {
		t.Fatalf("read_only = %#v, want true", treePayload["read_only"])
	}
	items := treePayload["items"].([]any)
	if len(items) == 0 {
		t.Fatal("items should include repo entries")
	}

	foundCmd := false
	foundOrchestratorDir := false
	for _, raw := range items {
		item := raw.(map[string]any)
		switch item["path"] {
		case "cmd":
			foundCmd = true
			if item["kind"] != "directory" {
				t.Fatalf("cmd kind = %#v, want directory", item["kind"])
			}
		case ".orchestrator":
			foundOrchestratorDir = true
			if item["kind"] != "directory" {
				t.Fatalf(".orchestrator kind = %#v, want directory", item["kind"])
			}
		}
	}
	if !foundCmd {
		t.Fatal("cmd directory not listed in repo tree")
	}
	if !foundOrchestratorDir {
		t.Fatal(".orchestrator directory not listed in repo tree")
	}

	contractsResponse := postControlAction(t, server.URL, `{
		"id":"req_tree_contracts",
		"type":"request",
		"action":"list_repo_tree",
		"payload":{"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`","path":".orchestrator","limit":50}
	}`)
	if !contractsResponse.OK {
		t.Fatalf("contractsResponse.OK = false, error = %#v", contractsResponse.Error)
	}
	contractsPayload := contractsResponse.Payload.(map[string]any)
	contractItems := contractsPayload["items"].([]any)
	foundBrief := false
	for _, raw := range contractItems {
		item := raw.(map[string]any)
		if item["path"] == ".orchestrator/brief.md" {
			foundBrief = true
			if item["editable_via_contract_editor"] != true {
				t.Fatalf("brief editable_via_contract_editor = %#v, want true", item["editable_via_contract_editor"])
			}
		}
	}
	if !foundBrief {
		t.Fatal(".orchestrator/brief.md not listed inside .orchestrator repo tree view")
	}

	fileResponse := postControlAction(t, server.URL, `{
		"id":"req_repo_file",
		"type":"request",
		"action":"open_repo_file",
		"payload":{"repo_path":"`+strings.ReplaceAll(repoRoot, `\`, `\\`)+`","path":"notes.txt"}
	}`)
	if !fileResponse.OK {
		t.Fatalf("fileResponse.OK = false, error = %#v", fileResponse.Error)
	}
	filePayload := fileResponse.Payload.(map[string]any)
	if filePayload["available"] != true {
		t.Fatalf("available = %#v, want true", filePayload["available"])
	}
	if filePayload["read_only"] != true {
		t.Fatalf("read_only = %#v, want true", filePayload["read_only"])
	}
	if filePayload["editable_via_contract_editor"] != false {
		t.Fatalf("editable_via_contract_editor = %#v, want false", filePayload["editable_via_contract_editor"])
	}
	if filePayload["content_type"] != "text/plain" {
		t.Fatalf("content_type = %#v, want text/plain", filePayload["content_type"])
	}
	if filePayload["content"] != "repo note\n" {
		t.Fatalf("content = %#v, want repo note", filePayload["content"])
	}
}

func slicesContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
