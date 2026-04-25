package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const DefaultRepository = "Zachariah-Jack/agentic_loop"

type Settings struct {
	Repository         string
	CurrentVersion     string
	IncludePrereleases bool
	HTTPClient         *http.Client
}

type Status struct {
	CheckedAt        time.Time `json:"checked_at,omitempty"`
	CurrentVersion   string    `json:"current_version"`
	LatestVersion    string    `json:"latest_version,omitempty"`
	UpdateAvailable  bool      `json:"update_available"`
	ReleaseURL       string    `json:"release_url,omitempty"`
	Changelog        string    `json:"changelog,omitempty"`
	Channel          string    `json:"channel,omitempty"`
	InstallSupported bool      `json:"install_supported"`
	InstallMessage   string    `json:"install_message,omitempty"`
	Error            string    `json:"error,omitempty"`
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func Check(ctx context.Context, settings Settings) (Status, error) {
	repo := strings.TrimSpace(settings.Repository)
	if repo == "" {
		repo = DefaultRepository
	}
	client := settings.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases", nil)
	if err != nil {
		return Status{}, err
	}
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("user-agent", "orchestrator-updater")
	resp, err := client.Do(req)
	if err != nil {
		return Status{
			CheckedAt:        time.Now().UTC(),
			CurrentVersion:   strings.TrimSpace(settings.CurrentVersion),
			InstallSupported: false,
			InstallMessage:   "Update install is not implemented yet; check/changelog/status are available.",
			Error:            err.Error(),
		}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("GitHub releases returned HTTP %d", resp.StatusCode)
		return Status{
			CheckedAt:        time.Now().UTC(),
			CurrentVersion:   strings.TrimSpace(settings.CurrentVersion),
			InstallSupported: false,
			InstallMessage:   "Update install is not implemented yet; check/changelog/status are available.",
			Error:            err.Error(),
		}, err
	}
	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return Status{}, err
	}
	release, found := chooseRelease(releases, settings.IncludePrereleases)
	status := Status{
		CheckedAt:        time.Now().UTC(),
		CurrentVersion:   strings.TrimSpace(settings.CurrentVersion),
		InstallSupported: false,
		InstallMessage:   "Install is intentionally not automated in this foundation slice. Download/install will be added after signed/checksummed Windows assets are published.",
	}
	if !found {
		status.Error = "No GitHub release was found for the configured channel."
		return status, nil
	}
	status.LatestVersion = release.TagName
	status.ReleaseURL = release.HTMLURL
	status.Changelog = strings.TrimSpace(release.Body)
	if release.Prerelease {
		status.Channel = "prerelease"
	} else {
		status.Channel = "stable"
	}
	status.UpdateAvailable = CompareVersions(status.LatestVersion, status.CurrentVersion) > 0
	return status, nil
}

func Install(_ context.Context, status Status) (Status, error) {
	status.InstallSupported = false
	status.InstallMessage = "install_update is not yet supported safely; use check/changelog/status and install published Windows assets manually for now"
	return status, errors.New(status.InstallMessage)
}

func chooseRelease(releases []githubRelease, includePrereleases bool) (githubRelease, bool) {
	for _, release := range releases {
		if release.Draft {
			continue
		}
		if release.Prerelease && !includePrereleases {
			continue
		}
		return release, true
	}
	return githubRelease{}, false
}

var versionPartPattern = regexp.MustCompile(`\d+`)

func CompareVersions(a string, b string) int {
	ap := versionParts(a)
	bp := versionParts(b)
	max := len(ap)
	if len(bp) > max {
		max = len(bp)
	}
	for i := 0; i < max; i++ {
		av, bv := 0, 0
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

func versionParts(value string) []int {
	matches := versionPartPattern.FindAllString(value, -1)
	parts := make([]int, 0, len(matches))
	for _, match := range matches {
		var out int
		for _, r := range match {
			out = out*10 + int(r-'0')
		}
		parts = append(parts, out)
	}
	return parts
}
