package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"orchestrator/internal/state"
)

const (
	artifactIndexPluginName = "artifact_index"
	artifactIndexToolName   = "artifact_index.write"
	reportArtifactDir       = ".orchestrator/artifacts/reports"
	artifactRootDir         = ".orchestrator/artifacts"
	maxArtifactPreviewBytes = 240
)

type artifactIndexPlugin struct {
	manifest Manifest
}

func newArtifactIndexPlugin(manifest Manifest, _ string) implementation {
	return artifactIndexPlugin{manifest: manifest}
}

func (p artifactIndexPlugin) CallTool(_ context.Context, request ToolRequest) (state.PluginToolResult, error) {
	if strings.TrimSpace(request.Call.Tool) != artifactIndexToolName {
		return state.PluginToolResult{
			Tool:    strings.TrimSpace(request.Call.Tool),
			Success: false,
			Message: "plugin tool not supported by artifact_index",
		}, nil
	}

	if len(request.Call.Arguments) > 0 {
		return state.PluginToolResult{
			Tool:    artifactIndexToolName,
			Success: false,
			Message: "artifact_index.write does not accept arguments in v1",
		}, nil
	}

	artifactPath, artifactPreview, artifactCount, err := writeArtifactIndexReport(request.Run, "tool")
	if err != nil {
		return state.PluginToolResult{
			Tool:    artifactIndexToolName,
			Success: false,
			Message: err.Error(),
		}, err
	}

	return state.PluginToolResult{
		Tool:    artifactIndexToolName,
		Success: true,
		Message: fmt.Sprintf("artifact index written with %d item(s)", artifactCount),
		Data: map[string]any{
			"artifact_count": artifactCount,
			"report_path":    artifactPath,
		},
		ArtifactPath:    artifactPath,
		ArtifactPreview: artifactPreview,
	}, nil
}

func (p artifactIndexPlugin) RunHook(_ context.Context, request HookRequest) (HookResult, error) {
	if request.Point != HookRunEnd {
		return HookResult{
			Success: true,
			Plugin:  p.manifest.Name,
			Hook:    request.Point,
		}, nil
	}

	artifactPath, artifactPreview, artifactCount, err := writeArtifactIndexReport(request.Run, "run_end")
	if err != nil {
		return HookResult{
			Plugin:  p.manifest.Name,
			Hook:    request.Point,
			Message: err.Error(),
		}, err
	}

	return HookResult{
		Success:         true,
		Plugin:          p.manifest.Name,
		Hook:            request.Point,
		Message:         fmt.Sprintf("artifact index updated with %d item(s)", artifactCount),
		ArtifactPath:    artifactPath,
		ArtifactPreview: artifactPreview,
	}, nil
}

func writeArtifactIndexReport(run state.Run, suffix string) (string, string, int, error) {
	artifactPaths, err := listArtifactPaths(run.RepoPath)
	if err != nil {
		return "", "", 0, err
	}

	report := map[string]any{
		"plugin":            artifactIndexPluginName,
		"run_id":            run.ID,
		"generated_at":      time.Now().UTC(),
		"stop_reason":       strings.TrimSpace(run.LatestStopReason),
		"latest_checkpoint": run.LatestCheckpoint,
		"artifact_count":    len(artifactPaths),
		"artifact_paths":    artifactPaths,
	}

	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", 0, err
	}

	fileName := fmt.Sprintf("artifact_index_%s_%s.json", sanitizeFileComponent(suffix), time.Now().UTC().Format("20060102T150405.000000000Z"))
	relativePath, err := writeReportArtifact(run, fileName, content)
	if err != nil {
		return "", "", 0, err
	}

	return relativePath, previewString(string(content), maxArtifactPreviewBytes), len(artifactPaths), nil
}

func listArtifactPaths(repoRoot string) ([]string, error) {
	artifactRoot := filepath.Join(repoRoot, filepath.FromSlash(artifactRootDir))
	if _, err := os.Stat(artifactRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	paths := make([]string, 0)
	err := filepath.WalkDir(artifactRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		relativePath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relativePath))
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(paths)
	return paths, nil
}

func writeReportArtifact(run state.Run, fileName string, content []byte) (string, error) {
	relativeDir := filepath.Join(reportArtifactDir, run.ID)
	absoluteDir := filepath.Join(run.RepoPath, filepath.FromSlash(relativeDir))
	if err := os.MkdirAll(absoluteDir, 0o755); err != nil {
		return "", err
	}

	absolutePath := filepath.Join(absoluteDir, fileName)
	if err := os.WriteFile(absolutePath, content, 0o600); err != nil {
		return "", err
	}

	return filepath.ToSlash(filepath.Join(relativeDir, fileName)), nil
}

func sanitizeFileComponent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "artifact"
	}

	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, trimmed)
	sanitized = strings.Trim(sanitized, "._-")
	if sanitized == "" {
		return "artifact"
	}
	return sanitized
}

func previewString(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:limit]) + "..."
}
