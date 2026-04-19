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
		},
		"required":             []string{"focus", "questions", "paths"},
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
