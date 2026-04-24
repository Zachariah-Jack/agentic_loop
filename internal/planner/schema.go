package planner

import "sort"

const (
	OutputSchemaName   = "planner_output_v1"
	OutputSchemaNameV1 = "planner_output_v1"
	OutputSchemaNameV2 = "planner_output_v2"
)

func OutputJSONSchema() map[string]any {
	return OutputJSONSchemaForContract(ContractVersionV1)
}

func OutputJSONSchemaForContract(contractVersion string) map[string]any {
	executeSchema := map[string]any{
		"type": []string{"object", "null"},
		"properties": map[string]any{
			"task": map[string]any{
				"type": "string",
			},
			"acceptance_criteria": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"minItems": 1,
			},
			"write_scope": nullableArrayOfStrings(),
		},
		"required":             []string{"task", "acceptance_criteria", "write_scope"},
		"additionalProperties": false,
	}

	askHumanSchema := map[string]any{
		"type": []string{"object", "null"},
		"properties": map[string]any{
			"question": map[string]any{
				"type": "string",
			},
			"context": nullableString(),
		},
		"required":             []string{"question", "context"},
		"additionalProperties": false,
	}

	collectContextSchema := map[string]any{
		"type": []string{"object", "null"},
		"properties": map[string]any{
			"focus": map[string]any{
				"type": "string",
			},
			"questions": nullableArrayOfStrings(),
			"paths":     nullableArrayOfStrings(),
			"tool_calls": map[string]any{
				"type": []string{"array", "null"},
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tool": map[string]any{
							"type": "string",
						},
						"arguments": strictNullableObject(map[string]any{}),
					},
					"required":             []string{"tool", "arguments"},
					"additionalProperties": false,
				},
			},
			"worker_actions": map[string]any{
				"type": []string{"array", "null"},
				"items": map[string]any{
					"type":        "object",
					"description": "Worker action items are strict. create requires worker_name and scope. dispatch requires worker_id or worker_name, plus task_summary and executor_prompt. list requires only the action. remove requires worker_id or worker_name. integrate requires worker_ids. apply requires apply_mode and either artifact_path or worker_ids.",
					"properties": map[string]any{
						"action": map[string]any{
							"type":        "string",
							"description": "One of create, dispatch, list, remove, integrate, or apply.",
							"enum": []string{
								string(WorkerActionCreate),
								string(WorkerActionDispatch),
								string(WorkerActionList),
								string(WorkerActionRemove),
								string(WorkerActionIntegrate),
								string(WorkerActionApply),
							},
						},
						"worker_id": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Worker id. Required for dispatch/remove when worker_name is not provided.",
						},
						"worker_ids": map[string]any{
							"type":        []string{"array", "null"},
							"description": "Worker ids. Required for integrate, or for apply when artifact_path is not provided.",
							"items": map[string]any{
								"type": "string",
							},
						},
						"worker_name": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Worker name. Required for create, and valid for dispatch/remove when worker_id is not provided.",
						},
						"scope": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Required for create. Use repo-work scope wording, not free-form policy.",
						},
						"task_summary": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Required for dispatch. Summarize the bounded worker task.",
						},
						"executor_prompt": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Required for dispatch. The exact bounded executor prompt for that worker workspace.",
						},
						"artifact_path": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Integration artifact path. Valid for apply; required there when worker_ids are not provided.",
						},
						"apply_mode": map[string]any{
							"type":        []string{"string", "null"},
							"description": "Required for apply. Must be abort_if_conflicts or apply_non_conflicting.",
							"enum": []any{
								string(WorkerApplyModeAbortIfConflicts),
								string(WorkerApplyModeNonConflicting),
								nil,
							},
						},
					},
					"required":             []string{"action", "worker_id", "worker_ids", "worker_name", "scope", "task_summary", "executor_prompt", "artifact_path", "apply_mode"},
					"additionalProperties": false,
				},
			},
			"worker_plan": map[string]any{
				"type": []string{"object", "null"},
				"properties": map[string]any{
					"workers": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":            map[string]any{"type": "string"},
								"scope":           map[string]any{"type": "string"},
								"task_summary":    map[string]any{"type": "string"},
								"executor_prompt": map[string]any{"type": "string"},
							},
							"required":             []string{"name", "scope", "task_summary", "executor_prompt"},
							"additionalProperties": false,
						},
						"minItems": 1,
					},
					"integration_requested": map[string]any{
						"type": "boolean",
					},
					"apply_mode": map[string]any{
						"type": "string",
						"enum": []string{
							string(WorkerApplyModeAbortIfConflicts),
							string(WorkerApplyModeNonConflicting),
							string(WorkerApplyModeUnavailable),
						},
					},
				},
				"required":             []string{"workers", "integration_requested", "apply_mode"},
				"additionalProperties": false,
			},
		},
		"required":             []string{"focus", "questions", "paths", "tool_calls", "worker_actions", "worker_plan"},
		"additionalProperties": false,
	}

	pauseSchema := map[string]any{
		"type": []string{"object", "null"},
		"properties": map[string]any{
			"reason": map[string]any{
				"type": "string",
			},
		},
		"required":             []string{"reason"},
		"additionalProperties": false,
	}

	completeSchema := map[string]any{
		"type": []string{"object", "null"},
		"properties": map[string]any{
			"summary": map[string]any{
				"type": "string",
			},
		},
		"required":             []string{"summary"},
		"additionalProperties": false,
	}

	operatorStatusSchema := strictNullableObject(map[string]any{
		"operator_message": map[string]any{
			"type": "string",
		},
		"current_focus": map[string]any{
			"type": "string",
		},
		"next_intended_step": map[string]any{
			"type": "string",
		},
		"why_this_step": map[string]any{
			"type": "string",
		},
		"progress_percent": map[string]any{
			"type":    "integer",
			"minimum": 0,
			"maximum": 100,
		},
		"progress_confidence": map[string]any{
			"type": "string",
			"enum": []string{
				string(ProgressConfidenceLow),
				string(ProgressConfidenceMedium),
				string(ProgressConfidenceHigh),
			},
		},
		"progress_basis": map[string]any{
			"type": "string",
		},
	})

	properties := map[string]any{
		"contract_version": map[string]any{
			"type":  "string",
			"const": contractVersion,
		},
		"outcome": map[string]any{
			"type": "string",
			"enum": []string{
				string(OutcomeExecute),
				string(OutcomeAskHuman),
				string(OutcomeCollectContext),
				string(OutcomePause),
				string(OutcomeComplete),
			},
		},
		"execute":         executeSchema,
		"ask_human":       askHumanSchema,
		"collect_context": collectContextSchema,
		"pause":           pauseSchema,
		"complete":        completeSchema,
	}
	required := []string{
		"contract_version",
		"outcome",
		"execute",
		"ask_human",
		"collect_context",
		"pause",
		"complete",
	}
	if contractVersion == ContractVersionV1 || contractVersion == ContractVersionV2 {
		properties["operator_status"] = operatorStatusSchema
		required = append(required, "operator_status")
	}

	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func OutputSchemaNameForContract(contractVersion string) string {
	switch contractVersion {
	case ContractVersionV2:
		return OutputSchemaNameV2
	default:
		return OutputSchemaNameV1
	}
}

func nullableArrayOfStrings() map[string]any {
	return map[string]any{
		"type": []string{"array", "null"},
		"items": map[string]any{
			"type": "string",
		},
	}
}

func nullableString() map[string]any {
	return map[string]any{
		"type": []string{"string", "null"},
	}
}

func strictNullableObject(properties map[string]any) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}

	return map[string]any{
		"type":                 []string{"object", "null"},
		"properties":           properties,
		"required":             requiredKeys(properties),
		"additionalProperties": false,
	}
}

func requiredKeys(properties map[string]any) []string {
	required := make([]string, 0, len(properties))
	for key := range properties {
		required = append(required, key)
	}
	sort.Strings(required)
	return required
}
