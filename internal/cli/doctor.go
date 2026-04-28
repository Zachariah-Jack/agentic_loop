package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/buildinfo"
	"orchestrator/internal/config"
	"orchestrator/internal/executor/appserver"
	"orchestrator/internal/journal"
	ntfybridge "orchestrator/internal/ntfy"
	"orchestrator/internal/plugins"
	"orchestrator/internal/state"
	workerctl "orchestrator/internal/workers"
)

func newDoctorCommand() Command {
	return Command{
		Name:    "doctor",
		Summary: "Check runtime, repo contract, planner, executor, ntfy, and persistence.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator doctor",
			"",
			"Prints grouped mechanical health checks for the installed runtime, target repo,",
			"planner prerequisites, executor readiness, ntfy bridge, and persistence.",
			"It does not run live planner or executor work.",
		),
		Run: runDoctor,
	}
}

func runDoctor(ctx context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newDoctorCommand().Description)
			return nil
		}
		return err
	}

	type check struct {
		section string
		label   string
		level   string
		detail  string
	}

	required := append([]repoContractEntry{
		{label: "repo marker .git", path: filepath.Join(inv.RepoRoot, ".git"), isDir: true},
	}, targetRepoContractEntries(inv.RepoRoot)...)

	build := buildinfo.Current()
	checks := make([]check, 0, len(required)+22)
	if executablePath, err := os.Executable(); err != nil {
		checks = append(checks, check{
			section: "runtime",
			label:   "binary path",
			level:   "WARN",
			detail:  err.Error(),
		})
	} else {
		checks = append(checks, check{
			section: "runtime",
			label:   "binary path",
			level:   "OK",
			detail:  executablePath,
		})
		checks = append(checks, check{
			section: "runtime",
			label:   "runtime mode",
			level:   "OK",
			detail:  runtimeMode(executablePath, inv.RepoRoot),
		})
	}
	checks = append(checks, check{
		section: "runtime",
		label:   "binary version",
		level:   "OK",
		detail:  firstNonEmpty(strings.TrimSpace(inv.Version), build.Version),
	})
	checks = append(checks, check{
		section: "runtime",
		label:   "binary revision",
		level:   "OK",
		detail:  build.Revision,
	})
	checks = append(checks, check{
		section: "runtime",
		label:   "binary build time",
		level:   "OK",
		detail:  build.BuildTime,
	})
	if launcher, err := inspectGlobalLauncher(ctx, inv); err != nil {
		checks = append(checks, check{
			section: "global_launcher",
			label:   "global launcher",
			level:   "WARN",
			detail:  err.Error(),
		})
	} else {
		globalChecks := globalLauncherDoctorChecks(launcher)
		for _, item := range globalChecks {
			checks = append(checks, check{
				section: "global_launcher",
				label:   item.label,
				level:   item.level,
				detail:  item.detail,
			})
		}
	}
	if scriptPath, shellPath, err := resolveGUILaunchAssets(); err != nil {
		checks = append(checks, check{
			section: "gui",
			label:   "aurora launcher assets",
			level:   "WARN",
			detail:  err.Error() + "; run from the orchestrator checkout or set ORCHESTRATOR_GUI_SHELL_DIR",
		})
	} else {
		checks = append(checks, check{
			section: "gui",
			label:   "aurora launcher assets",
			level:   "OK",
			detail:  fmt.Sprintf("shell=%s launcher=%s", shellPath, scriptPath),
		})
	}
	pathLevel, pathDetail := executableDirPATHStatus()
	checks = append(checks, check{
		section: "gui",
		label:   "global launch PATH",
		level:   pathLevel,
		detail:  pathDetail,
	})

	for _, requiredPath := range required {
		info, statErr := os.Stat(requiredPath.path)
		level := "OK"
		if statErr != nil || (requiredPath.isDir && statErr == nil && !info.IsDir()) || (!requiredPath.isDir && statErr == nil && info.IsDir()) {
			level = "FAIL"
		}
		checks = append(checks, check{
			section: "repo_contract",
			label:   requiredPath.label,
			level:   level,
			detail:  requiredPath.path,
		})
	}

	checks = append(checks, check{
		section: "planner",
		label:   "planner transport",
		level:   "OK",
		detail:  "responses_api",
	})
	if plannerAPIKey() == "" {
		checks = append(checks, check{
			section: "planner",
			label:   "planner API key",
			level:   "FAIL",
			detail:  "OPENAI_API_KEY missing",
		})
	} else {
		checks = append(checks, check{
			section: "planner",
			label:   "planner API key",
			level:   "OK",
			detail:  "present",
		})
	}
	modelHealth := buildModelHealthSnapshot(ctx, inv, nil)
	plannerModelLevel := "WARN"
	if modelHealth.Planner.VerificationState == "invalid" {
		plannerModelLevel = "FAIL"
	} else if modelHealth.Planner.VerificationState == "verified" {
		plannerModelLevel = "OK"
	}
	checks = append(checks, check{
		section: "planner",
		label:   "planner model",
		level:   plannerModelLevel,
		detail:  fmt.Sprintf("%s (%s)", valueOrUnavailable(modelHealth.Planner.ConfiguredModel), modelHealth.Planner.VerificationState),
	})

	cfgState := "missing"
	if _, err := config.Load(inv.ConfigPath); err == nil {
		cfgState = "loadable"
	} else if !errors.Is(err, os.ErrNotExist) {
		checks = append(checks, check{
			section: "config",
			label:   "config",
			level:   "FAIL",
			detail:  err.Error(),
		})
	}

	checks = append(checks, check{
		section: "config",
		label:   "config path",
		level:   "OK",
		detail:  fmt.Sprintf("%s (%s)", inv.ConfigPath, cfgState),
	})

	_, pluginSummary := plugins.Load(inv.RepoRoot)
	pluginLevel := "OK"
	if len(pluginSummary.Failures) > 0 {
		pluginLevel = "WARN"
	}
	checks = append(checks, check{
		section: "plugins",
		label:   "plugin directory",
		level:   pluginLevel,
		detail:  fmt.Sprintf("%s (found=%d loaded=%d failures=%d)", pluginSummary.Directory, pluginSummary.Found, pluginSummary.Loaded, len(pluginSummary.Failures)),
	})
	for _, failure := range pluginSummary.Failures {
		checks = append(checks, check{
			section: "plugins",
			label:   "plugin load failure",
			level:   "WARN",
			detail:  fmt.Sprintf("%s: %s", valueOrUnavailable(strings.TrimSpace(failure.Path)), failure.Message),
		})
	}

	if err := ensureTargetRepoContractDirs(inv.Layout); err != nil {
		checks = append(checks, check{
			section: "persistence",
			label:   "runtime directories",
			level:   "FAIL",
			detail:  err.Error(),
		})
	} else {
		checks = append(checks, check{
			section: "persistence",
			label:   "runtime directories",
			level:   "OK",
			detail:  inv.Layout.RootDir,
		})
	}

	if err := checkSQLiteSurface(ctx, inv.Layout); err != nil {
		checks = append(checks, check{
			section: "persistence",
			label:   "sqlite state path",
			level:   "FAIL",
			detail:  err.Error(),
		})
	} else {
		checks = append(checks, check{
			section: "persistence",
			label:   "sqlite state path",
			level:   "OK",
			detail:  inv.Layout.DBPath + " (read/write ready)",
		})
	}

	if err := checkJournalSurface(inv.Layout); err != nil {
		checks = append(checks, check{
			section: "persistence",
			label:   "journal path",
			level:   "FAIL",
			detail:  err.Error(),
		})
	} else {
		checks = append(checks, check{
			section: "persistence",
			label:   "journal path",
			level:   "OK",
			detail:  inv.Layout.JournalPath + " (read/write ready)",
		})
	}

	launchPlan, err := appserver.ResolveLaunchPlan()
	if err != nil {
		checks = append(checks, check{
			section: "executor",
			label:   "codex app-server",
			level:   "FAIL",
			detail:  err.Error(),
		})
	} else {
		checks = append(checks, check{
			section: "executor",
			label:   "codex app-server",
			level:   "OK",
			detail:  launchPlan.Command,
		})
	}
	executorModelLevel := "WARN"
	if modelHealth.Executor.VerificationState == "invalid" || modelHealth.Executor.VerificationState == "unavailable" || modelHealth.Executor.ModelUnavailable {
		executorModelLevel = "FAIL"
	} else if modelHealth.Executor.VerificationState == "verified" {
		executorModelLevel = "OK"
	}
	checks = append(checks, check{
		section: "executor",
		label:   "codex required model",
		level:   executorModelLevel,
		detail:  fmt.Sprintf("%s (%s)", valueOrUnavailable(modelHealth.Executor.RequestedModel), modelHealth.Executor.VerificationState),
	})
	checks = append(checks, check{
		section: "executor",
		label:   "codex access requirement",
		level:   executorModelLevel,
		detail:  fmt.Sprintf("%s, effort %s", valueOrUnavailable(modelHealth.Executor.AccessMode), valueOrUnavailable(modelHealth.Executor.Effort)),
	})
	if modelHealth.Executor.CodexExecutablePath != "" || modelHealth.Executor.CodexVersion != "" {
		checks = append(checks, check{
			section: "executor",
			label:   "codex resolved path",
			level:   executorModelLevel,
			detail:  fmt.Sprintf("%s (%s)", valueOrUnavailable(modelHealth.Executor.CodexExecutablePath), valueOrUnavailable(modelHealth.Executor.CodexVersion)),
		})
	}

	workerSupport := workerctl.DetectSupport(ctx, inv.RepoRoot)
	workerLevel := "OK"
	if !workerSupport.Available {
		workerLevel = "FAIL"
	}
	checks = append(checks, check{
		section: "workers",
		label:   "workers directory",
		level:   "OK",
		detail:  inv.Layout.WorkersDir,
	})
	checks = append(checks, check{
		section: "workers",
		label:   "git worktree support",
		level:   workerLevel,
		detail:  workerSupport.Detail,
	})

	if !ntfyConfigured(inv.Config) {
		checks = append(checks, check{
			section: "ntfy",
			label:   "ntfy config",
			level:   "INFO",
			detail:  "not configured (terminal fallback available)",
		})
	} else {
		client, err := ntfybridge.NewClient(inv.Config.NTFY)
		if err != nil {
			checks = append(checks, check{
				section: "ntfy",
				label:   "ntfy config",
				level:   "FAIL",
				detail:  err.Error(),
			})
		} else {
			checks = append(checks, check{
				section: "ntfy",
				label:   "ntfy config",
				level:   "OK",
				detail:  fmt.Sprintf("%s topic=%s", client.ServerURL(), client.Topic()),
			})

			healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			health, err := client.HealthCheck(healthCtx)
			if err != nil {
				checks = append(checks, check{
					section: "ntfy",
					label:   "ntfy readiness",
					level:   "WARN",
					detail:  err.Error(),
				})
			} else {
				checks = append(checks, check{
					section: "ntfy",
					label:   "ntfy readiness",
					level:   "OK",
					detail:  fmt.Sprintf("/v1/health healthy=%t", health.Healthy),
				})
			}
		}
	}

	hasFailure := false
	sections := []string{"runtime", "global_launcher", "gui", "repo_contract", "config", "plugins", "planner", "executor", "workers", "ntfy", "persistence"}
	for _, section := range sections {
		fmt.Fprintf(inv.Stdout, "%s:\n", section)
		for _, item := range checks {
			if item.section != section {
				continue
			}
			if item.level == "FAIL" {
				hasFailure = true
			}
			fmt.Fprintf(inv.Stdout, "  [%s] %s: %s\n", item.level, item.label, item.detail)
		}
	}

	if hasFailure {
		return errors.New("doctor found runtime issues")
	}

	return nil
}

func globalLauncherDoctorChecks(status globalLauncherStatus) []struct {
	label  string
	level  string
	detail string
} {
	level := "OK"
	if status.Status == "stale" || status.Status == "missing" || status.Status == "unknown" {
		level = "WARN"
	}
	out := []struct {
		label  string
		level  string
		detail string
	}{
		{label: "current binary path", level: "OK", detail: valueOrUnavailable(status.CurrentExecutable)},
		{label: "desired global binary", level: level, detail: valueOrUnavailable(status.TargetBinary)},
		{label: "global orchestrator winner", level: level, detail: valueOrUnavailable(status.ProcessWinner)},
		{label: "global launcher status", level: level, detail: status.Detail},
	}
	if status.Status != "ok" {
		command := "orchestrator install-global"
		if strings.TrimSpace(status.RepairCommand) != "" {
			command = status.RepairCommand
		}
		out = append(out, struct {
			label  string
			level  string
			detail string
		}{label: "repair command", level: "INFO", detail: command})
	}
	for i, candidate := range status.StaleCandidates {
		out = append(out, struct {
			label  string
			level  string
			detail string
		}{label: fmt.Sprintf("stale install %d", i+1), level: "WARN", detail: candidate.Path})
	}
	return out
}

func checkSQLiteSurface(ctx context.Context, layout state.Layout) error {
	store, err := state.Open(layout.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	return store.EnsureSchema(ctx)
}

func checkJournalSurface(layout state.Layout) error {
	journalWriter, err := journal.Open(layout.JournalPath)
	if err != nil {
		return err
	}

	file, err := os.Open(layout.JournalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.ReadAll(io.LimitReader(file, 1))
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	_ = journalWriter
	return nil
}

func runtimeMode(executablePath string, repoRoot string) string {
	executablePath = filepath.Clean(strings.TrimSpace(executablePath))
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	if executablePath == "" || repoRoot == "" {
		return "unknown"
	}

	executableLower := strings.ToLower(executablePath)
	repoLower := strings.ToLower(repoRoot)
	if executableLower == repoLower || strings.HasPrefix(executableLower, repoLower+string(os.PathSeparator)) {
		return "repo_checkout"
	}
	return "external_runtime"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
