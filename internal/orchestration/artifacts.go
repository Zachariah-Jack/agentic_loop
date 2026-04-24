package orchestration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/executor"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

const (
	artifactRootDir            = ".orchestrator/artifacts"
	plannerArtifactDir         = ".orchestrator/artifacts/planner"
	executorArtifactDir        = ".orchestrator/artifacts/executor"
	contextArtifactDir         = ".orchestrator/artifacts/context"
	humanArtifactDir           = ".orchestrator/artifacts/human"
	integrationArtifactDir     = ".orchestrator/artifacts/integration"
	reportArtifactDir          = ".orchestrator/artifacts/reports"
	maxContextArtifactBytes    = 16384
	maxContextArtifactEntries  = 200
	maxExecutorArtifactBytes   = 1024
	maxHumanReplyArtifactBytes = 1024
)

var knownRootOrchestrationReports = []string{
	"summary_of_contracts_and_prior_runs.md",
}

func writeRunArtifact(run state.Run, category string, fileName string, content []byte) (string, error) {
	relativeDir := filepath.Join(artifactRootDir, category, run.ID)
	absoluteDir := filepath.Join(run.RepoPath, relativeDir)
	if err := os.MkdirAll(absoluteDir, 0o755); err != nil {
		return "", err
	}

	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		fileName = fmt.Sprintf("artifact_%s.txt", time.Now().UTC().Format("20060102T150405.000000000Z"))
	}

	fileName = uniqueArtifactFileName(absoluteDir, fileName)
	absolutePath := filepath.Join(absoluteDir, fileName)
	if err := os.WriteFile(absolutePath, content, 0o600); err != nil {
		return "", err
	}

	return filepath.ToSlash(filepath.Join(relativeDir, fileName)), nil
}

func persistCollectedContextArtifact(run state.Run, item state.CollectedContextResult) (string, string) {
	if !item.Truncated || strings.TrimSpace(item.ResolvedPath) == "" {
		return "", ""
	}

	artifact := state.CollectedContextResult{
		RequestedPath: item.RequestedPath,
		ResolvedPath:  item.ResolvedPath,
		Kind:          item.Kind,
		Detail:        item.Detail,
		Truncated:     item.Truncated,
	}

	switch item.Kind {
	case "file":
		preview, truncated, err := readFilePreview(item.ResolvedPath, maxContextArtifactBytes)
		if err != nil {
			return "", previewString("artifact_write_failed: "+err.Error(), 240)
		}
		artifact.Preview = preview
		artifact.Truncated = truncated
	case "dir":
		entries, truncated, err := readDirectoryPreview(item.ResolvedPath, maxContextArtifactEntries)
		if err != nil {
			return "", previewString("artifact_write_failed: "+err.Error(), 240)
		}
		artifact.Entries = entries
		artifact.Truncated = truncated
	default:
		return "", ""
	}

	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	fileName := fmt.Sprintf(
		"context_%s.json",
		sanitizeArtifactComponent(filepath.Base(item.RequestedPath)),
	)
	relativePath, err := writeRunArtifact(run, "context", fileName, encoded)
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	return relativePath, previewString(string(encoded), 240)
}

func persistCollectedContextStateArtifact(run state.Run, collectedContext *state.CollectedContextState) (string, string) {
	if collectedContext == nil {
		return "", ""
	}

	artifact := *collectedContext
	artifact.ArtifactPath = ""
	artifact.ArtifactPreview = ""

	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	fileName := fmt.Sprintf("collected_context_%s.json", time.Now().UTC().Format("20060102T150405.000000000Z"))
	relativePath, err := writeRunArtifact(run, "context", fileName, encoded)
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	return relativePath, previewString(string(encoded), 240)
}

func persistHumanReplyArtifact(run state.Run, reply state.HumanReply) (string, string) {
	if len(reply.Payload) <= maxHumanReplyArtifactBytes {
		return "", ""
	}

	fileName := fmt.Sprintf("human_reply_%s.txt", sanitizeArtifactComponent(reply.ID))
	relativePath, err := writeRunArtifact(run, "human", fileName, []byte(reply.Payload))
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	return relativePath, previewString(reply.Payload, 240)
}

func persistExecutorOutputArtifact(run state.Run, result executor.TurnResult) (string, string) {
	if len(result.FinalMessage) <= maxExecutorArtifactBytes {
		return "", ""
	}

	fileName := fmt.Sprintf("executor_output_%s.txt", sanitizeArtifactComponent(result.TurnID))
	relativePath, err := writeRunArtifact(run, "executor", fileName, []byte(result.FinalMessage))
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	return relativePath, previewString(result.FinalMessage, 240)
}

func relocateKnownReportArtifact(run state.Run, output planner.OutputEnvelope) (string, string) {
	if !ShouldDispatchExecutor(output) {
		return "", ""
	}

	for _, reportName := range knownRootOrchestrationReports {
		if reportWriteAllowed(output, reportName) {
			continue
		}

		reportPath := filepath.Join(run.RepoPath, reportName)
		if !pathExists(reportPath) {
			continue
		}

		content, err := os.ReadFile(reportPath)
		if err != nil {
			return "", previewString("artifact_move_failed: "+err.Error(), 240)
		}

		relativePath, err := writeRunArtifact(run, "reports", reportName, content)
		if err != nil {
			return "", previewString("artifact_move_failed: "+err.Error(), 240)
		}
		if err := os.Remove(reportPath); err != nil {
			return "", previewString("artifact_move_failed: "+err.Error(), 240)
		}

		return relativePath, previewString(string(content), 240)
	}

	return "", ""
}

func persistIntegrationArtifact(run state.Run, integration state.IntegrationSummary) (string, string) {
	encoded, err := json.MarshalIndent(integration, "", "  ")
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	fileName := fmt.Sprintf("integration_%s.json", time.Now().UTC().Format("20060102T150405.000000000Z"))
	relativePath, err := writeRunArtifact(run, "integration", fileName, encoded)
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	return relativePath, previewString(string(encoded), 240)
}

func WriteIntegrationArtifact(run state.Run, integration state.IntegrationSummary) (string, string) {
	return persistIntegrationArtifact(run, integration)
}

func persistIntegrationApplyArtifact(run state.Run, apply state.IntegrationApplySummary) (string, string) {
	encoded, err := json.MarshalIndent(apply, "", "  ")
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	fileName := fmt.Sprintf("integration_apply_%s.json", time.Now().UTC().Format("20060102T150405.000000000Z"))
	relativePath, err := writeRunArtifact(run, "integration", fileName, encoded)
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	return relativePath, previewString(string(encoded), 240)
}

func reportWriteAllowed(output planner.OutputEnvelope, reportName string) bool {
	if output.Execute == nil {
		return false
	}

	normalizedReport := filepath.ToSlash(filepath.Clean(strings.TrimSpace(reportName)))
	for _, path := range nonEmpty(output.Execute.WriteScope) {
		normalizedPath := filepath.ToSlash(filepath.Clean(path))
		if normalizedPath == normalizedReport {
			return true
		}
	}

	return false
}

func sanitizeArtifactComponent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "." || trimmed == string(filepath.Separator) {
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

func uniqueArtifactFileName(absoluteDir string, fileName string) string {
	absolutePath := filepath.Join(absoluteDir, fileName)
	if _, err := os.Stat(absolutePath); os.IsNotExist(err) {
		return fileName
	}

	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	return fmt.Sprintf("%s_%s%s", base, time.Now().UTC().Format("20060102T150405.000000000Z"), ext)
}
