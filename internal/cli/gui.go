package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type guiRecentState struct {
	LastRepoPath string `json:"last_repo_path,omitempty"`
}

func newGUICommand() Command {
	return Command{
		Name:    "gui",
		Summary: "Launch the Aurora Orchestrator GUI from any folder.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator gui [--repo PATH] [--addr HOST:PORT] [--dry-run]",
			"",
			"Launches the Aurora Orchestrator / AI Mission Control dashboard.",
			"If launched inside a Git repo, that repo is selected. Outside a Git repo,",
			"the last GUI repo is reused when known; otherwise the current folder opens",
			"so the GUI can guide first-run setup.",
			"",
			"The GUI uses the real local control protocol and does not change planner,",
			"executor, or completion authority.",
		),
		Run: runGUI,
	}
}

func runGUI(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	repoFlag := fs.String("repo", "", "Target repo path to open in the GUI.")
	addr := fs.String("addr", "127.0.0.1:44777", "Control server listen address.")
	dryRun := fs.Bool("dry-run", false, "Print the launch plan without starting the GUI.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newGUICommand().Description)
			return nil
		}
		return err
	}

	repoPath, repoSource, err := selectGUIRepoPath(inv, *repoFlag)
	if err != nil {
		return err
	}
	if pathExists(filepath.Join(repoPath, ".git")) {
		_ = saveGUIRecentState(inv.ConfigPath, guiRecentState{LastRepoPath: repoPath})
	}

	scriptPath, shellPath, err := resolveGUILaunchAssets()
	if err != nil {
		printGUIAssetGuidance(inv.Stdout, repoPath, *addr, err)
		return err
	}

	fmt.Fprintln(inv.Stdout, "gui.product: Aurora Orchestrator / AI Mission Control for Windows")
	fmt.Fprintf(inv.Stdout, "gui.repo: %s (%s)\n", repoPath, repoSource)
	fmt.Fprintf(inv.Stdout, "gui.control_addr: %s\n", *addr)
	fmt.Fprintf(inv.Stdout, "gui.shell: %s\n", shellPath)
	fmt.Fprintf(inv.Stdout, "gui.launcher: %s\n", scriptPath)
	if *dryRun {
		fmt.Fprintln(inv.Stdout, "gui.dry_run: true")
		fmt.Fprintln(inv.Stdout, "gui.status: launch plan ready")
		return nil
	}

	cmd := exec.CommandContext(ctx, "powershell", "-ExecutionPolicy", "Bypass", "-File", scriptPath, "-RepoPath", repoPath, "-ControlAddr", *addr)
	cmd.Stdout = inv.Stdout
	cmd.Stderr = inv.Stderr
	cmd.Stdin = inv.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch gui: %w", err)
	}
	return nil
}

func selectGUIRepoPath(inv Invocation, explicit string) (string, string, error) {
	if strings.TrimSpace(explicit) != "" {
		abs, err := filepath.Abs(strings.TrimSpace(explicit))
		if err != nil {
			return "", "", err
		}
		return filepath.Clean(abs), "explicit --repo", nil
	}
	if pathExists(filepath.Join(inv.RepoRoot, ".git")) {
		return filepath.Clean(inv.RepoRoot), "current Git repo", nil
	}
	if recent, err := loadGUIRecentState(inv.ConfigPath); err == nil && strings.TrimSpace(recent.LastRepoPath) != "" {
		if pathExists(recent.LastRepoPath) {
			return filepath.Clean(recent.LastRepoPath), "last GUI repo", nil
		}
	}
	return filepath.Clean(inv.RepoRoot), "current folder setup", nil
}

func resolveGUILaunchAssets() (string, string, error) {
	if override := strings.TrimSpace(os.Getenv("ORCHESTRATOR_GUI_SHELL_DIR")); override != "" {
		shellPath := filepath.Clean(override)
		scriptPath := filepath.Clean(filepath.Join(shellPath, "..", "..", "scripts", "start-v2-dogfood.ps1"))
		if pathExists(filepath.Join(shellPath, "package.json")) && pathExists(scriptPath) {
			return scriptPath, shellPath, nil
		}
	}

	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executable))
	}

	for _, start := range candidates {
		root := filepath.Clean(start)
		for {
			shellPath := filepath.Join(root, "console", "v2-shell")
			scriptPath := filepath.Join(root, "scripts", "start-v2-dogfood.ps1")
			if pathExists(filepath.Join(shellPath, "package.json")) && pathExists(scriptPath) {
				return filepath.Clean(scriptPath), filepath.Clean(shellPath), nil
			}
			parent := filepath.Dir(root)
			if parent == root {
				break
			}
			root = parent
		}
	}
	return "", "", errors.New("GUI shell assets were not found")
}

func executableDirPATHStatus() (string, string) {
	executable, err := os.Executable()
	if err != nil {
		return "WARN", "unable to inspect executable path: " + err.Error()
	}
	dir := filepath.Clean(filepath.Dir(executable))
	pathValue := os.Getenv("PATH")
	for _, entry := range filepath.SplitList(pathValue) {
		if strings.EqualFold(filepath.Clean(strings.TrimSpace(entry)), dir) {
			return "OK", fmt.Sprintf("%s is present on PATH", dir)
		}
	}
	return "INFO", fmt.Sprintf("%s is not on PATH; add it to launch `orchestrator gui` from any terminal", dir)
}

func printGUIAssetGuidance(writer io.Writer, repoPath string, addr string, launchErr error) {
	fmt.Fprintln(writer, "gui.product: Aurora Orchestrator / AI Mission Control for Windows")
	fmt.Fprintf(writer, "gui.repo: %s\n", repoPath)
	fmt.Fprintf(writer, "gui.control_addr: %s\n", addr)
	fmt.Fprintf(writer, "gui.status: unavailable (%s)\n", launchErr)
	fmt.Fprintln(writer, "gui.next_steps:")
	fmt.Fprintln(writer, "  1. Run this from the orchestrator checkout, or set ORCHESTRATOR_GUI_SHELL_DIR to console\\v2-shell.")
	fmt.Fprintln(writer, "  2. From the orchestrator checkout, run: powershell -ExecutionPolicy Bypass -File .\\scripts\\start-v2-dogfood.ps1 -RepoPath \""+repoPath+"\"")
	fmt.Fprintln(writer, "  3. For a packaged install, include console/v2-shell and scripts/start-v2-dogfood.ps1 beside the binary.")
}

func guiRecentStatePath(configPath string) string {
	dir := filepath.Dir(strings.TrimSpace(configPath))
	if dir == "." || dir == "" {
		return filepath.Join(".", "gui-recent.json")
	}
	return filepath.Join(dir, "gui-recent.json")
}

func loadGUIRecentState(configPath string) (guiRecentState, error) {
	raw, err := os.ReadFile(guiRecentStatePath(configPath))
	if err != nil {
		return guiRecentState{}, err
	}
	var state guiRecentState
	if err := json.Unmarshal(raw, &state); err != nil {
		return guiRecentState{}, err
	}
	return state, nil
}

func saveGUIRecentState(configPath string, state guiRecentState) error {
	path := guiRecentStatePath(configPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
