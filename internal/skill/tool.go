package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

const ToolName = "load_skill"

type instructionTool struct {
	skills map[string]Skill
	schema json.RawMessage
}

type toolInput struct {
	Name string `json:"name"`
}

func RegisterTool(registry *toolpkg.Registry, discovered []Skill) error {
	if registry == nil {
		return fmt.Errorf("skill tool registry is required")
	}
	if len(discovered) == 0 {
		return fmt.Errorf("at least one discovered skill is required")
	}
	values := make(map[string]Skill, len(discovered))
	names := make([]string, 0, len(discovered))
	for _, item := range discovered {
		name := strings.TrimSpace(item.Name)
		if name == "" || (strings.TrimSpace(item.Instructions) == "" && strings.TrimSpace(item.Path) == "") {
			return fmt.Errorf("skill name and instructions path are required")
		}
		if _, exists := values[name]; exists {
			return fmt.Errorf("duplicate skill %q", name)
		}
		item.Name = name
		values[name] = item
		names = append(names, name)
	}
	sort.Strings(names)
	schema, err := json.Marshal(struct {
		Type                 string         `json:"type"`
		Required             []string       `json:"required"`
		Properties           map[string]any `json:"properties"`
		AdditionalProperties bool           `json:"additionalProperties"`
	}{
		Type: "object", Required: []string{"name"},
		Properties: map[string]any{"name": struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		}{Type: "string", Enum: names}},
		AdditionalProperties: false,
	})
	if err != nil {
		return fmt.Errorf("encode skill tool schema: %w", err)
	}
	return registry.Register(&instructionTool{skills: values, schema: schema})
}

func (tool *instructionTool) Spec() toolpkg.Spec {
	return toolpkg.Spec{
		Name: ToolName, Description: "Load the instructions for an available skill",
		Schema:   tool.schema,
		ReadOnly: true, ConcurrencySafe: true,
	}
}

func (tool *instructionTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	if _, err := tool.parse(arguments); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: ToolName, Action: permissions.ActionRead}, nil
}

func (tool *instructionTool) Run(_ context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	item, err := tool.parse(arguments)
	if err != nil {
		return core.ToolResult{}, err
	}
	instructions := item.Instructions
	if strings.TrimSpace(instructions) == "" {
		instructions, err = readSkillInstructions(item.Path)
		if err != nil {
			return core.ToolResult{}, err
		}
	}
	text := "Skill: " + item.Name + "\nSource: " + item.Source + "\n\n" + instructions
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: text}}}, nil
}

func readSkillInstructions(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("skill instructions path is empty")
	}
	root, err := filepath.Abs(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !insideRoot(root, resolved) {
		return "", fmt.Errorf("skill instructions path escapes its root")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxInstructionsBytes {
		return "", fmt.Errorf("skill instructions are invalid")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", fmt.Errorf("read skill instructions: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxInstructionsBytes+1))
	if err != nil {
		return "", fmt.Errorf("read skill instructions: %w", err)
	}
	after, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(after) != filepath.Clean(resolved) {
		return "", fmt.Errorf("skill instructions path changed while reading")
	}
	if int64(len(content)) > maxInstructionsBytes {
		return "", fmt.Errorf("skill instructions are too large")
	}
	return string(content), nil
}

func (tool *instructionTool) parse(arguments json.RawMessage) (Skill, error) {
	var input toolInput
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return Skill{}, err
	}
	item, exists := tool.skills[strings.TrimSpace(input.Name)]
	if !exists {
		return Skill{}, fmt.Errorf("skill %q is not available", input.Name)
	}
	return item, nil
}
