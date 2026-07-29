package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
)

const defaultSearchResultLimit = 200
const maxSearchResultLimit = 1000

type globFilesTool struct{ workspace string }

func NewGlobFiles(workspace string) tool.Tool { return &globFilesTool{workspace: workspace} }

func (glob *globFilesTool) Spec() tool.Spec {
	return tool.Spec{
		Name: "glob_files", Description: "Match workspace file paths with recursive glob patterns", ReadOnly: true, ConcurrencySafe: true,
		Schema: json.RawMessage(`{"type":"object","required":["pattern"],"properties":{"pattern":{"type":"string"},"path":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":1000}},"additionalProperties":false}`),
	}
}

func (glob *globFilesTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	input, err := decodeGlobInput(arguments)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: "glob_files", Action: permissions.ActionRead, Workspace: glob.workspace, Paths: []string{input.Path}}, nil
}

func (glob *globFilesTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	input, err := decodeGlobInput(arguments)
	if err != nil {
		return core.ToolResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}
	workspaceRoot, err := permissions.ResolvePath(glob.workspace, ".")
	if err != nil {
		return core.ToolResult{}, err
	}
	root, err := permissions.ResolvePath(workspaceRoot, input.Path)
	if err != nil {
		return core.ToolResult{}, err
	}
	var matches []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		relativeRoot, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		matched, err := doublestar.Match(input.Pattern, filepath.ToSlash(relativeRoot))
		if err != nil {
			return err
		}
		if !matched {
			return nil
		}
		resolvedPath, err := permissions.ResolvePath(workspaceRoot, path)
		if err != nil {
			return err
		}
		relativeWorkspace, err := filepath.Rel(workspaceRoot, resolvedPath)
		if err != nil {
			return err
		}
		matches = append(matches, filepath.ToSlash(relativeWorkspace))
		if len(matches) >= input.Limit {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("glob files: %w", err)
	}
	sort.Strings(matches)
	return textResult(strings.Join(matches, "\n")), nil
}

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Limit   int    `json:"limit"`
}

func decodeGlobInput(arguments json.RawMessage) (globInput, error) {
	var input globInput
	if err := json.Unmarshal(arguments, &input); err != nil {
		return globInput{}, err
	}
	input.Pattern = filepath.ToSlash(strings.TrimSpace(input.Pattern))
	if input.Pattern == "" || !doublestar.ValidatePattern(input.Pattern) {
		return globInput{}, fmt.Errorf("pattern must be a valid non-empty glob")
	}
	if filepath.IsAbs(input.Pattern) || input.Pattern == ".." || strings.HasPrefix(input.Pattern, "../") {
		return globInput{}, fmt.Errorf("pattern must be relative to the search path")
	}
	if input.Path == "" {
		input.Path = "."
	}
	if input.Limit == 0 {
		input.Limit = defaultSearchResultLimit
	}
	if input.Limit < 1 || input.Limit > maxSearchResultLimit {
		return globInput{}, fmt.Errorf("limit must be between 1 and %d", maxSearchResultLimit)
	}
	return input, nil
}
