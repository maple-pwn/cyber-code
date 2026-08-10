package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyber-code/internal/collaboration"
	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

func TestTaskRunPublishesSubagentObservations(t *testing.T) {
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 4,
		Execute: func(_ context.Context, request AgentRequest) (any, error) {
			for _, event := range []core.Event{
				{Type: core.EventThinkingDelta, Text: "checking"},
				{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "call-1", Name: "read_file"}},
				{Type: core.EventUsage, Usage: &core.Usage{InputTokens: 11, OutputTokens: 4}},
				{Type: core.EventCompleted, FinishReason: "stop"},
			} {
				if err := request.Emit(event); err != nil {
					return nil, err
				}
			}
			return "done", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	events := service.Observe(context.Background())

	result, err := service.run(context.Background(), json.RawMessage(`{"agent":"","prompt":"inspect","description":"review"}`))
	if err != nil || result.Status != TaskStatusCompleted {
		t.Fatalf("result=%#v err=%v", result, err)
	}

	got := receiveObservations(t, events, 7)
	wantTypes := []core.EventType{
		core.EventSubagentStarted, core.EventSubagentStatus,
		core.EventSubagentEvent, core.EventSubagentEvent, core.EventSubagentEvent, core.EventSubagentEvent,
		core.EventSubagentStatus,
	}
	for index, want := range wantTypes {
		if got[index].Type != want || got[index].Subagent == nil || got[index].Subagent.TaskID != result.ID {
			t.Fatalf("event[%d]=%#v, want %s for %s", index, got[index], want, result.ID)
		}
	}
	if got[0].Subagent.Status != string(TaskStatusPending) || got[1].Subagent.Status != string(TaskStatusRunning) || got[6].Subagent.Status != string(TaskStatusCompleted) {
		t.Fatalf("lifecycle = %q, %q, %q", got[0].Subagent.Status, got[1].Subagent.Status, got[6].Subagent.Status)
	}
	snapshots := service.Snapshots()
	if len(snapshots) != 1 || snapshots[0].RecentTool != "read_file" || snapshots[0].Usage.InputTokens != 11 || snapshots[0].Usage.OutputTokens != 4 {
		t.Fatalf("snapshots = %#v", snapshots)
	}
}

func TestTaskRunPublishesBackgroundAndFailedStatuses(t *testing.T) {
	release := make(chan struct{})
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2,
		Execute: func(_ context.Context, request AgentRequest) (any, error) {
			<-release
			if err := request.Emit(core.Event{Type: core.EventWarning, Text: "child warning"}); err != nil {
				return nil, err
			}
			return nil, errors.New("child failed")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	events := service.Observe(context.Background())

	result, err := service.run(context.Background(), json.RawMessage(`{"prompt":"inspect","background":true}`))
	if err != nil || result.Status != TaskStatusRunning {
		t.Fatalf("background result=%#v err=%v", result, err)
	}
	close(release)
	got := receiveUntilTerminalObservation(t, events)
	if got[0].Type != core.EventSubagentStarted || got[1].Type != core.EventSubagentStatus {
		t.Fatalf("initial lifecycle = %#v", got)
	}
	last := got[len(got)-1]
	if last.Type != core.EventSubagentStatus || last.Subagent.Status != string(TaskStatusFailed) {
		t.Fatalf("terminal observation = %#v", last)
	}
}

func TestTaskServiceDurableQueueDrainsAndRecoversPendingWork(t *testing.T) {
	directory := t.TempDir()
	queuePath := filepath.Join(directory, "tasks.json")
	boardPath := filepath.Join(directory, "board")
	board, err := collaboration.NewBoard(boardPath, collaboration.BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := OpenQueue(queuePath, QueueOptions{Capacity: 4, LeaseDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	service, err := NewToolService(ToolServiceOptions{
		Queue: queue, Board: board, ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2,
		Execute: func(_ context.Context, request AgentRequest) (any, error) {
			if request.Prompt == "first" {
				close(firstStarted)
				<-releaseFirst
			}
			return request.Prompt + " done", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.run(context.Background(), json.RawMessage(`{"prompt":"first","background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	<-firstStarted
	second, err := service.run(context.Background(), json.RawMessage(`{"prompt":"second","background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() { drained <- service.Drain(context.Background()) }()
	close(releaseFirst)
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	if result, err := service.result(first.ID); err != nil || result.Status != TaskStatusCompleted {
		t.Fatalf("first result=%#v err=%v", result, err)
	}
	if result, err := service.result(second.ID); err != nil || result.Status != TaskStatusPending {
		t.Fatalf("second before restart=%#v err=%v", result, err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	restartedQueue, err := OpenQueue(queuePath, QueueOptions{Capacity: 4, LeaseDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	restartedBoard, err := collaboration.NewBoard(boardPath, collaboration.BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var restartedCalls atomic.Int32
	restarted, err := NewToolService(ToolServiceOptions{
		Queue: restartedQueue, Board: restartedBoard, ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2,
		Execute: func(_ context.Context, request AgentRequest) (any, error) {
			restartedCalls.Add(1)
			return request.Prompt + " recovered", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		result, resultErr := restarted.result(second.ID)
		if resultErr == nil && result.Status == TaskStatusCompleted {
			if result.Result != "second recovered" || restartedCalls.Load() != 1 {
				t.Fatalf("recovered result=%#v calls=%d", result, restartedCalls.Load())
			}
			snapshots := restarted.Snapshots()
			if len(snapshots) != 1 || snapshots[0].QueueStatus != QueueCompleted || snapshots[0].Attempts != 1 {
				t.Fatalf("recovered snapshots=%#v", snapshots)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pending task %s was not recovered: queue=%#v", second.ID, restartedQueue.Snapshot())
}

func TestTaskServiceCancelsPendingDurableWork(t *testing.T) {
	queue := openTestQueue(t, QueueOptions{Capacity: 4})
	started := make(chan struct{})
	release := make(chan struct{})
	service, err := NewToolService(ToolServiceOptions{
		Queue: queue, ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2,
		Execute: func(_ context.Context, request AgentRequest) (any, error) {
			if request.Prompt == "active" {
				close(started)
				<-release
			}
			return "done", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.run(context.Background(), json.RawMessage(`{"prompt":"active","background":true}`)); err != nil {
		t.Fatal(err)
	}
	<-started
	pending, err := service.run(context.Background(), json.RawMessage(`{"prompt":"pending","background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.cancelTask(context.Background(), pending.ID); err != nil {
		t.Fatal(err)
	}
	result, err := service.result(pending.ID)
	if err != nil || result.Status != TaskStatusCancelled {
		t.Fatalf("cancelled result=%#v err=%v", result, err)
	}
	items := queue.Snapshot()
	if len(items) != 2 || items[1].Status != QueueCancelled {
		t.Fatalf("queue after cancellation=%#v", items)
	}
	close(release)
}

func TestTaskServiceRecoversAbandonedLeaseAndInterruptedBoardTask(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC)
	queuePath := filepath.Join(directory, "tasks.json")
	queue, err := OpenQueue(queuePath, QueueOptions{Capacity: 2, MaxAttempts: 3, LeaseDuration: time.Second, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	input := taskRunInput{Prompt: "recover interrupted", Description: "recover interrupted", MaxTurns: 2, PermissionMode: permissions.PermissionModeDefault, Background: true}
	payload, err := json.Marshal(queuedAgentTask{TaskID: "task-interrupted", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := queue.Enqueue(context.Background(), "task-interrupted", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.Claim(context.Background(), "crashed-worker"); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	boardPath := filepath.Join(directory, "board")
	board, err := collaboration.NewBoard(boardPath, collaboration.BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := board.Create(context.Background(), collaboration.Task{ID: "task-interrupted", Description: input.Description}); err != nil {
		t.Fatal(err)
	}
	if err := board.Transition(context.Background(), "task-interrupted", collaboration.TaskRunning, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	restartedQueue, err := OpenQueue(queuePath, QueueOptions{Capacity: 2, MaxAttempts: 3, LeaseDuration: time.Second, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	restartedBoard, err := collaboration.NewBoard(boardPath, collaboration.BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewToolService(ToolServiceOptions{
		Queue: restartedQueue, Board: restartedBoard, ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2,
		Execute: func(context.Context, AgentRequest) (any, error) { return "recovered", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		result, resultErr := service.result("task-interrupted")
		if resultErr == nil && result.Status == TaskStatusCompleted {
			items := restartedQueue.Snapshot()
			if len(items) != 1 || items[0].Status != QueueCompleted {
				time.Sleep(time.Millisecond)
				continue
			}
			stored, ok, boardErr := restartedBoard.Get(context.Background(), "task-interrupted")
			if boardErr != nil || !ok || stored.Status != collaboration.TaskCompleted {
				t.Fatalf("board task=%#v ok=%t err=%v", stored, ok, boardErr)
			}
			if items[0].Attempts != 2 {
				t.Fatalf("recovered queue=%#v", items)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("interrupted task was not recovered: queue=%#v", restartedQueue.Snapshot())
}

func TestTaskEmitterRejectsRecursiveSubagentObservation(t *testing.T) {
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2,
		Execute: func(_ context.Context, request AgentRequest) (any, error) {
			return nil, request.Emit(core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{TaskID: "nested"}})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.run(context.Background(), json.RawMessage(`{"prompt":"inspect"}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshots := service.Snapshots()
		if len(snapshots) == 1 && snapshots[0].Status == TaskStatusFailed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("recursive observation did not fail the task")
}

func receiveObservations(t *testing.T, events <-chan core.Event, count int) []core.Event {
	t.Helper()
	result := make([]core.Event, 0, count)
	for len(result) < count {
		select {
		case event, open := <-events:
			if !open {
				t.Fatalf("observation stream closed after %d events", len(result))
			}
			result = append(result, event)
		case <-time.After(time.Second):
			t.Fatalf("timed out after %d observations", len(result))
		}
	}
	return result
}

func receiveUntilTerminalObservation(t *testing.T, events <-chan core.Event) []core.Event {
	t.Helper()
	result := make([]core.Event, 0, 8)
	for {
		result = append(result, receiveObservations(t, events, 1)[0])
		last := result[len(result)-1]
		if last.Type == core.EventSubagentStatus && last.Subagent != nil {
			status := TaskStatus(last.Subagent.Status)
			if status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCancelled {
				return result
			}
		}
	}
}

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

func TestTaskRunAppliesNamedAgentDefinitionLimits(t *testing.T) {
	var captured AgentRequest
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModeAcceptEdits, ParentMaxTurns: 8,
		Definitions: []collaboration.Definition{{Name: "reviewer", MaxTurns: 3, PermissionMode: permissions.PermissionModePlan, Tools: []string{"read_file"}}},
		Execute:     func(_ context.Context, request AgentRequest) (any, error) { captured = request; return "ok", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	result, err := service.run(context.Background(), json.RawMessage(`{"agent":"reviewer","prompt":"review"}`))
	if err != nil || result.Status != TaskStatusCompleted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if captured.Definition == nil || captured.MaxTurns != 3 || captured.Mode != permissions.PermissionModePlan || len(captured.Definition.Tools) != 1 {
		t.Fatalf("request=%#v", captured)
	}
	if _, err := service.parseRun(json.RawMessage(`{"agent":"reviewer","prompt":"review","max_turns":4}`)); err == nil {
		t.Fatal("definition budget escalation accepted")
	}
}

func TestTaskRunPersistsLifecycleToCollaborationBoard(t *testing.T) {
	board, err := collaboration.NewBoard(t.TempDir(), collaboration.BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2, Board: board,
		Execute: func(context.Context, AgentRequest) (any, error) { return "done", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	result, err := service.run(context.Background(), json.RawMessage(`{"prompt":"work"}`))
	if err != nil {
		t.Fatal(err)
	}
	stored, ok, err := board.Get(context.Background(), result.ID)
	if err != nil || !ok || stored.Status != collaboration.TaskCompleted {
		t.Fatalf("stored=%#v ok=%v err=%v", stored, ok, err)
	}
}

func TestTaskCancelPersistsCancelledBoardState(t *testing.T) {
	board, err := collaboration.NewBoard(t.TempDir(), collaboration.BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	service, err := NewToolService(ToolServiceOptions{
		ParentMode: permissions.PermissionModeDefault, ParentMaxTurns: 2, Board: board,
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
	result, err := service.run(context.Background(), json.RawMessage(`{"prompt":"wait","background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := service.manager.KillTask(result.ID); err != nil {
		t.Fatal(err)
	}
	if err := board.Transition(context.Background(), result.ID, collaboration.TaskCancelled, "cancelled by parent"); err != nil {
		t.Fatal(err)
	}
	task, ok, err := board.Get(context.Background(), result.ID)
	if err != nil || !ok || task.Status != collaboration.TaskCancelled {
		t.Fatalf("task=%#v ok=%v err=%v", task, ok, err)
	}
}
