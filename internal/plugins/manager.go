package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"orchestrator/internal/executor"
	"orchestrator/internal/planner"
	"orchestrator/internal/state"
)

const (
	DefaultDirectory = "plugins"
	ManifestFileName = "plugin.json"

	HookRunStart      = "run.start"
	HookPlannerAfter  = "planner.after"
	HookExecutorAfter = "executor.after"
	HookRunEnd        = "run.end"
	HookFaultRecorded = "fault.recorded"
)

type Manifest struct {
	Name         string        `json:"name"`
	Version      string        `json:"version"`
	Enabled      bool          `json:"enabled"`
	Tools        []Tool        `json:"tools,omitempty"`
	Hooks        []string      `json:"hooks,omitempty"`
	ConfigFields []ConfigField `json:"config_fields,omitempty"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type ConfigField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type LoadIssue struct {
	Plugin  string
	Path    string
	Message string
}

type Summary struct {
	Directory string
	Found     int
	Loaded    int
	Enabled   bool
	Failures  []LoadIssue
}

type ToolRequest struct {
	Run      state.Run
	Manifest Manifest
	Call     planner.PluginToolCall
}

type HookRequest struct {
	Run            state.Run
	Manifest       Manifest
	Point          string
	PlannerResult  *planner.Result
	ExecutorResult *executor.TurnResult
	StopReason     string
	CycleError     string
}

type HookResult struct {
	Success         bool
	Plugin          string
	Hook            string
	Message         string
	ArtifactPath    string
	ArtifactPreview string
}

type implementation interface {
	CallTool(context.Context, ToolRequest) (state.PluginToolResult, error)
	RunHook(context.Context, HookRequest) (HookResult, error)
}

type factory func(Manifest, string) implementation

type Manager struct {
	directory string
	found     int
	failures  []LoadIssue
	plugins   []loadedPlugin
}

type loadedPlugin struct {
	manifest Manifest
	path     string
	impl     implementation
}

var builtinFactories = map[string]factory{
	artifactIndexPluginName: newArtifactIndexPlugin,
}

func Load(repoRoot string) (*Manager, Summary) {
	manager := &Manager{
		directory: filepath.Join(repoRoot, DefaultDirectory),
	}

	entries, err := os.ReadDir(manager.directory)
	if err != nil {
		if !os.IsNotExist(err) {
			manager.failures = append(manager.failures, LoadIssue{
				Path:    manager.directory,
				Message: err.Error(),
			})
		}
		return manager, manager.Summary()
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(manager.directory, entry.Name(), ManifestFileName)
		if _, err := os.Stat(manifestPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			manager.failures = append(manager.failures, LoadIssue{
				Plugin:  entry.Name(),
				Path:    manifestPath,
				Message: err.Error(),
			})
			continue
		}

		manager.found++
		manifest, err := readManifest(manifestPath)
		if err != nil {
			manager.failures = append(manager.failures, LoadIssue{
				Plugin:  entry.Name(),
				Path:    manifestPath,
				Message: err.Error(),
			})
			continue
		}
		if !manifest.Enabled {
			continue
		}

		builtinFactory, ok := builtinFactories[manifest.Name]
		if !ok {
			manager.failures = append(manager.failures, LoadIssue{
				Plugin:  manifest.Name,
				Path:    manifestPath,
				Message: "no local implementation registered for enabled plugin",
			})
			continue
		}

		manager.plugins = append(manager.plugins, loadedPlugin{
			manifest: manifest,
			path:     manifestPath,
			impl:     builtinFactory(manifest, filepath.Dir(manifestPath)),
		})
	}

	sort.Slice(manager.plugins, func(i, j int) bool {
		return manager.plugins[i].manifest.Name < manager.plugins[j].manifest.Name
	})

	return manager, manager.Summary()
}

func (m *Manager) Summary() Summary {
	if m == nil {
		return Summary{}
	}

	failures := make([]LoadIssue, 0, len(m.failures))
	failures = append(failures, m.failures...)
	return Summary{
		Directory: m.directory,
		Found:     m.found,
		Loaded:    len(m.plugins),
		Enabled:   len(m.plugins) > 0,
		Failures:  failures,
	}
}

func (m *Manager) ToolDescriptors() []planner.PluginToolDescriptor {
	if m == nil || len(m.plugins) == 0 {
		return nil
	}

	descriptors := make([]planner.PluginToolDescriptor, 0)
	for _, plugin := range m.plugins {
		for _, tool := range plugin.manifest.Tools {
			descriptors = append(descriptors, planner.PluginToolDescriptor{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			})
		}
	}

	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Name < descriptors[j].Name
	})
	return descriptors
}

func (m *Manager) ExecuteToolCall(ctx context.Context, run state.Run, call planner.PluginToolCall) (state.PluginToolResult, error) {
	if m == nil {
		return unavailableToolResult(call.Tool, "plugin manager unavailable"), nil
	}

	plugin, ok := m.findPluginForTool(call.Tool)
	if !ok {
		return unavailableToolResult(call.Tool, "plugin tool not loaded"), nil
	}

	result, err := plugin.impl.CallTool(ctx, ToolRequest{
		Run:      run,
		Manifest: plugin.manifest,
		Call:     call,
	})
	if strings.TrimSpace(result.Tool) == "" {
		result.Tool = strings.TrimSpace(call.Tool)
	}
	if err != nil {
		if strings.TrimSpace(result.Message) == "" {
			result.Message = err.Error()
		}
		result.Success = false
		return result, err
	}
	return result, nil
}

func (m *Manager) RunHooks(ctx context.Context, point string, request HookRequest) []HookResult {
	if m == nil || strings.TrimSpace(point) == "" {
		return nil
	}

	results := make([]HookResult, 0)
	for _, plugin := range m.plugins {
		if !hasHook(plugin.manifest.Hooks, point) {
			continue
		}

		req := request
		req.Manifest = plugin.manifest
		req.Point = point

		result, err := plugin.impl.RunHook(ctx, req)
		if strings.TrimSpace(result.Plugin) == "" {
			result.Plugin = plugin.manifest.Name
		}
		if strings.TrimSpace(result.Hook) == "" {
			result.Hook = point
		}
		if err != nil && strings.TrimSpace(result.Message) == "" {
			result.Message = err.Error()
		}
		if err != nil {
			result.Success = false
		}
		results = append(results, result)
	}

	return results
}

func readManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateManifest(manifest Manifest) error {
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("plugin name is required")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("plugin version is required")
	}

	seenTools := map[string]struct{}{}
	for _, tool := range manifest.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return fmt.Errorf("tool name is required")
		}
		if strings.TrimSpace(tool.Description) == "" {
			return fmt.Errorf("tool %q description is required", tool.Name)
		}
		if !strings.HasPrefix(tool.Name, manifest.Name+".") {
			return fmt.Errorf("tool %q must be prefixed with %q", tool.Name, manifest.Name+".")
		}
		if _, exists := seenTools[tool.Name]; exists {
			return fmt.Errorf("tool %q is duplicated", tool.Name)
		}
		seenTools[tool.Name] = struct{}{}
	}

	seenHooks := map[string]struct{}{}
	for _, hook := range manifest.Hooks {
		trimmed := strings.TrimSpace(hook)
		if trimmed == "" {
			return fmt.Errorf("hook name is required")
		}
		if !isKnownHook(trimmed) {
			return fmt.Errorf("hook %q is not supported", trimmed)
		}
		if _, exists := seenHooks[trimmed]; exists {
			return fmt.Errorf("hook %q is duplicated", trimmed)
		}
		seenHooks[trimmed] = struct{}{}
	}

	seenFields := map[string]struct{}{}
	for _, field := range manifest.ConfigFields {
		if strings.TrimSpace(field.Name) == "" {
			return fmt.Errorf("config field name is required")
		}
		if strings.TrimSpace(field.Type) == "" {
			return fmt.Errorf("config field %q type is required", field.Name)
		}
		if _, exists := seenFields[field.Name]; exists {
			return fmt.Errorf("config field %q is duplicated", field.Name)
		}
		seenFields[field.Name] = struct{}{}
	}

	return nil
}

func (m *Manager) findPluginForTool(toolName string) (loadedPlugin, bool) {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return loadedPlugin{}, false
	}

	for _, plugin := range m.plugins {
		for _, tool := range plugin.manifest.Tools {
			if tool.Name == trimmed {
				return plugin, true
			}
		}
	}

	return loadedPlugin{}, false
}

func isKnownHook(hook string) bool {
	switch hook {
	case HookRunStart, HookPlannerAfter, HookExecutorAfter, HookRunEnd, HookFaultRecorded:
		return true
	default:
		return false
	}
}

func hasHook(hooks []string, point string) bool {
	for _, hook := range hooks {
		if strings.TrimSpace(hook) == strings.TrimSpace(point) {
			return true
		}
	}
	return false
}

func unavailableToolResult(toolName string, message string) state.PluginToolResult {
	return state.PluginToolResult{
		Tool:    strings.TrimSpace(toolName),
		Success: false,
		Message: strings.TrimSpace(message),
	}
}
