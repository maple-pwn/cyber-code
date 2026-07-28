package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/provider"
	toolpkg "cyber-code/internal/tool"
)

func TestToolLoopExecutesAllowedAndReturnsDeniedResults(t *testing.T) {
	workspace := t.TempDir()
	registry := toolpkg.NewRegistry()
	read := &loopTool{name: "read_file", action: permissions.ActionRead, workspace: workspace, result: "contents", readOnly: true, concurrent: true}
	shell := &loopTool{name: "shell", action: permissions.ActionExecute, workspace: workspace, result: "executed"}
	if err := registry.Register(read); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(shell); err != nil {
		t.Fatal(err)
	}
	confirmations := 0
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeDefault, Confirmer: func(_ context.Context, request permissions.Request) (permissions.Decision, error) {
		confirmations++
		if request.Tool != "shell" {
			t.Fatalf("unexpected confirmation: %#v", request)
		}
		return permissions.Decision{Behavior: permissions.PermissionBehaviorDeny}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	runner := toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})
	model := &loopProvider{rounds: [][]core.Event{
		{
			{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "read-1", Name: "read_file", Arguments: json.RawMessage(`{}`)}},
			{Type: core.EventToolArgumentsDelta, ToolCallID: "read-1", ArgumentsDelta: `{"path":"README.MD"}`},
			{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "shell-1", Name: "shell", Arguments: json.RawMessage(`{"command":"remove files"}`)}},
			{Type: core.EventCompleted, FinishReason: "tool_calls"},
		},
		{{Type: core.EventTextDelta, Text: "summary"}, {Type: core.EventCompleted, FinishReason: "stop"}},
	}}
	engine := NewEngine(model, Options{Model: "model-test", MaxTurns: 4, Tools: registry, ToolRunner: runner})
	events := collectAgentEvents(t, engine.Run(context.Background(), "work"))

	completed := 0
	results := make([]*core.ToolResult, 0, 2)
	for _, event := range events {
		if event.Type == core.EventCompleted {
			completed++
		}
		if event.Type == core.EventToolResult {
			results = append(results, event.ToolResult)
		}
	}
	if completed != 1 || len(results) != 2 {
		t.Fatalf("events = %#v", events)
	}
	if results[0].ToolCallID != "read-1" || results[0].IsError || results[0].Content[0].Text != "contents" {
		t.Fatalf("read result = %#v", results[0])
	}
	if results[1].ToolCallID != "shell-1" || !results[1].IsError {
		t.Fatalf("shell result = %#v", results[1])
	}
	if confirmations != 1 || read.runCount() != 1 || shell.runCount() != 0 {
		t.Fatalf("confirmations=%d read=%d shell=%d", confirmations, read.runCount(), shell.runCount())
	}
	if len(model.requests) != 2 || len(model.requests[0].Tools) != 2 {
		t.Fatalf("provider requests = %#v", model.requests)
	}
	messages := model.requests[1].Messages
	if len(messages) != 3 || messages[1].Role != core.RoleAssistant || messages[2].Role != core.RoleTool || len(messages[2].Content) != 2 {
		t.Fatalf("second request messages = %#v", messages)
	}
	if messages[2].Content[0].ToolResult.ToolCallID != "read-1" || messages[2].Content[1].ToolResult.ToolCallID != "shell-1" {
		t.Fatalf("tool result order = %#v", messages[2].Content)
	}
}

func TestToolLoopRunsConcurrentReadOnlyToolsInParallelAndPreservesResultOrder(t *testing.T) {
	workspace := t.TempDir()
	entered := make(chan string, 2)
	release := make(chan struct{})
	registry := toolpkg.NewRegistry()
	for _, name := range []string{"read_first", "read_second"} {
		if err := registry.Register(&loopTool{
			name: name, action: permissions.ActionRead, workspace: workspace, result: name,
			readOnly: true, concurrent: true, entered: entered, release: release,
		}); err != nil {
			t.Fatal(err)
		}
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeDefault})
	if err != nil {
		t.Fatal(err)
	}
	model := &loopProvider{rounds: [][]core.Event{
		{
			{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "first", Name: "read_first", Arguments: json.RawMessage(`{}`)}},
			{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "second", Name: "read_second", Arguments: json.RawMessage(`{}`)}},
			{Type: core.EventCompleted, FinishReason: "tool_calls"},
		},
		{{Type: core.EventCompleted, FinishReason: "stop"}},
	}}
	engine := NewEngine(model, Options{MaxTurns: 2, Tools: registry, ToolRunner: toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})})
	eventsDone := collectAgentEventsAsync(engine.Run(context.Background(), "read"))

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case name := <-entered:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatalf("only these tools entered before release: %#v", seen)
		}
	}
	close(release)
	events := awaitAgentEvents(t, eventsDone)
	var resultIDs []string
	for _, event := range events {
		if event.Type == core.EventToolResult {
			resultIDs = append(resultIDs, event.ToolCallID)
		}
	}
	if len(resultIDs) != 2 || resultIDs[0] != "first" || resultIDs[1] != "second" {
		t.Fatalf("tool result IDs = %#v", resultIDs)
	}
}

func TestToolLoopRunsUnsafeToolsSerially(t *testing.T) {
	workspace := t.TempDir()
	firstEntered := make(chan string, 1)
	secondEntered := make(chan string, 1)
	firstRelease := make(chan struct{})
	registry := toolpkg.NewRegistry()
	first := &loopTool{name: "write_first", action: permissions.ActionWrite, workspace: workspace, entered: firstEntered, release: firstRelease}
	second := &loopTool{name: "write_second", action: permissions.ActionWrite, workspace: workspace, entered: secondEntered}
	if err := registry.Register(first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(second); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeAcceptEdits})
	if err != nil {
		t.Fatal(err)
	}
	model := &loopProvider{rounds: [][]core.Event{
		{
			{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "first", Name: "write_first", Arguments: json.RawMessage(`{}`)}},
			{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "second", Name: "write_second", Arguments: json.RawMessage(`{}`)}},
			{Type: core.EventCompleted, FinishReason: "tool_calls"},
		},
		{{Type: core.EventCompleted, FinishReason: "stop"}},
	}}
	engine := NewEngine(model, Options{MaxTurns: 2, Tools: registry, ToolRunner: toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})})
	eventsDone := collectAgentEventsAsync(engine.Run(context.Background(), "write"))

	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first unsafe tool did not start")
	}
	select {
	case <-secondEntered:
		t.Fatal("second unsafe tool started before the first completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(firstRelease)
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second unsafe tool did not start after the first completed")
	}
	awaitAgentEvents(t, eventsDone)
}

func TestToolLoopRejectsInvalidArgumentStreamWithoutExecution(t *testing.T) {
	workspace := t.TempDir()
	registry := toolpkg.NewRegistry()
	read := &loopTool{name: "read_file", action: permissions.ActionRead, workspace: workspace, readOnly: true, concurrent: true}
	if err := registry.Register(read); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeDefault})
	if err != nil {
		t.Fatal(err)
	}
	model := &loopProvider{rounds: [][]core.Event{{
		{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "read-1", Name: "read_file"}},
		{Type: core.EventToolArgumentsDelta, ToolCallID: "read-1", ArgumentsDelta: `{"path":`},
		{Type: core.EventCompleted, FinishReason: "tool_calls"},
	}}}
	engine := NewEngine(model, Options{MaxTurns: 2, Tools: registry, ToolRunner: toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})})
	events := collectAgentEvents(t, engine.Run(context.Background(), "read"))

	errors := 0
	completed := 0
	for _, event := range events {
		if event.Type == core.EventError {
			errors++
		}
		if event.Type == core.EventCompleted {
			completed++
		}
	}
	if errors != 1 || completed != 0 || read.runCount() != 0 {
		t.Fatalf("errors=%d completed=%d runs=%d events=%#v", errors, completed, read.runCount(), events)
	}
}

func TestToolLoopProviderErrorIsTheOnlyTerminalError(t *testing.T) {
	want := &core.Error{Kind: core.ErrorKindProvider, Op: "provider.stream", Message: "upstream failed"}
	model := &loopProvider{rounds: [][]core.Event{{{Type: core.EventError, Err: want}}}}
	engine := NewEngine(model, Options{MaxTurns: 2})
	events := collectAgentEvents(t, engine.Run(context.Background(), "read"))

	var terminal []core.Event
	for _, event := range events {
		if event.Type == core.EventError || event.Type == core.EventCompleted {
			terminal = append(terminal, event)
		}
	}
	if len(terminal) != 1 || terminal[0].Type != core.EventError || terminal[0].Err != want {
		t.Fatalf("terminal events = %#v; all events = %#v", terminal, events)
	}
}

func TestToolLoopMaxTurnsEmitsOneFinalCompletion(t *testing.T) {
	workspace := t.TempDir()
	registry := toolpkg.NewRegistry()
	read := &loopTool{name: "read_file", action: permissions.ActionRead, workspace: workspace, readOnly: true, concurrent: true}
	if err := registry.Register(read); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeDefault})
	if err != nil {
		t.Fatal(err)
	}
	model := &loopProvider{rounds: [][]core.Event{{
		{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "read-1", Name: "read_file", Arguments: json.RawMessage(`{}`)}},
		{Type: core.EventCompleted, FinishReason: "tool_calls"},
	}}}
	engine := NewEngine(model, Options{MaxTurns: 1, Tools: registry, ToolRunner: toolpkg.NewRunner(registry, broker, toolpkg.RunnerOptions{})})
	events := collectAgentEvents(t, engine.Run(context.Background(), "read"))

	var completions []core.Event
	for _, event := range events {
		if event.Type == core.EventCompleted {
			completions = append(completions, event)
		}
	}
	if len(completions) != 1 || completions[0].FinishReason != "max_turns" || len(model.requests) != 1 {
		t.Fatalf("completions=%#v requests=%d events=%#v", completions, len(model.requests), events)
	}
}

func collectAgentEventsAsync(events <-chan core.Event) <-chan []core.Event {
	done := make(chan []core.Event, 1)
	go func() {
		var collected []core.Event
		for event := range events {
			collected = append(collected, event)
		}
		done <- collected
	}()
	return done
}

func awaitAgentEvents(t *testing.T, done <-chan []core.Event) []core.Event {
	t.Helper()
	select {
	case events := <-done:
		return events
	case <-time.After(time.Second):
		t.Fatal("engine output did not close")
		return nil
	}
}

type loopProvider struct {
	mu       sync.Mutex
	rounds   [][]core.Event
	requests []core.Request
}

func (model *loopProvider) Name() string { return "loop" }
func (model *loopProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true, ToolCalls: true}, nil
}
func (model *loopProvider) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	model.mu.Lock()
	index := len(model.requests)
	model.requests = append(model.requests, request)
	events := append([]core.Event(nil), model.rounds[index]...)
	model.mu.Unlock()
	stream := make(chan core.Event)
	go func() {
		defer close(stream)
		for _, event := range events {
			select {
			case stream <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return stream, nil
}
func (model *loopProvider) CountTokens(context.Context, core.Request) (int, error) { return 0, nil }

type loopTool struct {
	mu         sync.Mutex
	name       string
	action     string
	workspace  string
	result     string
	readOnly   bool
	concurrent bool
	runs       int
	entered    chan<- string
	release    <-chan struct{}
}

func (tool *loopTool) Spec() toolpkg.Spec {
	return toolpkg.Spec{Name: tool.name, ReadOnly: tool.readOnly, ConcurrencySafe: tool.concurrent,
		Schema: json.RawMessage(`{"type":"object","additionalProperties":true}`)}
}
func (tool *loopTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Tool: tool.name, Action: tool.action, Workspace: tool.workspace, Command: commandArgument(arguments)}, nil
}
func (tool *loopTool) Run(ctx context.Context, _ json.RawMessage) (core.ToolResult, error) {
	tool.mu.Lock()
	tool.runs++
	tool.mu.Unlock()
	if tool.entered != nil {
		select {
		case tool.entered <- tool.name:
		case <-ctx.Done():
			return core.ToolResult{}, ctx.Err()
		}
	}
	if tool.release != nil {
		select {
		case <-tool.release:
		case <-ctx.Done():
			return core.ToolResult{}, ctx.Err()
		}
	}
	return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: tool.result}}}, nil
}
func (tool *loopTool) runCount() int {
	tool.mu.Lock()
	defer tool.mu.Unlock()
	return tool.runs
}
func commandArgument(arguments json.RawMessage) string {
	var input struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(arguments, &input)
	return input.Command
}
