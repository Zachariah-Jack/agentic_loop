package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"orchestrator/internal/config"
)

func newSetupCommand() Command {
	return Command{
		Name:    "setup",
		Summary: "Capture or refresh the current v1 operator config.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator setup [--yes]",
			"",
			"Loads the current config, captures planner model plus optional ntfy settings,",
			"and writes the config back durably for the current operator environment.",
			"OPENAI_API_KEY remains environment-based and is not written into config.",
			"`--yes` keeps existing values or defaults where possible and writes without prompting.",
		),
		Run: runSetup,
	}
}

func runSetup(_ context.Context, inv Invocation) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(inv.Stderr)
	yes := fs.Bool("yes", false, "Write existing values/defaults without prompting.")
	if err := fs.Parse(inv.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(inv.Stdout, newSetupCommand().Description)
			return nil
		}
		return err
	}

	cfg, existed, err := loadSetupConfig(inv.ConfigPath)
	if err != nil {
		return err
	}

	repoContract := inspectTargetRepoContract(inv.RepoRoot)
	if !repoContract.Ready {
		confirmed := false
		cfg.RepoContractConfirmed = &confirmed
	}

	mode := "interactive"
	if *yes {
		mode = "non_interactive_yes"
		if cfg.RepoContractConfirmed == nil && repoContract.Ready {
			confirmed := true
			cfg.RepoContractConfirmed = &confirmed
		}
	} else {
		prompter := newSetupPrompter(inv.Stdin, inv.Stdout)
		if err := runInteractiveSetup(prompter, inv, &cfg, repoContract); err != nil {
			return err
		}
	}

	if err := config.Save(inv.ConfigPath, cfg); err != nil {
		return err
	}

	writeSetupSummary(inv.Stdout, inv.ConfigPath, cfg, setupSummary{
		Mode:         mode,
		ConfigState:  setupConfigState(existed),
		RepoRoot:     inv.RepoRoot,
		RepoContract: repoContract,
	})
	return nil
}

type setupPrompter struct {
	reader *bufio.Reader
	stdout io.Writer
}

type setupSummary struct {
	Mode         string
	ConfigState  string
	RepoRoot     string
	RepoContract repoContractStatus
}

func newSetupPrompter(stdin io.Reader, stdout io.Writer) setupPrompter {
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	return setupPrompter{
		reader: bufio.NewReader(stdin),
		stdout: stdout,
	}
}

func runInteractiveSetup(prompter setupPrompter, inv Invocation, cfg *config.Config, repoContract repoContractStatus) error {
	fmt.Fprintf(inv.Stdout, "config.path: %s\n", inv.ConfigPath)
	fmt.Fprintf(inv.Stdout, "repo.root: %s\n", inv.RepoRoot)
	fmt.Fprintf(inv.Stdout, "planner_api_key.environment: %s\n", plannerAPIKeyStatus())
	fmt.Fprintf(inv.Stdout, "repo_contract.markers_ready: %t\n", repoContract.Ready)
	if len(repoContract.Missing) > 0 {
		fmt.Fprintf(inv.Stdout, "repo_contract.missing_markers: %s\n", strings.Join(repoContract.Missing, ", "))
	}
	fmt.Fprintln(inv.Stdout, "ntfy.auth_token.note: when set, it is stored in the config file for v1")

	plannerModel, err := prompter.promptValue("planner model", cfg.PlannerModel, cfg.PlannerModel)
	if err != nil {
		return err
	}
	cfg.PlannerModel = strings.TrimSpace(plannerModel)

	driftWatcherEnabled, err := prompter.promptBool("drift watcher enabled", cfg.DriftWatcherEnabled)
	if err != nil {
		return err
	}
	cfg.DriftWatcherEnabled = driftWatcherEnabled

	serverURL, err := prompter.promptValue("ntfy server URL", displayValue(cfg.NTFY.ServerURL), cfg.NTFY.ServerURL)
	if err != nil {
		return err
	}
	cfg.NTFY.ServerURL = strings.TrimSpace(serverURL)

	topic, err := prompter.promptValue("ntfy topic", displayValue(cfg.NTFY.Topic), cfg.NTFY.Topic)
	if err != nil {
		return err
	}
	cfg.NTFY.Topic = strings.TrimSpace(topic)

	token, err := prompter.promptValue("ntfy auth token", maskedSecret(cfg.NTFY.AuthToken), cfg.NTFY.AuthToken)
	if err != nil {
		return err
	}
	cfg.NTFY.AuthToken = strings.TrimSpace(token)

	if repoContract.Ready {
		currentConfirmed := repoContractConfirmedValue(*cfg, repoContract)
		confirmed, err := prompter.promptBool("repo contract markers ready", currentConfirmed)
		if err != nil {
			return err
		}
		cfg.RepoContractConfirmed = boolPtr(confirmed)
	}

	return nil
}

func (p setupPrompter) promptValue(label string, currentDisplay string, currentValue string) (string, error) {
	if p.stdout == nil {
		p.stdout = io.Discard
	}
	if strings.TrimSpace(currentDisplay) == "" {
		currentDisplay = "unset"
	}

	fmt.Fprintf(p.stdout, "%s [%s]: ", label, currentDisplay)
	line, err := p.readLine()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(line) == "" {
		return currentValue, nil
	}
	return line, nil
}

func (p setupPrompter) promptBool(label string, current bool) (bool, error) {
	defaultValue := "y/N"
	if current {
		defaultValue = "Y/n"
	}

	fmt.Fprintf(p.stdout, "%s [%s]: ", label, defaultValue)
	line, err := p.readLine()
	if err != nil {
		return false, err
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return current, nil
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return current, nil
	}
}

func (p setupPrompter) readLine() (string, error) {
	if p.reader == nil {
		return "", nil
	}

	line, err := p.reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return strings.TrimRight(line, "\r\n"), nil
		}
		return "", err
	}

	return strings.TrimRight(line, "\r\n"), nil
}

func loadSetupConfig(path string) (config.Config, bool, error) {
	cfg, err := config.Load(path)
	if err == nil {
		return cfg, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return config.Default(), false, nil
	}
	return config.Config{}, false, err
}

func repoContractConfirmedValue(cfg config.Config, repoContract repoContractStatus) bool {
	if cfg.RepoContractConfirmed != nil {
		return *cfg.RepoContractConfirmed
	}
	return repoContract.Ready
}

func writeSetupSummary(stdout io.Writer, configPath string, cfg config.Config, summary setupSummary) {
	fmt.Fprintf(stdout, "setup.mode: %s\n", summary.Mode)
	fmt.Fprintf(stdout, "config.path: %s\n", configPath)
	fmt.Fprintf(stdout, "config.state: %s\n", summary.ConfigState)
	fmt.Fprintf(stdout, "saved.planner_model: %s\n", cfg.PlannerModel)
	fmt.Fprintf(stdout, "planner_api_key.environment: %s\n", plannerAPIKeyStatus())
	fmt.Fprintf(stdout, "saved.review.drift_watcher_enabled: %t\n", cfg.DriftWatcherEnabled)
	fmt.Fprintf(stdout, "saved.ntfy.server_url: %s\n", displayValue(cfg.NTFY.ServerURL))
	fmt.Fprintf(stdout, "saved.ntfy.topic: %s\n", displayValue(cfg.NTFY.Topic))
	fmt.Fprintf(stdout, "saved.ntfy.auth_token: %s\n", maskedTokenSummary(cfg.NTFY.AuthToken))
	fmt.Fprintf(stdout, "ntfy.configured: %t\n", ntfyConfigured(cfg))
	fmt.Fprintf(stdout, "repo_contract.markers_ready: %t\n", summary.RepoContract.Ready)
	fmt.Fprintf(stdout, "repo_contract.confirmed: %t\n", repoContractConfirmedValue(cfg, summary.RepoContract))
	if len(summary.RepoContract.Missing) == 0 {
		fmt.Fprintln(stdout, "repo_contract.missing_markers: none")
	} else {
		fmt.Fprintf(stdout, "repo_contract.missing_markers: %s\n", strings.Join(summary.RepoContract.Missing, ", "))
	}
}

func setupConfigState(existed bool) string {
	if existed {
		return "updated"
	}
	return "created"
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unset"
	}
	return strings.TrimSpace(value)
}

func maskedTokenSummary(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unset"
	}
	return maskedSecret(value) + " (stored in config file)"
}

func maskedSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unset"
	}
	if len(trimmed) <= 4 {
		return strings.Repeat("*", len(trimmed))
	}
	return trimmed[:2] + strings.Repeat("*", len(trimmed)-4) + trimmed[len(trimmed)-2:]
}

func boolPtr(value bool) *bool {
	return &value
}
