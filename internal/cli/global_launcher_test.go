package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairedUserPathMovesTargetDirectoryToFront(t *testing.T) {
	oldDir := filepath.Join(t.TempDir(), "old")
	targetDir := filepath.Join(t.TempDir(), "current", "bin")
	otherDir := filepath.Join(t.TempDir(), "other")
	pathValue := strings.Join([]string{oldDir, targetDir, otherDir, targetDir}, string(os.PathListSeparator))

	got, changed := repairedUserPath(pathValue, targetDir)
	if !changed {
		t.Fatal("repairedUserPath changed = false, want true")
	}
	want := strings.Join([]string{filepath.Clean(targetDir), oldDir, otherDir}, string(os.PathListSeparator))
	if got != want {
		t.Fatalf("repairedUserPath() = %q, want %q", got, want)
	}
}

func TestGlobalLauncherDetectionFindsStaleWinner(t *testing.T) {
	oldDir := filepath.Join(t.TempDir(), "old")
	targetDir := filepath.Join(t.TempDir(), "current", "bin")
	mustWriteFile(t, filepath.Join(oldDir, "orchestrator.exe"), "old")
	mustWriteFile(t, filepath.Join(targetDir, "orchestrator.exe"), "current")

	status := inspectGlobalLauncherFromPaths(globalLauncherPathInput{
		OS:           "windows",
		RepoRoot:     filepath.Dir(targetDir),
		TargetDir:    targetDir,
		TargetBinary: filepath.Join(targetDir, "orchestrator.exe"),
		TargetExists: true,
		ProcessPath:  strings.Join([]string{oldDir, targetDir}, string(os.PathListSeparator)),
		UserPath:     strings.Join([]string{oldDir, targetDir}, string(os.PathListSeparator)),
		PATHEXT:      ".EXE;.CMD",
	})

	if status.Status != "stale" {
		t.Fatalf("Status = %q, want stale", status.Status)
	}
	if !samePath(status.ProcessWinner, filepath.Join(oldDir, "orchestrator.exe")) {
		t.Fatalf("ProcessWinner = %q, want old install", status.ProcessWinner)
	}
	if len(status.StaleCandidates) == 0 {
		t.Fatal("StaleCandidates is empty, want old install warning")
	}
	if !strings.HasPrefix(status.DesiredUserPath, filepath.Clean(targetDir)) {
		t.Fatalf("DesiredUserPath = %q, want target dir first", status.DesiredUserPath)
	}
}

func TestGlobalLauncherDetectionFindsCurrentWinner(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "current", "bin")
	mustWriteFile(t, filepath.Join(targetDir, "orchestrator.exe"), "current")

	status := inspectGlobalLauncherFromPaths(globalLauncherPathInput{
		OS:           "windows",
		RepoRoot:     filepath.Dir(targetDir),
		TargetDir:    targetDir,
		TargetBinary: filepath.Join(targetDir, "orchestrator.exe"),
		TargetExists: true,
		ProcessPath:  targetDir,
		UserPath:     targetDir,
		PATHEXT:      ".EXE;.CMD",
	})

	if status.Status != "ok" {
		t.Fatalf("Status = %q, want ok", status.Status)
	}
	if !samePath(status.ProcessWinner, filepath.Join(targetDir, "orchestrator.exe")) {
		t.Fatalf("ProcessWinner = %q, want target binary", status.ProcessWinner)
	}
}

func TestGlobalLauncherDoctorChecksRecommendRepairForStaleWinner(t *testing.T) {
	status := globalLauncherStatus{
		CurrentExecutable: `D:\old\orchestrator.exe`,
		TargetBinary:      `D:\Projects\agentic_loop\bin\orchestrator.exe`,
		ProcessWinner:     `D:\old\orchestrator.exe`,
		Status:            "stale",
		Detail:            "global orchestrator resolves to a different install",
		RepairCommand:     `& 'D:\Projects\agentic_loop\bin\orchestrator.exe' install-global`,
		StaleCandidates: []globalLauncherCandidate{
			{Path: `D:\old\orchestrator.exe`},
		},
	}

	checks := globalLauncherDoctorChecks(status)
	joined := ""
	for _, check := range checks {
		joined += check.label + "=" + check.level + ":" + check.detail + "\n"
	}
	for _, want := range []string{
		"global launcher status=WARN",
		"repair command=INFO:& 'D:\\Projects\\agentic_loop\\bin\\orchestrator.exe' install-global",
		"stale install 1=WARN:D:\\old\\orchestrator.exe",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("doctor checks missing %q\n%s", want, joined)
		}
	}
}

func TestInstallGlobalCommandIsRegistered(t *testing.T) {
	app := NewApp(Options{})
	if _, ok := app.commands["install-global"]; !ok {
		t.Fatal("install-global command is not registered")
	}
	if _, ok := app.commands["repair-global"]; !ok {
		t.Fatal("repair-global alias is not registered")
	}
}

func TestRunInstallGlobalDryRunDoesNotMutatePath(t *testing.T) {
	originalPath := os.Getenv("PATH")
	var stdout bytes.Buffer
	err := runInstallGlobal(context.Background(), Invocation{
		Args:   []string{"--dry-run"},
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("runInstallGlobal(--dry-run) error = %v\n%s", err, stdout.String())
	}
	if os.Getenv("PATH") != originalPath {
		t.Fatal("dry-run changed PATH")
	}
	for _, want := range []string{
		"global.install.dry_run: true",
		"global.install.target_binary:",
		"global.install.refresh_command:",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("dry-run output missing %q\n%s", want, stdout.String())
		}
	}
}
