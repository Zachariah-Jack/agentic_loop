package planner

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateInput(input InputEnvelope) error {
	var issues []string

	if input.ContractVersion != ContractVersionV1 {
		issues = append(issues, fmt.Sprintf("contract_version must be %q", ContractVersionV1))
	}
	if strings.TrimSpace(input.RunID) == "" {
		issues = append(issues, "run_id is required")
	}
	if strings.TrimSpace(input.RepoPath) == "" {
		issues = append(issues, "repo_path is required")
	}
	if strings.TrimSpace(input.Goal) == "" {
		issues = append(issues, "goal is required")
	}
	if strings.TrimSpace(input.RunStatus) == "" {
		issues = append(issues, "run_status is required")
	}

	issues = append(issues, validateCheckpoint("latest_checkpoint", input.LatestCheckpoint)...)
	issues = append(issues, validateCapabilities(input.Capabilities)...)

	for i, event := range input.RecentEvents {
		prefix := fmt.Sprintf("recent_events[%d]", i)
		if event.At.IsZero() {
			issues = append(issues, prefix+".at is required")
		}
		if strings.TrimSpace(event.Type) == "" {
			issues = append(issues, prefix+".type is required")
		}
		if strings.TrimSpace(event.Summary) == "" {
			issues = append(issues, prefix+".summary is required")
		}
	}

	for i, reply := range input.RawHumanReplies {
		prefix := fmt.Sprintf("raw_human_replies[%d]", i)
		if strings.TrimSpace(reply.ID) == "" {
			issues = append(issues, prefix+".id is required")
		}
		if strings.TrimSpace(reply.Source) == "" {
			issues = append(issues, prefix+".source is required")
		}
		if reply.ReceivedAt.IsZero() {
			issues = append(issues, prefix+".received_at is required")
		}
		if strings.TrimSpace(reply.Payload) == "" {
			issues = append(issues, prefix+".payload is required")
		}
	}

	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}

	return nil
}

func ValidateOutput(output OutputEnvelope) error {
	var issues []string

	if output.ContractVersion != ContractVersionV1 {
		issues = append(issues, fmt.Sprintf("contract_version must be %q", ContractVersionV1))
	}

	payloadCount := 0
	if output.Execute != nil {
		payloadCount++
	}
	if output.AskHuman != nil {
		payloadCount++
	}
	if output.CollectContext != nil {
		payloadCount++
	}
	if output.Pause != nil {
		payloadCount++
	}
	if output.Complete != nil {
		payloadCount++
	}
	if payloadCount != 1 {
		issues = append(issues, "exactly one outcome payload must be set")
	}

	switch output.Outcome {
	case OutcomeExecute:
		if output.Execute == nil {
			issues = append(issues, "execute payload is required when outcome=execute")
			break
		}
		if strings.TrimSpace(output.Execute.Task) == "" {
			issues = append(issues, "execute.task is required")
		}
		if len(nonEmpty(output.Execute.AcceptanceCriteria)) == 0 {
			issues = append(issues, "execute.acceptance_criteria must contain at least one non-empty item")
		}
		if output.AskHuman != nil || output.CollectContext != nil || output.Pause != nil || output.Complete != nil {
			issues = append(issues, "only execute payload may be set when outcome=execute")
		}
	case OutcomeAskHuman:
		if output.AskHuman == nil {
			issues = append(issues, "ask_human payload is required when outcome=ask_human")
			break
		}
		if strings.TrimSpace(output.AskHuman.Question) == "" {
			issues = append(issues, "ask_human.question is required")
		}
		if output.Execute != nil || output.CollectContext != nil || output.Pause != nil || output.Complete != nil {
			issues = append(issues, "only ask_human payload may be set when outcome=ask_human")
		}
	case OutcomeCollectContext:
		if output.CollectContext == nil {
			issues = append(issues, "collect_context payload is required when outcome=collect_context")
			break
		}
		if strings.TrimSpace(output.CollectContext.Focus) == "" {
			issues = append(issues, "collect_context.focus is required")
		}
		if len(nonEmpty(output.CollectContext.Questions)) == 0 && len(nonEmpty(output.CollectContext.Paths)) == 0 {
			issues = append(issues, "collect_context must include at least one non-empty question or path")
		}
		if output.Execute != nil || output.AskHuman != nil || output.Pause != nil || output.Complete != nil {
			issues = append(issues, "only collect_context payload may be set when outcome=collect_context")
		}
	case OutcomePause:
		if output.Pause == nil {
			issues = append(issues, "pause payload is required when outcome=pause")
			break
		}
		if strings.TrimSpace(output.Pause.Reason) == "" {
			issues = append(issues, "pause.reason is required")
		}
		if output.Execute != nil || output.AskHuman != nil || output.CollectContext != nil || output.Complete != nil {
			issues = append(issues, "only pause payload may be set when outcome=pause")
		}
	case OutcomeComplete:
		if output.Complete == nil {
			issues = append(issues, "complete payload is required when outcome=complete")
			break
		}
		if strings.TrimSpace(output.Complete.Summary) == "" {
			issues = append(issues, "complete.summary is required")
		}
		if output.Execute != nil || output.AskHuman != nil || output.CollectContext != nil || output.Pause != nil {
			issues = append(issues, "only complete payload may be set when outcome=complete")
		}
	default:
		issues = append(issues, "outcome must be one of execute, ask_human, collect_context, pause, complete")
	}

	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}

	return nil
}

func validateCheckpoint(prefix string, checkpoint Checkpoint) []string {
	var issues []string

	if checkpoint.Sequence <= 0 {
		issues = append(issues, prefix+".sequence must be greater than zero")
	}
	if strings.TrimSpace(checkpoint.Stage) == "" {
		issues = append(issues, prefix+".stage is required")
	}
	if strings.TrimSpace(checkpoint.Label) == "" {
		issues = append(issues, prefix+".label is required")
	}
	if checkpoint.CreatedAt.IsZero() {
		issues = append(issues, prefix+".created_at is required")
	}

	return issues
}

func validateCapabilities(markers CapabilityMarkers) []string {
	var issues []string

	for _, item := range []struct {
		name  string
		value CapabilityStatus
	}{
		{name: "capabilities.planner", value: markers.Planner},
		{name: "capabilities.executor", value: markers.Executor},
		{name: "capabilities.ntfy", value: markers.NTFY},
	} {
		if !isKnownCapabilityStatus(item.value) {
			issues = append(issues, item.name+" must be a known capability status")
		}
	}

	return issues
}

func isKnownCapabilityStatus(status CapabilityStatus) bool {
	switch status {
	case CapabilityContractOnly, CapabilityDeferred, CapabilityAvailable, CapabilityUnavailable:
		return true
	default:
		return false
	}
}

func nonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
