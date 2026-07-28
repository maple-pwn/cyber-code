package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/platform"
	"cyber-code/internal/tool"
	"mvdan.cc/sh/v3/syntax"
)

type shellTool struct {
	workspace string
	executor  platform.Executor
}

func NewShell(workspace string, executor platform.Executor) tool.Tool {
	return &shellTool{workspace: workspace, executor: executor}
}

func (shell *shellTool) Spec() tool.Spec {
	return tool.Spec{Name: "shell", Description: "Run a shell command in the workspace",
		Schema: json.RawMessage(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"},"timeout_ms":{"type":"number"},"sandbox":{"type":"boolean"}},"additionalProperties":false}`)}
}

type shellInput struct {
	Command   string `json:"command"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	Sandbox   bool   `json:"sandbox,omitempty"`
}

func (shell *shellTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	input, err := parseShellInput(arguments)
	if err != nil {
		return permissions.Request{}, err
	}
	return ShellPermissionRequest("shell", shell.workspace, input.Command)
}

// ShellPermissionRequest applies the canonical shell redirection analysis to
// any command executed on behalf of a tool such as shell or hooks.
func ShellPermissionRequest(toolName, workspace, command string) (permissions.Request, error) {
	paths, err := redirectPaths(command)
	if err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: toolName, Action: permissions.ActionExecute, Workspace: workspace, Command: command, Paths: paths}, nil
}

func redirectPaths(command string) ([]string, error) {
	parsed, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, fmt.Errorf("parse shell command: %w", err)
	}
	var paths []string
	var walkErr error
	syntax.Walk(parsed, func(node syntax.Node) bool {
		redirect, ok := node.(*syntax.Redirect)
		if !ok || !redirectUsesPath(redirect.Op) || walkErr != nil {
			return walkErr == nil
		}
		path, ok := literalShellWord(redirect.Word)
		if !ok || path == "" {
			walkErr = fmt.Errorf("dynamic redirection paths are not allowed")
			return false
		}
		if path != "/dev/null" && !strings.EqualFold(path, "NUL") {
			paths = append(paths, path)
		}
		return true
	})
	return paths, walkErr
}

func redirectUsesPath(operator syntax.RedirOperator) bool {
	switch operator {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrIn, syntax.RdrInOut, syntax.ClbOut, syntax.RdrAll, syntax.AppAll:
		return true
	default:
		return false
	}
}

func literalShellWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var value strings.Builder
	for _, part := range word.Parts {
		switch typed := part.(type) {
		case *syntax.Lit:
			value.WriteString(typed.Value)
		case *syntax.SglQuoted:
			value.WriteString(typed.Value)
		case *syntax.DblQuoted:
			for _, quotedPart := range typed.Parts {
				literal, ok := quotedPart.(*syntax.Lit)
				if !ok {
					return "", false
				}
				value.WriteString(literal.Value)
			}
		default:
			return "", false
		}
	}
	return value.String(), true
}

func (shell *shellTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	input, err := parseShellInput(arguments)
	if err != nil {
		return core.ToolResult{}, err
	}
	if shell.executor == nil {
		return core.ToolResult{}, fmt.Errorf("platform executor is unavailable")
	}
	timeout := 2 * time.Minute
	if input.TimeoutMS > 0 {
		timeout = time.Duration(input.TimeoutMS) * time.Millisecond
	}
	if timeout > 10*time.Minute {
		return core.ToolResult{}, fmt.Errorf("timeout exceeds 10 minutes")
	}
	result, err := shell.executor.Run(ctx, platform.ExecRequest{Command: input.Command, Workspace: shell.workspace, Timeout: timeout, Sandbox: input.Sandbox})
	if err != nil {
		return core.ToolResult{}, err
	}
	output := result.Stdout
	if result.Stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += result.Stderr
	}
	return textResult(output), nil
}

func parseShellInput(arguments json.RawMessage) (shellInput, error) {
	var input shellInput
	if err := json.Unmarshal(arguments, &input); err != nil {
		return input, err
	}
	input.Command = strings.TrimSpace(input.Command)
	if input.Command == "" {
		return input, fmt.Errorf("command is required")
	}
	if input.TimeoutMS < 0 {
		return input, fmt.Errorf("timeout_ms must not be negative")
	}
	return input, nil
}
