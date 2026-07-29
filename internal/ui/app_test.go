package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tool/builtin"
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

func TestModelViewCorrelatesToolStateAndAccumulatesUsage(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 80, Height: 24})
	model.applyEvent(core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "call-1", Name: "read_file"}})
	model.applyEvent(core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "call-2", Name: "shell"}})
	model.applyEvent(core.Event{Type: core.EventUsage, Usage: &core.Usage{InputTokens: 10, OutputTokens: 3, CacheReadInputTokens: 2}})
	model.applyEvent(core.Event{Type: core.EventUsage, Usage: &core.Usage{InputTokens: 2, OutputTokens: 1, CacheCreationInputTokens: 4}})
	model.applyEvent(core.Event{Type: core.EventToolResult, ToolResult: &core.ToolResult{ToolCallID: "call-2", IsError: true}})
	model.applyEvent(core.Event{Type: core.EventToolResult, ToolResult: &core.ToolResult{ToolCallID: "call-1"}})

	view := model.View()
	for _, want := range []string{
		"✓ read_file",
		"✗ shell",
		"tokens: input=12 output=4 cache_read=2 cache_creation=4",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %q", want, view)
		}
	}
}

func TestModelViewUsesAnimatedProcessingIndicatorWithoutMovingInput(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 60, Height: 12})
	model.Processing = true
	model.StatusText = "Working"
	before := ansi.Strip(model.View())
	if !strings.Contains(before, "⠋ Working") {
		t.Fatalf("initial processing indicator is not connected: %q", before)
	}
	inputLine := strings.LastIndex(before, "Type your message")

	updated, command := model.Update(processingTickMsg{})
	model = updated.(*Model)
	after := ansi.Strip(model.View())
	if !strings.Contains(after, "⠙ Working") || command == nil {
		t.Fatalf("processing tick did not advance or reschedule: view=%q command=%v", after, command)
	}
	if got := strings.LastIndex(after, "Type your message"); got != inputLine {
		t.Fatalf("input moved from byte %d to %d after spinner tick", inputLine, got)
	}
}

func TestModelViewUsesStructuredToolResultAndFilePreview(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 80, Height: 18})
	model.Processing = true
	model.applyEvent(core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{
		ID: "read-1", Name: "read_file", Arguments: []byte(`{"path":"internal/app.go"}`),
	}})
	model.applyEvent(core.Event{Type: core.EventToolResult, ToolResult: &core.ToolResult{
		ToolCallID: "read-1", Content: []core.ContentBlock{{Type: core.ContentText, Text: "alpha\nbeta"}},
	}})

	view := ansi.Strip(model.View())
	for _, want := range []string{"✓ read_file", "internal/app.go", "1│ alpha", "2│ beta"} {
		if !strings.Contains(view, want) {
			t.Fatalf("structured tool view missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, "Tool: read_file: succeeded") {
		t.Fatalf("legacy tool status leaked into structured view: %q", view)
	}
}

func TestModelCollapsesCompletedToolOutputAndClearsItOnNextTurn(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 80, Height: 18})
	model.Processing = true
	model.applyEvent(core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{
		ID: "shell-1", Name: "shell", Arguments: []byte(`{"command":"ls"}`),
	}})
	model.applyEvent(core.Event{Type: core.EventToolResult, ToolResult: &core.ToolResult{
		ToolCallID: "shell-1", Content: []core.ContentBlock{{Type: core.ContentText, Text: "coverage.out\ncyber-code-static"}},
	}})
	if view := ansi.Strip(model.View()); !strings.Contains(view, "coverage.out") {
		t.Fatalf("running turn hid tool output: %q", view)
	}

	model.applyEvent(core.Event{Type: core.EventCompleted, FinishReason: "stop"})
	view := ansi.Strip(model.View())
	if strings.Contains(view, "coverage.out") || !strings.Contains(view, "shell") {
		t.Fatalf("completed tool was not collapsed to a summary: %q", view)
	}

	model.Input.SetValue("next turn")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if command == nil || len(model.Tools) != 0 || len(model.toolIndexes) != 0 {
		t.Fatalf("new turn retained old tool presentation: tools=%#v indexes=%#v", model.Tools, model.toolIndexes)
	}
}

func TestModelViewKeepsInputVisibleWithinViewport(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 40, Height: 12})
	for index := 0; index < 20; index++ {
		model.AddMessage("assistant", fmt.Sprintf("message-%02d", index))
	}
	view := model.View()
	if lines := strings.Count(view, "\n") + 1; lines > 12 {
		t.Fatalf("view uses %d lines for a 12-line viewport:\n%s", lines, view)
	}
	for _, want := range []string{"message-19", "Type your message"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %q", want, view)
		}
	}
}

func TestModelMultilineDoesNotSubmitUntilPlainEnter(t *testing.T) {
	runner := &uiTestRunner{}
	model := NewModel(runner, ModelOptions{})
	model.Input.SetValue("first")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	model = updated.(*Model)
	if command != nil || runner.prompt != "" || model.Input.Value != "first\n" {
		t.Fatalf("alt-enter submitted: prompt=%q input=%q", runner.prompt, model.Input.Value)
	}
	model.Input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if command == nil {
		t.Fatal("plain enter did not submit multiline input")
	}
	_ = command()
	if runner.prompt != "first\nsecond" {
		t.Fatalf("submitted prompt = %q", runner.prompt)
	}
}

func TestModelTabCompletesSlashCommandsAndWorkspacePaths(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(&uiTestRunner{}, ModelOptions{Workspace: workspace, CommandNames: []string{"status", "skills"}})
	model.Input.SetValue("/sta")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	if model.Input.Value != "/status " {
		t.Fatalf("command completion = %q", model.Input.Value)
	}
	model.Input.SetValue("read REA")
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	if model.Input.Value != "read README.md " {
		t.Fatalf("path completion = %q", model.Input.Value)
	}
}

func TestNewModelDefaultsCompletionWorkspaceToCurrentDirectory(t *testing.T) {
	workspace := t.TempDir()
	t.Chdir(workspace)
	if err := os.WriteFile(filepath.Join(workspace, "CURRENT.md"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := NewModel(&uiTestRunner{}, ModelOptions{})
	model.Input.SetValue("open CUR")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(*Model)
	if model.Input.Value != "open CURRENT.md " {
		t.Fatalf("default workspace completion = %q", model.Input.Value)
	}
}

func TestModelViewUsesCyberCodeBrand(t *testing.T) {
	view := NewModel(&uiTestRunner{}, ModelOptions{Width: 80, Height: 24}).View()
	if !strings.Contains(view, "cyber-code") || strings.Contains(strings.ToLower(view), "claude code") {
		t.Fatalf("view does not use cyber-code brand: %q", view)
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

func TestControlBridgeChangesActiveModelVimMode(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 80, Height: 24})
	model.Input.SetValue("保留 text")
	bridge := NewControlBridge()
	bridge.Attach(func(message tea.Msg) {
		updated, _ := model.Update(message)
		model = updated.(*Model)
	})
	defer bridge.Detach()

	if err := bridge.SetVimMode(true); err != nil {
		t.Fatal(err)
	}
	if !model.Input.VimEnabled || model.Input.Value != "保留 text" {
		t.Fatalf("vim=%t input=%q", model.Input.VimEnabled, model.Input.Value)
	}
	bridge.Detach()
	if err := bridge.SetVimMode(false); err == nil {
		t.Fatal("detached control bridge accepted a UI state change")
	}
}

func TestControlBridgeClearsConversationPresentation(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 80, Height: 24})
	model.Messages = []Message{{Role: "user", Content: "old prompt"}}
	model.Tools = []ToolPresentation{{ID: "tool-1", Name: "read_file", Status: "succeeded"}}
	model.toolIndexes["tool-1"] = 0
	bridge := NewControlBridge()
	bridge.Attach(func(message tea.Msg) {
		updated, _ := model.Update(message)
		model = updated.(*Model)
	})

	if err := bridge.ClearConversation(); err != nil {
		t.Fatal(err)
	}
	if len(model.Messages) != 0 || len(model.Tools) != 0 || len(model.toolIndexes) != 0 {
		t.Fatalf("presentation was not cleared: messages=%#v tools=%#v indexes=%#v", model.Messages, model.Tools, model.toolIndexes)
	}
}

func TestModelAnswersStructuredUserQuestion(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{})
	reply := make(chan QuestionAnswer, 1)
	updated, command := model.Update(QuestionRequestMsg{
		Question: builtin.Question{Prompt: "Choose", Options: []string{"first", "second"}}, Respond: reply,
	})
	model = updated.(*Model)
	if command != nil || model.QuestionSelect == nil || !strings.Contains(model.View(), "Choose") {
		t.Fatalf("question state = %#v", model.QuestionSelect)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(*Model)
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if command == nil || model.QuestionSelect != nil {
		t.Fatalf("question did not finish")
	}
	_ = command()
	answer := <-reply
	if answer.Err != nil || answer.Value != "second" {
		t.Fatalf("answer = %#v", answer)
	}
}

func TestQuestionBridgeReturnsContextCancellation(t *testing.T) {
	bridge := NewQuestionBridge()
	bridge.Attach(func(tea.Msg) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := bridge.Ask(ctx, builtin.Question{Prompt: "cancel me"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
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
	if bridge.Attached() {
		t.Fatal("new permission bridge is attached")
	}
	bridge.Attach(func(message tea.Msg) {
		request := message.(PermissionRequestMsg)
		request.Respond <- permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}
	})
	if !bridge.Attached() {
		t.Fatal("permission bridge did not report attachment")
	}
	decision, err := bridge.Confirm(context.Background(), permissions.Request{Tool: "shell", Action: permissions.ActionExecute})
	if err != nil || decision.Behavior != permissions.PermissionBehaviorAllow {
		t.Fatalf("decision = %#v, error = %v", decision, err)
	}
	bridge.Detach()
	if bridge.Attached() {
		t.Fatal("detached permission bridge still reports attachment")
	}
	decision, err = bridge.Confirm(context.Background(), permissions.Request{Tool: "shell", Action: permissions.ActionExecute})
	if err != nil || decision.Behavior != permissions.PermissionBehaviorDeny {
		t.Fatalf("detached decision = %#v, error = %v", decision, err)
	}
}

func TestPermissionDescriptionIncludesNetworkTarget(t *testing.T) {
	description := permissionDescription(permissions.Request{Action: permissions.ActionNetwork, Network: []string{"mcp.example.test"}})
	if !strings.Contains(description, "mcp.example.test") {
		t.Fatalf("network permission description = %q", description)
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
