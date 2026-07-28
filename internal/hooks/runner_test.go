package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-code-go/internal/platform"
)

func TestHookRunnerPreservesOrderMergesResultsAndStopsOnDenial(t *testing.T) {
	registry := NewRegistry()
	var order []string
	registry.Register(HookEventPreToolUse, func(context.Context, HookInput) (HookOutput, error) {
		order = append(order, "first")
		return HookOutput{Continue: true, UpdatedInput: map[string]any{"path": "updated.txt"}, AdditionalContext: "first context"}, nil
	})
	registry.Register(HookEventPreToolUse, func(context.Context, HookInput) (HookOutput, error) {
		order = append(order, "second")
		return HookOutput{Continue: false, Decision: "block", Reason: "policy rejected it"}, nil
	})
	registry.Register(HookEventPreToolUse, func(context.Context, HookInput) (HookOutput, error) {
		order = append(order, "third")
		return HookOutput{Continue: true}, nil
	})
	runner, err := NewRunner(RunnerOptions{Registry: registry, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runner.Run(context.Background(), HookInput{
		EventName: HookEventPreToolUse, ToolName: "read_file", ToolInput: map[string]any{"path": "original.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "first,second" || !outcome.Denied || outcome.Reason != "policy rejected it" {
		t.Fatalf("order=%#v outcome=%#v", order, outcome)
	}
	if outcome.UpdatedInput["path"] != "updated.txt" || len(outcome.AdditionalContext) != 1 {
		t.Fatalf("merged outcome = %#v", outcome)
	}
}

func TestCommandHookUsesJSONInputAndTimeoutCancellation(t *testing.T) {
	executor := &blockingHookExecutor{entered: make(chan platform.ExecRequest, 1)}
	runner, err := NewRunner(RunnerOptions{
		Registry: NewRegistry(), Executor: executor, Workspace: t.TempDir(), Timeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.RegisterCommand(HookEventUserPromptSubmit, "hook-command"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = runner.Run(context.Background(), HookInput{EventName: HookEventUserPromptSubmit, Prompt: "hello"})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("Run error=%v duration=%v", err, time.Since(started))
	}
	request := <-executor.entered
	if request.Command != "hook-command" || request.Workspace == "" || request.Stdin == "" || !json.Valid([]byte(request.Stdin)) {
		t.Fatalf("exec request = %#v", request)
	}
	if !executor.canceled() {
		t.Fatal("hook executor did not observe cancellation")
	}
}

func TestCommandHookRejectsOversizedOutput(t *testing.T) {
	executor := &staticHookExecutor{result: platform.ExecResult{Stdout: `{"continue":true,"additionalContext":"` + strings.Repeat("x", 100) + `"}`}}
	runner, err := NewRunner(RunnerOptions{
		Registry: NewRegistry(), Executor: executor, Workspace: t.TempDir(), MaxOutputBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.RegisterCommand(HookEventPostToolUse, "hook-command"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), HookInput{EventName: HookEventPostToolUse}); err == nil || !strings.Contains(err.Error(), "output") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestCommandHookRejectsTrailingJSONValue(t *testing.T) {
	executor := &staticHookExecutor{result: platform.ExecResult{Stdout: `{"continue":true} {"continue":true}`}}
	runner, err := NewRunner(RunnerOptions{Registry: NewRegistry(), Executor: executor, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.RegisterCommand(HookEventPostToolUse, "hook-command"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), HookInput{EventName: HookEventPostToolUse}); err == nil {
		t.Fatal("trailing JSON value was accepted")
	}
}

func TestHookRunnerLimitsAggregateAdditionalContext(t *testing.T) {
	registry := NewRegistry()
	for range 2 {
		registry.Register(HookEventPostToolUse, func(context.Context, HookInput) (HookOutput, error) {
			return HookOutput{Continue: true, AdditionalContext: strings.Repeat("x", 20)}, nil
		})
	}
	runner, err := NewRunner(RunnerOptions{Registry: registry, Workspace: t.TempDir(), MaxOutputBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), HookInput{EventName: HookEventPostToolUse}); err == nil {
		t.Fatal("aggregate hook context limit was not enforced")
	}
}

func TestHookRunnerTreatsContinueFalseAsDenial(t *testing.T) {
	registry := NewRegistry()
	registry.Register(HookEventPreToolUse, func(context.Context, HookInput) (HookOutput, error) {
		return HookOutput{Continue: false, StopReason: "stop requested"}, nil
	})
	runner, err := NewRunner(RunnerOptions{Registry: registry, Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := runner.Run(context.Background(), HookInput{EventName: HookEventPreToolUse})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Denied || outcome.Reason != "stop requested" {
		t.Fatalf("outcome = %#v", outcome)
	}
}

type blockingHookExecutor struct {
	entered     chan platform.ExecRequest
	mu          sync.Mutex
	wasCanceled bool
}

func (executor *blockingHookExecutor) Run(ctx context.Context, request platform.ExecRequest) (platform.ExecResult, error) {
	executor.entered <- request
	<-ctx.Done()
	executor.mu.Lock()
	executor.wasCanceled = true
	executor.mu.Unlock()
	return platform.ExecResult{}, ctx.Err()
}

func (executor *blockingHookExecutor) canceled() bool {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return executor.wasCanceled
}

type staticHookExecutor struct{ result platform.ExecResult }

func (executor *staticHookExecutor) Run(context.Context, platform.ExecRequest) (platform.ExecResult, error) {
	return executor.result, nil
}
