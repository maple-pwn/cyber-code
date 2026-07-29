// Package gitworkflow provides bounded, permission-neutral Git workflows.
// Callers must wrap the executor with their authorization policy.
package gitworkflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cyber-code/internal/platform"
)

const (
	maxOutputBytes        = 128 << 10
	maxCommitMessageBytes = 16 << 10
	gitTimeout            = 30 * time.Second
)

type Service struct {
	workspace string
	executor  platform.Executor
}

func NewService(workspace string, executor platform.Executor) (*Service, error) {
	if executor == nil {
		return nil, fmt.Errorf("Git executor is required")
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve Git workspace: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve Git workspace links: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("inspect Git workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Git workspace is not a directory")
	}
	return &Service{workspace: resolved, executor: executor}, nil
}

func (service *Service) Diff(ctx context.Context) (string, error) {
	return service.run(ctx, "git --no-pager diff --no-ext-diff --", "")
}

func (service *Service) Review(ctx context.Context) (string, error) {
	return service.run(ctx, "git status --short --branch && git --no-pager diff --stat -- && git --no-pager diff --cached --stat --", "")
}

func (service *Service) Commit(ctx context.Context, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("commit message is required")
	}
	if len(message) > maxCommitMessageBytes {
		return "", fmt.Errorf("commit message exceeds %d bytes", maxCommitMessageBytes)
	}
	return service.run(ctx, "git commit --file=-", message+"\n")
}

func (service *Service) run(ctx context.Context, command, stdin string) (string, error) {
	result, err := service.executor.Run(ctx, platform.ExecRequest{
		Command: command, Workspace: service.workspace, Stdin: stdin, Timeout: gitTimeout,
	})
	output := strings.TrimSpace(strings.TrimSpace(result.Stdout) + "\n" + strings.TrimSpace(result.Stderr))
	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes] + "\n... output truncated"
	}
	if err != nil {
		return "", fmt.Errorf("Git workflow failed: %w: %s", err, output)
	}
	if output == "" {
		return "No changes.", nil
	}
	return output, nil
}
