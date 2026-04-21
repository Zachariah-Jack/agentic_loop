package workers

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"orchestrator/internal/state"
)

func BuildIntegrationSummary(repoRoot string, selectedWorkers []state.Worker) (state.IntegrationSummary, error) {
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	if repoRoot == "" {
		return state.IntegrationSummary{}, errors.New("repo root is required")
	}
	if len(selectedWorkers) == 0 {
		return state.IntegrationSummary{}, errors.New("at least one worker is required for integration preview")
	}

	baseSnapshot, err := snapshotWorkspace(repoRoot)
	if err != nil {
		return state.IntegrationSummary{}, err
	}

	summary := state.IntegrationSummary{
		WorkerIDs: make([]string, 0, len(selectedWorkers)),
		Workers:   make([]state.IntegrationWorkerSummary, 0, len(selectedWorkers)),
	}
	pathOwners := make(map[string][]state.Worker)
	topLevelOwners := make(map[string][]state.Worker)
	uniqueChanged := make(map[string]struct{})

	for _, worker := range selectedWorkers {
		workerPath := filepath.Clean(strings.TrimSpace(worker.WorktreePath))
		if workerPath == "" {
			return state.IntegrationSummary{}, fmt.Errorf("worker %s has no worktree path", worker.ID)
		}

		workerSnapshot, err := snapshotWorkspace(workerPath)
		if err != nil {
			return state.IntegrationSummary{}, err
		}

		fileList, diffSummary := diffSnapshots(baseSnapshot, workerSnapshot)
		for _, path := range fileList {
			uniqueChanged[path] = struct{}{}
			pathOwners[path] = append(pathOwners[path], worker)
			topLevel := topLevelPath(path)
			if topLevel != "" {
				topLevelOwners[topLevel] = append(topLevelOwners[topLevel], worker)
			}
		}

		summary.WorkerIDs = append(summary.WorkerIDs, worker.ID)
		summary.Workers = append(summary.Workers, state.IntegrationWorkerSummary{
			WorkerID:            worker.ID,
			WorkerName:          worker.WorkerName,
			WorktreePath:        worker.WorktreePath,
			WorkerResultSummary: worker.WorkerResultSummary,
			FileList:            fileList,
			DiffSummary:         diffSummary,
		})
	}

	conflicts := buildConflictCandidates(pathOwners, topLevelOwners)
	summary.ConflictCandidates = conflicts
	summary.IntegrationPreview = fmt.Sprintf(
		"Read-only integration preview for %d worker(s): %d changed file(s), %d conflict candidate(s).",
		len(selectedWorkers),
		len(uniqueChanged),
		len(conflicts),
	)
	return summary, nil
}

type fileSnapshot map[string][32]byte

func snapshotWorkspace(root string) (fileSnapshot, error) {
	snapshot := make(fileSnapshot)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}

		if d.IsDir() {
			if shouldSkipIntegrationDir(relative) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipIntegrationFile(relative) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[relative] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func shouldSkipIntegrationDir(relative string) bool {
	switch relative {
	case ".git", ".orchestrator/state", ".orchestrator/logs", ".orchestrator/artifacts":
		return true
	default:
		return false
	}
}

func shouldSkipIntegrationFile(relative string) bool {
	return strings.HasPrefix(relative, ".git/") ||
		strings.HasPrefix(relative, ".orchestrator/state/") ||
		strings.HasPrefix(relative, ".orchestrator/logs/") ||
		strings.HasPrefix(relative, ".orchestrator/artifacts/")
}

func diffSnapshots(base fileSnapshot, worker fileSnapshot) ([]string, []string) {
	paths := make(map[string]struct{}, len(base)+len(worker))
	for path := range base {
		paths[path] = struct{}{}
	}
	for path := range worker {
		paths[path] = struct{}{}
	}

	fileList := make([]string, 0)
	diffSummary := make([]string, 0)
	for path := range paths {
		baseHash, hasBase := base[path]
		workerHash, hasWorker := worker[path]
		switch {
		case !hasBase && hasWorker:
			fileList = append(fileList, path)
			diffSummary = append(diffSummary, "added: "+path)
		case hasBase && !hasWorker:
			fileList = append(fileList, path)
			diffSummary = append(diffSummary, "deleted: "+path)
		case hasBase && hasWorker && baseHash != workerHash:
			fileList = append(fileList, path)
			diffSummary = append(diffSummary, "modified: "+path)
		}
	}

	sort.Strings(fileList)
	sort.Strings(diffSummary)
	return fileList, diffSummary
}

func buildConflictCandidates(pathOwners map[string][]state.Worker, topLevelOwners map[string][]state.Worker) []state.ConflictCandidate {
	candidates := make([]state.ConflictCandidate, 0)
	for path, owners := range pathOwners {
		if len(uniqueWorkerIDs(owners)) <= 1 {
			continue
		}
		candidates = append(candidates, state.ConflictCandidate{
			Path:        path,
			Reason:      "same_file_touched",
			WorkerIDs:   workerIDs(owners),
			WorkerNames: workerNames(owners),
		})
	}
	for path, owners := range topLevelOwners {
		if len(uniqueWorkerIDs(owners)) <= 1 {
			continue
		}
		candidates = append(candidates, state.ConflictCandidate{
			Path:        path,
			Reason:      "shared_top_level_path",
			WorkerIDs:   workerIDs(owners),
			WorkerNames: workerNames(owners),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Path == candidates[j].Path {
			return candidates[i].Reason < candidates[j].Reason
		}
		return candidates[i].Path < candidates[j].Path
	})
	return candidates
}

func uniqueWorkerIDs(workers []state.Worker) []string {
	seen := make(map[string]struct{}, len(workers))
	ids := make([]string, 0, len(workers))
	for _, worker := range workers {
		if _, ok := seen[worker.ID]; ok {
			continue
		}
		seen[worker.ID] = struct{}{}
		ids = append(ids, worker.ID)
	}
	sort.Strings(ids)
	return ids
}

func workerIDs(workers []state.Worker) []string {
	return uniqueWorkerIDs(workers)
}

func workerNames(workers []state.Worker) []string {
	seen := make(map[string]struct{}, len(workers))
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		name := strings.TrimSpace(worker.WorkerName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func topLevelPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
