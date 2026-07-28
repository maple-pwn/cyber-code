package lsp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

func TestRegisterToolsAddsReadOnlyLSPQueries(t *testing.T) {
	service := &lspToolService{}
	registry := toolpkg.NewRegistry()
	if err := RegisterTools(registry, service); err != nil {
		t.Fatal(err)
	}
	specs := registry.Specs()
	want := []string{"lsp_completion", "lsp_definition", "lsp_diagnostics", "lsp_hover", "lsp_references"}
	if len(specs) != len(want) {
		t.Fatalf("specs = %#v", specs)
	}
	for index, spec := range specs {
		if spec.Name != want[index] || !spec.ReadOnly || !spec.ConcurrencySafe {
			t.Fatalf("spec[%d] = %#v", index, spec)
		}
	}
}

func TestLSPToolAuthorizesWorkspaceReadAndReturnsJSON(t *testing.T) {
	service := &lspToolService{}
	registry := toolpkg.NewRegistry()
	if err := RegisterTools(registry, service); err != nil {
		t.Fatal(err)
	}
	model, ok := registry.Get("lsp_definition")
	if !ok {
		t.Fatal("definition tool not registered")
	}
	arguments := json.RawMessage(`{"workspace":"/workspace","language":"go","file":"/workspace/main.go","line":2,"character":3}`)
	request, err := model.Authorize(context.Background(), arguments)
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != permissions.ActionRead || request.Workspace != "/workspace" || len(request.Paths) != 1 || request.Paths[0] != "/workspace/main.go" {
		t.Fatalf("permission request = %#v", request)
	}
	result, err := model.Run(context.Background(), arguments)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "definition.go") {
		t.Fatalf("result = %#v", result)
	}
	if service.definition.Position != (Position{Line: 2, Character: 3}) {
		t.Fatalf("query = %#v", service.definition)
	}
}

func TestLSPToolsInvokeEveryServiceOperation(t *testing.T) {
	service := &lspToolService{}
	registry := toolpkg.NewRegistry()
	if err := RegisterTools(registry, service); err != nil {
		t.Fatal(err)
	}
	runner := toolpkg.NewRunner(registry, allowLSPTools{}, toolpkg.RunnerOptions{})
	for _, invocation := range []struct {
		name string
		args string
	}{
		{"lsp_diagnostics", `{"workspace":"/workspace","language":"go","file":"/workspace/main.go"}`},
		{"lsp_definition", `{"workspace":"/workspace","language":"go","file":"/workspace/main.go","line":0,"character":0}`},
		{"lsp_references", `{"workspace":"/workspace","language":"go","file":"/workspace/main.go","line":0,"character":0,"include_declaration":true}`},
		{"lsp_completion", `{"workspace":"/workspace","language":"go","file":"/workspace/main.go","line":0,"character":0}`},
		{"lsp_hover", `{"workspace":"/workspace","language":"go","file":"/workspace/main.go","line":0,"character":0}`},
	} {
		if _, err := runner.Run(context.Background(), invocation.name, json.RawMessage(invocation.args)); err != nil {
			t.Fatalf("%s: %v", invocation.name, err)
		}
	}
	if service.calls != 5 || !service.includeDeclaration {
		t.Fatalf("calls = %d, include declaration = %v", service.calls, service.includeDeclaration)
	}
}

type lspToolService struct {
	calls              int
	definition         Query
	includeDeclaration bool
}

func (service *lspToolService) Definition(_ context.Context, query Query) ([]Location, error) {
	service.calls++
	service.definition = query
	return []Location{{URI: "file:///definition.go"}}, nil
}
func (service *lspToolService) References(_ context.Context, _ Query, includeDeclaration bool) ([]Location, error) {
	service.calls++
	service.includeDeclaration = includeDeclaration
	return []Location{{URI: "file:///reference.go"}}, nil
}
func (service *lspToolService) Completion(context.Context, Query) (CompletionList, error) {
	service.calls++
	return CompletionList{Items: []CompletionItem{{Label: "Println"}}}, nil
}
func (service *lspToolService) Hover(context.Context, Query) (*Hover, error) {
	service.calls++
	return &Hover{Contents: "hover"}, nil
}
func (service *lspToolService) Diagnostics(context.Context, Query) ([]Diagnostic, error) {
	service.calls++
	return []Diagnostic{{Message: "diagnostic"}}, nil
}

type allowLSPTools struct{}

func (allowLSPTools) Decide(context.Context, permissions.Request) (permissions.Decision, error) {
	return permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}, nil
}
