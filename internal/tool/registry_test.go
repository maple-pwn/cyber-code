package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
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

func TestRegistryRejectsNilUnnamedAndMalformedSchemas(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); err == nil {
		t.Fatal("nil tool was accepted")
	}
	for _, spec := range []Spec{
		{Name: "", Schema: json.RawMessage(`{"type":"object"}`)},
		{Name: "array-root", Schema: json.RawMessage(`{"type":"array"}`)},
		{Name: "bad-required", Schema: json.RawMessage(`{"type":"object","required":"value"}`)},
		{Name: "bad-properties", Schema: json.RawMessage(`{"type":"object","properties":[]}`)},
	} {
		if err := registry.Register(&fakeTool{spec: spec}); err == nil {
			t.Fatalf("invalid spec was accepted: %#v", spec)
		}
	}
}

func TestRegistryGetAndSpecsAreDeterministic(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"zeta", "alpha"} {
		if err := registry.Register(&fakeTool{spec: Spec{Name: name, Schema: json.RawMessage(`{"type":"object"}`)}}); err != nil {
			t.Fatal(err)
		}
	}
	if model, ok := registry.Get("alpha"); !ok || model.Spec().Name != "alpha" {
		t.Fatalf("Get(alpha) = %#v, %v", model, ok)
	}
	if model, ok := registry.Get("missing"); ok || model != nil {
		t.Fatalf("Get(missing) = %#v, %v", model, ok)
	}
	if specs := registry.Specs(); len(specs) != 2 || specs[0].Name != "alpha" || specs[1].Name != "zeta" {
		t.Fatalf("Specs() = %#v", specs)
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

func TestRunnerReportsUnavailableDependenciesAndToolErrors(t *testing.T) {
	if got := (*Runner)(nil).WithAuthorizer(&fakeAuthorizer{}); got != nil {
		t.Fatal("nil runner rebinding returned a runner")
	}
	if _, err := NewRunner(nil, nil, RunnerOptions{}).Run(context.Background(), "missing", json.RawMessage(`{}`)); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("nil registry error = %v", err)
	}
	registry := NewRegistry()
	model := &fakeTool{spec: Spec{Name: "errors", Schema: json.RawMessage(`{"type":"object"}`)}}
	if err := registry.Register(model); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunner(registry, nil, RunnerOptions{}).Run(context.Background(), "errors", json.RawMessage(`{}`)); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("nil authorizer error = %v", err)
	}
	if _, err := NewRunner(registry, &fakeAuthorizer{err: errors.New("broker failed")}, RunnerOptions{}).Run(context.Background(), "errors", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "decide") {
		t.Fatalf("broker error = %v", err)
	}
	model.authorizeErr = errors.New("bad authorization input")
	if _, err := NewRunner(registry, &fakeAuthorizer{}, RunnerOptions{}).Run(context.Background(), "errors", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "authorize") {
		t.Fatalf("authorize error = %v", err)
	}
	model.authorizeErr = nil
	model.runErr = errors.New("execution failed")
	if _, err := NewRunner(registry, &fakeAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}, RunnerOptions{}).Run(context.Background(), "errors", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "execution failed") {
		t.Fatalf("execution error = %v", err)
	}
}

func TestArgumentValidationCoversSupportedJSONTypes(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"required":["string"],
		"properties":{
			"string":{"type":"string"},
			"boolean":{"type":"boolean"},
			"number":{"type":"number"},
			"integer":{"type":"integer"},
			"object":{"type":"object"},
			"array":{"type":"array"},
			"future":{"type":"future"}
		},
		"additionalProperties":false
	}`)
	valid := json.RawMessage(`{"string":"value","boolean":true,"number":1.5,"integer":2,"object":{},"array":[],"future":null}`)
	if err := validateArguments(schema, valid); err != nil {
		t.Fatalf("valid arguments: %v", err)
	}
	for _, arguments := range []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(`{}`),
		json.RawMessage(`{"string":"ok","extra":true}`),
		json.RawMessage(`{"string":false}`),
		json.RawMessage(`{"string":"ok","boolean":"true"}`),
		json.RawMessage(`{"string":"ok","number":"1"}`),
		json.RawMessage(`{"string":"ok","integer":"2"}`),
		json.RawMessage(`{"string":"ok","object":[]}`),
		json.RawMessage(`{"string":"ok","array":{}}`),
	} {
		if err := validateArguments(schema, arguments); err == nil {
			t.Fatalf("invalid arguments were accepted: %s", arguments)
		}
	}
	badRequired := json.RawMessage(`{"type":"object","required":[7]}`)
	if err := validateArguments(badRequired, json.RawMessage(`{}`)); err == nil {
		t.Fatal("non-string required entry was accepted")
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

func TestRegistryCloneIsIndependent(t *testing.T) {
	original := NewRegistry()
	if err := original.Register(&fakeTool{spec: Spec{Name: "base", Schema: json.RawMessage(`{"type":"object"}`)}}); err != nil {
		t.Fatal(err)
	}
	cloned := original.Clone()
	if cloned == nil {
		t.Fatal("Clone returned nil")
	}
	if _, ok := cloned.Get("base"); !ok {
		t.Fatal("clone omitted registered tool")
	}
	if err := original.Register(&fakeTool{spec: Spec{Name: "parent_only", Schema: json.RawMessage(`{"type":"object"}`)}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := cloned.Get("parent_only"); ok {
		t.Fatal("clone aliases parent registrations")
	}
}

func TestRegistrySubsetCannotIntroduceUnknownTools(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"read_file", "shell"} {
		if err := registry.Register(&fakeTool{spec: Spec{Name: name, Schema: json.RawMessage(`{"type":"object"}`)}}); err != nil {
			t.Fatal(err)
		}
	}
	subset, err := registry.Subset([]string{"read_file"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := subset.Get("read_file"); !ok {
		t.Fatal("allowed tool missing")
	}
	if _, ok := subset.Get("shell"); ok {
		t.Fatal("unrequested tool leaked")
	}
	if _, err := registry.Subset([]string{"missing"}); err == nil {
		t.Fatal("unknown tool was accepted")
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
	authorizeErr   error
	runErr         error
	authorizeCalls int
	runCalls       int
}

func (tool *fakeTool) Spec() Spec { return tool.spec }
func (tool *fakeTool) Authorize(context.Context, json.RawMessage) (permissions.Request, error) {
	tool.authorizeCalls++
	if tool.authorizeErr != nil {
		return permissions.Request{}, tool.authorizeErr
	}
	return permissions.Request{Tool: tool.spec.Name, Action: permissions.ActionRead}, nil
}
func (tool *fakeTool) Run(context.Context, json.RawMessage) (core.ToolResult, error) {
	tool.runCalls++
	if tool.runErr != nil {
		return core.ToolResult{}, tool.runErr
	}
	if tool.result.Content != nil {
		return tool.result, nil
	}
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: "ok"}}}, nil
}

type fakeAuthorizer struct {
	calls    int
	decision permissions.Decision
	err      error
}

func (authorizer *fakeAuthorizer) Decide(context.Context, permissions.Request) (permissions.Decision, error) {
	authorizer.calls++
	return authorizer.decision, authorizer.err
}
