package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type globalLauncherCommandOptions struct {
	DryRun bool
}

type globalLauncherCandidate struct {
	Path     string
	Dir      string
	PathRank int
}

type globalLauncherStatus struct {
	OS                string
	RepoRoot          string
	CurrentExecutable string
	TargetDir         string
	TargetBinary      string
	TargetExists      bool
	ProcessWinner     string
	UserWinner        string
	Status            string
	Detail            string
	ProcessCandidates []globalLauncherCandidate
	UserCandidates    []globalLauncherCandidate
	StaleCandidates   []globalLauncherCandidate
	UserPathChanged   bool
	DesiredUserPath   string
	RefreshCommand    string
	RepairCommand     string
}

type globalLauncherInstallResult struct {
	Status         globalLauncherStatus
	Built          bool
	BuildDetail    string
	UserPathSet    bool
	ProcessPathSet bool
	DryRun         bool
}

func newInstallGlobalCommand(name string) Command {
	summary := "Install or repair the global orchestrator launcher for this checkout."
	if name == "repair-global" {
		summary = "Alias for install-global; repairs the global orchestrator launcher."
	}
	return Command{
		Name:    name,
		Summary: summary,
		Description: stringsJoin(
			"Usage:",
			"  orchestrator "+name+" [--dry-run]",
			"",
			"Builds the current checkout binary into repo-relative bin\\orchestrator.exe,",
			"moves that bin folder to the front of the User PATH on Windows, updates",
			"the current process PATH, and reports which orchestrator executable wins.",
			"",
			"It does not require admin rights and does not delete old installs.",
		),
		Run: runInstallGlobal,
	}
}

func runInstallGlobal(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("install-global", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	dryRun := fs.Bool("dry-run", false, "Print the global launcher repair plan without building or changing PATH.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newInstallGlobalCommand("install-global").Description)
			return nil
		}
		return err
	}

	result, err := installGlobalLauncher(ctx, inv, globalLauncherCommandOptions{DryRun: *dryRun})
	printGlobalLauncherInstallResult(inv.Stdout, result)
	return err
}

func installGlobalLauncher(ctx context.Context, inv Invocation, options globalLauncherCommandOptions) (globalLauncherInstallResult, error) {
	status, err := inspectGlobalLauncher(ctx, inv)
	if err != nil {
		return globalLauncherInstallResult{Status: status, DryRun: options.DryRun}, err
	}
	result := globalLauncherInstallResult{
		Status: status,
		DryRun: options.DryRun,
	}
	if options.DryRun {
		return result, nil
	}
	if runtime.GOOS != "windows" {
		return result, errors.New("install-global currently supports Windows User PATH repair only")
	}
	if strings.TrimSpace(status.RepoRoot) == "" {
		return result, errors.New("orchestrator checkout root could not be resolved")
	}

	buildDetail, err := buildGlobalLauncherBinary(ctx, status.RepoRoot, status.TargetBinary)
	result.BuildDetail = buildDetail
	if err != nil {
		return result, err
	}
	result.Built = true

	if err := setUserPath(ctx, status.DesiredUserPath); err != nil {
		return result, err
	}
	result.UserPathSet = true
	setCurrentProcessPath(prependPathDir(os.Getenv("PATH"), status.TargetDir))
	result.ProcessPathSet = true

	refreshed, refreshErr := inspectGlobalLauncher(ctx, inv)
	if refreshErr == nil {
		result.Status = refreshed
	}
	return result, refreshErr
}

func printGlobalLauncherInstallResult(stdout interface {
	Write([]byte) (int, error)
}, result globalLauncherInstallResult) {
	status := result.Status
	fmt.Fprintln(stdout, "global.install.product: Aurora Orchestrator")
	fmt.Fprintf(stdout, "global.install.os: %s\n", status.OS)
	fmt.Fprintf(stdout, "global.install.repo_root: %s\n", valueOrUnavailable(status.RepoRoot))
	fmt.Fprintf(stdout, "global.install.current_executable: %s\n", valueOrUnavailable(status.CurrentExecutable))
	fmt.Fprintf(stdout, "global.install.target_binary: %s\n", valueOrUnavailable(status.TargetBinary))
	if result.DryRun {
		fmt.Fprintln(stdout, "global.install.dry_run: true")
		fmt.Fprintln(stdout, "global.install.build: planned")
		fmt.Fprintln(stdout, "global.install.user_path: planned")
	} else {
		fmt.Fprintf(stdout, "global.install.build: %s\n", boolStatus(result.Built))
		if strings.TrimSpace(result.BuildDetail) != "" {
			fmt.Fprintf(stdout, "global.install.build_detail: %s\n", result.BuildDetail)
		}
		fmt.Fprintf(stdout, "global.install.user_path: %s\n", boolStatus(result.UserPathSet))
		fmt.Fprintf(stdout, "global.install.current_process_path: %s\n", boolStatus(result.ProcessPathSet))
	}
	fmt.Fprintf(stdout, "global.install.status: %s\n", status.Status)
	fmt.Fprintf(stdout, "global.install.detail: %s\n", status.Detail)
	fmt.Fprintf(stdout, "global.install.winner: %s\n", valueOrUnavailable(status.ProcessWinner))
	if len(status.StaleCandidates) > 0 {
		for i, candidate := range status.StaleCandidates {
			fmt.Fprintf(stdout, "global.install.stale.%d: %s\n", i+1, candidate.Path)
		}
	}
	fmt.Fprintln(stdout, "global.install.refresh: Restart PowerShell, or run this exact command to use the repaired PATH in this session.")
	fmt.Fprintf(stdout, "global.install.refresh_command: %s\n", status.RefreshCommand)
	fmt.Fprintf(stdout, "global.install.test_command: & %s version\n", quotePowerShell(status.TargetBinary))
}

func inspectGlobalLauncher(ctx context.Context, inv Invocation) (globalLauncherStatus, error) {
	currentExecutable := ""
	if executable, err := os.Executable(); err == nil {
		currentExecutable = filepath.Clean(executable)
	}
	repoRoot, repoErr := resolveOrchestratorCheckoutRoot(inv)
	targetDir := ""
	targetBinary := ""
	targetExists := false
	if repoRoot != "" {
		targetDir = filepath.Join(repoRoot, "bin")
		targetBinary = filepath.Join(targetDir, executableName("orchestrator"))
		targetExists = pathExists(targetBinary)
	}

	userPath := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		if read, err := readUserPath(ctx); err == nil {
			userPath = read
		}
	}
	processPath := os.Getenv("PATH")
	pathext := os.Getenv("PATHEXT")

	status := inspectGlobalLauncherFromPaths(globalLauncherPathInput{
		OS:                runtime.GOOS,
		RepoRoot:          repoRoot,
		CurrentExecutable: currentExecutable,
		TargetDir:         targetDir,
		TargetBinary:      targetBinary,
		TargetExists:      targetExists,
		ProcessPath:       processPath,
		UserPath:          userPath,
		PATHEXT:           pathext,
	})
	if repoErr != nil {
		status.Status = "unknown"
		status.Detail = repoErr.Error()
		return status, nil
	}
	return status, nil
}

type globalLauncherPathInput struct {
	OS                string
	RepoRoot          string
	CurrentExecutable string
	TargetDir         string
	TargetBinary      string
	TargetExists      bool
	ProcessPath       string
	UserPath          string
	PATHEXT           string
}

func inspectGlobalLauncherFromPaths(input globalLauncherPathInput) globalLauncherStatus {
	processCandidates := findOrchestratorCommands(input.ProcessPath, input.PATHEXT, input.OS)
	userCandidates := findOrchestratorCommands(input.UserPath, input.PATHEXT, input.OS)
	processWinner := ""
	if len(processCandidates) > 0 {
		processWinner = processCandidates[0].Path
	}
	userWinner := ""
	if len(userCandidates) > 0 {
		userWinner = userCandidates[0].Path
	}
	desiredUserPath := input.UserPath
	userPathChanged := false
	if strings.TrimSpace(input.TargetDir) != "" {
		desiredUserPath, userPathChanged = repairedUserPath(input.UserPath, input.TargetDir)
	}

	status := "missing"
	detail := "global orchestrator command was not found on PATH; run orchestrator install-global"
	if samePath(processWinner, input.TargetBinary) {
		status = "ok"
		detail = "global orchestrator resolves to this checkout"
	} else if processWinner != "" {
		status = "stale"
		detail = "global orchestrator resolves to a different install; run orchestrator install-global"
		if !input.TargetExists && strings.TrimSpace(input.TargetBinary) != "" {
			detail = "global orchestrator resolves to a different install and the target binary has not been built yet; run orchestrator install-global"
		}
	} else if !input.TargetExists && strings.TrimSpace(input.TargetBinary) != "" {
		status = "missing"
		detail = "target binary has not been built yet; run orchestrator install-global"
	}
	if strings.TrimSpace(input.TargetBinary) == "" {
		status = "unknown"
		detail = "target binary path is unavailable because checkout root was not resolved"
	}

	stale := make([]globalLauncherCandidate, 0)
	for _, candidate := range processCandidates {
		if !samePath(candidate.Path, input.TargetBinary) {
			stale = append(stale, candidate)
		}
	}

	refreshCommand := "$env:Path = " + quotePowerShell(input.TargetDir+";") + " + $env:Path; orchestrator version"
	repairCommand := repairCommandForLauncher(input)

	return globalLauncherStatus{
		OS:                input.OS,
		RepoRoot:          cleanPathOptional(input.RepoRoot),
		CurrentExecutable: cleanPathOptional(input.CurrentExecutable),
		TargetDir:         cleanPathOptional(input.TargetDir),
		TargetBinary:      cleanPathOptional(input.TargetBinary),
		TargetExists:      input.TargetExists,
		ProcessWinner:     cleanPathOptional(processWinner),
		UserWinner:        cleanPathOptional(userWinner),
		Status:            status,
		Detail:            detail,
		ProcessCandidates: processCandidates,
		UserCandidates:    userCandidates,
		StaleCandidates:   stale,
		UserPathChanged:   userPathChanged,
		DesiredUserPath:   desiredUserPath,
		RefreshCommand:    refreshCommand,
		RepairCommand:     repairCommand,
	}
}

func repairCommandForLauncher(input globalLauncherPathInput) string {
	current := strings.TrimSpace(input.CurrentExecutable)
	if current != "" && !looksLikeGoRunExecutable(current) {
		return "& " + quotePowerShell(current) + " install-global"
	}
	if strings.TrimSpace(input.RepoRoot) != "" {
		return "Set-Location " + quotePowerShell(input.RepoRoot) + "; go run .\\cmd\\orchestrator install-global"
	}
	if strings.TrimSpace(input.TargetBinary) != "" && input.TargetExists {
		return "& " + quotePowerShell(input.TargetBinary) + " install-global"
	}
	return ""
}

func looksLikeGoRunExecutable(path string) bool {
	lower := strings.ToLower(filepath.Clean(strings.TrimSpace(path)))
	return strings.Contains(lower, strings.ToLower(filepath.Join("go-build"))) ||
		strings.Contains(lower, strings.ToLower(filepath.Join("Temp", "go-build"))) ||
		strings.Contains(lower, strings.ToLower(filepath.Join("Local", "Temp", "go-build")))
}

func cleanPathOptional(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func resolveOrchestratorCheckoutRoot(inv Invocation) (string, error) {
	candidates := []string{}
	if env := strings.TrimSpace(os.Getenv("ORCHESTRATOR_REPO_ROOT")); env != "" {
		candidates = append(candidates, env)
	}
	if strings.TrimSpace(inv.RepoRoot) != "" {
		candidates = append(candidates, inv.RepoRoot)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executable))
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Dir(file))
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		cleaned := filepath.Clean(strings.TrimSpace(candidate))
		if cleaned == "." || cleaned == "" || seen[strings.ToLower(cleaned)] {
			continue
		}
		seen[strings.ToLower(cleaned)] = true
		if root, ok := findOrchestratorCheckoutRoot(cleaned); ok {
			return root, nil
		}
	}
	return "", errors.New("could not locate the orchestrator checkout root; run this command from the checkout or set ORCHESTRATOR_REPO_ROOT")
}

func findOrchestratorCheckoutRoot(start string) (string, bool) {
	current := filepath.Clean(start)
	for {
		goModPath := filepath.Join(current, "go.mod")
		mainPath := filepath.Join(current, "cmd", "orchestrator", "main.go")
		shellPath := filepath.Join(current, "console", "v2-shell", "package.json")
		if pathExists(goModPath) && pathExists(mainPath) && pathExists(shellPath) {
			if raw, err := os.ReadFile(goModPath); err == nil && strings.Contains(string(raw), "module orchestrator") {
				return current, true
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", false
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func buildGlobalLauncherBinary(ctx context.Context, repoRoot string, targetBinary string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(targetBinary), 0o755); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", targetBinary, "./cmd/orchestrator")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return detail, fmt.Errorf("build global launcher binary: %s", detail)
	}
	verify := exec.CommandContext(ctx, targetBinary, "version")
	verify.Dir = repoRoot
	verifyOutput, verifyErr := verify.CombinedOutput()
	if verifyErr != nil {
		detail := strings.TrimSpace(string(verifyOutput))
		if detail == "" {
			detail = verifyErr.Error()
		}
		return detail, fmt.Errorf("verify global launcher binary: %s", detail)
	}
	detail := strings.ReplaceAll(strings.TrimSpace(string(verifyOutput)), "\r\n", "; ")
	detail = strings.ReplaceAll(detail, "\n", "; ")
	return detail, nil
}

func readUserPath(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "[Environment]::GetEnvironmentVariable('Path','User')").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

func setUserPath(ctx context.Context, pathValue string) error {
	cmd := exec.CommandContext(
		ctx,
		"powershell",
		"-NoProfile",
		"-Command",
		"[Environment]::SetEnvironmentVariable('Path', $env:ORCHESTRATOR_REPAIRED_USER_PATH, 'User')",
	)
	cmd.Env = append(os.Environ(), "ORCHESTRATOR_REPAIRED_USER_PATH="+pathValue)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("update User PATH: %s", detail)
	}
	return nil
}

func repairedUserPath(pathValue string, targetDir string) (string, bool) {
	targetDir = filepath.Clean(strings.TrimSpace(targetDir))
	if targetDir == "" {
		return pathValue, false
	}
	entries := filepath.SplitList(pathValue)
	repaired := []string{targetDir}
	for _, entry := range entries {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" || samePath(trimmed, targetDir) {
			continue
		}
		repaired = append(repaired, trimmed)
	}
	next := strings.Join(repaired, string(os.PathListSeparator))
	return next, next != pathValue
}

func prependPathDir(pathValue string, targetDir string) string {
	next, _ := repairedUserPath(pathValue, targetDir)
	return next
}

func setCurrentProcessPath(pathValue string) {
	_ = os.Setenv("PATH", pathValue)
	if runtime.GOOS == "windows" {
		_ = os.Setenv("Path", pathValue)
	}
}

func findOrchestratorCommands(pathValue string, pathext string, goos string) []globalLauncherCandidate {
	extensions := executableExtensions(pathext, goos)
	seen := map[string]bool{}
	var out []globalLauncherCandidate
	for rank, dir := range filepath.SplitList(pathValue) {
		cleanDir := strings.TrimSpace(dir)
		if cleanDir == "" {
			continue
		}
		for _, ext := range extensions {
			path := filepath.Join(cleanDir, "orchestrator"+ext)
			if !pathExists(path) {
				continue
			}
			key := strings.ToLower(filepath.Clean(path))
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, globalLauncherCandidate{
				Path:     filepath.Clean(path),
				Dir:      filepath.Clean(cleanDir),
				PathRank: rank,
			})
		}
	}
	return out
}

func executableExtensions(pathext string, goos string) []string {
	if goos != "windows" {
		return []string{""}
	}
	if strings.TrimSpace(pathext) == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	var out []string
	for _, ext := range strings.Split(pathext, ";") {
		trimmed := strings.TrimSpace(ext)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, ".") {
			trimmed = "." + trimmed
		}
		out = append(out, strings.ToLower(trimmed))
	}
	if len(out) == 0 {
		return []string{".exe", ".cmd", ".bat"}
	}
	return out
}

func samePath(a string, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "not_changed"
}

func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
