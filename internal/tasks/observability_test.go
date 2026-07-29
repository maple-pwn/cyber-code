package tasks

import (
	"context"
	"testing"
	"time"

	"cyber-code/internal/core"
)

func TestObservationHubPublishesOrderedEventsAndSnapshots(t *testing.T) {
	hub := newObservationHub(4)
	defer hub.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := hub.observe(ctx)

	hub.publish(core.Event{Type: core.EventSubagentStarted, Subagent: &core.SubagentEvent{
		TaskID: "task-1", Agent: "reviewer", Description: "review changes", Status: string(TaskStatusRunning),
	}})
	hub.publish(core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{
		TaskID: "task-1", Event: &core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{Name: "read_file"}},
	}})
	hub.publish(core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{
		TaskID: "task-1", Event: &core.Event{Type: core.EventUsage, Usage: &core.Usage{InputTokens: 10, OutputTokens: 3}},
	}})
	hub.publish(core.Event{Type: core.EventSubagentStatus, Subagent: &core.SubagentEvent{
		TaskID: "task-1", Status: string(TaskStatusCompleted),
	}})

	for _, want := range []core.EventType{core.EventSubagentStarted, core.EventSubagentEvent, core.EventSubagentEvent, core.EventSubagentStatus} {
		select {
		case event := <-events:
			if event.Type != want || event.Subagent == nil || event.Subagent.TaskID != "task-1" {
				t.Fatalf("event = %#v, want %s", event, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", want)
		}
	}
	snapshots := hub.snapshots()
	if len(snapshots) != 1 || snapshots[0].ID != "task-1" || snapshots[0].Agent != "reviewer" || snapshots[0].Status != TaskStatusCompleted {
		t.Fatalf("snapshots = %#v", snapshots)
	}
	if snapshots[0].RecentTool != "read_file" || snapshots[0].Usage.InputTokens != 10 || snapshots[0].Usage.OutputTokens != 3 {
		t.Fatalf("snapshot progress = %#v", snapshots[0])
	}
}

func TestObservationHubSlowSubscriberDoesNotBlockAndMarksTruncation(t *testing.T) {
	hub := newObservationHub(1)
	defer hub.close()
	slow := hub.observe(context.Background())
	fast := hub.observe(context.Background())
	_ = slow

	hub.publish(core.Event{Type: core.EventSubagentStarted, Subagent: &core.SubagentEvent{TaskID: "task-1", Status: string(TaskStatusRunning)}})
	<-fast
	done := make(chan struct{})
	go func() {
		hub.publish(core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{TaskID: "task-1", Event: &core.Event{Type: core.EventTextDelta, Text: "next"}}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked publisher")
	}
	if event := <-fast; event.Subagent == nil || event.Subagent.Event == nil || event.Subagent.Event.Text != "next" {
		t.Fatalf("fast subscriber event = %#v", event)
	}
	snapshots := hub.snapshots()
	if len(snapshots) != 1 || !snapshots[0].Truncated {
		t.Fatalf("slow subscriber did not mark truncation: %#v", snapshots)
	}
}

func TestObservationHubClosesCanceledSubscription(t *testing.T) {
	hub := newObservationHub(1)
	defer hub.close()
	ctx, cancel := context.WithCancel(context.Background())
	events := hub.observe(ctx)
	cancel()
	select {
	case _, open := <-events:
		if open {
			t.Fatal("canceled subscription remains open")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled subscription did not close")
	}
}

func TestManagerProgressUpdatesPersistThroughRegistryCopies(t *testing.T) {
	manager := NewManager(nil)
	defer manager.Close()
	task, err := manager.SpawnLocalAgent(context.Background(), "review", "sub-agent", "review changes", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateProgress(task.ID, AgentProgress{TokenCount: 9}); err != nil {
		t.Fatal(err)
	}
	if err := manager.AddToolActivity(task.ID, ToolActivity{ToolName: "read_file"}); err != nil {
		t.Fatal(err)
	}
	got := manager.GetTask(task.ID).(*LocalAgentTaskState)
	if got.Progress == nil || got.Progress.TokenCount != 9 || got.Progress.ToolUseCount != 1 || got.Progress.LastActivity == nil || got.Progress.LastActivity.ToolName != "read_file" {
		t.Fatalf("persisted progress = %#v", got.Progress)
	}
}

func TestToolServiceExposesAndClosesObservationStream(t *testing.T) {
	service, err := NewToolService(ToolServiceOptions{Execute: func(context.Context, AgentRequest) (any, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	events := service.Observe(context.Background())
	if snapshots := service.Snapshots(); len(snapshots) != 0 {
		t.Fatalf("initial snapshots = %#v", snapshots)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-events:
		if open {
			t.Fatal("closed service left observation stream open")
		}
	case <-time.After(time.Second):
		t.Fatal("observation stream did not close")
	}
}
