package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
	"claude-code-go/internal/tool"
)

const maxFileBytes = 16 << 20

type fileTool struct {
	workspace string
	spec      tool.Spec
	action    string
}

func NewReadFile(workspace string) tool.Tool {
	return &fileTool{workspace: workspace, action: permissions.ActionRead, spec: tool.Spec{
		Name: "read_file", Description: "Read a UTF-8 file", ReadOnly: true, ConcurrencySafe: true,
		Schema: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}},"additionalProperties":false}`),
	}}
}

func NewWriteFile(workspace string) tool.Tool {
	return &fileTool{workspace: workspace, action: permissions.ActionWrite, spec: tool.Spec{
		Name: "write_file", Description: "Atomically write a file",
		Schema: json.RawMessage(`{"type":"object","required":["path","content"],"properties":{"path":{"type":"string"},"content":{"type":"string"}},"additionalProperties":false}`),
	}}
}

func NewEditFile(workspace string) tool.Tool {
	return &fileTool{workspace: workspace, action: permissions.ActionWrite, spec: tool.Spec{
		Name: "edit_file", Description: "Replace one unique text occurrence in a file",
		Schema: json.RawMessage(`{"type":"object","required":["path","old_text","new_text"],"properties":{"path":{"type":"string"},"old_text":{"type":"string"},"new_text":{"type":"string"}},"additionalProperties":false}`),
	}}
}

func (file *fileTool) Spec() tool.Spec { return file.spec }

func (file *fileTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: file.spec.Name, Action: file.action, Workspace: file.workspace, Paths: []string{input.Path}}, nil
}

func (file *fileTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	switch file.spec.Name {
	case "read_file":
		var input struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(arguments, &input); err != nil {
			return core.ToolResult{}, err
		}
		content, err := readLimitedFile(input.Path)
		if err != nil {
			return core.ToolResult{}, err
		}
		return textResult(string(content)), nil
	case "write_file":
		var input struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(arguments, &input); err != nil {
			return core.ToolResult{}, err
		}
		if err := atomicWrite(ctx, input.Path, []byte(input.Content)); err != nil {
			return core.ToolResult{}, err
		}
		return textResult("file written"), nil
	case "edit_file":
		var input struct {
			Path    string `json:"path"`
			OldText string `json:"old_text"`
			NewText string `json:"new_text"`
		}
		if err := json.Unmarshal(arguments, &input); err != nil {
			return core.ToolResult{}, err
		}
		content, err := readLimitedFile(input.Path)
		if err != nil {
			return core.ToolResult{}, err
		}
		if count := strings.Count(string(content), input.OldText); count != 1 {
			return core.ToolResult{}, fmt.Errorf("old_text matched %d times; expected exactly once", count)
		}
		replaced := strings.Replace(string(content), input.OldText, input.NewText, 1)
		if err := atomicWrite(ctx, input.Path, []byte(replaced)); err != nil {
			return core.ToolResult{}, err
		}
		return textResult("file edited"), nil
	default:
		return core.ToolResult{}, fmt.Errorf("unsupported file operation")
	}
}

func readLimitedFile(path string) ([]byte, error) {
	opened, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	content, err := io.ReadAll(io.LimitReader(opened, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxFileBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxFileBytes)
	}
	return content, nil
}

func atomicWrite(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".claude-go-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return err
	}
	keep = true
	return syncDirectory(directory)
}

func textResult(text string) core.ToolResult {
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: text}}}
}
