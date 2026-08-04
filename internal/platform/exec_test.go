package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProcessEnvironmentFiltersSecrets(t *testing.T) {
	environment := FilterEnvironment([]string{
		"PATH=/bin", "HOME=/home/test", "ANTHROPIC_API_KEY=secret", "ACCESS_TOKEN=secret", "UNRELATED=value",
	}, map[string]string{"LANG": "C", "AWS_SECRET_ACCESS_KEY": "secret"})
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "HOME=/home/test") || !strings.Contains(joined, "LANG=C") {
		t.Fatalf("allowed environment missing: %q", joined)
	}
	for _, forbidden := range []string{"ANTHROPIC", "TOKEN", "AWS_SECRET", "UNRELATED", "secret"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("environment leaked %q: %q", forbidden, joined)
		}
	}
}

func TestProcessUsesFixedWorkspaceAndReportsPlatformSandbox(t *testing.T) {
	workspace := t.TempDir()
	runner := NewRunner(Options{LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	command := "pwd"
	if runtime.GOOS == "windows" {
		command = "cd"
	}
	result, err := runner.Run(context.Background(), ExecRequest{Command: command, Workspace: workspace, Sandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(workspace)
	if !strings.EqualFold(filepath.Clean(strings.TrimSpace(result.Stdout)), filepath.Clean(resolved)) {
		t.Fatalf("stdout = %q, want workspace %q", result.Stdout, resolved)
	}
	if result.Isolation != expectedBestEffortIsolation() {
		t.Fatalf("isolation = %q", result.Isolation)
	}
}

func TestProcessTimeoutReturnsContextError(t *testing.T) {
	runner := NewRunner(Options{})
	started := time.Now()
	_, err := runner.Run(context.Background(), ExecRequest{Command: "sleep 30", Workspace: t.TempDir(), Timeout: 20 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("timed out process was not terminated promptly")
	}
}

func TestProcessRejectsInvalidWorkspace(t *testing.T) {
	runner := NewRunner(Options{})
	_, err := runner.Run(context.Background(), ExecRequest{Command: "pwd", Workspace: filepath.Join(t.TempDir(), "missing")})
	if err == nil || !os.IsNotExist(errors.Unwrap(err)) {
		t.Fatalf("error = %v", err)
	}
}

func TestRequiredSandboxFailsClosedWhenStrongIsolationUnavailable(t *testing.T) {
	runner := NewRunner(Options{SandboxMode: SandboxRequired, LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	_, err := runner.Run(context.Background(), ExecRequest{Command: "pwd", Workspace: t.TempDir(), Sandbox: true})
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestSandboxOffDoesNotClaimPolicyIsolation(t *testing.T) {
	runner := NewRunner(Options{SandboxMode: SandboxOff})
	result, err := runner.Run(context.Background(), ExecRequest{Command: "pwd", Workspace: t.TempDir(), Sandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Isolation == IsolationBubblewrap || result.Isolation == IsolationPolicyOnly {
		t.Fatalf("isolation = %q", result.Isolation)
	}
}

func TestManagedProcessRequiredSandboxFailsClosed(t *testing.T) {
	runner := NewRunner(Options{SandboxMode: SandboxRequired, LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	_, err := runner.Start(context.Background(), ProcessRequest{Command: "sh", Args: []string{"-c", "true"}, Workspace: t.TempDir()})
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("error = %v", err)
	}
}
