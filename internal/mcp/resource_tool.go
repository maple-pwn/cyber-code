package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

const resourceToolName = "mcp_read_resource"

type resourceTool struct {
	manager *Manager
}

type resourceToolInput struct {
	Server string `json:"server"`
	URI    string `json:"uri"`
}

func RegisterResourceTool(registry *toolpkg.Registry, manager *Manager) error {
	if registry == nil || manager == nil {
		return fmt.Errorf("MCP resource registry and manager are required")
	}
	if _, exists := registry.Get(resourceToolName); exists {
		return fmt.Errorf("MCP resource tool is already registered")
	}
	return registry.Register(&resourceTool{manager: manager})
}

func (tool *resourceTool) Spec() toolpkg.Spec {
	return toolpkg.Spec{
		Name: resourceToolName, Description: "Read a resource advertised by a connected MCP server",
		Schema:   json.RawMessage(`{"type":"object","required":["server","uri"],"properties":{"server":{"type":"string"},"uri":{"type":"string"}},"additionalProperties":false}`),
		ReadOnly: true, ConcurrencySafe: true,
	}
}

func (tool *resourceTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	if _, err := parseResourceToolInput(arguments); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: resourceToolName, Action: permissions.ActionRead}, nil
}

func (tool *resourceTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	input, err := parseResourceToolInput(arguments)
	if err != nil {
		return core.ToolResult{}, err
	}
	found := false
	for _, resource := range tool.manager.Resources(input.Server) {
		if resource.URI == input.URI {
			found = true
			break
		}
	}
	if !found {
		return core.ToolResult{}, fmt.Errorf("MCP resource %q was not advertised by server %q", input.URI, input.Server)
	}
	read, err := tool.manager.ReadResource(ctx, input.Server, input.URI)
	if err != nil {
		return core.ToolResult{}, err
	}
	encoded, err := json.Marshal(read.Contents)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("encode MCP resource: %w", err)
	}
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: string(encoded)}}}, nil
}

func parseResourceToolInput(arguments json.RawMessage) (resourceToolInput, error) {
	var input resourceToolInput
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if strings.TrimSpace(input.Server) == "" || strings.TrimSpace(input.URI) == "" {
		return input, fmt.Errorf("MCP resource server and URI are required")
	}
	return input, nil
}
