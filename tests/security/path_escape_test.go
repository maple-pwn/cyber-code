package security_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
	"cyber-code/internal/tool/builtin"
)

func TestTraversalAndShellRedirectionCannotEscapeWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeBypass, ModeSource: permissions.SourceCliArg})
	if err != nil {
		t.Fatal(err)
	}

	decision, err := broker.Decide(context.Background(), permissions.Request{
		Tool: "write_file", Action: permissions.ActionWrite, Workspace: workspace,
		Paths: []string{filepath.Join(workspace, "..", "outside", "secret.txt")},
	})
	if err != nil || decision.Behavior != permissions.PermissionBehaviorDeny {
		t.Fatalf("traversal decision = %#v, error = %v", decision, err)
	}

	shell := builtin.NewShell(workspace, nil)
	request, err := shell.Authorize(context.Background(), json.RawMessage(`{"command":"printf secret > ../outside/secret.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	decision, err = broker.Decide(context.Background(), request)
	if err != nil || decision.Behavior != permissions.PermissionBehaviorDeny {
		t.Fatalf("redirection decision = %#v, error = %v", decision, err)
	}
}

func TestFileToolRechecksSymlinkBoundaryAtExecution(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	safeDirectory := filepath.Join(workspace, "safe")
	if err := os.MkdirAll(safeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	fileTool := builtin.NewWriteFile(workspace)
	arguments := json.RawMessage(`{"path":"` + filepath.ToSlash(filepath.Join(safeDirectory, "secret.txt")) + `","content":"secret"}`)
	request, err := fileTool.Authorize(context.Background(), arguments)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeAcceptEdits})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := broker.Decide(context.Background(), request)
	if err != nil || decision.Behavior != permissions.PermissionBehaviorAllow {
		t.Fatalf("initial decision = %#v, error = %v", decision, err)
	}
	if err := os.Remove(safeDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, safeDirectory); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	_, err = fileTool.Run(context.Background(), arguments)
	if err == nil {
		t.Fatal("write followed a symlink introduced after authorization")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "secret.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside file exists or stat failed unexpectedly: %v", statErr)
	}
	var _ tool.Tool = fileTool
}

func TestGrepGlobToolsRecheckSymlinkBoundaryAtExecution(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	safeDirectory := filepath.Join(workspace, "safe")
	if err := os.MkdirAll(safeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("needle"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct {
		name string
		tool tool.Tool
		args json.RawMessage
	}{
		{name: "grep", tool: builtin.NewGrepFiles(workspace), args: json.RawMessage(`{"pattern":"needle","path":"safe"}`)},
		{name: "glob", tool: builtin.NewGlobFiles(workspace), args: json.RawMessage(`{"pattern":"**/*","path":"safe"}`)},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			if _, err := candidate.tool.Authorize(context.Background(), candidate.args); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(safeDirectory); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, safeDirectory); err != nil {
				if restoreErr := os.MkdirAll(safeDirectory, 0o700); restoreErr != nil {
					t.Fatal(restoreErr)
				}
				t.Skipf("symlink creation unavailable: %v", err)
			}
			if _, err := candidate.tool.Run(context.Background(), candidate.args); err == nil {
				t.Fatal("search followed a symlink introduced after authorization")
			}
			if err := os.Remove(safeDirectory); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(safeDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
		})
	}
}
