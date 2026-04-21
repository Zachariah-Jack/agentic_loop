package planner

const OutputSchemaName = "planner_output_v1"

func OutputJSONSchema() map[string]any {
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
						"arguments": map[string]any{
							"type": []string{"object", "null"},
						},
					},
					"required":             []string{"tool", "arguments"},
					"additionalProperties": false,
				},
			},
			"worker_actions": map[string]any{
				"type": []string{"array", "null"},
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type": "string",
							"enum": []string{
								string(WorkerActionCreate),
								string(WorkerActionDispatch),
								string(WorkerActionList),
								string(WorkerActionRemove),
								string(WorkerActionIntegrate),
								string(WorkerActionApply),
							},
						},
						"worker_id":  nullableString(),
						"worker_ids": nullableArrayOfStrings(),
						"worker_name": map[string]any{
							"type": []string{"string", "null"},
						},
						"scope":           nullableString(),
						"task_summary":    nullableString(),
						"executor_prompt": nullableString(),
						"artifact_path":   nullableString(),
						"apply_mode": map[string]any{
							"type": []string{"string", "null"},
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

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"contract_version": map[string]any{
				"type":  "string",
				"const": ContractVersionV1,
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
		},
		"required": []string{
			"contract_version",
			"outcome",
			"execute",
			"ask_human",
			"collect_context",
			"pause",
			"complete",
		},
		"additionalProperties": false,
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
