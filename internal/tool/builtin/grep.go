package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
)

const maxSearchFileBytes = 4 << 20
const maxSearchLineRunes = 4096

type grepFilesTool struct{ workspace string }

func NewGrepFiles(workspace string) tool.Tool { return &grepFilesTool{workspace: workspace} }

func (grep *grepFilesTool) Spec() tool.Spec {
	return tool.Spec{
		Name: "grep_files", Description: "Search workspace file contents with a regular expression", ReadOnly: true, ConcurrencySafe: true,
		Schema: json.RawMessage(`{"type":"object","required":["pattern"],"properties":{"pattern":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":1000}},"additionalProperties":false}`),
	}
}

func (grep *grepFilesTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	input, _, err := decodeGrepInput(arguments)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: "grep_files", Action: permissions.ActionRead, Workspace: grep.workspace, Paths: []string{input.Path}}, nil
}

func (grep *grepFilesTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	input, expression, err := decodeGrepInput(arguments)
	if err != nil {
		return core.ToolResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}
	workspaceRoot, err := permissions.ResolvePath(grep.workspace, ".")
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
		matched, err := doublestar.Match(input.Glob, filepath.ToSlash(relativeRoot))
		if err != nil || !matched {
			return err
		}
		resolvedPath, err := permissions.ResolvePath(workspaceRoot, path)
		if err != nil {
			return err
		}
		info, err := os.Stat(resolvedPath)
		if err != nil {
			return err
		}
		if info.Size() > maxSearchFileBytes {
			return nil
		}
		content, err := os.ReadFile(resolvedPath)
		if err != nil {
			return err
		}
		if len(content) > maxSearchFileBytes || bytes.IndexByte(content, 0) >= 0 {
			return nil
		}
		relativeWorkspace, err := filepath.Rel(workspaceRoot, resolvedPath)
		if err != nil {
			return err
		}
		for index, line := range strings.Split(string(content), "\n") {
			if err := ctx.Err(); err != nil {
				return err
			}
			line = strings.TrimSuffix(line, "\r")
			if !expression.MatchString(line) {
				continue
			}
			matches = append(matches, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(relativeWorkspace), index+1, truncateSearchLine(line)))
			if len(matches) >= input.Limit {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("grep files: %w", err)
	}
	return textResult(strings.Join(matches, "\n")), nil
}

type grepInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
	Limit   int    `json:"limit"`
}

func decodeGrepInput(arguments json.RawMessage) (grepInput, *regexp.Regexp, error) {
	var input grepInput
	if err := json.Unmarshal(arguments, &input); err != nil {
		return grepInput{}, nil, err
	}
	if strings.TrimSpace(input.Pattern) == "" {
		return grepInput{}, nil, fmt.Errorf("pattern must not be empty")
	}
	expression, err := regexp.Compile(input.Pattern)
	if err != nil {
		return grepInput{}, nil, fmt.Errorf("compile pattern: %w", err)
	}
	if input.Path == "" {
		input.Path = "."
	}
	if input.Glob == "" {
		input.Glob = "**"
	}
	input.Glob = filepath.ToSlash(input.Glob)
	if !doublestar.ValidatePattern(input.Glob) || filepath.IsAbs(input.Glob) || input.Glob == ".." || strings.HasPrefix(input.Glob, "../") {
		return grepInput{}, nil, fmt.Errorf("glob must be a valid relative pattern")
	}
	if input.Limit == 0 {
		input.Limit = defaultSearchResultLimit
	}
	if input.Limit < 1 || input.Limit > maxSearchResultLimit {
		return grepInput{}, nil, fmt.Errorf("limit must be between 1 and %d", maxSearchResultLimit)
	}
	return input, expression, nil
}

func truncateSearchLine(line string) string {
	runes := []rune(line)
	if len(runes) <= maxSearchLineRunes {
		return line
	}
	return string(runes[:maxSearchLineRunes])
}
