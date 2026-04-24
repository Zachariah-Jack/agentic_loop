package planner

import (
	"fmt"
	"strings"
	"testing"
)

func TestOutputJSONSchemaMarksAllObjectNodesStrict(t *testing.T) {
	for _, contractVersion := range []string{ContractVersionV1, ContractVersionV2} {
		schema := OutputJSONSchemaForContract(contractVersion)
		if err := assertStrictObjectNodes("root", schema); err != nil {
			t.Fatalf("%s strict object validation failed: %v", contractVersion, err)
		}
	}
}

func TestOutputJSONSchemaObjectRequiredArraysCoverProperties(t *testing.T) {
	for _, contractVersion := range []string{ContractVersionV1, ContractVersionV2} {
		schema := OutputJSONSchemaForContract(contractVersion)
		if err := assertRequiredCoversProperties("root", schema); err != nil {
			t.Fatalf("%s required/property validation failed: %v", contractVersion, err)
		}
	}
}

func TestOutputJSONSchemaDescribesWorkerDispatchRequirements(t *testing.T) {
	schema := OutputJSONSchema()

	collectContext := schemaProperty(t, schema, "collect_context")
	workerActions := schemaProperty(t, collectContext, "worker_actions")
	items, ok := workerActions["items"].(map[string]any)
	if !ok {
		t.Fatalf("worker_actions.items = %#v, want object schema", workerActions["items"])
	}

	description, _ := items["description"].(string)
	for _, want := range []string{
		"dispatch requires worker_id or worker_name, plus task_summary and executor_prompt",
		"create requires worker_name and scope",
		"integrate requires worker_ids",
		"apply requires apply_mode and either artifact_path or worker_ids",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("worker_actions.items.description missing %q\n%s", want, description)
		}
	}

	properties, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("worker_actions.items.properties = %#v, want object", items["properties"])
	}
	taskSummary := schemaNode(t, properties, "task_summary")
	executorPrompt := schemaNode(t, properties, "executor_prompt")
	if !strings.Contains(stringValue(taskSummary["description"]), "Required for dispatch") {
		t.Fatalf("task_summary description = %q", stringValue(taskSummary["description"]))
	}
	if !strings.Contains(stringValue(executorPrompt["description"]), "Required for dispatch") {
		t.Fatalf("executor_prompt description = %q", stringValue(executorPrompt["description"]))
	}
}

func TestOutputJSONSchemaForContractV2IncludesOperatorStatus(t *testing.T) {
	schema := OutputJSONSchemaForContract(ContractVersionV2)

	rootRequired, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("schema.required = %#v, want []string", schema["required"])
	}
	if !containsString(rootRequired, "operator_status") {
		t.Fatalf("planner.v2 schema.required missing operator_status")
	}

	operatorStatus := schemaProperty(t, schema, "operator_status")
	required, ok := operatorStatus["required"].([]string)
	if !ok {
		t.Fatalf("operator_status.required = %#v, want []string", operatorStatus["required"])
	}
	for _, want := range []string{
		"operator_message",
		"current_focus",
		"next_intended_step",
		"why_this_step",
		"progress_percent",
		"progress_confidence",
		"progress_basis",
	} {
		if !containsString(required, want) {
			t.Fatalf("operator_status.required missing %q", want)
		}
	}

	properties, ok := operatorStatus["properties"].(map[string]any)
	if !ok {
		t.Fatalf("operator_status.properties = %#v, want object", operatorStatus["properties"])
	}
	progressPercent := schemaNode(t, properties, "progress_percent")
	if progressPercent["type"] != "integer" {
		t.Fatalf("progress_percent.type = %#v, want integer", progressPercent["type"])
	}
}

func TestOutputJSONSchemaForContractV1RequiresNullableOperatorStatus(t *testing.T) {
	schema := OutputJSONSchemaForContract(ContractVersionV1)

	operatorStatus := schemaProperty(t, schema, "operator_status")
	if hasObjectType(operatorStatus["type"]) == false {
		t.Fatalf("operator_status.type = %#v, want object-or-null", operatorStatus["type"])
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("schema.required = %#v, want []string", schema["required"])
	}
	if !containsString(required, "operator_status") {
		t.Fatalf("planner.v1 schema.required missing operator_status")
	}
}

func assertStrictObjectNodes(path string, node any) error {
	switch typed := node.(type) {
	case map[string]any:
		if hasObjectType(typed["type"]) {
			value, ok := typed["additionalProperties"]
			if !ok {
				return fmt.Errorf("%s is missing additionalProperties", path)
			}
			flag, ok := value.(bool)
			if !ok || flag {
				return fmt.Errorf("%s must set additionalProperties=false", path)
			}
		}

		for key, child := range typed {
			switch key {
			case "properties":
				properties, ok := child.(map[string]any)
				if !ok {
					continue
				}
				for propertyName, propertySchema := range properties {
					if err := assertStrictObjectNodes(path+".properties."+propertyName, propertySchema); err != nil {
						return err
					}
				}
			case "items":
				if err := assertStrictObjectNodes(path+".items", child); err != nil {
					return err
				}
			}
		}
	case []any:
		for index, child := range typed {
			if err := assertStrictObjectNodes(fmt.Sprintf("%s[%d]", path, index), child); err != nil {
				return err
			}
		}
	}

	return nil
}

func assertRequiredCoversProperties(path string, node any) error {
	switch typed := node.(type) {
	case map[string]any:
		if hasObjectType(typed["type"]) {
			required, ok := stringSlice(typed["required"])
			if !ok {
				return fmt.Errorf("%s is missing required array", path)
			}
			properties, ok := typed["properties"].(map[string]any)
			if !ok {
				return fmt.Errorf("%s is missing properties object", path)
			}
			for propertyName := range properties {
				if !containsString(required, propertyName) {
					return fmt.Errorf("%s.required missing %q", path, propertyName)
				}
			}
		}

		for key, child := range typed {
			switch key {
			case "properties":
				properties, ok := child.(map[string]any)
				if !ok {
					continue
				}
				for propertyName, propertySchema := range properties {
					if err := assertRequiredCoversProperties(path+".properties."+propertyName, propertySchema); err != nil {
						return err
					}
				}
			case "items":
				if err := assertRequiredCoversProperties(path+".items", child); err != nil {
					return err
				}
			}
		}
	case []any:
		for index, child := range typed {
			if err := assertRequiredCoversProperties(fmt.Sprintf("%s[%d]", path, index), child); err != nil {
				return err
			}
		}
	}

	return nil
}

func schemaProperty(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties = %#v, want object", schema["properties"])
	}
	return schemaNode(t, properties, name)
}

func schemaNode(t *testing.T, properties map[string]any, name string) map[string]any {
	t.Helper()

	node, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("properties[%q] = %#v, want object schema", name, properties[name])
	}
	return node
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func hasObjectType(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "object"
	case []string:
		for _, item := range typed {
			if item == "object" {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok && text == "object" {
				return true
			}
		}
	}

	return false
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func stringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			items = append(items, text)
		}
		return items, true
	default:
		return nil, false
	}
}
