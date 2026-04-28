package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"orchestrator/internal/activity"
	"orchestrator/internal/config"
	"orchestrator/internal/logging"
	"orchestrator/internal/runtimecfg"
	"orchestrator/internal/state"
)

type Options struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
}

type App struct {
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	version  string
	commands map[string]Command
}

type Command struct {
	Name        string
	Summary     string
	Description string
	Run         func(context.Context, Invocation) error
}

type Invocation struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Args       []string
	Config     config.Config
	ConfigPath string
	RuntimeCfg *runtimecfg.Manager
	Events     *activity.Broker
	Logger     *logging.Logger
	RepoRoot   string
	Layout     state.Layout
	Version    string
}

func NewApp(opts Options) *App {
	app := &App{
		stdin:   opts.Stdin,
		stdout:  opts.Stdout,
		stderr:  opts.Stderr,
		version: opts.Version,
	}

	app.commands = map[string]Command{
		"auto":           newAutoCommand(),
		"control":        newControlCommand(),
		"continue":       newContinueCommand(),
		"doctor":         newDoctorCommand(),
		"executor":       newExecutorCommand(),
		"executor-probe": newExecutorProbeCommand(),
		"gui":            newGUICommand(),
		"history":        newHistoryCommand(),
		"init":           newInitCommand(),
		"resume":         newResumeCommand(),
		"run":            newRunCommand(),
		"setup":          newSetupCommand(),
		"settings":       newSettingsCommand(),
		"status":         newStatusCommand(),
		"update":         newUpdateCommand(),
		"version":        newVersionCommand(),
		"workers":        newWorkersCommand(),
	}

	return app
}

func (a *App) Execute(ctx context.Context, args []string) error {
	global, remainder, err := parseGlobalFlags(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			a.printRootHelp()
			return nil
		}
		return err
	}

	if len(remainder) == 0 {
		a.printRootHelp()
		return nil
	}

	name := remainder[0]
	if name == "help" {
		a.printRequestedHelp(remainder[1:])
		return nil
	}

	cmd, ok := a.commands[name]
	if !ok {
		return fmt.Errorf("unknown command %q", name)
	}

	cfgPath, err := config.ResolvePath(global.ConfigPath)
	if err != nil {
		return err
	}

	cfg := config.Default()
	if loaded, err := config.Load(cfgPath); err == nil {
		cfg = loaded
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	repoRoot := resolveRepoRoot(wd)
	layout := state.ResolveLayout(repoRoot)
	logger := logging.New(a.stderr, cfg.LogLevel)
	runtimeManager := runtimecfg.NewManager(cfgPath, cfg)
	inv := Invocation{
		Stdin:      a.stdin,
		Stdout:     a.stdout,
		Stderr:     a.stderr,
		Args:       remainder[1:],
		Config:     runtimeManager.Snapshot(),
		ConfigPath: cfgPath,
		RuntimeCfg: runtimeManager,
		Events:     activity.NewBroker(activity.DefaultHistoryLimit),
		Logger:     logger,
		RepoRoot:   repoRoot,
		Layout:     layout,
		Version:    a.version,
	}

	return cmd.Run(ctx, inv)
}

type globalFlags struct {
	ConfigPath string
}

func parseGlobalFlags(args []string) (globalFlags, []string, error) {
	var out globalFlags

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			return out, nil, flag.ErrHelp
		case arg == "--config":
			if i+1 >= len(args) {
				return out, nil, fmt.Errorf("missing value for --config")
			}
			out.ConfigPath = args[i+1]
			i++
		case strings.HasPrefix(arg, "--config="):
			out.ConfigPath = strings.TrimPrefix(arg, "--config=")
		case strings.HasPrefix(arg, "-"):
			return out, nil, fmt.Errorf("unknown global flag %q", arg)
		default:
			return out, args[i:], nil
		}
	}

	return out, nil, nil
}

func (a *App) printRootHelp() {
	fmt.Fprintln(a.stdout, "orchestrator is an inert CLI shell for a planner-led orchestrator.")
	fmt.Fprintln(a.stdout, "")
	fmt.Fprintln(a.stdout, "The CLI manages local setup, target-repo scaffolding, persistence, visibility, and runtime wiring. It does not own planner decisions, executor work, or stop conditions.")
	fmt.Fprintln(a.stdout, "")
	fmt.Fprintln(a.stdout, "Usage:")
	fmt.Fprintln(a.stdout, "  orchestrator [--config PATH] <command> [args]")
	fmt.Fprintln(a.stdout, "")
	fmt.Fprintln(a.stdout, "Typical flow:")
	fmt.Fprintln(a.stdout, "  orchestrator gui")
	fmt.Fprintln(a.stdout, "  setup -> init -> run -> continue/status/history/doctor")
	fmt.Fprintln(a.stdout, "")
	fmt.Fprintln(a.stdout, "Commands:")

	names := make([]string, 0, len(a.commands))
	for name := range a.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cmd := a.commands[name]
		fmt.Fprintf(a.stdout, "  %-8s %s\n", cmd.Name, cmd.Summary)
	}

	fmt.Fprintln(a.stdout, "")
	fmt.Fprintln(a.stdout, "Global flags:")
	fmt.Fprintln(a.stdout, "  --config PATH  Use an explicit config file path.")
	fmt.Fprintln(a.stdout, "  -h, --help     Show this help.")
	fmt.Fprintln(a.stdout, "")
	fmt.Fprintln(a.stdout, "Use \"orchestrator help <command>\" for command details.")
}

func (a *App) printRequestedHelp(args []string) {
	if len(args) == 0 {
		a.printRootHelp()
		return
	}

	cmd, ok := a.commands[args[0]]
	if !ok {
		fmt.Fprintf(a.stderr, "error: unknown command %q\n", args[0])
		return
	}

	fmt.Fprintln(a.stdout, cmd.Description)
}

func stringsJoin(lines ...string) string {
	return strings.Join(lines, "\n")
}

func resolveRepoRoot(start string) string {
	current := filepath.Clean(start)
	for {
		if info, err := os.Stat(filepath.Join(current, ".git")); err == nil && info.IsDir() {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(start)
		}
		current = parent
	}
}
