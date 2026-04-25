package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	appName    = "orchestrator"
	configFile = "config.json"
)

const (
	PlannerModelLatestGPT5  = "gpt-5-latest"
	PlannerModelMinimumGPT5 = "gpt-5.4"

	RequiredCodexExecutorModel     = "gpt-5.5"
	RequiredCodexApprovalPolicy    = "never"
	RequiredCodexSandboxMode       = "danger-full-access"
	RequiredCodexReasoningEffort   = "xhigh"
	RequiredCodexTurnSandboxPolicy = "dangerFullAccess"

	VerbosityQuiet   = "quiet"
	VerbosityNormal  = "normal"
	VerbosityVerbose = "verbose"
	VerbosityTrace   = "trace"
)

type Config struct {
	Version                int         `json:"version"`
	LogLevel               string      `json:"log_level"`
	Verbosity              string      `json:"verbosity"`
	PlannerModel           string      `json:"planner_model"`
	WorkerConcurrencyLimit int         `json:"worker_concurrency_limit,omitempty"`
	DriftWatcherEnabled    bool        `json:"drift_watcher_enabled,omitempty"`
	RepoContractConfirmed  *bool       `json:"repo_contract_confirmed,omitempty"`
	Timeouts               Timeouts    `json:"timeouts"`
	Permissions            Permissions `json:"permissions"`
	Updates                Updates     `json:"updates"`
	NTFY                   NTFYConfig  `json:"ntfy"`
}

type Timeouts struct {
	PlannerRequestTimeout string `json:"planner_request_timeout"`
	ExecutorIdleTimeout   string `json:"executor_idle_timeout"`
	ExecutorTurnTimeout   string `json:"executor_turn_timeout"`
	SubagentTimeout       string `json:"subagent_timeout"`
	ShellCommandTimeout   string `json:"shell_command_timeout"`
	InstallTimeout        string `json:"install_timeout"`
	HumanWaitTimeout      string `json:"human_wait_timeout"`
}

type Permissions struct {
	Profile                         string `json:"profile"`
	AskBeforeInstallingPrograms     bool   `json:"ask_before_installing_programs"`
	AskBeforeInstallingDependencies bool   `json:"ask_before_installing_dependencies"`
	AskBeforeOutsideRepoChanges     bool   `json:"ask_before_modifying_files_outside_repo"`
	AskBeforeDeletingFiles          bool   `json:"ask_before_deleting_files"`
	AskBeforeRunningTests           bool   `json:"ask_before_running_tests"`
	AskBeforeEmulatorTesting        bool   `json:"ask_before_emulator_device_testing"`
	AskBeforeNetworkCalls           bool   `json:"ask_before_network_calls"`
	AskBeforeGitCommits             bool   `json:"ask_before_git_commits"`
	AskBeforeGitPushes              bool   `json:"ask_before_git_pushes"`
	AskBeforeUpdaterInstalls        bool   `json:"ask_before_updater_installs"`
	AskBeforeWorkerParallelism      bool   `json:"ask_before_worker_parallelism"`
	AskBeforeExecutorSteering       bool   `json:"ask_before_executor_steering"`
	AskBeforePlannerDirection       bool   `json:"ask_before_planner_direction_changes"`
}

type Updates struct {
	UpdateChannel        string   `json:"update_channel"`
	AutoCheckUpdates     bool     `json:"auto_check_updates"`
	AutoDownloadUpdates  bool     `json:"auto_download_updates"`
	AutoInstallUpdates   bool     `json:"auto_install_updates"`
	AskBeforeUpdate      bool     `json:"ask_before_update"`
	IncludePrereleases   bool     `json:"include_prereleases"`
	UpdateCheckInterval  string   `json:"update_check_interval"`
	LastUpdateCheckAt    string   `json:"last_update_check_at,omitempty"`
	LastSeenVersion      string   `json:"last_seen_version,omitempty"`
	LastInstalledVersion string   `json:"last_installed_version,omitempty"`
	SkippedVersions      []string `json:"skipped_versions,omitempty"`
}

type NTFYConfig struct {
	ServerURL string `json:"server_url"`
	Topic     string `json:"topic"`
	// AuthToken is stored in the plain JSON config file for v1 when set.
	AuthToken string `json:"auth_token,omitempty"`
}

func Default() Config {
	return Config{
		Version:                1,
		LogLevel:               "info",
		Verbosity:              "normal",
		PlannerModel:           PlannerModelLatestGPT5,
		WorkerConcurrencyLimit: 2,
		Timeouts:               DefaultTimeouts(),
		Permissions:            DefaultPermissions(),
		Updates:                DefaultUpdates(),
	}
}

func DefaultTimeouts() Timeouts {
	return Timeouts{
		PlannerRequestTimeout: "2m",
		ExecutorIdleTimeout:   "unlimited",
		ExecutorTurnTimeout:   "unlimited",
		SubagentTimeout:       "unlimited",
		ShellCommandTimeout:   "30m",
		InstallTimeout:        "2h",
		HumanWaitTimeout:      "unlimited",
	}
}

func DefaultPermissions() Permissions {
	return Permissions{
		Profile:                         "balanced",
		AskBeforeInstallingPrograms:     true,
		AskBeforeInstallingDependencies: false,
		AskBeforeOutsideRepoChanges:     true,
		AskBeforeDeletingFiles:          true,
		AskBeforeRunningTests:           false,
		AskBeforeEmulatorTesting:        true,
		AskBeforeNetworkCalls:           false,
		AskBeforeGitCommits:             true,
		AskBeforeGitPushes:              true,
		AskBeforeUpdaterInstalls:        true,
		AskBeforeWorkerParallelism:      true,
		AskBeforeExecutorSteering:       true,
		AskBeforePlannerDirection:       true,
	}
}

func DefaultUpdates() Updates {
	return Updates{
		UpdateChannel:       "stable",
		AutoCheckUpdates:    true,
		AutoDownloadUpdates: false,
		AutoInstallUpdates:  false,
		AskBeforeUpdate:     true,
		IncludePrereleases:  false,
		UpdateCheckInterval: "24h",
	}
}

func ResolvePath(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}

	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(root, appName, configFile), nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	if cfg.Version == 0 {
		return Config{}, errors.New("config version is required")
	}

	return WithDefaults(cfg), nil
}

func Save(path string, cfg Config) error {
	cfg = WithDefaults(cfg)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o600)
}

func WithDefaults(cfg Config) Config {
	defaults := Default()

	if cfg.Version == 0 {
		cfg.Version = defaults.Version
	}
	if strings.TrimSpace(cfg.LogLevel) == "" {
		cfg.LogLevel = defaults.LogLevel
	}
	if strings.TrimSpace(cfg.Verbosity) == "" {
		cfg.Verbosity = defaults.Verbosity
	}
	if strings.TrimSpace(cfg.PlannerModel) == "" {
		cfg.PlannerModel = defaults.PlannerModel
	}
	if cfg.WorkerConcurrencyLimit <= 0 {
		cfg.WorkerConcurrencyLimit = defaults.WorkerConcurrencyLimit
	}
	cfg.Timeouts = NormalizeTimeouts(cfg.Timeouts)
	cfg.Permissions = NormalizePermissions(cfg.Permissions)
	cfg.Updates = NormalizeUpdates(cfg.Updates)

	return cfg
}

func NormalizeVerbosity(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", VerbosityNormal:
		return VerbosityNormal, nil
	case VerbosityQuiet:
		return VerbosityQuiet, nil
	case VerbosityVerbose:
		return VerbosityVerbose, nil
	case VerbosityTrace:
		return VerbosityTrace, nil
	default:
		return "", errors.New("verbosity must be one of quiet, normal, verbose, or trace")
	}
}

func NormalizeTimeouts(value Timeouts) Timeouts {
	defaults := DefaultTimeouts()
	if strings.TrimSpace(value.PlannerRequestTimeout) == "" {
		value.PlannerRequestTimeout = defaults.PlannerRequestTimeout
	}
	if strings.TrimSpace(value.ExecutorIdleTimeout) == "" {
		value.ExecutorIdleTimeout = defaults.ExecutorIdleTimeout
	}
	if strings.TrimSpace(value.ExecutorTurnTimeout) == "" {
		value.ExecutorTurnTimeout = defaults.ExecutorTurnTimeout
	}
	if strings.TrimSpace(value.SubagentTimeout) == "" {
		value.SubagentTimeout = defaults.SubagentTimeout
	}
	if strings.TrimSpace(value.ShellCommandTimeout) == "" {
		value.ShellCommandTimeout = defaults.ShellCommandTimeout
	}
	if strings.TrimSpace(value.InstallTimeout) == "" {
		value.InstallTimeout = defaults.InstallTimeout
	}
	if strings.TrimSpace(value.HumanWaitTimeout) == "" {
		value.HumanWaitTimeout = defaults.HumanWaitTimeout
	}
	value.PlannerRequestTimeout = NormalizeTimeoutValue(value.PlannerRequestTimeout)
	value.ExecutorIdleTimeout = NormalizeTimeoutValue(value.ExecutorIdleTimeout)
	value.ExecutorTurnTimeout = NormalizeTimeoutValue(value.ExecutorTurnTimeout)
	value.SubagentTimeout = NormalizeTimeoutValue(value.SubagentTimeout)
	value.ShellCommandTimeout = NormalizeTimeoutValue(value.ShellCommandTimeout)
	value.InstallTimeout = NormalizeTimeoutValue(value.InstallTimeout)
	value.HumanWaitTimeout = NormalizeTimeoutValue(value.HumanWaitTimeout)
	return value
}

func NormalizeTimeoutValue(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case "", "default":
		return ""
	case "0", "null", "none", "no-limit", "no limit", "unlimited":
		return "unlimited"
	default:
		return trimmed
	}
}

func ValidateTimeoutValue(name string, value string) error {
	normalized := NormalizeTimeoutValue(value)
	if normalized == "" || normalized == "unlimited" {
		return nil
	}
	duration, err := time.ParseDuration(normalized)
	if err != nil {
		return fmt.Errorf("%s must be a Go duration such as 30m or unlimited", name)
	}
	if duration <= 0 {
		return fmt.Errorf("%s must be positive or unlimited", name)
	}
	return nil
}

func TimeoutDuration(value string) (time.Duration, bool, error) {
	normalized := NormalizeTimeoutValue(value)
	if normalized == "" || normalized == "unlimited" {
		return 0, true, nil
	}
	duration, err := time.ParseDuration(normalized)
	if err != nil {
		return 0, false, err
	}
	if duration <= 0 {
		return 0, false, errors.New("timeout duration must be positive")
	}
	return duration, false, nil
}

func ExecutorTurnTimeoutDuration(cfg Config) (time.Duration, bool, error) {
	cfg = WithDefaults(cfg)
	return TimeoutDuration(cfg.Timeouts.ExecutorTurnTimeout)
}

func NormalizePermissions(value Permissions) Permissions {
	defaults := DefaultPermissions()
	profile, err := NormalizePermissionProfile(value.Profile)
	if err != nil {
		profile = defaults.Profile
	}
	value.Profile = profile
	return value
}

func NormalizePermissionProfile(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "balanced":
		return "balanced", nil
	case "guided":
		return "guided", nil
	case "autonomous":
		return "autonomous", nil
	case "full_send", "full-send", "full send", "lab", "lab_mode", "lab-mode", "lab mode":
		return "full_send", nil
	default:
		return "", errors.New("permission profile must be guided, balanced, autonomous, or full_send")
	}
}

func NormalizeUpdates(value Updates) Updates {
	defaults := DefaultUpdates()
	switch strings.ToLower(strings.TrimSpace(value.UpdateChannel)) {
	case "", "stable":
		value.UpdateChannel = "stable"
	case "prerelease":
		value.UpdateChannel = "prerelease"
		value.IncludePrereleases = true
	case "dev":
		value.UpdateChannel = "dev"
		value.IncludePrereleases = true
	default:
		value.UpdateChannel = defaults.UpdateChannel
	}
	if strings.TrimSpace(value.UpdateCheckInterval) == "" {
		value.UpdateCheckInterval = defaults.UpdateCheckInterval
	}
	if err := ValidateTimeoutValue("update_check_interval", value.UpdateCheckInterval); err != nil {
		value.UpdateCheckInterval = defaults.UpdateCheckInterval
	}
	return value
}
