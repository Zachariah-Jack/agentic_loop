package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsReleaseAssetsExist(t *testing.T) {
	t.Helper()

	root := filepath.Join("..", "..")
	for _, asset := range []struct {
		path     string
		contains []string
	}{
		{
			path: filepath.Join(root, "scripts", "build-release.ps1"),
			contains: []string{
				"go build",
				"Compress-Archive",
				"orchestrator/internal/buildinfo.Version",
			},
		},
		{
			path: filepath.Join(root, "scripts", "build-installer.ps1"),
			contains: []string{
				"ISCC",
				"build-release.ps1",
				"install\\windows\\orchestrator.iss",
			},
		},
		{
			path: filepath.Join(root, "install", "windows", "orchestrator.iss"),
			contains: []string{
				"[Setup]",
				"[Files]",
				"Add 'orchestrator' to your PATH",
			},
		},
		{
			path: filepath.Join(root, "docs", "WINDOWS_INSTALL_AND_RELEASE.md"),
			contains: []string{
				"build-release.ps1",
				"build-installer.ps1",
				"OPENAI_API_KEY",
			},
		},
	} {
		data, err := os.ReadFile(asset.path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", asset.path, err)
		}
		text := string(data)
		for _, want := range asset.contains {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", asset.path, want)
			}
		}
	}
}
