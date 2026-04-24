package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"orchestrator/internal/control"
	"orchestrator/internal/journal"
	"orchestrator/internal/state"
)

const (
	maxProtocolArtifactBytes = 256 * 1024
	maxProtocolContractBytes = 256 * 1024
	maxProtocolRoadmapBytes  = 8 * 1024
)

var canonicalContractFiles = []string{
	".orchestrator/brief.md",
	".orchestrator/roadmap.md",
	".orchestrator/decisions.md",
	".orchestrator/human-notes.md",
	"AGENTS.md",
}

type controlArtifactsSnapshot struct {
	Count      int                      `json:"count"`
	LatestPath string                   `json:"latest_path,omitempty"`
	Items      []controlArtifactSummary `json:"items,omitempty"`
	Message    string                   `json:"message,omitempty"`
}

type controlArtifactSummary struct {
	Path      string `json:"path"`
	Category  string `json:"category,omitempty"`
	Source    string `json:"source,omitempty"`
	EventType string `json:"event_type,omitempty"`
	Preview   string `json:"preview,omitempty"`
	At        string `json:"at,omitempty"`
	Latest    bool   `json:"latest,omitempty"`
}

type controlArtifactContentSnapshot struct {
	Available   bool   `json:"available"`
	Path        string `json:"path"`
	Category    string `json:"category,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Content     string `json:"content,omitempty"`
	ByteSize    int64  `json:"byte_size,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Message     string `json:"message,omitempty"`
}

type controlContractFileListSnapshot struct {
	Count int                          `json:"count"`
	Files []controlContractFileSummary `json:"files"`
}

type controlContractFileSummary struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	ModifiedAt string `json:"modified_at,omitempty"`
	ByteSize   int64  `json:"byte_size,omitempty"`
}

type controlContractFileContentSnapshot struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Content    string `json:"content,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	ByteSize   int64  `json:"byte_size,omitempty"`
}

type controlContractFileSaveSnapshot struct {
	Path       string `json:"path"`
	Saved      bool   `json:"saved"`
	ModifiedAt string `json:"modified_at,omitempty"`
	ByteSize   int64  `json:"byte_size,omitempty"`
}

type artifactRef struct {
	Path      string
	Category  string
	Source    string
	EventType string
	Preview   string
	At        time.Time
}

func buildControlArtifactsSnapshot(run state.Run, events []journal.Event, limit int) controlArtifactsSnapshot {
	if limit <= 0 {
		limit = 12
	}

	latestPath := strings.TrimSpace(latestArtifactPath(run, events))
	summaries := collectArtifactRefs(run, events, limit)
	items := make([]controlArtifactSummary, 0, len(summaries))
	for _, ref := range summaries {
		items = append(items, controlArtifactSummary{
			Path:      ref.Path,
			Category:  ref.Category,
			Source:    ref.Source,
			EventType: ref.EventType,
			Preview:   previewString(ref.Preview, 240),
			At:        formatSnapshotTime(ref.At),
			Latest:    latestPath != "" && ref.Path == latestPath,
		})
	}

	message := ""
	if len(items) == 0 {
		message = "no artifacts are currently recorded for this run"
	}

	return controlArtifactsSnapshot{
		Count:      len(items),
		LatestPath: latestPath,
		Items:      items,
		Message:    message,
	}
}

func buildControlRoadmapSnapshot(repoRoot string) controlRoadmapSnapshot {
	const roadmapPath = ".orchestrator/roadmap.md"

	snapshot := controlRoadmapSnapshot{
		Present: false,
		Path:    roadmapPath,
		Message: "roadmap context is unavailable",
	}

	absolutePath, err := resolveRepoRelativePath(repoRoot, roadmapPath)
	if err != nil {
		return snapshot
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			snapshot.Message = "roadmap file is not present yet"
			return snapshot
		}
		snapshot.Message = err.Error()
		return snapshot
	}
	if info.IsDir() {
		snapshot.Message = "roadmap path points to a directory"
		return snapshot
	}

	content, truncated, err := readTextFileLimited(absolutePath, maxProtocolRoadmapBytes)
	if err != nil {
		snapshot.Message = err.Error()
		return snapshot
	}

	alignmentText := strings.TrimSpace(content)
	if truncated {
		alignmentText = previewString(alignmentText, 1200)
	}
	if alignmentText == "" {
		alignmentText = "roadmap file is present but currently empty"
	}

	snapshot.Present = true
	snapshot.Preview = alignmentText
	snapshot.AlignmentText = alignmentText
	snapshot.ModifiedAt = formatSnapshotTime(info.ModTime().UTC())
	snapshot.Message = ""
	return snapshot
}

func listRecentArtifacts(ctx context.Context, inv Invocation, request control.ListArtifactsRequest) (controlArtifactsSnapshot, error) {
	if !pathExists(inv.Layout.DBPath) {
		return controlArtifactsSnapshot{
			Count:   0,
			Message: "runtime state is not initialized for artifact inspection",
		}, nil
	}

	store, err := openExistingStore(inv.Layout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlArtifactsSnapshot{Count: 0, Message: "runtime state is not initialized for artifact inspection"}, nil
		}
		return controlArtifactsSnapshot{}, err
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		return controlArtifactsSnapshot{}, err
	}

	run, found, err := resolveControlRun(ctx, store, strings.TrimSpace(request.RunID))
	if err != nil {
		return controlArtifactsSnapshot{}, err
	}
	if !found {
		return controlArtifactsSnapshot{Count: 0, Message: "no run is available for artifact inspection"}, nil
	}

	events, err := latestRunEvents(inv.Layout, run.ID, 128)
	if err != nil {
		return controlArtifactsSnapshot{}, err
	}

	snapshot := buildControlArtifactsSnapshot(run, events, request.Limit)
	if category := strings.TrimSpace(request.Category); category != "" {
		filtered := make([]controlArtifactSummary, 0, len(snapshot.Items))
		for _, item := range snapshot.Items {
			if strings.EqualFold(item.Category, category) {
				filtered = append(filtered, item)
			}
		}
		snapshot.Items = filtered
		snapshot.Count = len(filtered)
		if len(filtered) == 0 {
			snapshot.Message = fmt.Sprintf("no artifacts are currently recorded for category %s", category)
		}
	}

	return snapshot, nil
}

func getArtifact(ctx context.Context, inv Invocation, request control.ArtifactRequest) (controlArtifactContentSnapshot, error) {
	_ = ctx

	artifactPath, absolutePath, err := resolveArtifactPath(inv.RepoRoot, request.ArtifactPath)
	if err != nil {
		return controlArtifactContentSnapshot{}, err
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlArtifactContentSnapshot{
				Available: false,
				Path:      artifactPath,
				Category:  inferArtifactCategory(artifactPath),
				Message:   "artifact not found",
			}, nil
		}
		return controlArtifactContentSnapshot{}, err
	}
	if info.IsDir() {
		return controlArtifactContentSnapshot{
			Available: false,
			Path:      artifactPath,
			Category:  inferArtifactCategory(artifactPath),
			Message:   "artifact path points to a directory",
		}, nil
	}

	content, truncated, err := readTextFileLimited(absolutePath, maxProtocolArtifactBytes)
	if err != nil {
		return controlArtifactContentSnapshot{}, err
	}

	return controlArtifactContentSnapshot{
		Available:   true,
		Path:        artifactPath,
		Category:    inferArtifactCategory(artifactPath),
		ContentType: artifactContentType(artifactPath),
		Content:     content,
		ByteSize:    info.Size(),
		Truncated:   truncated,
	}, nil
}

func listContractFiles(_ context.Context, inv Invocation, request control.ListContractFilesRequest) (controlContractFileListSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlContractFileListSnapshot{}, err
	}

	files := make([]controlContractFileSummary, 0, len(canonicalContractFiles))
	for _, relPath := range canonicalContractFiles {
		absolutePath := filepath.Join(repoRoot, filepath.FromSlash(relPath))
		info, err := os.Stat(absolutePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				files = append(files, controlContractFileSummary{
					Path:   relPath,
					Exists: false,
				})
				continue
			}
			return controlContractFileListSnapshot{}, err
		}

		files = append(files, controlContractFileSummary{
			Path:       relPath,
			Exists:     true,
			ModifiedAt: formatSnapshotTime(info.ModTime().UTC()),
			ByteSize:   info.Size(),
		})
	}

	return controlContractFileListSnapshot{
		Count: len(files),
		Files: files,
	}, nil
}

func openContractFile(_ context.Context, inv Invocation, request control.ContractFileRequest) (controlContractFileContentSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlContractFileContentSnapshot{}, err
	}

	contractPath, absolutePath, err := resolveContractFilePath(repoRoot, request.Path)
	if err != nil {
		return controlContractFileContentSnapshot{}, err
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return controlContractFileContentSnapshot{
				Path:   contractPath,
				Exists: false,
			}, nil
		}
		return controlContractFileContentSnapshot{}, err
	}
	if info.IsDir() {
		return controlContractFileContentSnapshot{}, fmt.Errorf("%s is a directory", contractPath)
	}

	content, truncated, err := readTextFileLimited(absolutePath, maxProtocolContractBytes)
	if err != nil {
		return controlContractFileContentSnapshot{}, err
	}
	if truncated {
		content = previewString(content, maxProtocolContractBytes)
	}

	return controlContractFileContentSnapshot{
		Path:       contractPath,
		Exists:     true,
		Content:    content,
		ModifiedAt: formatSnapshotTime(info.ModTime().UTC()),
		ByteSize:   info.Size(),
	}, nil
}

func saveContractFile(_ context.Context, inv Invocation, request control.SaveContractFileRequest) (controlContractFileSaveSnapshot, error) {
	repoRoot, err := resolveRequestedRepoRoot(inv.RepoRoot, request.RepoPath)
	if err != nil {
		return controlContractFileSaveSnapshot{}, err
	}

	contractPath, absolutePath, err := resolveContractFilePath(repoRoot, request.Path)
	if err != nil {
		return controlContractFileSaveSnapshot{}, err
	}

	if strings.TrimSpace(request.ExpectedMTime) != "" {
		info, err := os.Stat(absolutePath)
		if err == nil {
			actual := formatSnapshotTime(info.ModTime().UTC())
			if actual != strings.TrimSpace(request.ExpectedMTime) {
				return controlContractFileSaveSnapshot{}, fmt.Errorf("contract file %s changed since it was opened", contractPath)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return controlContractFileSaveSnapshot{}, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return controlContractFileSaveSnapshot{}, err
	}
	if err := os.WriteFile(absolutePath, []byte(request.Content), 0o644); err != nil {
		return controlContractFileSaveSnapshot{}, err
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		return controlContractFileSaveSnapshot{}, err
	}

	return controlContractFileSaveSnapshot{
		Path:       contractPath,
		Saved:      true,
		ModifiedAt: formatSnapshotTime(info.ModTime().UTC()),
		ByteSize:   info.Size(),
	}, nil
}

func collectArtifactRefs(run state.Run, events []journal.Event, limit int) []artifactRef {
	seen := map[string]bool{}
	refs := make([]artifactRef, 0, limit)

	appendRef := func(ref artifactRef) {
		ref.Path = strings.TrimSpace(ref.Path)
		if ref.Path == "" || seen[ref.Path] {
			return
		}
		if ref.Category == "" {
			ref.Category = inferArtifactCategory(ref.Path)
		}
		refs = append(refs, ref)
		seen[ref.Path] = true
	}

	for idx := len(events) - 1; idx >= 0; idx-- {
		event := events[idx]
		path := strings.TrimSpace(event.ArtifactPath)
		if path == "" {
			continue
		}
		appendRef(artifactRef{
			Path:      path,
			Category:  inferArtifactCategory(path),
			Source:    "event_stream",
			EventType: strings.TrimSpace(event.Type),
			Preview:   strings.TrimSpace(event.ArtifactPreview),
			At:        event.At,
		})
		if len(refs) >= limit {
			return refs
		}
	}

	for _, ref := range collectRunArtifactRefs(run) {
		appendRef(ref)
		if len(refs) >= limit {
			break
		}
	}

	return refs
}

func collectRunArtifactRefs(run state.Run) []artifactRef {
	refs := []artifactRef{}
	if run.CollectedContext == nil {
		return refs
	}

	appendRef := func(path string, preview string, source string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		refs = append(refs, artifactRef{
			Path:     strings.TrimSpace(path),
			Category: inferArtifactCategory(path),
			Source:   source,
			Preview:  strings.TrimSpace(preview),
		})
	}

	appendRef(run.CollectedContext.ArtifactPath, run.CollectedContext.ArtifactPreview, "collected_context")
	for _, item := range run.CollectedContext.ToolResults {
		appendRef(item.ArtifactPath, item.ArtifactPreview, "plugin_tool_result")
	}
	for _, item := range run.CollectedContext.WorkerResults {
		appendRef(item.ArtifactPath, item.ArtifactPreview, "worker_action_result")
		if item.Apply != nil {
			appendRef(item.Apply.SourceArtifactPath, item.Apply.AfterSummary, "worker_action_apply_source")
		}
	}
	if run.CollectedContext.WorkerPlan != nil {
		appendRef(run.CollectedContext.WorkerPlan.IntegrationArtifactPath, run.CollectedContext.WorkerPlan.IntegrationPreview, "worker_plan_integration")
		appendRef(run.CollectedContext.WorkerPlan.ApplyArtifactPath, summaryOrEmpty(run.CollectedContext.WorkerPlan.Apply), "worker_plan_apply")
		if run.CollectedContext.WorkerPlan.Apply != nil {
			appendRef(run.CollectedContext.WorkerPlan.Apply.SourceArtifactPath, run.CollectedContext.WorkerPlan.Apply.AfterSummary, "worker_plan_apply_source")
		}
	}

	return refs
}

func summaryOrEmpty(apply *state.IntegrationApplySummary) string {
	if apply == nil {
		return ""
	}
	if strings.TrimSpace(apply.AfterSummary) != "" {
		return strings.TrimSpace(apply.AfterSummary)
	}
	return strings.TrimSpace(apply.BeforeSummary)
}

func inferArtifactCategory(artifactPath string) string {
	normalized := normalizeRelativePath(artifactPath)
	parts := strings.Split(normalized, "/")
	if len(parts) >= 3 && parts[0] == ".orchestrator" && parts[1] == "artifacts" {
		return parts[2]
	}
	return ""
}

func resolveArtifactPath(repoRoot string, artifactPath string) (string, string, error) {
	normalized := normalizeRelativePath(artifactPath)
	if normalized == "" {
		return "", "", errors.New("artifact_path is required")
	}
	if normalized != ".orchestrator/artifacts" && !strings.HasPrefix(normalized, ".orchestrator/artifacts/") {
		return "", "", errors.New("artifact_path must stay under .orchestrator/artifacts")
	}
	absolutePath, err := resolveRepoRelativePath(repoRoot, normalized)
	if err != nil {
		return "", "", err
	}
	return normalized, absolutePath, nil
}

func resolveRequestedRepoRoot(defaultRepoRoot string, requestedRepoRoot string) (string, error) {
	defaultRoot := filepath.Clean(strings.TrimSpace(defaultRepoRoot))
	if defaultRoot == "" {
		return "", errors.New("server repo root is unavailable")
	}

	requested := filepath.Clean(strings.TrimSpace(requestedRepoRoot))
	if requested == "" {
		return defaultRoot, nil
	}
	if !strings.EqualFold(requested, defaultRoot) {
		return "", errors.New("repo_path must match the server repo root in this slice")
	}
	return defaultRoot, nil
}

func resolveContractFilePath(repoRoot string, contractPath string) (string, string, error) {
	normalized := normalizeRelativePath(contractPath)
	if normalized == "" {
		return "", "", errors.New("path is required")
	}
	if !slices.Contains(canonicalContractFiles, normalized) {
		return "", "", fmt.Errorf("%s is not an editable contract file in this slice", normalized)
	}
	absolutePath, err := resolveRepoRelativePath(repoRoot, normalized)
	if err != nil {
		return "", "", err
	}
	return normalized, absolutePath, nil
}

func resolveRepoRelativePath(repoRoot string, relativePath string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(repoRoot))
	if root == "" {
		return "", errors.New("repo root is unavailable")
	}
	if strings.TrimSpace(relativePath) == "" {
		return "", errors.New("relative path is required")
	}

	absolutePath := filepath.Clean(filepath.Join(root, filepath.FromSlash(relativePath)))
	rel, err := filepath.Rel(root, absolutePath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the repo root")
	}
	return absolutePath, nil
}

func normalizeRelativePath(value string) string {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" {
		return ""
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return ""
	}
	return cleaned
}

func readTextFileLimited(absolutePath string, limit int64) (string, bool, error) {
	file, err := os.Open(absolutePath)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	if limit <= 0 {
		limit = maxProtocolArtifactBytes
	}

	reader := io.LimitReader(file, limit+1)
	content, err := io.ReadAll(reader)
	if err != nil {
		return "", false, err
	}
	truncated := int64(len(content)) > limit
	if truncated {
		content = content[:limit]
	}
	return string(content), truncated, nil
}

func artifactContentType(artifactPath string) string {
	switch strings.ToLower(filepath.Ext(artifactPath)) {
	case ".json":
		return "application/json"
	case ".md":
		return "text/markdown"
	default:
		return "text/plain"
	}
}
