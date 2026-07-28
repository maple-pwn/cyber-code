package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cyber-code/internal/core"
	"cyber-code/internal/hooks"
	"cyber-code/internal/permissions"
)

func TestHookDoesNotRunWhenBrokerDeniedTool(t *testing.T) {
	registry := NewRegistry()
	model := &fakeTool{spec: Spec{Name: "write", Schema: json.RawMessage(`{"type":"object"}`)}}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	hookCalls := 0
	hookRegistry := hooks.NewRegistry()
	hookRegistry.Register(hooks.HookEventPreToolUse, func(context.Context, hooks.HookInput) (hooks.HookOutput, error) {
		hookCalls++
		return hooks.HookOutput{Decision: "approve"}, nil
	})
	hookRunner, err := hooks.NewRunner(hooks.RunnerOptions{Registry: hookRegistry, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(registry, &fakeAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorDeny}}, RunnerOptions{Hooks: hookRunner})
	if _, err := runner.Run(context.Background(), "write", json.RawMessage(`{}`)); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("Run error = %v", err)
	}
	if hookCalls != 0 || model.runCalls != 0 {
		t.Fatalf("hook calls=%d tool runs=%d", hookCalls, model.runCalls)
	}
}

func TestPreHookCanDenyAllowedTool(t *testing.T) {
	registry := NewRegistry()
	model := &fakeTool{spec: Spec{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	hookRegistry := hooks.NewRegistry()
	hookRegistry.Register(hooks.HookEventPreToolUse, func(context.Context, hooks.HookInput) (hooks.HookOutput, error) {
		return hooks.HookOutput{Decision: "block", Reason: "blocked by hook"}, nil
	})
	hookRunner, err := hooks.NewRunner(hooks.RunnerOptions{Registry: hookRegistry, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(registry, &fakeAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}, RunnerOptions{Hooks: hookRunner})
	if _, err := runner.Run(context.Background(), "read", json.RawMessage(`{}`)); !errors.Is(err, ErrHookDenied) {
		t.Fatalf("Run error = %v", err)
	}
	if model.runCalls != 0 {
		t.Fatal("hook-denied tool executed")
	}
}

func TestHookInputTransformationIsRevalidatedAndReauthorized(t *testing.T) {
	registry := NewRegistry()
	model := &actionTool{}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	hookRegistry := hooks.NewRegistry()
	hookRegistry.Register(hooks.HookEventPreToolUse, func(context.Context, hooks.HookInput) (hooks.HookOutput, error) {
		return hooks.HookOutput{Continue: true, UpdatedInput: map[string]any{"action": "write"}}, nil
	})
	hookRunner, err := hooks.NewRunner(hooks.RunnerOptions{Registry: hookRegistry, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &actionAuthorizer{}
	runner := NewRunner(registry, authorizer, RunnerOptions{Hooks: hookRunner})
	if _, err := runner.Run(context.Background(), "action", json.RawMessage(`{"action":"read"}`)); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("Run error = %v", err)
	}
	if strings.Join(authorizer.actions, ",") != "read,write" || model.runs != 0 {
		t.Fatalf("authorized actions=%#v tool runs=%d", authorizer.actions, model.runs)
	}
}

func TestToolHooksRunPreToolPostAndAppendContext(t *testing.T) {
	registry := NewRegistry()
	model := &orderedTool{order: &[]string{}}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	hookRegistry := hooks.NewRegistry()
	hookRegistry.Register(hooks.HookEventPreToolUse, func(context.Context, hooks.HookInput) (hooks.HookOutput, error) {
		*model.order = append(*model.order, "pre")
		return hooks.HookOutput{Continue: true}, nil
	})
	hookRegistry.Register(hooks.HookEventPostToolUse, func(_ context.Context, input hooks.HookInput) (hooks.HookOutput, error) {
		*model.order = append(*model.order, "post")
		if input.ToolResult == nil {
			t.Fatal("post hook did not receive tool result")
		}
		return hooks.HookOutput{Continue: true, AdditionalContext: "post context"}, nil
	})
	hookRunner, err := hooks.NewRunner(hooks.RunnerOptions{Registry: hookRegistry, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(registry, &fakeAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}, RunnerOptions{Hooks: hookRunner, SessionID: "session"})
	result, err := runner.Run(context.Background(), "ordered", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(*model.order, ",") != "pre,tool,post" || len(result.Content) != 2 || result.Content[1].Text != "post context" {
		t.Fatalf("order=%#v result=%#v", *model.order, result)
	}
}

type actionTool struct{ runs int }

func (tool *actionTool) Spec() Spec {
	return Spec{Name: "action", Schema: json.RawMessage(`{"type":"object","required":["action"],"properties":{"action":{"type":"string"}},"additionalProperties":false}`)}
}
func (tool *actionTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	var input struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: "action", Action: input.Action}, nil
}
func (tool *actionTool) Run(context.Context, json.RawMessage) (core.ToolResult, error) {
	tool.runs++
	return core.ToolResult{}, nil
}

type actionAuthorizer struct{ actions []string }

func (authorizer *actionAuthorizer) Decide(_ context.Context, request permissions.Request) (permissions.Decision, error) {
	authorizer.actions = append(authorizer.actions, request.Action)
	behavior := permissions.PermissionBehaviorAllow
	if request.Action == permissions.ActionWrite {
		behavior = permissions.PermissionBehaviorDeny
	}
	return permissions.Decision{Behavior: behavior}, nil
}

type orderedTool struct{ order *[]string }

func (tool *orderedTool) Spec() Spec {
	return Spec{Name: "ordered", Schema: json.RawMessage(`{"type":"object"}`)}
}
func (tool *orderedTool) Authorize(context.Context, json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Tool: "ordered", Action: permissions.ActionRead}, nil
}
func (tool *orderedTool) Run(context.Context, json.RawMessage) (core.ToolResult, error) {
	*tool.order = append(*tool.order, "tool")
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: "result"}}}, nil
}
