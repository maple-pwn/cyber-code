package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"claude-code-go/internal/platform"
)

func TestExecutorCancellationWinsOverHandlerFailure(t *testing.T) {
	registry := NewRegistry()
	task := CreateLocalShellTask("shell-1", "long-running", t.TempDir(), "test")
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry)
	started := make(chan struct{})
	if err := executor.ExecuteLocalShell(context.Background(), task, func(ctx context.Context, _ *LocalShellTaskState) (*int, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := executor.KillTask("shell-1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if registry.Get("shell-1").GetBase().Status == TaskStatusCancelled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task status = %s", registry.Get("shell-1").GetBase().Status)
}

func TestExecutorRejectsUnregisteredAndAlreadyRunningTask(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)
	task := CreateLocalShellTask("shell-1", "echo", t.TempDir(), "test")
	handler := func(context.Context, *LocalShellTaskState) (*int, error) { return nil, errors.New("unused") }
	if err := executor.ExecuteLocalShell(context.Background(), task, handler); err == nil {
		t.Fatal("unregistered task was executed")
	}
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	if err := registry.Transition(task.ID, TaskStatusRunning, nil); err != nil {
		t.Fatal(err)
	}
	if err := executor.ExecuteLocalShell(context.Background(), task, handler); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("already running error = %v", err)
	}
}

func TestExecutorRejectsNilHandlerWithoutStartingTask(t *testing.T) {
	registry := NewRegistry()
	task := CreateLocalShellTask("shell-1", "echo", t.TempDir(), "test")
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry)
	if err := executor.ExecuteLocalShell(context.Background(), task, nil); err == nil {
		t.Fatal("nil handler was accepted")
	}
	if status := registry.Get(task.ID).GetBase().Status; status != TaskStatusPending {
		t.Fatalf("status = %s", status)
	}
}

func TestExecutorCloseCancelsAndWaitsForBackgroundTasks(t *testing.T) {
	registry := NewRegistry()
	task := CreateLocalShellTask("shell-1", "long", t.TempDir(), "test")
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry)
	exited := make(chan struct{})
	if err := executor.ExecuteLocalShell(context.Background(), task, func(ctx context.Context, _ *LocalShellTaskState) (*int, error) {
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if err := executor.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("Close returned before task exited")
	}
	if registry.Get(task.ID).GetBase().Status != TaskStatusCancelled {
		t.Fatalf("status = %s", registry.Get(task.ID).GetBase().Status)
	}
}

func TestExecutorRejectsExecutionAfterCloseWithoutChangingTask(t *testing.T) {
	registry := NewRegistry()
	task := CreateLocalShellTask("shell-1", "echo", t.TempDir(), "test")
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry)
	if err := executor.Close(); err != nil {
		t.Fatal(err)
	}
	err := executor.ExecuteLocalShell(context.Background(), task, func(context.Context, *LocalShellTaskState) (*int, error) {
		t.Fatal("handler ran after Close")
		return nil, nil
	})
	if !errors.Is(err, ErrExecutorClosed) {
		t.Fatalf("error = %v", err)
	}
	if status := registry.Get(task.ID).GetBase().Status; status != TaskStatusPending {
		t.Fatalf("status = %s", status)
	}
}

func TestExecutorManagedShellStoresBoundedOutput(t *testing.T) {
	registry := NewRegistry()
	task := CreateLocalShellTask("shell-1", "command", t.TempDir(), "test")
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry)
	runner := platformExecutorFunc(func(_ context.Context, request platform.ExecRequest) (platform.ExecResult, error) {
		if request.Command != task.Command || request.Workspace != task.Directory || !request.Sandbox {
			t.Fatalf("request = %#v", request)
		}
		return platform.ExecResult{Stdout: "abcdef", Stderr: "gh", ExitCode: 7}, nil
	})
	if err := executor.ExecuteManagedShell(context.Background(), task, runner, ManagedShellOptions{Sandbox: true, MaxOutputBytes: 5}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for registry.Get(task.ID).GetBase().Status != TaskStatusCompleted && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := registry.Get(task.ID).(*LocalShellTaskState)
	if got.ExitCode == nil || *got.ExitCode != 7 {
		t.Fatalf("task = %#v", got)
	}
	output, err := registry.ReadOutput(task.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(output.Data) != "ef\ngh" || !output.Truncated {
		t.Fatalf("output = %#v", output)
	}
}

func TestExecutorManagedShellPreservesFailedExitCodeAndOutput(t *testing.T) {
	registry := NewRegistry()
	task := CreateLocalShellTask("shell-1", "bad-command", t.TempDir(), "test")
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry)
	runner := platformExecutorFunc(func(context.Context, platform.ExecRequest) (platform.ExecResult, error) {
		return platform.ExecResult{Stderr: "failure", ExitCode: 9}, errors.New("process exited")
	})
	if err := executor.ExecuteManagedShell(context.Background(), task, runner, ManagedShellOptions{MaxOutputBytes: 100}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for registry.Get(task.ID).GetBase().Status == TaskStatusRunning && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := registry.Get(task.ID).(*LocalShellTaskState)
	if got.Status != TaskStatusFailed || got.ExitCode == nil || *got.ExitCode != 9 {
		t.Fatalf("task = %#v", got)
	}
	output, err := registry.ReadOutput(task.ID, 0, 100)
	if err != nil || string(output.Data) != "failure" {
		t.Fatalf("output = %#v, error = %v", output, err)
	}
}

type platformExecutorFunc func(context.Context, platform.ExecRequest) (platform.ExecResult, error)

func (run platformExecutorFunc) Run(ctx context.Context, request platform.ExecRequest) (platform.ExecResult, error) {
	return run(ctx, request)
}

func TestManagerWorksWithoutLegacyAppStateStore(t *testing.T) {
	manager := NewManager(nil)
	task, err := manager.SpawnLocalShell(context.Background(), "echo", t.TempDir(), "test", true)
	if err != nil {
		t.Fatal(err)
	}
	if manager.GetTask(task.ID) == nil {
		t.Fatal("spawned task is missing")
	}
}

func TestManagerRetrievePersistsRetrievedFlag(t *testing.T) {
	manager := NewManager(nil)
	task, err := manager.SpawnLocalAgent(context.Background(), "prompt", "child", "test", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartExecution(context.Background(), task.ID, func(context.Context, *LocalAgentTaskState) (interface{}, error) {
		return "done", nil
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for manager.GetTask(task.ID).GetBase().Status != TaskStatusCompleted && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	result, err := manager.RetrieveTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result != "done" || !manager.GetTask(task.ID).(*LocalAgentTaskState).Retrieved {
		t.Fatalf("result = %#v, task = %#v", result, manager.GetTask(task.ID))
	}
}
