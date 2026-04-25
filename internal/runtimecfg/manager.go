package runtimecfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	"orchestrator/internal/config"
)

type Patch struct {
	Verbosity         *string         `json:"verbosity,omitempty"`
	Timeouts          TimeoutPatch    `json:"timeouts,omitempty"`
	PermissionProfile *string         `json:"permission_profile,omitempty"`
	Permissions       PermissionPatch `json:"permissions,omitempty"`
	Updates           UpdatePatch     `json:"updates,omitempty"`
}

type OptionalString struct {
	Set   bool
	Value string
}

func (s *OptionalString) UnmarshalJSON(data []byte) error {
	s.Set = true
	if strings.EqualFold(strings.TrimSpace(string(data)), "null") {
		s.Value = "unlimited"
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.Value = value
	return nil
}

type TimeoutPatch struct {
	PlannerRequestTimeout OptionalString `json:"planner_request_timeout,omitempty"`
	ExecutorIdleTimeout   OptionalString `json:"executor_idle_timeout,omitempty"`
	ExecutorTurnTimeout   OptionalString `json:"executor_turn_timeout,omitempty"`
	SubagentTimeout       OptionalString `json:"subagent_timeout,omitempty"`
	ShellCommandTimeout   OptionalString `json:"shell_command_timeout,omitempty"`
	InstallTimeout        OptionalString `json:"install_timeout,omitempty"`
	HumanWaitTimeout      OptionalString `json:"human_wait_timeout,omitempty"`
}

type UpdatePatch struct {
	UpdateChannel       *string        `json:"update_channel,omitempty"`
	AutoCheckUpdates    *bool          `json:"auto_check_updates,omitempty"`
	AutoDownloadUpdates *bool          `json:"auto_download_updates,omitempty"`
	AutoInstallUpdates  *bool          `json:"auto_install_updates,omitempty"`
	AskBeforeUpdate     *bool          `json:"ask_before_update,omitempty"`
	IncludePrereleases  *bool          `json:"include_prereleases,omitempty"`
	UpdateCheckInterval OptionalString `json:"update_check_interval,omitempty"`
	SkippedVersions     []string       `json:"skipped_versions,omitempty"`
}

type PermissionPatch struct {
	AskBeforeInstallingPrograms     *bool `json:"ask_before_installing_programs,omitempty"`
	AskBeforeInstallingDependencies *bool `json:"ask_before_installing_dependencies,omitempty"`
	AskBeforeOutsideRepoChanges     *bool `json:"ask_before_modifying_files_outside_repo,omitempty"`
	AskBeforeDeletingFiles          *bool `json:"ask_before_deleting_files,omitempty"`
	AskBeforeRunningTests           *bool `json:"ask_before_running_tests,omitempty"`
	AskBeforeEmulatorTesting        *bool `json:"ask_before_emulator_device_testing,omitempty"`
	AskBeforeNetworkCalls           *bool `json:"ask_before_network_calls,omitempty"`
	AskBeforeGitCommits             *bool `json:"ask_before_git_commits,omitempty"`
	AskBeforeGitPushes              *bool `json:"ask_before_git_pushes,omitempty"`
	AskBeforeUpdaterInstalls        *bool `json:"ask_before_updater_installs,omitempty"`
	AskBeforeWorkerParallelism      *bool `json:"ask_before_worker_parallelism,omitempty"`
	AskBeforeExecutorSteering       *bool `json:"ask_before_executor_steering,omitempty"`
	AskBeforePlannerDirection       *bool `json:"ask_before_planner_direction_changes,omitempty"`
}

type Manager struct {
	path string

	mu  sync.RWMutex
	cfg config.Config
}

func NewManager(path string, initial config.Config) *Manager {
	return &Manager{
		path: strings.TrimSpace(path),
		cfg:  config.WithDefaults(initial),
	}
}

func (m *Manager) Path() string {
	if m == nil {
		return ""
	}
	return m.path
}

func (m *Manager) Snapshot() config.Config {
	if m == nil {
		return config.Default()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) ReloadFromDisk() (config.Config, bool, error) {
	if m == nil {
		return config.Default(), false, nil
	}

	if strings.TrimSpace(m.path) == "" {
		cfg := m.Snapshot()
		return cfg, false, nil
	}

	loaded, err := config.Load(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := m.Snapshot()
			return cfg, false, nil
		}
		return config.Config{}, false, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	changed := !reflect.DeepEqual(m.cfg, loaded)
	m.cfg = loaded
	return m.cfg, changed, nil
}

func (m *Manager) SetVerbosity(value string) (config.Config, bool, error) {
	normalized, err := config.NormalizeVerbosity(value)
	if err != nil {
		return config.Config{}, false, err
	}

	m.mu.Lock()
	cfg := m.cfg
	changed := cfg.Verbosity != normalized
	cfg.Verbosity = normalized
	cfg = config.WithDefaults(cfg)
	m.cfg = cfg
	m.mu.Unlock()

	if err := m.persist(cfg); err != nil {
		return config.Config{}, false, err
	}

	return cfg, changed, nil
}

func (m *Manager) ApplyPatch(patch Patch) (config.Config, bool, error) {
	if !patch.HasChanges() {
		cfg := m.Snapshot()
		return cfg, false, nil
	}

	m.mu.Lock()
	cfg := m.cfg
	before := config.WithDefaults(cfg)

	if patch.Verbosity != nil {
		normalized, err := config.NormalizeVerbosity(*patch.Verbosity)
		if err != nil {
			m.mu.Unlock()
			return config.Config{}, false, err
		}
		cfg.Verbosity = normalized
	}
	if err := applyTimeoutPatch(&cfg, patch.Timeouts); err != nil {
		m.mu.Unlock()
		return config.Config{}, false, err
	}
	if patch.PermissionProfile != nil {
		permissions, err := config.PermissionPreset(*patch.PermissionProfile)
		if err != nil {
			m.mu.Unlock()
			return config.Config{}, false, err
		}
		cfg.Permissions = permissions
	}
	applyPermissionPatch(&cfg, patch.Permissions)
	applyUpdatePatch(&cfg, patch.Updates)

	cfg = config.WithDefaults(cfg)
	changed := !reflect.DeepEqual(before, cfg)
	m.cfg = cfg
	m.mu.Unlock()

	if err := m.persist(cfg); err != nil {
		return config.Config{}, false, err
	}

	return cfg, changed, nil
}

func (m *Manager) persist(cfg config.Config) error {
	if m == nil || strings.TrimSpace(m.path) == "" {
		return nil
	}
	return config.Save(m.path, cfg)
}

func (p Patch) HasChanges() bool {
	return p.Verbosity != nil ||
		p.Timeouts.HasChanges() ||
		p.PermissionProfile != nil ||
		p.Permissions.HasChanges() ||
		p.Updates.HasChanges()
}

func (p TimeoutPatch) HasChanges() bool {
	return p.PlannerRequestTimeout.Set ||
		p.ExecutorIdleTimeout.Set ||
		p.ExecutorTurnTimeout.Set ||
		p.SubagentTimeout.Set ||
		p.ShellCommandTimeout.Set ||
		p.InstallTimeout.Set ||
		p.HumanWaitTimeout.Set
}

func (p UpdatePatch) HasChanges() bool {
	return p.UpdateChannel != nil ||
		p.AutoCheckUpdates != nil ||
		p.AutoDownloadUpdates != nil ||
		p.AutoInstallUpdates != nil ||
		p.AskBeforeUpdate != nil ||
		p.IncludePrereleases != nil ||
		p.UpdateCheckInterval.Set ||
		p.SkippedVersions != nil
}

func (p PermissionPatch) HasChanges() bool {
	return p.AskBeforeInstallingPrograms != nil ||
		p.AskBeforeInstallingDependencies != nil ||
		p.AskBeforeOutsideRepoChanges != nil ||
		p.AskBeforeDeletingFiles != nil ||
		p.AskBeforeRunningTests != nil ||
		p.AskBeforeEmulatorTesting != nil ||
		p.AskBeforeNetworkCalls != nil ||
		p.AskBeforeGitCommits != nil ||
		p.AskBeforeGitPushes != nil ||
		p.AskBeforeUpdaterInstalls != nil ||
		p.AskBeforeWorkerParallelism != nil ||
		p.AskBeforeExecutorSteering != nil ||
		p.AskBeforePlannerDirection != nil
}

func applyTimeoutPatch(cfg *config.Config, patch TimeoutPatch) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	timeouts := config.NormalizeTimeouts(cfg.Timeouts)
	apply := func(name string, value OptionalString, target *string) error {
		if !value.Set {
			return nil
		}
		normalized := config.NormalizeTimeoutValue(value.Value)
		if normalized == "" {
			normalized = config.NormalizeTimeoutValue("unlimited")
		}
		if err := config.ValidateTimeoutValue(name, normalized); err != nil {
			return err
		}
		*target = normalized
		return nil
	}
	if err := apply("planner_request_timeout", patch.PlannerRequestTimeout, &timeouts.PlannerRequestTimeout); err != nil {
		return err
	}
	if err := apply("executor_idle_timeout", patch.ExecutorIdleTimeout, &timeouts.ExecutorIdleTimeout); err != nil {
		return err
	}
	if err := apply("executor_turn_timeout", patch.ExecutorTurnTimeout, &timeouts.ExecutorTurnTimeout); err != nil {
		return err
	}
	if err := apply("subagent_timeout", patch.SubagentTimeout, &timeouts.SubagentTimeout); err != nil {
		return err
	}
	if err := apply("shell_command_timeout", patch.ShellCommandTimeout, &timeouts.ShellCommandTimeout); err != nil {
		return err
	}
	if err := apply("install_timeout", patch.InstallTimeout, &timeouts.InstallTimeout); err != nil {
		return err
	}
	if err := apply("human_wait_timeout", patch.HumanWaitTimeout, &timeouts.HumanWaitTimeout); err != nil {
		return err
	}
	cfg.Timeouts = config.NormalizeTimeouts(timeouts)
	return nil
}

func applyPermissionPatch(cfg *config.Config, patch PermissionPatch) {
	if cfg == nil || !patch.HasChanges() {
		return
	}
	permissions := config.NormalizePermissions(cfg.Permissions)
	apply := func(value *bool, target *bool) {
		if value != nil {
			*target = *value
		}
	}
	apply(patch.AskBeforeInstallingPrograms, &permissions.AskBeforeInstallingPrograms)
	apply(patch.AskBeforeInstallingDependencies, &permissions.AskBeforeInstallingDependencies)
	apply(patch.AskBeforeOutsideRepoChanges, &permissions.AskBeforeOutsideRepoChanges)
	apply(patch.AskBeforeDeletingFiles, &permissions.AskBeforeDeletingFiles)
	apply(patch.AskBeforeRunningTests, &permissions.AskBeforeRunningTests)
	apply(patch.AskBeforeEmulatorTesting, &permissions.AskBeforeEmulatorTesting)
	apply(patch.AskBeforeNetworkCalls, &permissions.AskBeforeNetworkCalls)
	apply(patch.AskBeforeGitCommits, &permissions.AskBeforeGitCommits)
	apply(patch.AskBeforeGitPushes, &permissions.AskBeforeGitPushes)
	apply(patch.AskBeforeUpdaterInstalls, &permissions.AskBeforeUpdaterInstalls)
	apply(patch.AskBeforeWorkerParallelism, &permissions.AskBeforeWorkerParallelism)
	apply(patch.AskBeforeExecutorSteering, &permissions.AskBeforeExecutorSteering)
	apply(patch.AskBeforePlannerDirection, &permissions.AskBeforePlannerDirection)
	cfg.Permissions = permissions
}

func applyUpdatePatch(cfg *config.Config, patch UpdatePatch) {
	if cfg == nil {
		return
	}
	updates := config.NormalizeUpdates(cfg.Updates)
	if patch.UpdateChannel != nil {
		updates.UpdateChannel = strings.TrimSpace(*patch.UpdateChannel)
	}
	if patch.AutoCheckUpdates != nil {
		updates.AutoCheckUpdates = *patch.AutoCheckUpdates
	}
	if patch.AutoDownloadUpdates != nil {
		updates.AutoDownloadUpdates = *patch.AutoDownloadUpdates
	}
	if patch.AutoInstallUpdates != nil {
		updates.AutoInstallUpdates = *patch.AutoInstallUpdates
	}
	if patch.AskBeforeUpdate != nil {
		updates.AskBeforeUpdate = *patch.AskBeforeUpdate
	}
	if patch.IncludePrereleases != nil {
		updates.IncludePrereleases = *patch.IncludePrereleases
	}
	if patch.UpdateCheckInterval.Set {
		updates.UpdateCheckInterval = patch.UpdateCheckInterval.Value
	}
	if patch.SkippedVersions != nil {
		updates.SkippedVersions = append([]string(nil), patch.SkippedVersions...)
	}
	cfg.Updates = config.NormalizeUpdates(updates)
}
