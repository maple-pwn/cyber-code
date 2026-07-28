package tasks

import (
	"errors"
	"math"
	"testing"
)

func TestRegistryStoresSnapshotsAndDoesNotExposeMutableTasks(t *testing.T) {
	registry := NewRegistry()
	task := CreateLocalShellTask("task-1", "echo hi", t.TempDir(), "test")
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	task.Command = "changed"
	task.Status = TaskStatusFailed
	got := registry.Get("task-1").(*LocalShellTaskState)
	if got.Command != "echo hi" || got.Status != TaskStatusPending {
		t.Fatalf("stored task aliases caller: %#v", got)
	}
	got.Command = "changed again"
	if registry.Get("task-1").(*LocalShellTaskState).Command != "echo hi" {
		t.Fatal("Get returned mutable registry state")
	}
}

func TestRegistryDeepCopiesNestedAgentState(t *testing.T) {
	registry := NewRegistry()
	result := map[string]interface{}{"items": []string{"before"}}
	message := map[string]interface{}{"text": []byte("before")}
	input := map[string]interface{}{"path": []string{"before"}}
	task := CreateLocalAgentTask("task-1", "prompt", "child", "test")
	task.Result = result
	task.Messages = []interface{}{message}
	task.Progress = &AgentProgress{LastActivity: &ToolActivity{ToolName: "read", Input: input}}
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	result["items"].([]string)[0] = "changed"
	message["text"].([]byte)[0] = 'X'
	input["path"].([]string)[0] = "changed"

	got := registry.Get(task.ID).(*LocalAgentTaskState)
	if got.Result.(map[string]interface{})["items"].([]string)[0] != "before" ||
		string(got.Messages[0].(map[string]interface{})["text"].([]byte)) != "before" ||
		got.Progress.LastActivity.Input["path"].([]string)[0] != "before" {
		t.Fatalf("stored task aliases caller: %#v", got)
	}
	got.Result.(map[string]interface{})["items"].([]string)[0] = "changed again"
	got.Progress.LastActivity.Input["path"].([]string)[0] = "changed again"
	again := registry.Get(task.ID).(*LocalAgentTaskState)
	if again.Result.(map[string]interface{})["items"].([]string)[0] != "before" ||
		again.Progress.LastActivity.Input["path"].([]string)[0] != "before" {
		t.Fatal("Get exposed nested mutable task state")
	}
}

func TestRegistryEnforcesTaskStateMachine(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(CreateLocalShellTask("task-1", "echo hi", t.TempDir(), "test")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Transition("task-1", TaskStatusCompleted, nil); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("pending -> completed error = %v", err)
	}
	if err := registry.Transition("task-1", TaskStatusRunning, nil); err != nil {
		t.Fatal(err)
	}
	if err := registry.Transition("task-1", TaskStatusCancelled, errors.New("user canceled")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Transition("task-1", TaskStatusRunning, nil); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("cancelled -> running error = %v", err)
	}
	got := registry.Get("task-1").(*LocalShellTaskState)
	if got.Status != TaskStatusCancelled || got.EndTime == nil || got.Error != "user canceled" {
		t.Fatalf("task = %#v", got)
	}
}

func TestRegistryUpdateCannotChangeTaskIdentity(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(CreateLocalShellTask("task-1", "echo", t.TempDir(), "test")); err != nil {
		t.Fatal(err)
	}
	err := registry.Update("task-1", func(state TaskState) TaskState {
		state.GetBase().ID = "task-2"
		return state
	})
	if !errors.Is(err, ErrInvalidTaskUpdate) {
		t.Fatalf("identity update error = %v", err)
	}
	if registry.Get("task-1").GetBase().ID != "task-1" || registry.Get("task-2") != nil {
		t.Fatal("invalid identity update changed registry state")
	}
}

func TestRegistryRejectsUnsupportedTaskState(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(&unsupportedTaskState{base: TaskStateBase{ID: "custom", Status: TaskStatusPending}})
	if err == nil || registry.Get("custom") != nil {
		t.Fatalf("register error = %v, task = %#v", err, registry.Get("custom"))
	}
}

type unsupportedTaskState struct{ base TaskStateBase }

func (task *unsupportedTaskState) GetBase() *TaskStateBase { return &task.base }

func TestRegistryBoundsAndPagesTaskOutput(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(CreateLocalShellTask("task-1", "echo", t.TempDir(), "test")); err != nil {
		t.Fatal(err)
	}
	if err := registry.AppendOutput("task-1", []byte("abcdef"), 4); err != nil {
		t.Fatal(err)
	}
	first, err := registry.ReadOutput("task-1", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Data) != "cd" || first.StartOffset != 2 || first.NextOffset != 4 || first.Total != 6 || !first.Truncated {
		t.Fatalf("first page = %#v", first)
	}
	second, err := registry.ReadOutput("task-1", first.NextOffset, 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(second.Data) != "ef" || second.NextOffset != 6 {
		t.Fatalf("second page = %#v", second)
	}
	first.Data[0] = 'X'
	again, _ := registry.ReadOutput("task-1", 0, 2)
	if string(again.Data) != "cd" {
		t.Fatal("output page aliases registry storage")
	}
}

func TestRegistryOutputOffsetsRemainStableAcrossTruncation(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(CreateLocalShellTask("task-1", "echo", t.TempDir(), "test")); err != nil {
		t.Fatal(err)
	}
	if err := registry.AppendOutput("task-1", []byte("abcd"), 4); err != nil {
		t.Fatal(err)
	}
	first, err := registry.ReadOutput("task-1", 0, 4)
	if err != nil || string(first.Data) != "abcd" || first.NextOffset != 4 {
		t.Fatalf("first page = %#v, error = %v", first, err)
	}
	if err := registry.AppendOutput("task-1", []byte("ef"), 4); err != nil {
		t.Fatal(err)
	}
	second, err := registry.ReadOutput("task-1", first.NextOffset, 4)
	if err != nil || string(second.Data) != "ef" || second.StartOffset != 2 || second.NextOffset != 6 || second.Total != 6 {
		t.Fatalf("second page = %#v, error = %v", second, err)
	}
}

func TestRegistryOutputPageLimitCannotOverflow(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(CreateLocalShellTask("task-1", "echo", t.TempDir(), "test")); err != nil {
		t.Fatal(err)
	}
	if err := registry.AppendOutput("task-1", []byte("abc"), 3); err != nil {
		t.Fatal(err)
	}
	page, err := registry.ReadOutput("task-1", 1, math.MaxInt)
	if err != nil || string(page.Data) != "bc" || page.NextOffset != 3 {
		t.Fatalf("page = %#v, error = %v", page, err)
	}
}
