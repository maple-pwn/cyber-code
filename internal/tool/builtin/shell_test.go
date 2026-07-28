package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"claude-code-go/internal/platform"
	"claude-code-go/internal/tool"
)

func TestShellAuthorizationIncludesParsedCommand(t *testing.T) {
	shell := NewShell(t.TempDir(), &fakeExecutor{})
	request, err := shell.Authorize(context.Background(), json.RawMessage(`{"command":"printf hello > ../outside.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Tool != "shell" || request.Action != "execute" || request.Command != "printf hello > ../outside.txt" || request.Workspace == "" {
		t.Fatalf("request = %#v", request)
	}
	if len(request.Paths) != 1 || request.Paths[0] != "../outside.txt" {
		t.Fatalf("redirect paths = %#v", request.Paths)
	}
}

func TestShellUsesPlatformExecutor(t *testing.T) {
	executor := &fakeExecutor{result: platform.ExecResult{Stdout: "hello", Isolation: platform.IsolationPolicyOnly}}
	shell := NewShell(t.TempDir(), executor)
	result, err := shell.Run(context.Background(), json.RawMessage(`{"command":"printf hello","timeout_ms":100}`))
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || executor.request.Command != "printf hello" || executor.request.Workspace == "" {
		t.Fatalf("executor request = %#v", executor.request)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Fatalf("result = %#v", result)
	}
	var _ tool.Tool = shell
}

type fakeExecutor struct {
	calls   int
	request platform.ExecRequest
	result  platform.ExecResult
}

func (executor *fakeExecutor) Run(_ context.Context, request platform.ExecRequest) (platform.ExecResult, error) {
	executor.calls++
	executor.request = request
	return executor.result, nil
}
