package cli

import (
	"context"
	"path/filepath"
	"testing"

	"cyber-code/internal/permissions"
	"cyber-code/internal/platform"
)

func TestAuthorizedExecutorRejectsRedirectOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	delegate := &recordingExecutor{}
	broker, err := permissions.NewBroker(permissions.Options{
		Mode: permissions.PermissionModeBypass, ModeSource: permissions.SourceCliArg,
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &authorizedExecutor{tool: "hook", broker: broker, delegate: delegate}
	_, err = executor.Run(context.Background(), platform.ExecRequest{
		Workspace: workspace,
		Command:   "echo escaped > " + filepath.Join("..", "outside.txt"),
	})
	if err == nil || delegate.called {
		t.Fatalf("outside redirect error=%v delegate_called=%t", err, delegate.called)
	}
}

type recordingExecutor struct{ called bool }

func (executor *recordingExecutor) Run(context.Context, platform.ExecRequest) (platform.ExecResult, error) {
	executor.called = true
	return platform.ExecResult{}, nil
}
