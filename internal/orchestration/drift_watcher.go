package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orchestrator/internal/journal"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

const (
	driftWatcherName          = "drift_watcher"
	reviewArtifactDir         = ".orchestrator/artifacts/reviews"
	maxDriftEvidenceFileBytes = 4096
)

type DriftWatcher interface {
	Review(context.Context, DriftReviewRequest) (DriftReviewResult, error)
}

type DriftReviewRequest struct {
	Run           state.Run
	PlannerResult planner.Result
	RecentEvents  []journal.Event
}

type DriftReviewResult struct {
	Summary  planner.DriftReviewSummary
	Artifact []byte
}

type deterministicDriftWatcher struct{}

func NewDeterministicDriftWatcher() DriftWatcher {
	return deterministicDriftWatcher{}
}

func (deterministicDriftWatcher) Review(_ context.Context, req DriftReviewRequest) (DriftReviewResult, error) {
	report := planner.DriftReviewSummary{
		Reviewer: driftWatcherName,
	}

	paths := []string{
		".orchestrator/brief.md",
		".orchestrator/roadmap.md",
		".orchestrator/decisions.md",
	}

	documentSnippets := make([]string, 0, len(paths))
	evidencePaths := make([]string, 0, len(paths))
	missingContext := make([]string, 0, len(paths))
	concerns := make([]string, 0, 4)
	recommended := make([]string, 0, 4)

	for _, relativePath := range paths {
		absolutePath := filepath.Join(req.Run.RepoPath, filepath.FromSlash(relativePath))
		preview, _, err := readFilePreview(absolutePath, maxDriftEvidenceFileBytes)
		if err != nil {
			if os.IsNotExist(err) {
				missingContext = append(missingContext, relativePath)
				continue
			}
			missingContext = append(missingContext, relativePath+" (read_failed)")
			concerns = append(concerns, relativePath+" could not be read for drift review")
			continue
		}

		trimmed := strings.TrimSpace(preview)
		if trimmed == "" {
			missingContext = append(missingContext, relativePath+" (empty)")
			continue
		}

		evidencePaths = append(evidencePaths, relativePath)
		documentSnippets = append(documentSnippets, trimmed)
	}

	taskSummary := driftTaskSummary(req.PlannerResult.Output)
	if taskSummary == "" {
		concerns = append(concerns, "planner forward-work outcome did not expose a task or focus summary for drift review")
	} else if len(documentSnippets) > 0 && !hasLexicalOverlap(taskSummary, strings.Join(documentSnippets, "\n")) {
		concerns = append(concerns, "planner task summary has no obvious lexical overlap with brief, roadmap, or decisions")
		recommended = append(recommended, "If the planned work is still correct, restate how it aligns with the repo brief, roadmap, or decisions before forward work.")
	}

	if execute := req.PlannerResult.Output.Execute; execute != nil && len(nonEmpty(execute.WriteScope)) == 0 {
		concerns = append(concerns, "planner execute outcome omitted write_scope, which weakens drift review against the target repo contract")
		recommended = append(recommended, "If execution proceeds, tighten execute.write_scope to exact repo-root-relative paths.")
	}

	if len(missingContext) > 0 {
		recommended = append(recommended, "If missing repo-contract context matters, collect or restore it before irreversible forward work.")
	}

	if recentFailureCount(req.RecentEvents) > 0 {
		concerns = append(concerns, "recent run history includes failure events that may matter for the next forward step")
		recommended = append(recommended, "If those recent failures are still relevant, account for them explicitly in the next planner outcome.")
	}

	report.Concerns = concerns
	report.MissingContext = missingContext
	report.RecommendedPlannerAdjustments = recommended
	report.EvidencePaths = evidencePaths
	report.Aligned = len(report.Concerns) == 0 && len(report.MissingContext) == 0

	artifact := struct {
		Reviewer               string              `json:"reviewer"`
		ReviewedOutcome        planner.OutcomeKind `json:"reviewed_outcome"`
		PlannerTaskSummary     string              `json:"planner_task_summary,omitempty"`
		LatestStableCheckpoint state.Checkpoint    `json:"latest_stable_checkpoint"`
		Aligned                bool                `json:"aligned"`
		Concerns               []string            `json:"concerns,omitempty"`
		MissingContext         []string            `json:"missing_context,omitempty"`
		RecommendedAdjustments []string            `json:"recommended_planner_adjustments,omitempty"`
		EvidencePaths          []string            `json:"evidence_paths,omitempty"`
	}{
		Reviewer:               report.Reviewer,
		ReviewedOutcome:        req.PlannerResult.Output.Outcome,
		PlannerTaskSummary:     taskSummary,
		LatestStableCheckpoint: req.Run.LatestCheckpoint,
		Aligned:                report.Aligned,
		Concerns:               report.Concerns,
		MissingContext:         report.MissingContext,
		RecommendedAdjustments: report.RecommendedPlannerAdjustments,
		EvidencePaths:          report.EvidencePaths,
	}

	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return DriftReviewResult{}, err
	}

	return DriftReviewResult{
		Summary:  report,
		Artifact: encoded,
	}, nil
}

func driftTaskSummary(output planner.OutputEnvelope) string {
	switch {
	case output.Execute != nil:
		parts := []string{strings.TrimSpace(output.Execute.Task)}
		if acceptance := nonEmpty(output.Execute.AcceptanceCriteria); len(acceptance) > 0 {
			parts = append(parts, acceptance...)
		}
		if writeScope := nonEmpty(output.Execute.WriteScope); len(writeScope) > 0 {
			parts = append(parts, writeScope...)
		}
		return strings.TrimSpace(strings.Join(parts, " | "))
	case output.CollectContext != nil:
		parts := []string{strings.TrimSpace(output.CollectContext.Focus)}
		if questions := nonEmpty(output.CollectContext.Questions); len(questions) > 0 {
			parts = append(parts, questions...)
		}
		if paths := nonEmpty(output.CollectContext.Paths); len(paths) > 0 {
			parts = append(parts, paths...)
		}
		return strings.TrimSpace(strings.Join(parts, " | "))
	default:
		return ""
	}
}

func hasLexicalOverlap(left string, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}

	leftTokens := tokenSet(left)
	if len(leftTokens) == 0 {
		return false
	}

	for token := range tokenSet(right) {
		if _, ok := leftTokens[token]; ok {
			return true
		}
	}

	return false
}

func tokenSet(value string) map[string]struct{} {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})

	tokens := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if len(part) < 4 {
			continue
		}
		switch part {
		case "this", "that", "with", "from", "into", "then", "than", "repo", "path", "paths", "roadmap", "brief":
			continue
		}
		tokens[part] = struct{}{}
	}
	return tokens
}

func recentFailureCount(events []journal.Event) int {
	count := 0
	for _, event := range events {
		if strings.Contains(strings.TrimSpace(event.Type), ".failed") {
			count++
		}
	}
	return count
}

func persistDriftReviewArtifact(run state.Run, plannerResult planner.Result, report DriftReviewResult) (string, string) {
	if len(report.Artifact) == 0 {
		return "", ""
	}

	fileName := fmt.Sprintf(
		"drift_review_%s_%s.json",
		sanitizeArtifactComponent(string(plannerResult.Output.Outcome)),
		time.Now().UTC().Format("20060102T150405.000000000Z"),
	)
	relativePath, err := writeRunArtifact(run, "reviews", fileName, report.Artifact)
	if err != nil {
		return "", previewString("artifact_write_failed: "+err.Error(), 240)
	}

	return relativePath, previewString(string(report.Artifact), 240)
}
