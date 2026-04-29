package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"orchestrator/internal/buildinfo"
	"orchestrator/internal/config"
	"orchestrator/internal/control"
	"orchestrator/internal/runtimecfg"
	"orchestrator/internal/updater"
)

type controlUpdateSettingsSnapshot struct {
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

type controlUpdateStatusSnapshot struct {
	Settings         controlUpdateSettingsSnapshot `json:"settings"`
	CheckedAt        string                        `json:"checked_at,omitempty"`
	CurrentVersion   string                        `json:"current_version"`
	LatestVersion    string                        `json:"latest_version,omitempty"`
	UpdateAvailable  bool                          `json:"update_available"`
	ReleaseURL       string                        `json:"release_url,omitempty"`
	Changelog        string                        `json:"changelog,omitempty"`
	Channel          string                        `json:"channel,omitempty"`
	InstallSupported bool                          `json:"install_supported"`
	InstallMessage   string                        `json:"install_message,omitempty"`
	Error            string                        `json:"error,omitempty"`
	Message          string                        `json:"message,omitempty"`
}

type updateActionRequest struct {
	IncludePrereleases *bool  `json:"include_prereleases,omitempty"`
	Version            string `json:"version,omitempty"`
}

func newUpdateCommand() Command {
	return Command{
		Name:    "update",
		Summary: "Check GitHub releases and show update/changelog status.",
		Description: stringsJoin(
			"Usage:",
			"  orchestrator update check [--include-prereleases]",
			"  orchestrator update status",
			"  orchestrator update changelog",
			"  orchestrator update install",
			"",
			"Uses GitHub releases as the update source. Install is currently a truthful foundation command and is not yet automated.",
		),
		Run: runUpdate,
	}
}

func runUpdate(ctx context.Context, inv Invocation) error {
	if len(inv.Args) == 0 {
		fmt.Fprintln(inv.Stdout, newUpdateCommand().Description)
		return nil
	}
	switch inv.Args[0] {
	case "check":
		fs := flag.NewFlagSet("update check", flag.ContinueOnError)
		fs.SetOutput(inv.Stderr)
		includePrereleases := fs.Bool("include-prereleases", false, "Include prerelease GitHub releases.")
		if err := fs.Parse(inv.Args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprintln(inv.Stdout, newUpdateCommand().Description)
				return nil
			}
			return err
		}
		status, err := checkUpdateStatus(ctx, inv, updateActionRequest{IncludePrereleases: includePrereleases})
		printUpdateStatus(inv, status)
		return err
	case "status":
		printUpdateStatus(inv, buildUpdateStatusSnapshot(inv, currentConfig(inv).Updates, nil))
		return nil
	case "changelog":
		status, err := checkUpdateStatus(ctx, inv, updateActionRequest{})
		if strings.TrimSpace(status.Changelog) == "" {
			fmt.Fprintln(inv.Stdout, "No changelog is available for the configured channel.")
		} else {
			fmt.Fprintln(inv.Stdout, status.Changelog)
		}
		return err
	case "install":
		status, err := installUpdate(ctx, inv, updateActionRequest{})
		printUpdateStatus(inv, status)
		return err
	default:
		return fmt.Errorf("unknown update action %q", inv.Args[0])
	}
}

func checkUpdateStatus(ctx context.Context, inv Invocation, request updateActionRequest) (controlUpdateStatusSnapshot, error) {
	cfg := currentConfig(inv)
	includePrereleases := cfg.Updates.IncludePrereleases
	if request.IncludePrereleases != nil {
		includePrereleases = *request.IncludePrereleases
	}
	status, err := updater.Check(ctx, updater.Settings{
		CurrentVersion:     runtimeVersion(inv),
		IncludePrereleases: includePrereleases,
	})
	snapshot := buildUpdateStatusSnapshot(inv, cfg.Updates, &status)
	if err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func installUpdate(ctx context.Context, inv Invocation, request updateActionRequest) (controlUpdateStatusSnapshot, error) {
	status, _ := checkUpdateStatus(ctx, inv, request)
	installed, err := updater.Install(ctx, updater.Status{
		CurrentVersion:   status.CurrentVersion,
		LatestVersion:    status.LatestVersion,
		ReleaseURL:       status.ReleaseURL,
		Changelog:        status.Changelog,
		InstallSupported: status.InstallSupported,
		InstallMessage:   status.InstallMessage,
	})
	snapshot := buildUpdateStatusSnapshot(inv, currentConfig(inv).Updates, &installed)
	if err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func skipUpdate(ctx context.Context, inv *Invocation, request control.UpdateRequest) (controlUpdateStatusSnapshot, error) {
	if inv == nil {
		return controlUpdateStatusSnapshot{}, errors.New("invocation is required")
	}
	version := strings.TrimSpace(request.Version)
	if version == "" {
		status, _ := checkUpdateStatus(ctx, *inv, updateActionRequest{IncludePrereleases: request.IncludePrereleases})
		version = strings.TrimSpace(status.LatestVersion)
	}
	if version == "" {
		return buildUpdateStatusSnapshot(*inv, currentConfig(*inv).Updates, nil), errors.New("no update version is available to skip")
	}
	cfg := currentConfig(*inv)
	skipped := append([]string(nil), cfg.Updates.SkippedVersions...)
	if !containsStringFold(skipped, version) {
		skipped = append(skipped, version)
	}
	patch := runtimecfg.Patch{
		Updates: runtimecfg.UpdatePatch{SkippedVersions: skipped},
	}
	if inv.RuntimeCfg != nil {
		next, _, err := inv.RuntimeCfg.ApplyPatch(patch)
		if err != nil {
			return controlUpdateStatusSnapshot{}, err
		}
		inv.Config = next
	} else {
		manager := runtimecfg.NewManager("", inv.Config)
		next, _, err := manager.ApplyPatch(patch)
		if err != nil {
			return controlUpdateStatusSnapshot{}, err
		}
		inv.Config = next
	}
	_ = ctx
	return buildUpdateStatusSnapshot(*inv, currentConfig(*inv).Updates, nil), nil
}

func containsStringFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func buildUpdateStatusSnapshot(inv Invocation, settings config.Updates, status *updater.Status) controlUpdateStatusSnapshot {
	build := buildinfo.Current()
	snapshot := controlUpdateStatusSnapshot{
		Settings:         buildUpdateSettingsSnapshot(settings),
		CurrentVersion:   runtimeVersion(inv),
		InstallSupported: false,
		InstallMessage:   "Install is not automated yet; update check and changelog display are available.",
		Message:          "GitHub release checks are available. Safe Windows self-install remains deferred until signed/checksummed assets are published.",
	}
	if status == nil {
		return snapshot
	}
	snapshot.CheckedAt = formatSnapshotTime(status.CheckedAt)
	snapshot.CurrentVersion = firstNonEmpty(strings.TrimSpace(status.CurrentVersion), runtimeVersion(inv), build.Version)
	snapshot.LatestVersion = strings.TrimSpace(status.LatestVersion)
	snapshot.UpdateAvailable = status.UpdateAvailable
	snapshot.ReleaseURL = strings.TrimSpace(status.ReleaseURL)
	snapshot.Changelog = strings.TrimSpace(status.Changelog)
	snapshot.Channel = strings.TrimSpace(status.Channel)
	snapshot.InstallSupported = status.InstallSupported
	snapshot.InstallMessage = strings.TrimSpace(status.InstallMessage)
	snapshot.Error = strings.TrimSpace(status.Error)
	if snapshot.CheckedAt == "" && !status.CheckedAt.IsZero() {
		snapshot.CheckedAt = status.CheckedAt.UTC().Format(time.RFC3339Nano)
	}
	return snapshot
}

func printUpdateStatus(inv Invocation, status controlUpdateStatusSnapshot) {
	fmt.Fprintf(inv.Stdout, "update.status:\n")
	fmt.Fprintf(inv.Stdout, "  current_version: %s\n", valueOrUnavailable(status.CurrentVersion))
	fmt.Fprintf(inv.Stdout, "  latest_version: %s\n", valueOrUnavailable(status.LatestVersion))
	fmt.Fprintf(inv.Stdout, "  update_available: %t\n", status.UpdateAvailable)
	fmt.Fprintf(inv.Stdout, "  channel: %s\n", valueOrUnavailable(status.Channel))
	fmt.Fprintf(inv.Stdout, "  release_url: %s\n", valueOrUnavailable(status.ReleaseURL))
	fmt.Fprintf(inv.Stdout, "  install_supported: %t\n", status.InstallSupported)
	if strings.TrimSpace(status.InstallMessage) != "" {
		fmt.Fprintf(inv.Stdout, "  install_message: %s\n", status.InstallMessage)
	}
	if strings.TrimSpace(status.Error) != "" {
		fmt.Fprintf(inv.Stdout, "  error: %s\n", status.Error)
	}
}
