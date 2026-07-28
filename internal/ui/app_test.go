package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
)

func TestModelEnterSubmitsPromptThroughCommandAndConsumesRuntimeEvents(t *testing.T) {
	runner := &uiTestRunner{events: []core.Event{
		{Type: core.EventTextDelta, Text: "hello"},
		{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "call-1", Name: "read_file"}},
		{Type: core.EventToolResult, ToolResult: &core.ToolResult{ToolCallID: "call-1", Content: []core.ContentBlock{{Type: core.ContentText, Text: "file"}}}},
		{Type: core.EventCompleted, FinishReason: "stop"},
	}}
	model := NewModel(runner, ModelOptions{Width: 80, Height: 24})
	model.Input.SetValue("inspect")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if runner.prompt != "" || command == nil {
		t.Fatal("runtime was called outside Bubble Tea command")
	}
	message := command()
	model, command = updateModel(t, model, message)
	for command != nil {
		model, command = updateModel(t, model, command())
	}
	if runner.prompt != "inspect" || model.Processing || model.Input.Value != "" {
		t.Fatalf("prompt = %q, processing = %v, input = %q", runner.prompt, model.Processing, model.Input.Value)
	}
	view := model.View()
	for _, text := range []string{"inspect", "hello", "read_file", "file"} {
		if !strings.Contains(view, text) {
			t.Fatalf("view missing %q: %q", text, view)
		}
	}
}

func TestModelVimModeTogglePreservesUnicodeInput(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 80, Height: 24})
	model.Input.SetValue("保留 text")
	model.SetVimMode(true)
	model.SetVimMode(false)
	if model.Input.Value != "保留 text" || model.Input.CursorPos != 7 {
		t.Fatalf("input = %q, cursor = %d", model.Input.Value, model.Input.CursorPos)
	}
}

func TestModelPermissionMessageUsesDialogAndRespondsThroughMessage(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 80, Height: 24})
	responses := make(chan permissions.Decision, 1)
	updated, _ := model.Update(PermissionRequestMsg{
		Request: permissions.Request{Tool: "shell", Command: "go test ./..."}, Respond: responses,
	})
	model = updated.(*Model)
	if model.Permission == nil || !strings.Contains(model.View(), "go test ./...") {
		t.Fatal("permission dialog was not rendered")
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if command == nil {
		t.Fatal("permission response command is nil")
	}
	_ = command()
	decision := <-responses
	if decision.Behavior != permissions.PermissionBehaviorAllow || model.Permission != nil {
		t.Fatalf("decision = %#v, dialog = %#v", decision, model.Permission)
	}
}

func TestPermissionBridgeAttachesAndDefaultsToDenyWhenDetached(t *testing.T) {
	bridge := NewPermissionBridge()
	bridge.Attach(func(message tea.Msg) {
		request := message.(PermissionRequestMsg)
		request.Respond <- permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}
	})
	decision, err := bridge.Confirm(context.Background(), permissions.Request{Tool: "shell", Action: permissions.ActionExecute})
	if err != nil || decision.Behavior != permissions.PermissionBehaviorAllow {
		t.Fatalf("decision = %#v, error = %v", decision, err)
	}
	bridge.Detach()
	decision, err = bridge.Confirm(context.Background(), permissions.Request{Tool: "shell", Action: permissions.ActionExecute})
	if err != nil || decision.Behavior != permissions.PermissionBehaviorDeny {
		t.Fatalf("detached decision = %#v, error = %v", decision, err)
	}
}

func updateModel(t *testing.T, model *Model, message tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	updated, command := model.Update(message)
	result, ok := updated.(*Model)
	if !ok {
		t.Fatalf("model type = %T", updated)
	}
	return result, command
}

type uiTestRunner struct {
	prompt string
	events []core.Event
}

func (runner *uiTestRunner) Run(_ context.Context, prompt string) <-chan core.Event {
	runner.prompt = prompt
	events := make(chan core.Event, len(runner.events))
	for _, event := range runner.events {
		events <- event
	}
	close(events)
	return events
}
