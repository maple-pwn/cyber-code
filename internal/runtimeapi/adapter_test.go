package runtimeapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cyber-code/internal/core"
)

func TestAdapterMapsCoreTaskToolAndTerminalLifecycle(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("runtime-1", "task-1")
	tests := []struct {
		name string
		in   core.Event
		want string
	}{
		{"tool starts", core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "call-1", Name: "shell", Arguments: json.RawMessage(`{"command":"pwd"}`)}}, "tool.started"},
		{"terminal fails", core.Event{Type: core.EventToolResult, ToolCallID: "call-2", ToolResult: &core.ToolResult{ToolCallID: "call-2", IsError: true}}, "tool.failed"},
		{"task completes", core.Event{Type: core.EventCompleted}, "task.completed"},
		{"task fails", core.Event{Type: core.EventError, Err: &core.Error{Message: "provider failed"}}, "task.failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			drafts := adapter.Adapt(test.in)
			if len(drafts) != 1 || drafts[0].Type != test.want {
				t.Fatalf("Adapt(%s) = %+v, want one %s event", test.in.Type, drafts, test.want)
			}
		})
	}
	terminal := adapter.Adapt(core.Event{Type: core.EventToolResult, ToolCallID: "call-1", ToolResult: &core.ToolResult{
		ToolCallID: "call-1", Content: []core.ContentBlock{{Type: core.ContentText, Text: "pwd\n/workspace"}},
	}})
	if len(terminal) != 2 || terminal[0].Type != "evidence.committed" || terminal[1].Type != "tool.completed" {
		t.Fatalf("shell terminal result = %+v", terminal)
	}
	completedPayload := terminal[1].Payload.(map[string]any)
	if evidenceIDs, ok := completedPayload["evidenceIds"].([]string); !ok || len(evidenceIDs) != 1 {
		t.Fatalf("terminal evidence IDs = %#v", completedPayload["evidenceIds"])
	}
	adapter.Adapt(core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "call-3", Name: "shell", Arguments: json.RawMessage(`{"command":"go test"}`)}})
	failedTerminal := adapter.Adapt(core.Event{Type: core.EventToolResult, ToolCallID: "call-3", ToolResult: &core.ToolResult{
		ToolCallID: "call-3", IsError: true, Content: []core.ContentBlock{{Type: core.ContentText, Text: "compile error: undefined symbol"}},
	}})
	if len(failedTerminal) != 2 || failedTerminal[0].Type != "evidence.committed" || failedTerminal[1].Type != "tool.failed" {
		t.Fatalf("failed shell terminal result = %+v", failedTerminal)
	}
	if reason := failedTerminal[1].Payload.(map[string]any)["reason"]; reason != "compile error: undefined symbol" {
		t.Fatalf("failed terminal reason = %#v", reason)
	}
}

func TestAdapterMapsSubagentLifecycleAndIgnoresTextDeltas(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("runtime-1", "task-1")
	started := adapter.Adapt(core.Event{Type: core.EventSubagentStarted, Subagent: &core.SubagentEvent{TaskID: "child-1", Agent: "reviewer", Description: "Review"}})
	if len(started) != 1 || started[0].Type != "agent.started" || started[0].Source.AgentID != "child-1" {
		t.Fatalf("subagent start = %+v", started)
	}
	failed := adapter.Adapt(core.Event{Type: core.EventSubagentStatus, Subagent: &core.SubagentEvent{TaskID: "child-1", Status: "failed"}})
	if len(failed) != 1 || failed[0].Type != "agent.failed" {
		t.Fatalf("subagent failure = %+v", failed)
	}
	progress := adapter.Adapt(core.Event{Type: core.EventSubagentStatus, Subagent: &core.SubagentEvent{TaskID: "child-2", Status: "running", RecentTool: "grep"}})
	if len(progress) != 1 || progress[0].Type != "agent.progressed" {
		t.Fatalf("subagent progress = %+v", progress)
	}
	if drafts := adapter.Adapt(core.Event{Type: core.EventTextDelta, Text: "hello"}); len(drafts) != 0 {
		t.Fatalf("text delta must not become an authority event: %+v", drafts)
	}
}

func TestAdapterProjectsCoreEventsThroughAuthorityStore(t *testing.T) {
	t.Parallel()
	service := newTestService(t, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC))
	adapter := NewAdapter("runtime-1", "task-1")
	if _, err := service.CreateTask(context.Background(), "task-1", "Audit"); err != nil {
		t.Fatal(err)
	}
	events, err := adapter.Project(context.Background(), service, core.Event{Type: core.EventCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Cursor != 2 {
		t.Fatalf("projected events = %+v", events)
	}
	_, snapshot, err := service.Store().Load(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Task == nil || snapshot.Task.Status != "completed" || snapshot.CommittedCursor != 2 {
		t.Fatalf("authority snapshot = %+v", snapshot)
	}
	if drafts := adapter.Adapt(core.Event{Type: core.EventUserMessage, Text: "untrusted title"}); len(drafts) != 0 {
		t.Fatalf("user text must not become an authority event: %+v", drafts)
	}
}
