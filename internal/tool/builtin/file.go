package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/product"
	"cyber-code/internal/tool"
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
		Name: "edit_file", Description: "Atomically replace one or more unique text occurrences in a file",
		Schema: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"},"old_text":{"type":"string"},"new_text":{"type":"string"},"edits":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"object","required":["old_text","new_text"],"properties":{"old_text":{"type":"string"},"new_text":{"type":"string"}},"additionalProperties":false}},"expected_sha256":{"type":"string"}},"additionalProperties":false}`),
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
		path, err := permissions.ResolvePath(file.workspace, input.Path)
		if err != nil {
			return core.ToolResult{}, err
		}
		content, err := readLimitedFile(path)
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
		path, err := permissions.ResolvePath(file.workspace, input.Path)
		if err != nil {
			return core.ToolResult{}, err
		}
		diffPath, err := relativeDiffPath(file.workspace, path)
		if err != nil {
			return core.ToolResult{}, err
		}
		oldContent, err := readLimitedFile(path)
		if err != nil && !os.IsNotExist(err) {
			return core.ToolResult{}, err
		}
		if err := atomicWrite(ctx, path, []byte(input.Content)); err != nil {
			return core.ToolResult{}, err
		}
		return fileResult("file written", diffPath, string(oldContent), input.Content), nil
	case "edit_file":
		var input struct {
			Path           string     `json:"path"`
			OldText        *string    `json:"old_text"`
			NewText        *string    `json:"new_text"`
			Edits          []textEdit `json:"edits"`
			ExpectedSHA256 string     `json:"expected_sha256"`
		}
		if err := json.Unmarshal(arguments, &input); err != nil {
			return core.ToolResult{}, err
		}
		path, err := permissions.ResolvePath(file.workspace, input.Path)
		if err != nil {
			return core.ToolResult{}, err
		}
		diffPath, err := relativeDiffPath(file.workspace, path)
		if err != nil {
			return core.ToolResult{}, err
		}
		content, err := readLimitedFile(path)
		if err != nil {
			return core.ToolResult{}, err
		}
		if input.ExpectedSHA256 != "" {
			digest := sha256.Sum256(content)
			if !strings.EqualFold(input.ExpectedSHA256, fmt.Sprintf("%x", digest)) {
				return core.ToolResult{}, fmt.Errorf("file changed since the expected version")
			}
		}
		hasLegacy := input.OldText != nil || input.NewText != nil
		hasMulti := input.Edits != nil
		if hasLegacy == hasMulti {
			return core.ToolResult{}, fmt.Errorf("provide either old_text/new_text or edits, but not both")
		}
		edits := input.Edits
		if hasLegacy {
			if input.OldText == nil || input.NewText == nil {
				return core.ToolResult{}, fmt.Errorf("old_text and new_text must be provided together")
			}
			edits = []textEdit{{OldText: *input.OldText, NewText: *input.NewText}}
		}
		replaced, err := applyTextEdits(string(content), edits)
		if err != nil {
			return core.ToolResult{}, err
		}
		latest, err := readLimitedFile(path)
		if err != nil {
			return core.ToolResult{}, err
		}
		if string(latest) != string(content) {
			return core.ToolResult{}, fmt.Errorf("file changed during edit; retry with fresh content")
		}
		if err := atomicWrite(ctx, path, []byte(replaced)); err != nil {
			return core.ToolResult{}, err
		}
		return fileResult("file edited", diffPath, string(content), replaced), nil
	default:
		return core.ToolResult{}, fmt.Errorf("unsupported file operation")
	}
}

type textEdit struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type locatedTextEdit struct {
	start, end int
	newText    string
}

func applyTextEdits(content string, edits []textEdit) (string, error) {
	if len(edits) == 0 {
		return "", fmt.Errorf("edits must contain at least one replacement")
	}
	if len(edits) > 100 {
		return "", fmt.Errorf("edits must not contain more than 100 replacements")
	}
	located := make([]locatedTextEdit, 0, len(edits))
	for index, edit := range edits {
		if edit.OldText == "" {
			return "", fmt.Errorf("edits[%d].old_text must not be empty", index)
		}
		if count := strings.Count(content, edit.OldText); count != 1 {
			return "", fmt.Errorf("edits[%d].old_text matched %d times; expected exactly once", index, count)
		}
		start := strings.Index(content, edit.OldText)
		located = append(located, locatedTextEdit{start: start, end: start + len(edit.OldText), newText: edit.NewText})
	}
	sort.Slice(located, func(i, j int) bool { return located[i].start < located[j].start })
	for index := 1; index < len(located); index++ {
		if located[index].start < located[index-1].end {
			return "", fmt.Errorf("edits overlap in the original file")
		}
	}
	var output strings.Builder
	output.Grow(len(content))
	cursor := 0
	for _, edit := range located {
		output.WriteString(content[cursor:edit.start])
		output.WriteString(edit.newText)
		cursor = edit.end
	}
	output.WriteString(content[cursor:])
	return output.String(), nil
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
	temporary, err := os.CreateTemp(directory, "."+product.Name+"-*")
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

func relativeDiffPath(workspace, path string) (string, error) {
	root, err := permissions.ResolvePath(workspace, ".")
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("resolve diff path: %w", err)
	}
	return filepath.ToSlash(relative), nil
}

func fileResult(message, path, oldText, newText string) core.ToolResult {
	result := textResult(message)
	result.Diff = &core.FileDiff{Path: path, OldText: oldText, NewText: newText}
	return result
}
