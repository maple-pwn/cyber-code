package ui

import (
	"context"
	"testing"

	"cyber-code/internal/core"
)

func TestModelStartsObservationAndRoutesChildEventsByTask(t *testing.T) {
	runner := newObservableUIRunner()
	model := NewModel(runner, ModelOptions{Width: 80, Height: 24})
	command := model.Init()
	if command == nil {
		t.Fatal("Init did not start observation")
	}
	updated, command := model.Update(command())
	model = updated.(*Model)
	if command == nil || runner.observeCalls != 1 {
		t.Fatalf("observation state: command=%v calls=%d", command != nil, runner.observeCalls)
	}

	runner.observations <- core.Event{Type: core.EventSubagentStarted, Subagent: &core.SubagentEvent{
		TaskID: "task-a", Agent: "reviewer", Description: "review changes", Status: "pending",
	}}
	runner.observations <- core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{
		TaskID: "task-a", Event: &core.Event{Type: core.EventTextDelta, Text: "child answer"},
	}}
	runner.observations <- core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{
		TaskID: "task-b", Event: &core.Event{Type: core.EventThinkingDelta, Text: "other thinking"},
	}}
	for range 3 {
		message := command()
		updated, command = model.Update(message)
		model = updated.(*Model)
	}
	first := model.subagents["task-a"]
	second := model.subagents["task-b"]
	if first == nil || second == nil || len(first.Messages) != 1 || first.Messages[0].Content != "child answer" || len(second.Messages) != 1 || second.Messages[0].Role != "thinking" {
		t.Fatalf("subagent views = %#v / %#v", first, second)
	}
	if len(model.Messages) != 0 || model.Usage != (core.Usage{}) {
		t.Fatalf("parent state was changed: messages=%#v usage=%#v", model.Messages, model.Usage)
	}
}

func TestSubagentViewRetainsToolUsageStatusAndTruncation(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{})
	model.applyObservation(core.Event{Type: core.EventSubagentStarted, Subagent: &core.SubagentEvent{
		TaskID: "task-1", Agent: "worker", Description: "implement", Status: "running",
	}})
	model.applyObservation(core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{
		TaskID: "task-1", Event: &core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "call-1", Name: "shell"}},
	}})
	model.applyObservation(core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{
		TaskID: "task-1", Event: &core.Event{Type: core.EventUsage, Usage: &core.Usage{InputTokens: 4, OutputTokens: 2}},
	}})
	model.applyObservation(core.Event{Type: core.EventSubagentEvent, Subagent: &core.SubagentEvent{
		TaskID: "task-1", Event: &core.Event{Type: core.EventToolResult, ToolCallID: "call-1", ToolResult: &core.ToolResult{ToolCallID: "call-1", IsError: true, Content: []core.ContentBlock{{Type: core.ContentText, Text: "failed"}}}}, Truncated: true,
	}})
	model.applyObservation(core.Event{Type: core.EventSubagentStatus, Subagent: &core.SubagentEvent{TaskID: "task-1", Status: "failed", Usage: &core.Usage{InputTokens: 4, OutputTokens: 2}}})
	view := model.subagents["task-1"]
	if view == nil || view.Status != "failed" || !view.Truncated || view.Usage.InputTokens != 4 || view.Usage.OutputTokens != 2 {
		t.Fatalf("subagent state = %#v", view)
	}
	if len(view.Tools) != 1 || view.Tools[0].Status != "failed" || view.Tools[0].Output != "failed" {
		t.Fatalf("subagent tools = %#v", view.Tools)
	}
}

type observableUIRunner struct {
	observations chan core.Event
	observeCalls int
}

func newObservableUIRunner() *observableUIRunner {
	return &observableUIRunner{observations: make(chan core.Event, 16)}
}

func (runner *observableUIRunner) Run(context.Context, string) <-chan core.Event {
	stream := make(chan core.Event)
	close(stream)
	return stream
}

func (runner *observableUIRunner) Observe(context.Context) <-chan core.Event {
	runner.observeCalls++
	return runner.observations
}
