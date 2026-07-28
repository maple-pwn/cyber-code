package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
)

func TestRunnerWithAuthorizerPreservesConfigurationAndRebindsPermissions(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(runnerTestTool{}); err != nil {
		t.Fatal(err)
	}
	parent := NewRunner(registry, runnerTestAuthorizer{allow: true}, RunnerOptions{MaxResultBytes: 2})
	child := parent.WithAuthorizer(runnerTestAuthorizer{allow: false})
	if child == parent {
		t.Fatal("permission rebinding returned the parent runner")
	}
	if _, err := child.Run(context.Background(), "test", json.RawMessage(`{}`)); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("child error = %v", err)
	}
	result, err := parent.Run(context.Background(), "test", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content[0].Text != "ab" {
		t.Fatalf("preserved result limit = %#v", result)
	}
}

type runnerTestAuthorizer struct{ allow bool }

func (a runnerTestAuthorizer) Decide(context.Context, permissions.Request) (permissions.Decision, error) {
	behavior := permissions.PermissionBehaviorDeny
	if a.allow {
		behavior = permissions.PermissionBehaviorAllow
	}
	return permissions.Decision{Behavior: behavior}, nil
}

type runnerTestTool struct{}

func (runnerTestTool) Spec() Spec {
	return Spec{Name: "test", Schema: json.RawMessage(`{"type":"object"}`)}
}
func (runnerTestTool) Authorize(context.Context, json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Tool: "test", Action: permissions.ActionRead}, nil
}
func (runnerTestTool) Run(context.Context, json.RawMessage) (core.ToolResult, error) {
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: "abcdef"}}}, nil
}
