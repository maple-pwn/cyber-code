package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

const ToolName = "load_skill"

type instructionTool struct {
	skills map[string]Skill
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
	for _, item := range discovered {
		name := strings.TrimSpace(item.Name)
		if name == "" || strings.TrimSpace(item.Instructions) == "" {
			return fmt.Errorf("skill name and instructions are required")
		}
		if _, exists := values[name]; exists {
			return fmt.Errorf("duplicate skill %q", name)
		}
		item.Name = name
		values[name] = item
	}
	return registry.Register(&instructionTool{skills: values})
}

func (tool *instructionTool) Spec() toolpkg.Spec {
	return toolpkg.Spec{
		Name: ToolName, Description: "Load the instructions for an available skill",
		Schema:   json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}},"additionalProperties":false}`),
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
	text := "Skill: " + item.Name + "\nSource: " + item.Source + "\n\n" + item.Instructions
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: text}}}, nil
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
