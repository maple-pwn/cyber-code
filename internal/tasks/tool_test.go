package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

func TestTaskToolsRunForegroundAgentAndReportStatus(t *testing.T) {
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 4,
		Execute: func(_ context.Context, request AgentRequest) (any, error) {
			if request.Prompt != "inspect repository" || request.MaxTurns != 2 || request.Mode != permissions.PermissionModePlan {
				t.Fatalf("agent request = %#v", request)
			}
			return "child result", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	registry := toolpkg.NewRegistry()
	if err := RegisterTools(registry, service); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeBypass, ModeSource: permissions.SourceCliArg})
	if err != nil {
		t.Fatal(err)
	}
	runner := toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})
	result, err := runner.Run(context.Background(), "task_run", json.RawMessage(`{"prompt":"inspect repository","description":"review","max_turns":2,"permission_mode":"plan"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started taskToolResult
	if len(result.Content) != 1 || json.Unmarshal([]byte(result.Content[0].Text), &started) != nil {
		t.Fatalf("task result = %#v", result)
	}
	if started.ID == "" || started.Status != TaskStatusCompleted || started.Result != "child result" {
		t.Fatalf("task result = %#v", started)
	}
	status, err := runner.Run(context.Background(), "task_status", json.RawMessage(`{"id":"`+started.ID+`"}`))
	if err != nil || !strings.Contains(status.Content[0].Text, `"status":"completed"`) {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
}

func TestTaskToolsCancelBackgroundAgent(t *testing.T) {
	started := make(chan struct{})
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2,
		Execute: func(ctx context.Context, _ AgentRequest) (any, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	registry := toolpkg.NewRegistry()
	if err := RegisterTools(registry, service); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeBypass, ModeSource: permissions.SourceCliArg})
	if err != nil {
		t.Fatal(err)
	}
	runner := toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})
	result, err := runner.Run(context.Background(), "task_run", json.RawMessage(`{"prompt":"wait","background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var task taskToolResult
	if err := json.Unmarshal([]byte(result.Content[0].Text), &task); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background agent did not start")
	}
	if _, err := runner.Run(context.Background(), "task_cancel", json.RawMessage(`{"id":"`+task.ID+`"}`)); err != nil {
		t.Fatal(err)
	}
	status, err := runner.Run(context.Background(), "task_status", json.RawMessage(`{"id":"`+task.ID+`"}`))
	if err != nil || !strings.Contains(status.Content[0].Text, `"status":"cancelled"`) {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
}

func TestTaskRunRejectsChildEscalationBeforeExecution(t *testing.T) {
	called := false
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModePlan, ParentMaxTurns: 2,
		Execute: func(context.Context, AgentRequest) (any, error) { called = true; return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	registry := toolpkg.NewRegistry()
	if err := RegisterTools(registry, service); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeBypass, ModeSource: permissions.SourceCliArg})
	if err != nil {
		t.Fatal(err)
	}
	runner := toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})
	for _, arguments := range []string{
		`{"prompt":"x","max_turns":3}`,
		`{"prompt":"x","permission_mode":"bypass"}`,
	} {
		if _, err := runner.Run(context.Background(), "task_run", json.RawMessage(arguments)); err == nil {
			t.Fatalf("unsafe child request %s was accepted", arguments)
		}
	}
	if called {
		t.Fatal("unsafe child request reached executor")
	}
}
