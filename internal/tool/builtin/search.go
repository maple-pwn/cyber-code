package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
	"claude-code-go/internal/tool"
)

type searchTool struct{ workspace string }

func NewSearchFiles(workspace string) tool.Tool { return &searchTool{workspace: workspace} }
func (search *searchTool) Spec() tool.Spec {
	return tool.Spec{Name: "search_files", Description: "Search text in workspace files", ReadOnly: true, ConcurrencySafe: true,
		Schema: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"path":{"type":"string"}},"additionalProperties":false}`)}
}
func (search *searchTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return permissions.Request{}, err
	}
	if input.Path == "" {
		input.Path = search.workspace
	}
	return permissions.Request{Tool: "search_files", Action: permissions.ActionRead, Workspace: search.workspace, Paths: []string{input.Path}}, nil
}
func (search *searchTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	var input struct{ Query, Path string }
	if err := json.Unmarshal(arguments, &input); err != nil {
		return core.ToolResult{}, err
	}
	if input.Path == "" {
		input.Path = search.workspace
	}
	var matches []string
	err := filepath.WalkDir(input.Path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || len(matches) >= 200 {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		content, readErr := readLimitedFile(path)
		if readErr == nil && strings.Contains(string(content), input.Query) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("search files: %w", err)
	}
	return textResult(strings.Join(matches, "\n")), nil
}
