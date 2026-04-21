package workers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	lookPath           = exec.LookPath
	execCommandContext = exec.CommandContext
)

type Support struct {
	Available bool
	GitPath   string
	Detail    string
}

type Manager struct {
	RepoRoot   string
	WorkersDir string
}

func NewManager(repoRoot string, workersDir string) Manager {
	return Manager{
		RepoRoot:   filepath.Clean(repoRoot),
		WorkersDir: filepath.Clean(workersDir),
	}
}

func DetectSupport(ctx context.Context, repoRoot string) Support {
	gitPath, err := lookPath("git")
	if err != nil {
		return Support{
			Available: false,
			Detail:    "git not found on PATH",
		}
	}

	if _, err := runGit(ctx, repoRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return Support{
			Available: false,
			GitPath:   gitPath,
			Detail:    "repo is not a git worktree-capable checkout: " + err.Error(),
		}
	}

	if _, err := runGit(ctx, repoRoot, "worktree", "list"); err != nil {
		return Support{
			Available: false,
			GitPath:   gitPath,
			Detail:    "git worktree command unavailable: " + err.Error(),
		}
	}

	return Support{
		Available: true,
		GitPath:   gitPath,
		Detail:    "git worktree support available",
	}
}

func (m Manager) PlannedPath(name string) (string, error) {
	if strings.TrimSpace(m.RepoRoot) == "" {
		return "", errors.New("repo root is required")
	}
	if strings.TrimSpace(m.WorkersDir) == "" {
		return "", errors.New("workers directory is required")
	}

	slug := sanitizeWorkerName(name)
	if slug == "" {
		return "", errors.New("worker name must contain at least one path-safe character")
	}

	path := filepath.Join(m.WorkersDir, slug)
	if filepath.Clean(path) == filepath.Clean(m.RepoRoot) {
		return "", errors.New("worker path may not reuse the main repo working tree")
	}
	if !pathWithinBase(m.WorkersDir, path) {
		return "", errors.New("worker path must remain within the isolated workers directory")
	}
	return path, nil
}

func (m Manager) Create(ctx context.Context, workerName string) (string, error) {
	support := DetectSupport(ctx, m.RepoRoot)
	if !support.Available {
		return "", errors.New(support.Detail)
	}

	plannedPath, err := m.PlannedPath(workerName)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(m.WorkersDir, 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(plannedPath); err == nil {
		return "", fmt.Errorf("worker path already exists: %s", plannedPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if _, err := runGit(ctx, m.RepoRoot, "worktree", "add", "--detach", plannedPath, "HEAD"); err != nil {
		return "", err
	}

	return plannedPath, nil
}

func (m Manager) Remove(ctx context.Context, worktreePath string) error {
	cleanPath := filepath.Clean(strings.TrimSpace(worktreePath))
	if cleanPath == "" {
		return errors.New("worker path is required")
	}
	if !pathWithinBase(m.WorkersDir, cleanPath) {
		return errors.New("worker path is outside the isolated workers directory")
	}

	support := DetectSupport(ctx, m.RepoRoot)
	if !support.Available {
		return errors.New(support.Detail)
	}

	if _, err := os.Stat(cleanPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	_, err := runGit(ctx, m.RepoRoot, "worktree", "remove", "--force", cleanPath)
	return err
}

func pathWithinBase(base string, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(target))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sanitizeWorkerName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var builder strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}

	return strings.Trim(builder.String(), "-")
}

func runGit(ctx context.Context, repoRoot string, args ...string) (string, error) {
	gitPath, err := lookPath("git")
	if err != nil {
		return "", err
	}

	cmdArgs := append([]string{"-C", repoRoot}, args...)
	cmd := execCommandContext(ctx, gitPath, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return "", errors.New(detail)
	}

	return strings.TrimSpace(string(output)), nil
}
