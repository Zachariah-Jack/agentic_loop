package workers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

const (
	integrationApplyStatusCompleted = "completed"
	integrationApplyStatusFailed    = "failed"
)

func LoadIntegrationArtifact(repoRoot string, artifactPath string) (state.IntegrationSummary, string, error) {
	resolvedRelativePath, resolvedAbsolutePath, err := resolveIntegrationArtifactPath(repoRoot, artifactPath)
	if err != nil {
		return state.IntegrationSummary{}, "", err
	}

	content, err := os.ReadFile(resolvedAbsolutePath)
	if err != nil {
		return state.IntegrationSummary{}, "", err
	}

	var summary state.IntegrationSummary
	if err := json.Unmarshal(content, &summary); err != nil {
		return state.IntegrationSummary{}, "", err
	}

	return summary, resolvedRelativePath, nil
}

func ApplyIntegration(repoRoot string, integration state.IntegrationSummary, sourceArtifactPath string, mode string) (state.IntegrationApplySummary, error) {
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	if repoRoot == "" {
		return state.IntegrationApplySummary{}, errors.New("repo root is required")
	}
	if !isSupportedIntegrationApplyMode(mode) {
		return state.IntegrationApplySummary{}, fmt.Errorf("unsupported integration apply mode: %s", strings.TrimSpace(mode))
	}

	totalCandidateFiles := 0
	for _, worker := range integration.Workers {
		totalCandidateFiles += len(worker.FileList)
	}

	result := state.IntegrationApplySummary{
		Status:             integrationApplyStatusCompleted,
		SourceArtifactPath: filepath.ToSlash(strings.TrimSpace(sourceArtifactPath)),
		ApplyMode:          strings.TrimSpace(mode),
		ConflictCandidates: cloneConflictCandidates(integration.ConflictCandidates),
		BeforeSummary: fmt.Sprintf(
			"integration apply input: %d worker(s), %d candidate file change(s), %d conflict candidate(s).",
			len(integration.Workers),
			totalCandidateFiles,
			len(integration.ConflictCandidates),
		),
	}

	if mode == string(planner.WorkerApplyModeAbortIfConflicts) && len(integration.ConflictCandidates) > 0 {
		result.Status = integrationApplyStatusFailed
		result.FilesSkipped = buildAbortSkippedFiles(integration)
		result.AfterSummary = fmt.Sprintf(
			"integration apply refused: %d conflict candidate(s) present and mode is abort_if_conflicts.",
			len(integration.ConflictCandidates),
		)
		return result, nil
	}

	conflictPaths := buildConflictPathFilter(integration.ConflictCandidates)
	appliedPaths := make(map[string]struct{})

	for _, worker := range integration.Workers {
		diffKinds := diffKindsByPath(worker.DiffSummary)
		for _, relativePath := range worker.FileList {
			relativePath = filepath.ToSlash(strings.TrimSpace(relativePath))
			if relativePath == "" {
				continue
			}

			changeKind := strings.TrimSpace(diffKinds[relativePath])
			if changeKind == "" {
				result.Status = integrationApplyStatusFailed
				result.FilesSkipped = append(result.FilesSkipped, state.IntegrationSkippedFile{
					WorkerID:   worker.WorkerID,
					WorkerName: worker.WorkerName,
					Path:       relativePath,
					ChangeKind: "unknown",
					Reason:     "missing_diff_summary",
				})
				continue
			}

			if _, alreadyApplied := appliedPaths[relativePath]; alreadyApplied {
				result.FilesSkipped = append(result.FilesSkipped, state.IntegrationSkippedFile{
					WorkerID:   worker.WorkerID,
					WorkerName: worker.WorkerName,
					Path:       relativePath,
					ChangeKind: changeKind,
					Reason:     "already_applied",
				})
				continue
			}

			if mode == string(planner.WorkerApplyModeNonConflicting) && isConflictingApplyPath(relativePath, conflictPaths) {
				result.FilesSkipped = append(result.FilesSkipped, state.IntegrationSkippedFile{
					WorkerID:   worker.WorkerID,
					WorkerName: worker.WorkerName,
					Path:       relativePath,
					ChangeKind: changeKind,
					Reason:     "conflict_candidate",
				})
				continue
			}

			if err := applyIntegrationFile(repoRoot, worker.WorktreePath, relativePath, changeKind); err != nil {
				result.Status = integrationApplyStatusFailed
				result.FilesSkipped = append(result.FilesSkipped, state.IntegrationSkippedFile{
					WorkerID:   worker.WorkerID,
					WorkerName: worker.WorkerName,
					Path:       relativePath,
					ChangeKind: changeKind,
					Reason:     err.Error(),
				})
				continue
			}

			appliedPaths[relativePath] = struct{}{}
			result.FilesApplied = append(result.FilesApplied, state.IntegrationAppliedFile{
				WorkerID:   worker.WorkerID,
				WorkerName: worker.WorkerName,
				Path:       relativePath,
				ChangeKind: changeKind,
			})
		}
	}

	result.AfterSummary = fmt.Sprintf(
		"integration apply %s: applied %d file(s), skipped %d file(s) using %s.",
		result.Status,
		len(result.FilesApplied),
		len(result.FilesSkipped),
		result.ApplyMode,
	)
	return result, nil
}

func resolveIntegrationArtifactPath(repoRoot string, artifactPath string) (string, string, error) {
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	if repoRoot == "" {
		return "", "", errors.New("repo root is required")
	}

	trimmed := strings.TrimSpace(artifactPath)
	if trimmed == "" {
		return "", "", errors.New("integration artifact path is required")
	}

	resolvedAbsolutePath := trimmed
	if !filepath.IsAbs(resolvedAbsolutePath) {
		resolvedAbsolutePath = filepath.Join(repoRoot, filepath.FromSlash(trimmed))
	}
	resolvedAbsolutePath = filepath.Clean(resolvedAbsolutePath)

	relativePath, err := filepath.Rel(repoRoot, resolvedAbsolutePath)
	if err != nil {
		return "", "", errors.New("integration artifact path must resolve under the repo root")
	}
	relativePath = filepath.ToSlash(relativePath)
	if relativePath == ".." || strings.HasPrefix(relativePath, "../") {
		return "", "", errors.New("integration artifact path must resolve under the repo root")
	}
	if !strings.HasPrefix(relativePath, filepath.ToSlash(filepath.Join(".orchestrator", "artifacts", "integration"))+"/") {
		return "", "", errors.New("integration artifact path must point under .orchestrator/artifacts/integration/")
	}

	return relativePath, resolvedAbsolutePath, nil
}

func buildConflictPathFilter(candidates []state.ConflictCandidate) map[string]struct{} {
	conflictPaths := make(map[string]struct{})

	for _, candidate := range candidates {
		path := filepath.ToSlash(strings.TrimSpace(candidate.Path))
		if strings.TrimSpace(candidate.Reason) == "same_file_touched" && path != "" {
			conflictPaths[path] = struct{}{}
		}
	}

	return conflictPaths
}

func buildAbortSkippedFiles(integration state.IntegrationSummary) []state.IntegrationSkippedFile {
	skipped := make([]state.IntegrationSkippedFile, 0)
	for _, worker := range integration.Workers {
		diffKinds := diffKindsByPath(worker.DiffSummary)
		for _, relativePath := range worker.FileList {
			relativePath = filepath.ToSlash(strings.TrimSpace(relativePath))
			if relativePath == "" {
				continue
			}
			skipped = append(skipped, state.IntegrationSkippedFile{
				WorkerID:   worker.WorkerID,
				WorkerName: worker.WorkerName,
				Path:       relativePath,
				ChangeKind: fallbackChangeKind(strings.TrimSpace(diffKinds[relativePath])),
				Reason:     "aborted_due_to_conflicts",
			})
		}
	}
	return skipped
}

func diffKindsByPath(diffSummary []string) map[string]string {
	kinds := make(map[string]string, len(diffSummary))
	for _, item := range diffSummary {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, path, found := strings.Cut(item, ": ")
		if !found {
			continue
		}
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		kinds[path] = strings.TrimSpace(prefix)
	}
	return kinds
}

func isConflictingApplyPath(relativePath string, conflictPaths map[string]struct{}) bool {
	if _, found := conflictPaths[relativePath]; found {
		return true
	}
	return false
}

func applyIntegrationFile(repoRoot string, workerRoot string, relativePath string, changeKind string) error {
	destinationPath, err := resolveWorkspaceRelativePath(repoRoot, relativePath)
	if err != nil {
		return err
	}

	switch strings.TrimSpace(changeKind) {
	case "deleted":
		if err := os.Remove(destinationPath); err != nil {
			if os.IsNotExist(err) {
				return errors.New("already_absent")
			}
			return fmt.Errorf("remove_failed: %w", err)
		}
		trimEmptyParents(destinationPath, repoRoot)
		return nil
	case "added", "modified":
		sourcePath, err := resolveWorkspaceRelativePath(workerRoot, relativePath)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			if os.IsNotExist(err) {
				return errors.New("worker_source_missing")
			}
			return fmt.Errorf("worker_read_failed: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return fmt.Errorf("mkdir_failed: %w", err)
		}
		if err := os.WriteFile(destinationPath, content, 0o600); err != nil {
			return fmt.Errorf("write_failed: %w", err)
		}
		return nil
	default:
		return errors.New("unsupported_change_kind")
	}
}

func resolveWorkspaceRelativePath(root string, relativePath string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return "", errors.New("workspace root is required")
	}

	trimmed := filepath.ToSlash(strings.TrimSpace(relativePath))
	if trimmed == "" {
		return "", errors.New("relative path is required")
	}
	if filepath.IsAbs(trimmed) {
		return "", errors.New("absolute paths are not allowed")
	}

	cleaned := filepath.Clean(filepath.FromSlash(trimmed))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("path_outside_workspace_root")
	}

	resolvedPath := filepath.Clean(filepath.Join(root, cleaned))
	relativeToRoot, err := filepath.Rel(root, resolvedPath)
	if err != nil {
		return "", errors.New("path_outside_workspace_root")
	}
	if relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", errors.New("path_outside_workspace_root")
	}

	return resolvedPath, nil
}

func trimEmptyParents(path string, root string) {
	root = filepath.Clean(root)
	current := filepath.Dir(path)
	for current != "" && current != root {
		err := os.Remove(current)
		if err != nil {
			return
		}
		current = filepath.Dir(current)
	}
}

func cloneConflictCandidates(candidates []state.ConflictCandidate) []state.ConflictCandidate {
	cloned := make([]state.ConflictCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		cloned = append(cloned, state.ConflictCandidate{
			Path:        candidate.Path,
			Reason:      candidate.Reason,
			WorkerIDs:   append([]string(nil), candidate.WorkerIDs...),
			WorkerNames: append([]string(nil), candidate.WorkerNames...),
		})
	}
	return cloned
}

func isSupportedIntegrationApplyMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case string(planner.WorkerApplyModeAbortIfConflicts), string(planner.WorkerApplyModeNonConflicting):
		return true
	default:
		return false
	}
}

func fallbackChangeKind(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return strings.TrimSpace(value)
}
