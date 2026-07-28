package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/platform"
	"cyber-code/internal/tool"
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

func TestShellSpecAndRedirectValidation(t *testing.T) {
	shell := NewShell(t.TempDir(), &fakeExecutor{})
	spec := shell.Spec()
	if spec.Name != "shell" || !json.Valid(spec.Schema) {
		t.Fatalf("spec = %#v", spec)
	}
	request, err := shell.Authorize(context.Background(), json.RawMessage(`{"command":"cat < 'input.txt' > /dev/null 2>> NUL"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Paths) != 1 || request.Paths[0] != "input.txt" {
		t.Fatalf("redirect paths = %#v", request.Paths)
	}
	for _, arguments := range []json.RawMessage{
		json.RawMessage(`{`),
		json.RawMessage(`{"command":""}`),
		json.RawMessage(`{"command":"echo ok","timeout_ms":-1}`),
		json.RawMessage(`{"command":"echo ok > $TARGET"}`),
		json.RawMessage(`{"command":"echo 'unterminated"}`),
	} {
		if _, err := shell.Authorize(context.Background(), arguments); err == nil {
			t.Fatalf("invalid shell input was accepted: %s", arguments)
		}
	}
}

func TestShellRunValidatesExecutorTimeoutAndOutput(t *testing.T) {
	workspace := t.TempDir()
	if _, err := NewShell(workspace, nil).Run(context.Background(), json.RawMessage(`{"command":"echo ok"}`)); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("nil executor error = %v", err)
	}
	if _, err := NewShell(workspace, &fakeExecutor{}).Run(context.Background(), json.RawMessage(`{"command":"echo ok","timeout_ms":600001}`)); err == nil || !strings.Contains(err.Error(), "10 minutes") {
		t.Fatalf("excessive timeout error = %v", err)
	}
	sentinel := errors.New("executor failed")
	if _, err := NewShell(workspace, &fakeExecutor{err: sentinel}).Run(context.Background(), json.RawMessage(`{"command":"echo ok"}`)); !errors.Is(err, sentinel) {
		t.Fatalf("executor error = %v", err)
	}
	executor := &fakeExecutor{result: platform.ExecResult{Stdout: "stdout", Stderr: "stderr"}}
	result, err := NewShell(workspace, executor).Run(context.Background(), json.RawMessage(`{"command":"echo ok","sandbox":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content[0].Text != "stdout\nstderr" || executor.request.Timeout != 2*time.Minute || !executor.request.Sandbox {
		t.Fatalf("result = %#v, request = %#v", result, executor.request)
	}
	executor.result = platform.ExecResult{Stderr: "only stderr"}
	result, err = NewShell(workspace, executor).Run(context.Background(), json.RawMessage(`{"command":"echo ok"}`))
	if err != nil || result.Content[0].Text != "only stderr" {
		t.Fatalf("stderr-only result = %#v, error = %v", result, err)
	}
}

type fakeExecutor struct {
	calls   int
	request platform.ExecRequest
	result  platform.ExecResult
	err     error
}

func (executor *fakeExecutor) Run(_ context.Context, request platform.ExecRequest) (platform.ExecResult, error) {
	executor.calls++
	executor.request = request
	return executor.result, executor.err
}
