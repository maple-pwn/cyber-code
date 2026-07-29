package gitworkflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyber-code/internal/platform"
)

func TestServiceDiffReviewAndCommitOnlyStagedChanges(t *testing.T) {
	workspace := t.TempDir()
	runner := platform.NewRunner(platform.Options{SandboxMode: platform.SandboxOff})
	runGit(t, runner, workspace, "git init")
	runGit(t, runner, workspace, "git config user.email test@example.invalid")
	runGit(t, runner, workspace, "git config user.name cyber-code-test")
	writeGitFile(t, workspace, "tracked.txt", "initial\n")
	runGit(t, runner, workspace, "git add tracked.txt")
	runGit(t, runner, workspace, "git commit -m initial")

	writeGitFile(t, workspace, "tracked.txt", "unstaged\n")
	writeGitFile(t, workspace, "staged.txt", "staged\n")
	runGit(t, runner, workspace, "git add staged.txt")

	service, err := NewService(workspace, runner)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := service.Diff(context.Background())
	if err != nil || !strings.Contains(diff, "-initial") || !strings.Contains(diff, "+unstaged") {
		t.Fatalf("diff = %q, error = %v", diff, err)
	}
	review, err := service.Review(context.Background())
	if err != nil || !strings.Contains(review, "staged.txt") || !strings.Contains(review, "tracked.txt") {
		t.Fatalf("review = %q, error = %v", review, err)
	}
	committed, err := service.Commit(context.Background(), "test staged only")
	if err != nil || !strings.Contains(committed, "test staged only") {
		t.Fatalf("commit = %q, error = %v", committed, err)
	}
	show := runGit(t, runner, workspace, "git show --name-only --pretty=format: HEAD")
	if !strings.Contains(show, "staged.txt") || strings.Contains(show, "tracked.txt") {
		t.Fatalf("commit included unexpected files: %q", show)
	}
	status := runGit(t, runner, workspace, "git status --short")
	if !strings.Contains(status, " M tracked.txt") {
		t.Fatalf("unstaged change was not preserved: %q", status)
	}
}

func TestServiceRejectsEmptyOrOversizedCommitMessages(t *testing.T) {
	service, err := NewService(t.TempDir(), platform.NewRunner(platform.Options{SandboxMode: platform.SandboxOff}))
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"", strings.Repeat("x", maxCommitMessageBytes+1)} {
		if _, err := service.Commit(context.Background(), message); err == nil {
			t.Fatalf("commit message of length %d was accepted", len(message))
		}
	}
}

func runGit(t *testing.T, runner platform.Executor, workspace, command string) string {
	t.Helper()
	result, err := runner.Run(context.Background(), platform.ExecRequest{Command: command, Workspace: workspace})
	if err != nil {
		t.Fatalf("%s: %v\n%s", command, err, result.Stderr)
	}
	return result.Stdout
}

func writeGitFile(t *testing.T, workspace, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
