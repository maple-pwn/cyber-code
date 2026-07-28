package lsp

import (
	"context"
	"encoding/json"
	"fmt"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

type Service interface {
	Definition(context.Context, Query) ([]Location, error)
	References(context.Context, Query, bool) ([]Location, error)
	Completion(context.Context, Query) (CompletionList, error)
	Hover(context.Context, Query) (*Hover, error)
	Diagnostics(context.Context, Query) ([]Diagnostic, error)
}

func RegisterTools(registry *toolpkg.Registry, service Service) error {
	if registry == nil || service == nil {
		return fmt.Errorf("LSP tool registry and service are required")
	}
	tools := []*lspTool{
		newLSPTool("lsp_diagnostics", "Read language server diagnostics", false, service),
		newLSPTool("lsp_definition", "Find the definition at a source position", true, service),
		newLSPTool("lsp_references", "Find references at a source position", true, service),
		newLSPTool("lsp_completion", "Get completions at a source position", true, service),
		newLSPTool("lsp_hover", "Get hover information at a source position", true, service),
	}
	for _, model := range tools {
		if _, exists := registry.Get(model.spec.Name); exists {
			return fmt.Errorf("LSP tool %q is already registered", model.spec.Name)
		}
	}
	for _, model := range tools {
		if err := registry.Register(model); err != nil {
			return err
		}
	}
	return nil
}

type lspTool struct {
	spec    toolpkg.Spec
	service Service
}

type lspToolInput struct {
	Workspace          string `json:"workspace"`
	Language           string `json:"language"`
	File               string `json:"file"`
	Line               int    `json:"line"`
	Character          int    `json:"character"`
	IncludeDeclaration bool   `json:"include_declaration"`
}

func newLSPTool(name, description string, position bool, service Service) *lspTool {
	properties := `"workspace":{"type":"string"},"language":{"type":"string"},"file":{"type":"string"}`
	required := `"workspace","language","file"`
	if position {
		properties += `,"line":{"type":"number"},"character":{"type":"number"}`
		required += `,"line","character"`
	}
	if name == "lsp_references" {
		properties += `,"include_declaration":{"type":"boolean"}`
	}
	schema := json.RawMessage(`{"type":"object","required":[` + required + `],"properties":{` + properties + `},"additionalProperties":false}`)
	return &lspTool{service: service, spec: toolpkg.Spec{
		Name: name, Description: description, Schema: schema, ReadOnly: true, ConcurrencySafe: true,
	}}
}

func (tool *lspTool) Spec() toolpkg.Spec { return tool.spec }

func (tool *lspTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	input, err := parseLSPToolInput(arguments)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{
		Tool: tool.spec.Name, Action: permissions.ActionRead, Workspace: input.Workspace, Paths: []string{input.File},
	}, nil
}

func (tool *lspTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	input, err := parseLSPToolInput(arguments)
	if err != nil {
		return core.ToolResult{}, err
	}
	query := Query{
		Workspace: input.Workspace, Language: input.Language, File: input.File,
		Position: Position{Line: input.Line, Character: input.Character},
	}
	var value any
	switch tool.spec.Name {
	case "lsp_diagnostics":
		value, err = tool.service.Diagnostics(ctx, query)
	case "lsp_definition":
		value, err = tool.service.Definition(ctx, query)
	case "lsp_references":
		value, err = tool.service.References(ctx, query, input.IncludeDeclaration)
	case "lsp_completion":
		value, err = tool.service.Completion(ctx, query)
	case "lsp_hover":
		value, err = tool.service.Hover(ctx, query)
	default:
		return core.ToolResult{}, fmt.Errorf("unsupported LSP tool %q", tool.spec.Name)
	}
	if err != nil {
		return core.ToolResult{}, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("encode LSP result: %w", err)
	}
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: string(encoded)}}}, nil
}

func parseLSPToolInput(arguments json.RawMessage) (lspToolInput, error) {
	var input lspToolInput
	if err := json.Unmarshal(arguments, &input); err != nil {
		return input, err
	}
	if input.Workspace == "" || input.Language == "" || input.File == "" {
		return input, fmt.Errorf("workspace, language, and file are required")
	}
	if input.Line < 0 || input.Character < 0 {
		return input, fmt.Errorf("line and character must not be negative")
	}
	return input, nil
}
