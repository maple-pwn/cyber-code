package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-code-go/internal/core"
	"claude-code-go/internal/provider"
)

func TestEngineAddsCyberCodeSystemIdentity(t *testing.T) {
	fake := &fakeProvider{events: []core.Event{{Type: core.EventCompleted, FinishReason: "stop"}}}
	engine := NewEngine(fake, Options{Model: "model-test"})

	collectAgentEvents(t, engine.Run(context.Background(), "hello"))

	if len(fake.request.System) != 1 || fake.request.System[0].Type != core.ContentText {
		t.Fatalf("system prompt = %#v", fake.request.System)
	}
	identity := strings.ToLower(fake.request.System[0].Text)
	for _, required := range []string{"cyber-code", "independent", "coding agent", "do not claim"} {
		if !strings.Contains(identity, required) {
			t.Fatalf("system identity missing %q: %q", required, fake.request.System[0].Text)
		}
	}
}

func TestEngineStreamsTurnAndCommitsHistory(t *testing.T) {
	fake := &fakeProvider{events: []core.Event{
		{Type: core.EventTextDelta, Text: "hello"},
		{Type: core.EventUsage, Usage: &core.Usage{InputTokens: 2, OutputTokens: 1}},
		{Type: core.EventCompleted, FinishReason: "stop"},
	}}
	engine := NewEngine(fake, Options{Model: "model-test", MaxTurns: 4})
	events := collectAgentEvents(t, engine.Run(context.Background(), "hi"))
	wantTypes := []core.EventType{core.EventUserMessage, core.EventTextDelta, core.EventUsage, core.EventCompleted}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %#v", events)
	}
	for index, want := range wantTypes {
		if events[index].Type != want {
			t.Fatalf("event %d = %q, want %q", index, events[index].Type, want)
		}
	}
	if fake.request.Model != "model-test" || len(fake.request.Messages) != 1 || fake.request.Messages[0].Role != core.RoleUser {
		t.Fatalf("provider request = %#v", fake.request)
	}
	history := engine.History()
	if len(history) != 2 || history[0].Role != core.RoleUser || history[1].Role != core.RoleAssistant || history[1].Content[0].Text != "hello" {
		t.Fatalf("history = %#v", history)
	}
	history[0].Content[0].Text = "mutated"
	if engine.History()[0].Content[0].Text != "hi" {
		t.Fatal("History returned mutable engine state")
	}
}

func TestEngineProviderStartErrorBecomesEvent(t *testing.T) {
	fake := &fakeProvider{streamError: errors.New("unavailable")}
	engine := NewEngine(fake, Options{})

	events := collectAgentEvents(t, engine.Run(context.Background(), "hi"))
	if len(events) != 2 || events[0].Type != core.EventUserMessage || events[1].Type != core.EventError {
		t.Fatalf("events = %#v", events)
	}
	if events[1].Err == nil || events[1].Err.Kind != core.ErrorKindProvider || events[1].Err.Cause == nil {
		t.Fatalf("error event = %#v", events[1])
	}
	if history := engine.History(); len(history) != 1 || history[0].Role != core.RoleUser {
		t.Fatalf("history = %#v", history)
	}
}

func TestEngineOnlyCommitsAssistantAfterCompletion(t *testing.T) {
	fake := &fakeProvider{events: []core.Event{{Type: core.EventTextDelta, Text: "partial"}}}
	engine := NewEngine(fake, Options{})

	collectAgentEvents(t, engine.Run(context.Background(), "hi"))
	if history := engine.History(); len(history) != 1 || history[0].Role != core.RoleUser {
		t.Fatalf("history = %#v", history)
	}
}

func TestEngineHistoryDeepCopiesNestedContent(t *testing.T) {
	engine := NewEngine(&fakeProvider{}, Options{})
	engine.history = []core.Message{{
		Role: core.RoleAssistant,
		Content: []core.ContentBlock{{
			Type: core.ContentToolCall,
			ToolCall: &core.ToolCall{
				ID:        "call-1",
				Name:      "read_file",
				Arguments: json.RawMessage(`{"path":"README.MD"}`),
			},
		}, {
			Type: core.ContentToolResult,
			ToolResult: &core.ToolResult{
				ToolCallID: "call-1",
				Content:    []core.ContentBlock{{Type: core.ContentText, Text: "contents"}},
			},
		}},
	}}

	history := engine.History()
	history[0].Content[0].ToolCall.Arguments[9] = 'X'
	history[0].Content[1].ToolResult.Content[0].Text = "mutated"

	again := engine.History()
	if string(again[0].Content[0].ToolCall.Arguments) != `{"path":"README.MD"}` ||
		again[0].Content[1].ToolResult.Content[0].Text != "contents" {
		t.Fatalf("history changed through returned value: %#v", again)
	}
}

func TestEngineCancellationClosesOutput(t *testing.T) {
	fake := &fakeProvider{waitForCancellation: true}
	engine := NewEngine(fake, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	output := engine.Run(ctx, "hi")
	if event := <-output; event.Type != core.EventUserMessage {
		t.Fatalf("first event = %q", event.Type)
	}
	cancel()
	select {
	case _, ok := <-output:
		if ok {
			t.Fatal("engine emitted an event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("engine output did not close after cancellation")
	}
}

func TestEngineSerializesConcurrentTurns(t *testing.T) {
	model := &serialProvider{
		firstEntered: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	engine := NewEngine(model, Options{})
	first := engine.Run(context.Background(), "first")
	if event := <-first; event.Type != core.EventUserMessage {
		t.Fatalf("first event = %q", event.Type)
	}
	<-model.firstEntered

	second := engine.Run(context.Background(), "second")
	var early *core.Event
	select {
	case event := <-second:
		early = &event
	case <-time.After(20 * time.Millisecond):
	}
	close(model.releaseFirst)
	collectAgentEvents(t, first)
	collectAgentEvents(t, second)
	if early != nil {
		t.Fatalf("second turn began before first completed: %#v", *early)
	}

	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.requests) != 2 || len(model.requests[1].Messages) != 3 || model.requests[1].Messages[1].Role != core.RoleAssistant {
		t.Fatalf("provider requests = %#v", model.requests)
	}
}

func collectAgentEvents(t *testing.T, events <-chan core.Event) []core.Event {
	t.Helper()
	var result []core.Event
	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return result
			}
			result = append(result, event)
		case <-deadline:
			t.Fatalf("engine output did not close: %#v", result)
		}
	}
}

type fakeProvider struct {
	events              []core.Event
	waitForCancellation bool
	streamError         error
	request             core.Request
}

func (fake *fakeProvider) Name() string { return "fake" }
func (fake *fakeProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true}, nil
}
func (fake *fakeProvider) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	fake.request = request
	if fake.streamError != nil {
		return nil, fake.streamError
	}
	stream := make(chan core.Event)
	go func() {
		defer close(stream)
		if fake.waitForCancellation {
			<-ctx.Done()
			return
		}
		for _, event := range fake.events {
			select {
			case stream <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return stream, nil
}
func (fake *fakeProvider) CountTokens(context.Context, core.Request) (int, error) { return 0, nil }

type serialProvider struct {
	mu           sync.Mutex
	requests     []core.Request
	firstEntered chan struct{}
	releaseFirst chan struct{}
}

func (p *serialProvider) Name() string { return "serial" }
func (p *serialProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true}, nil
}
func (p *serialProvider) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	call := len(p.requests)
	p.mu.Unlock()
	if call == 1 {
		close(p.firstEntered)
		select {
		case <-p.releaseFirst:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	stream := make(chan core.Event, 1)
	stream <- core.Event{Type: core.EventCompleted}
	close(stream)
	return stream, nil
}
func (p *serialProvider) CountTokens(context.Context, core.Request) (int, error) { return 0, nil }
