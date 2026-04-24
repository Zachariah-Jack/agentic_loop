package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"orchestrator/internal/autofill"
	"orchestrator/internal/control"
)

const (
	maxProtocolRepoFileBytes  = 256 * 1024
	defaultRepoTreeEntryLimit = 200
	maxAutofillReferenceBytes = 64 * 1024
)

type controlAIAutofillSnapshot struct {
	Available   bool                         `json:"available"`
	RepoPath    string                       `json:"repo_path,omitempty"`
	Model       string                       `json:"model,omitempty"`
	Message     string                       `json:"message,omitempty"`
	ResponseID  string                       `json:"response_id,omitempty"`
	GeneratedAt string                       `json:"generated_at,omitempty"`
	Files       []controlAIAutofillDraftFile `json:"files,omitempty"`
}

type controlAIAutofillDraftFile struct {
	Path          string `json:"path"`
	Summary       string `json:"summary,omitempty"`
	Content       string `json:"content"`
	Existing      bool   `json:"existing"`
	ExistingMTime string `json:"existing_mtime,omitempty"`
}

type controlRepoTreeSnapshot struct {
	RepoPath   string                `json:"repo_path,omitempty"`
	Path       string                `json:"path,omitempty"`
	ParentPath string                `json:"parent_path,omitempty"`
	Count      int                   `json:"count"`
	ReadOnly   bool                  `json:"read_only"`
	Items      []controlRepoTreeItem `json:"items,omitempty"`
	Message    string                `json:"message,omitempty"`
}

type controlRepoTreeItem struct {
	Name                      string `json:"name"`
	Path                      string `json:"path"`
	Kind                      string `json:"kind"`
	ReadOnly                  bool   `json:"read_only"`
	EditableViaContractEditor bool   `json:"editable_via_contract_editor"`
	ByteSize                  int64  `json:"byte_size,omitempty"`
	ModifiedAt                string `json:"modified_at,omitempty"`
}

type controlRepoFileSnapshot struct {
	Available                 bool   `json:"available"`
	Path                      string `json:"path"`
	ContentType               string `json:"content_type,omitempty"`
	Content                   string `json:"content,omitempty"`
	ByteSize                  int64  `json:"byte_size,omitempty"`
	Truncated                 bool   `json:"truncated,omitempty"`
	ReadOnly                  bool   `json:"read_only"`
	EditableViaContractEditor bool   `json:"editable_via_contract_editor"`
	Message                   string `json:"message,omitempty"`
}

type autofillDraftingClient interface {
	Draft(context.Context, autofill.Request) (autofill.Result, error)
}

var newAIAutofillClient = func(ctx context.Context, inv Invocation) autofillDraftingClient {
	return &autofill.Client{
		APIKey: plannerAPIKey(),
		Model:  resolvePlannerAPIModel(ctx, inv),
	}
}

func runAIAutofill(ctx context.Context, inv Invocation, request control.RunAIAutofillRequest) (controlAIAutofillSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlAIAutofillSnapshot{}, err
	}

	targets, err := normalizeAutofillTargets(request.Targets)
	if err != nil {
		return controlAIAutofillSnapshot{}, err
	}

	client := newAIAutofillClient(ctx, inv)
	result, err := client.Draft(ctx, autofill.Request{
		RepoPath:       repoRoot,
		Targets:        targets,
		Answers:        autofillAnswersFromMap(request.Answers),
		ExistingFiles:  loadAutofillExistingFiles(repoRoot, targets),
		ReferenceFiles: loadAutofillReferenceFiles(repoRoot, targets),
		RepoTopLevel:   listAutofillRepoTopLevel(repoRoot),
	})
	if err != nil {
		return controlAIAutofillSnapshot{}, err
	}

	files := make([]controlAIAutofillDraftFile, 0, len(result.Files))
	for _, draft := range result.Files {
		item := controlAIAutofillDraftFile{
			Path:    strings.TrimSpace(draft.Path),
			Summary: strings.TrimSpace(draft.Summary),
			Content: draft.Content,
		}
		if info, ok := statRepoFile(repoRoot, draft.Path); ok {
			item.Existing = true
			item.ExistingMTime = formatSnapshotTime(info.ModTime().UTC())
		}
		files = append(files, item)
	}

	snapshot := controlAIAutofillSnapshot{
		Available:   true,
		RepoPath:    repoRoot,
		Model:       resolvePlannerModel(inv),
		Message:     strings.TrimSpace(result.Message),
		ResponseID:  strings.TrimSpace(result.ResponseID),
		GeneratedAt: formatSnapshotTime(time.Now().UTC()),
		Files:       files,
	}

	emitEngineEvent(inv, "contract_autofill_generated", map[string]any{
		"repo_path":    repoRoot,
		"targets":      targets,
		"response_id":  snapshot.ResponseID,
		"generated_at": snapshot.GeneratedAt,
		"file_count":   len(files),
	})

	return snapshot, nil
}

func listRepoTree(_ context.Context, inv Invocation, request control.RepoTreeRequest) (controlRepoTreeSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlRepoTreeSnapshot{}, err
	}

	currentPath := normalizeRelativePath(request.Path)
	absolutePath := repoRoot
	if currentPath != "" {
		absolutePath, err = resolveRepoRelativePath(repoRoot, currentPath)
		if err != nil {
			return controlRepoTreeSnapshot{}, err
		}
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlRepoTreeSnapshot{
				RepoPath: repoRoot,
				Path:     currentPath,
				ReadOnly: true,
				Message:  "repo tree path not found",
			}, nil
		}
		return controlRepoTreeSnapshot{}, err
	}
	if !info.IsDir() {
		return controlRepoTreeSnapshot{}, fmt.Errorf("%s is not a directory", pathOrRoot(currentPath))
	}

	entries, err := os.ReadDir(absolutePath)
	if err != nil {
		return controlRepoTreeSnapshot{}, err
	}

	items := make([]controlRepoTreeItem, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name())
		if name == "" || name == ".git" {
			continue
		}

		relPath := joinRepoPath(currentPath, name)
		item := controlRepoTreeItem{
			Name:                      name,
			Path:                      relPath,
			ReadOnly:                  true,
			EditableViaContractEditor: slices.Contains(canonicalContractFiles, relPath),
		}

		info, err := entry.Info()
		if err != nil {
			return controlRepoTreeSnapshot{}, err
		}
		if info.IsDir() {
			item.Kind = "directory"
		} else {
			item.Kind = "file"
			item.ByteSize = info.Size()
		}
		item.ModifiedAt = formatSnapshotTime(info.ModTime().UTC())
		items = append(items, item)
	}

	slices.SortFunc(items, func(a, b controlRepoTreeItem) int {
		if a.Kind != b.Kind {
			if a.Kind == "directory" {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	limit := request.Limit
	if limit <= 0 {
		limit = defaultRepoTreeEntryLimit
	}
	if len(items) > limit {
		items = items[:limit]
	}

	snapshot := controlRepoTreeSnapshot{
		RepoPath: repoRoot,
		Path:     currentPath,
		Count:    len(items),
		ReadOnly: true,
		Items:    items,
	}
	if currentPath != "" {
		snapshot.ParentPath = parentRepoPath(currentPath)
	}
	if len(items) == 0 {
		snapshot.Message = "no repo entries are available at this path"
	}
	return snapshot, nil
}

func openRepoFile(_ context.Context, inv Invocation, request control.RepoFileRequest) (controlRepoFileSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlRepoFileSnapshot{}, err
	}

	relativePath := normalizeRelativePath(request.Path)
	if relativePath == "" {
		return controlRepoFileSnapshot{}, errors.New("path is required")
	}

	absolutePath, err := resolveRepoRelativePath(repoRoot, relativePath)
	if err != nil {
		return controlRepoFileSnapshot{}, err
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlRepoFileSnapshot{
				Available: false,
				Path:      relativePath,
				ReadOnly:  true,
				Message:   "repo file not found",
			}, nil
		}
		return controlRepoFileSnapshot{}, err
	}
	if info.IsDir() {
		return controlRepoFileSnapshot{
			Available: false,
			Path:      relativePath,
			ReadOnly:  true,
			Message:   "path points to a directory",
		}, nil
	}

	content, truncated, err := readTextFileLimited(absolutePath, maxProtocolRepoFileBytes)
	if err != nil {
		return controlRepoFileSnapshot{}, err
	}

	return controlRepoFileSnapshot{
		Available:                 true,
		Path:                      relativePath,
		ContentType:               repoFileContentType(relativePath),
		Content:                   content,
		ByteSize:                  info.Size(),
		Truncated:                 truncated,
		ReadOnly:                  true,
		EditableViaContractEditor: slices.Contains(canonicalContractFiles, relativePath),
	}, nil
}

func normalizeAutofillTargets(targets []string) ([]string, error) {
	if len(targets) == 0 {
		return nil, errors.New("at least one autofill target is required")
	}

	normalized := make([]string, 0, len(targets))
	seen := map[string]bool{}
	for _, target := range targets {
		rel := normalizeRelativePath(target)
		if rel == "" {
			return nil, errors.New("autofill target path is required")
		}
		if !slices.Contains(canonicalContractFiles, rel) {
			return nil, fmt.Errorf("%s is not an autofill target in this slice", rel)
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		normalized = append(normalized, rel)
	}
	return normalized, nil
}

func autofillAnswersFromMap(raw map[string]any) autofill.Answers {
	return autofill.Answers{
		ProjectSummary: answerString(raw, "project_summary"),
		DesiredOutcome: answerString(raw, "desired_outcome"),
		UsersPlatform:  answerString(raw, "users_platform"),
		Constraints:    answerString(raw, "constraints"),
		Milestones:     answerString(raw, "milestones"),
		Decisions:      answerString(raw, "decisions"),
		Notes:          answerString(raw, "notes"),
	}
}

func loadAutofillExistingFiles(repoRoot string, targets []string) []autofill.ExistingFile {
	files := make([]autofill.ExistingFile, 0, len(targets))
	for _, relPath := range targets {
		absolutePath, err := resolveRepoRelativePath(repoRoot, relPath)
		if err != nil {
			continue
		}
		content, ok := readOptionalTextFile(absolutePath, maxProtocolContractBytes)
		if !ok {
			continue
		}
		files = append(files, autofill.ExistingFile{
			Path:    relPath,
			Content: content,
		})
	}
	return files
}

func loadAutofillReferenceFiles(repoRoot string, targets []string) []autofill.ExistingFile {
	targetSet := map[string]bool{}
	for _, target := range targets {
		targetSet[target] = true
	}

	referencePaths := []string{"README.md"}
	for _, relPath := range canonicalContractFiles {
		if !targetSet[relPath] {
			referencePaths = append(referencePaths, relPath)
		}
	}

	references := make([]autofill.ExistingFile, 0, len(referencePaths))
	for _, relPath := range referencePaths {
		absolutePath, err := resolveRepoRelativePath(repoRoot, relPath)
		if err != nil {
			continue
		}
		content, ok := readOptionalTextFile(absolutePath, maxAutofillReferenceBytes)
		if !ok {
			continue
		}
		references = append(references, autofill.ExistingFile{
			Path:    relPath,
			Content: content,
		})
	}
	return references
}

func listAutofillRepoTopLevel(repoRoot string) []string {
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return nil
	}

	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name())
		if name == "" || name == ".git" {
			continue
		}
		if entry.IsDir() {
			items = append(items, name+"/")
		} else {
			items = append(items, name)
		}
	}
	slices.Sort(items)
	return items
}

func readOptionalTextFile(path string, limit int64) (string, bool) {
	content, _, err := readTextFileLimited(path, limit)
	if err != nil {
		return "", false
	}
	return content, true
}

func statRepoFile(repoRoot string, relativePath string) (os.FileInfo, bool) {
	absolutePath, err := resolveRepoRelativePath(repoRoot, relativePath)
	if err != nil {
		return nil, false
	}
	info, err := os.Stat(absolutePath)
	if err != nil || info.IsDir() {
		return nil, false
	}
	return info, true
}

func answerString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	value, ok := raw[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func pathOrRoot(path string) string {
	if strings.TrimSpace(path) == "" {
		return "."
	}
	return path
}

func parentRepoPath(current string) string {
	normalized := normalizeRelativePath(current)
	if normalized == "" || !strings.Contains(normalized, "/") {
		return ""
	}
	return filepath.ToSlash(filepath.Dir(filepath.FromSlash(normalized)))
}

func joinRepoPath(base string, name string) string {
	if strings.TrimSpace(base) == "" {
		return normalizeRelativePath(name)
	}
	return normalizeRelativePath(base + "/" + name)
}

func repoFileContentType(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".json":
		return "application/json"
	case ".md":
		return "text/markdown"
	case ".html":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js", ".mjs", ".cjs":
		return "text/javascript"
	case ".ts", ".tsx":
		return "text/typescript"
	default:
		return "text/plain"
	}
}
