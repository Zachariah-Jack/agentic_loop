package cli

import (
	"fmt"
	"strings"
	"time"

	"orchestrator/internal/config"
	"orchestrator/internal/state"
)

func buildTimeoutSettingsSnapshot(cfg config.Config) controlTimeoutSettingsSnapshot {
	timeouts := config.NormalizeTimeouts(cfg.Timeouts)
	return controlTimeoutSettingsSnapshot{
		PlannerRequestTimeout: timeoutValueSnapshot(timeouts.PlannerRequestTimeout, "future planner API requests"),
		ExecutorIdleTimeout:   timeoutValueSnapshot(timeouts.ExecutorIdleTimeout, "future executor idle checks"),
		ExecutorTurnTimeout:   timeoutValueSnapshot(timeouts.ExecutorTurnTimeout, "future Codex executor turns"),
		SubagentTimeout:       timeoutValueSnapshot(timeouts.SubagentTimeout, "future sub-agent tasks"),
		ShellCommandTimeout:   timeoutValueSnapshot(timeouts.ShellCommandTimeout, "future shell commands"),
		InstallTimeout:        timeoutValueSnapshot(timeouts.InstallTimeout, "future dependency installs"),
		HumanWaitTimeout:      timeoutValueSnapshot(timeouts.HumanWaitTimeout, "future human waits"),
		Message:               "timeout changes are persisted immediately; active operations use them when the transport can accept a new deadline, otherwise on the next turn or command",
	}
}

func timeoutValueSnapshot(value string, description string) controlTimeoutValueSnapshot {
	normalized := config.NormalizeTimeoutValue(value)
	if normalized == "" {
		normalized = "unlimited"
	}
	_, unlimited, err := config.TimeoutDuration(normalized)
	if err != nil {
		return controlTimeoutValueSnapshot{
			Value:       normalized,
			Unlimited:   false,
			AppliesAt:   "invalid",
			Description: fmt.Sprintf("%s; invalid timeout: %v", description, err),
		}
	}
	return controlTimeoutValueSnapshot{
		Value:       normalized,
		Unlimited:   unlimited,
		AppliesAt:   "next_operation",
		Description: description,
	}
}

func buildPermissionSnapshot(permissions config.Permissions) controlPermissionSnapshot {
	permissions = config.NormalizePermissions(permissions)
	return controlPermissionSnapshot{
		Profile:                         permissions.Profile,
		AskBeforeInstallingPrograms:     permissions.AskBeforeInstallingPrograms,
		AskBeforeInstallingDependencies: permissions.AskBeforeInstallingDependencies,
		AskBeforeOutsideRepoChanges:     permissions.AskBeforeOutsideRepoChanges,
		AskBeforeDeletingFiles:          permissions.AskBeforeDeletingFiles,
		AskBeforeRunningTests:           permissions.AskBeforeRunningTests,
		AskBeforeEmulatorTesting:        permissions.AskBeforeEmulatorTesting,
		AskBeforeNetworkCalls:           permissions.AskBeforeNetworkCalls,
		AskBeforeGitCommits:             permissions.AskBeforeGitCommits,
		AskBeforeGitPushes:              permissions.AskBeforeGitPushes,
		AskBeforeUpdaterInstalls:        permissions.AskBeforeUpdaterInstalls,
		AskBeforeWorkerParallelism:      permissions.AskBeforeWorkerParallelism,
		AskBeforeExecutorSteering:       permissions.AskBeforeExecutorSteering,
		AskBeforePlannerDirection:       permissions.AskBeforePlannerDirection,
		Message:                         permissionProfileMessage(permissions.Profile),
	}
}

func permissionProfileMessage(profile string) string {
	switch strings.TrimSpace(profile) {
	case "guided":
		return "Guided mode asks before routine setup and direction changes."
	case "autonomous":
		return "Autonomous mode allows routine build, test, install, and setup work while still asking for high-risk actions."
	case "full_send":
		return "Full Send / Lab Mode allows broad autonomous setup inside the configured repo scope, while still requiring human approval for secrets, billing, admin elevation, reboots, and unrelated private folders."
	default:
		return "Balanced mode asks for medium/high-risk actions while allowing normal build and test work."
	}
}

func buildUpdateSettingsSnapshot(updates config.Updates) controlUpdateSettingsSnapshot {
	updates = config.NormalizeUpdates(updates)
	return controlUpdateSettingsSnapshot{
		UpdateChannel:        updates.UpdateChannel,
		AutoCheckUpdates:     updates.AutoCheckUpdates,
		AutoDownloadUpdates:  updates.AutoDownloadUpdates,
		AutoInstallUpdates:   updates.AutoInstallUpdates,
		AskBeforeUpdate:      updates.AskBeforeUpdate,
		IncludePrereleases:   updates.IncludePrereleases,
		UpdateCheckInterval:  updates.UpdateCheckInterval,
		LastUpdateCheckAt:    updates.LastUpdateCheckAt,
		LastSeenVersion:      updates.LastSeenVersion,
		LastInstalledVersion: updates.LastInstalledVersion,
		SkippedVersions:      append([]string(nil), updates.SkippedVersions...),
	}
}

func formatDurationMillis(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64(duration / time.Millisecond)
}

func buildBuildTimeSnapshot(item state.BuildTime, found bool, active bool, now time.Time) controlBuildTimeSnapshot {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !found {
		return controlBuildTimeSnapshot{
			TotalBuildTimeLabel: "0s",
			CurrentStepLabel:    "No active build step",
			Message:             "Total Build Time starts accumulating when the orchestrator loop is actively running.",
		}
	}
	total := time.Duration(item.TotalBuildTimeMS) * time.Millisecond
	currentRun := time.Duration(0)
	currentStep := time.Duration(0)
	if active && !item.CurrentActiveSessionStartedAt.IsZero() {
		activeElapsed := now.Sub(item.CurrentActiveSessionStartedAt)
		if activeElapsed > 0 {
			total += activeElapsed
			currentRun = activeElapsed
		}
	}
	if active && !item.CurrentStepStartedAt.IsZero() {
		currentStep = now.Sub(item.CurrentStepStartedAt)
		if currentStep < 0 {
			currentStep = 0
		}
	}
	label := strings.TrimSpace(item.CurrentStepLabel)
	if label == "" {
		if active {
			label = "Orchestrator loop active"
		} else {
			label = "No active build step"
		}
	}
	return controlBuildTimeSnapshot{
		TotalBuildTimeMS:           formatDurationMillis(total),
		TotalBuildTimeLabel:        formatHumanDuration(total),
		CurrentRunTimeMS:           formatDurationMillis(currentRun),
		CurrentRunTimeLabel:        formatHumanDuration(currentRun),
		CurrentStepStartedAt:       formatSnapshotTime(item.CurrentStepStartedAt),
		CurrentStepTimeMS:          formatDurationMillis(currentStep),
		CurrentStepTimeLabel:       formatHumanDuration(currentStep),
		CurrentStepLabel:           label,
		CurrentActiveSessionStart:  formatSnapshotTime(item.CurrentActiveSessionStartedAt),
		LastActiveSessionEndedAt:   formatSnapshotTime(item.LastActiveSessionEndedAt),
		PlannerActiveDurationMS:    item.PlannerActiveDurationMS,
		ExecutorActiveDurationMS:   item.ExecutorActiveDurationMS,
		ExecutorThinkingDurationMS: item.ExecutorThinkingDurationMS,
		CommandActiveDurationMS:    item.CommandActiveDurationMS,
		InstallActiveDurationMS:    item.InstallActiveDurationMS,
		TestActiveDurationMS:       item.TestActiveDurationMS,
		HumanWaitDurationMS:        item.HumanWaitDurationMS,
		BlockedDurationMS:          item.BlockedDurationMS,
		Message:                    "Total Build Time counts active loop-running time only; idle, stopped, blocked, and waiting-for-human time is not added.",
	}
}
