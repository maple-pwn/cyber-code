package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
)

func TestRegistryRejectsDuplicateAndInvalidTools(t *testing.T) {
	registry := NewRegistry()
	valid := &fakeTool{spec: Spec{Name: "echo", Schema: json.RawMessage(`{"type":"object"}`)}}
	if err := registry.Register(valid); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(valid); err == nil {
		t.Fatal("duplicate tool was accepted")
	}
	if err := registry.Register(&fakeTool{spec: Spec{Name: "bad", Schema: json.RawMessage(`{"type":`)}}); err == nil {
		t.Fatal("invalid schema was accepted")
	}
}

func TestRunnerValidatesThenAuthorizesEveryExecution(t *testing.T) {
	registry := NewRegistry()
	model := &fakeTool{spec: Spec{
		Name:   "echo",
		Schema: json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"string"}},"additionalProperties":false}`),
	}}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	authorizer := &fakeAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}
	runner := NewRunner(registry, authorizer, RunnerOptions{})

	if _, err := runner.Run(context.Background(), "echo", json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing required argument was accepted")
	}
	if authorizer.calls != 0 || model.authorizeCalls != 0 || model.runCalls != 0 {
		t.Fatal("invalid input reached authorization or execution")
	}
	for range 2 {
		if _, err := runner.Run(context.Background(), "echo", json.RawMessage(`{"value":"ok"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if authorizer.calls != 2 || model.authorizeCalls != 2 || model.runCalls != 2 {
		t.Fatalf("calls: broker=%d authorize=%d run=%d", authorizer.calls, model.authorizeCalls, model.runCalls)
	}
}

func TestRunnerDoesNotRunDeniedTool(t *testing.T) {
	registry := NewRegistry()
	model := &fakeTool{spec: Spec{Name: "write", Schema: json.RawMessage(`{"type":"object"}`)}}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(registry, &fakeAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorDeny}}, RunnerOptions{})
	if _, err := runner.Run(context.Background(), "write", json.RawMessage(`{}`)); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("error = %v", err)
	}
	if model.runCalls != 0 {
		t.Fatal("denied tool executed")
	}
}

func TestRegistrySnapshotsSchemaAtRegistration(t *testing.T) {
	registry := NewRegistry()
	model := &fakeTool{spec: Spec{Name: "echo", Schema: json.RawMessage(`{"type":"object"}`)}}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	model.spec.Schema[0] = 'X'
	specs := registry.Specs()
	if len(specs) != 1 || !json.Valid(specs[0].Schema) {
		t.Fatalf("registered schema changed: %#v", specs)
	}
	specs[0].Schema[0] = 'Y'
	if !json.Valid(registry.Specs()[0].Schema) {
		t.Fatal("Specs returned mutable registry state")
	}
	runner := NewRunner(registry, &fakeAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}, RunnerOptions{})
	if _, err := runner.Run(context.Background(), "echo", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("runner did not use registered schema snapshot: %v", err)
	}
}

func TestRunnerTruncatesNestedToolResultContent(t *testing.T) {
	registry := NewRegistry()
	model := &fakeTool{
		spec: Spec{Name: "large", Schema: json.RawMessage(`{"type":"object"}`)},
		result: core.ToolResult{Content: []core.ContentBlock{
			{Type: core.ContentText, Text: "hello"},
			{Type: core.ContentToolResult, ToolResult: &core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: "world"}}}},
		}},
	}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(registry, &fakeAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}, RunnerOptions{MaxResultBytes: 4})
	result, err := runner.Run(context.Background(), "large", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content[0].Text != "hell" || result.Content[1].ToolResult.Content[0].Text != "" {
		t.Fatalf("result was not recursively truncated: %#v", result)
	}
}

type fakeTool struct {
	spec           Spec
	result         core.ToolResult
	authorizeCalls int
	runCalls       int
}

func (tool *fakeTool) Spec() Spec { return tool.spec }
func (tool *fakeTool) Authorize(context.Context, json.RawMessage) (permissions.Request, error) {
	tool.authorizeCalls++
	return permissions.Request{Tool: tool.spec.Name, Action: permissions.ActionRead}, nil
}
func (tool *fakeTool) Run(context.Context, json.RawMessage) (core.ToolResult, error) {
	tool.runCalls++
	if tool.result.Content != nil {
		return tool.result, nil
	}
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: "ok"}}}, nil
}

type fakeAuthorizer struct {
	calls    int
	decision permissions.Decision
}

func (authorizer *fakeAuthorizer) Decide(context.Context, permissions.Request) (permissions.Decision, error) {
	authorizer.calls++
	return authorizer.decision, nil
}
