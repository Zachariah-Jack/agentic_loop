package workers

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManagerCreateCreatesIsolatedWorktree(t *testing.T) {
	repoRoot := t.TempDir()
	installFakeGit(t)

	manager := NewManager(repoRoot, filepath.Join(t.TempDir(), "workers"))
	worktreePath, err := manager.Create(context.Background(), "UI Worker")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if !pathWithinBase(manager.WorkersDir, worktreePath) {
		t.Fatalf("worktree path %q is not within %q", worktreePath, manager.WorkersDir)
	}
	if strings.HasPrefix(strings.ToLower(worktreePath), strings.ToLower(repoRoot+string(filepath.Separator))) {
		t.Fatalf("worktree path %q unexpectedly lives inside repo root %q", worktreePath, repoRoot)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("expected created worktree path %s: %v", worktreePath, err)
	}
}

func TestManagerCreateRejectsDuplicatePath(t *testing.T) {
	repoRoot := t.TempDir()
	installFakeGit(t)

	manager := NewManager(repoRoot, filepath.Join(t.TempDir(), "workers"))
	if _, err := manager.Create(context.Background(), "api-worker"); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}

	if _, err := manager.Create(context.Background(), "api-worker"); err == nil {
		t.Fatal("Create(second) unexpectedly succeeded for duplicate worktree path")
	}
}

func TestDetectSupportRejectsMissingGit(t *testing.T) {
	originalLookPath := lookPath
	lookPath = func(file string) (string, error) {
		return "", os.ErrNotExist
	}
	defer func() {
		lookPath = originalLookPath
	}()

	support := DetectSupport(context.Background(), t.TempDir())
	if support.Available {
		t.Fatal("DetectSupport() unexpectedly reported git worktree support available")
	}
}

func installFakeGit(t *testing.T) {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", binDir, err)
	}

	if runtime.GOOS == "windows" {
		script := `@echo off
setlocal EnableDelayedExpansion
if "%1"=="-C" (
  set REPO=%2
  shift
  shift
)
if "%1"=="rev-parse" (
  if "%FAKE_GIT_FAIL_REVPARSE%"=="1" (
    echo fake rev-parse failure 1>&2
    exit /b 1
  )
  echo true
  exit /b 0
)
if "%1"=="worktree" (
  if "%2"=="list" (
    echo !REPO!
    exit /b 0
  )
  if "%2"=="add" (
    if "%FAKE_GIT_FAIL_ADD%"=="1" (
      echo fake worktree add failure 1>&2
      exit /b 1
    )
    set TARGET=%4
    mkdir "!TARGET!" >nul 2>nul
    >"!TARGET!\.git" echo gitdir: fake
    exit /b 0
  )
  if "%2"=="remove" (
    if "%FAKE_GIT_FAIL_REMOVE%"=="1" (
      echo fake worktree remove failure 1>&2
      exit /b 1
    )
    set TARGET=%4
    if exist "!TARGET!" rmdir /s /q "!TARGET!"
    exit /b 0
  )
)
echo unsupported fake git invocation 1>&2
exit /b 1
`
		if err := os.WriteFile(filepath.Join(binDir, "git.cmd"), []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile(git.cmd) error = %v", err)
		}
	} else {
		script := `#!/bin/sh
if [ "$1" = "-C" ]; then
  REPO="$2"
  shift 2
fi
if [ "$1" = "rev-parse" ]; then
  if [ "${FAKE_GIT_FAIL_REVPARSE:-0}" = "1" ]; then
    echo "fake rev-parse failure" >&2
    exit 1
  fi
  echo true
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "list" ]; then
  echo "${REPO:-.}"
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "add" ]; then
  if [ "${FAKE_GIT_FAIL_ADD:-0}" = "1" ]; then
    echo "fake worktree add failure" >&2
    exit 1
  fi
  TARGET="$4"
  mkdir -p "$TARGET"
  printf 'gitdir: fake\n' > "$TARGET/.git"
  exit 0
fi
if [ "$1" = "worktree" ] && [ "$2" = "remove" ]; then
  if [ "${FAKE_GIT_FAIL_REMOVE:-0}" = "1" ]; then
    echo "fake worktree remove failure" >&2
    exit 1
  fi
  TARGET="$4"
  rm -rf "$TARGET"
  exit 0
fi
echo "unsupported fake git invocation" >&2
exit 1
`
		if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile(git) error = %v", err)
		}
	}

	pathValue := binDir
	if existing := os.Getenv("PATH"); existing != "" {
		pathValue += string(os.PathListSeparator) + existing
	}
	t.Setenv("PATH", pathValue)
}
