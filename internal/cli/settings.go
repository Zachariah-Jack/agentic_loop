package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"orchestrator/internal/config"
	"orchestrator/internal/runtimecfg"
)

func newSettingsCommand() Command {
	return Command{
		Name:    "settings",
		Summary: "View or update runtime settings such as timeouts and permission profile.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator settings show",
			"  orchestrator settings set-timeout NAME VALUE",
			"  orchestrator settings set-permission PROFILE",
			"",
			"Timeout VALUE can be a duration like 30m, 2h, or unlimited.",
			"Timeout NAME examples: executor_turn_timeout, human_wait_timeout, install_timeout.",
		),
		Run: runSettings,
	}
}

func runSettings(_ context.Context, inv Invocation) error {
	if len(inv.Args) == 0 || inv.Args[0] == "show" {
		printSettings(inv)
		return nil
	}
	switch inv.Args[0] {
	case "set-timeout":
		fs := flag.NewFlagSet("settings set-timeout", flag.ContinueOnError)
		fs.SetOutput(inv.Stderr)
		if err := fs.Parse(inv.Args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprintln(inv.Stdout, newSettingsCommand().Description)
				return nil
			}
			return err
		}
		if fs.NArg() != 2 {
			return errors.New("settings set-timeout requires NAME and VALUE")
		}
		patch, err := timeoutPatchByName(fs.Arg(0), fs.Arg(1))
		if err != nil {
			return err
		}
		cfg, _, err := inv.RuntimeCfg.ApplyPatch(runtimecfg.Patch{Timeouts: patch})
		if err != nil {
			return err
		}
		inv.Config = cfg
		printSettings(inv)
		return nil
	case "set-permission":
		fs := flag.NewFlagSet("settings set-permission", flag.ContinueOnError)
		fs.SetOutput(inv.Stderr)
		if err := fs.Parse(inv.Args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("settings set-permission requires PROFILE")
		}
		profile := fs.Arg(0)
		cfg, _, err := inv.RuntimeCfg.ApplyPatch(runtimecfg.Patch{PermissionProfile: &profile})
		if err != nil {
			return err
		}
		inv.Config = cfg
		printSettings(inv)
		return nil
	default:
		return fmt.Errorf("unknown settings action %q", inv.Args[0])
	}
}

func printSettings(inv Invocation) {
	cfg := currentConfig(inv)
	fmt.Fprintln(inv.Stdout, "settings:")
	fmt.Fprintf(inv.Stdout, "  config_path: %s\n", inv.ConfigPath)
	fmt.Fprintf(inv.Stdout, "  verbosity: %s\n", cfg.Verbosity)
	fmt.Fprintf(inv.Stdout, "  planner_model: %s\n", resolvePlannerModel(inv))
	fmt.Fprintf(inv.Stdout, "  permission_profile: %s\n", cfg.Permissions.Profile)
	fmt.Fprintln(inv.Stdout, "  timeouts:")
	fmt.Fprintf(inv.Stdout, "    planner_request_timeout: %s\n", cfg.Timeouts.PlannerRequestTimeout)
	fmt.Fprintf(inv.Stdout, "    executor_idle_timeout: %s\n", cfg.Timeouts.ExecutorIdleTimeout)
	fmt.Fprintf(inv.Stdout, "    executor_turn_timeout: %s\n", cfg.Timeouts.ExecutorTurnTimeout)
	fmt.Fprintf(inv.Stdout, "    subagent_timeout: %s\n", cfg.Timeouts.SubagentTimeout)
	fmt.Fprintf(inv.Stdout, "    shell_command_timeout: %s\n", cfg.Timeouts.ShellCommandTimeout)
	fmt.Fprintf(inv.Stdout, "    install_timeout: %s\n", cfg.Timeouts.InstallTimeout)
	fmt.Fprintf(inv.Stdout, "    human_wait_timeout: %s\n", cfg.Timeouts.HumanWaitTimeout)
}

func timeoutPatchByName(name string, value string) (runtimecfg.TimeoutPatch, error) {
	if err := config.ValidateTimeoutValue(name, value); err != nil {
		return runtimecfg.TimeoutPatch{}, err
	}
	v := runtimecfg.OptionalString{Set: true, Value: value}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "planner_request_timeout":
		return runtimecfg.TimeoutPatch{PlannerRequestTimeout: v}, nil
	case "executor_idle_timeout":
		return runtimecfg.TimeoutPatch{ExecutorIdleTimeout: v}, nil
	case "executor_turn_timeout":
		return runtimecfg.TimeoutPatch{ExecutorTurnTimeout: v}, nil
	case "subagent_timeout":
		return runtimecfg.TimeoutPatch{SubagentTimeout: v}, nil
	case "shell_command_timeout":
		return runtimecfg.TimeoutPatch{ShellCommandTimeout: v}, nil
	case "install_timeout":
		return runtimecfg.TimeoutPatch{InstallTimeout: v}, nil
	case "human_wait_timeout":
		return runtimecfg.TimeoutPatch{HumanWaitTimeout: v}, nil
	default:
		return runtimecfg.TimeoutPatch{}, fmt.Errorf("unknown timeout field %q", name)
	}
}
