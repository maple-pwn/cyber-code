package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"cyber-code/internal/contextbuilder"
	"cyber-code/internal/core"
	"cyber-code/internal/provider"
	"cyber-code/internal/session"
)

func TestEngineCompactsHistoryBeforeProviderRequest(t *testing.T) {
	compactor, err := session.NewCompactor(session.CompactOptions{
		ThresholdTokens:    50,
		KeepRecentMessages: 2,
		Summarize: func(context.Context, []core.Message) (string, error) {
			return "derived summary", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &compactProvider{count: 100, events: []core.Event{{Type: core.EventCompleted, FinishReason: "stop"}}}
	initial := textHistory("old user", "old assistant", "recent user", "recent assistant")
	engine := NewEngine(model, Options{Model: "model", InitialHistory: initial, Compactor: compactor})
	events := collectAgentEvents(t, engine.Run(context.Background(), "new question"))

	if len(events) != 3 || events[0].Type != core.EventUserMessage || events[1].Type != core.EventCompacted || events[2].Type != core.EventCompleted {
		t.Fatalf("events = %#v", events)
	}
	if events[1].Message == nil || events[1].CoveredMessages != 3 {
		t.Fatalf("compact event = %#v", events[1])
	}
	model.mu.Lock()
	request := model.request
	model.mu.Unlock()
	if len(request.Messages) != 3 || request.Messages[0].Role != core.RoleSystem || request.Messages[0].Content[0].Text != "derived summary" || request.Messages[2].Content[0].Text != "new question" {
		t.Fatalf("compacted provider request = %#v", request)
	}
	if initial[0].Content[0].Text != "old user" {
		t.Fatalf("initial history was mutated: %#v", initial)
	}
}

func TestEngineAllowsOversizedContextToReachCompactor(t *testing.T) {
	compactor, err := session.NewCompactor(session.CompactOptions{
		ThresholdTokens: 50, KeepRecentMessages: 1,
		Summarize: func(context.Context, []core.Message) (string, error) { return "small summary", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	builder, err := contextbuilder.New(contextbuilder.Options{ContextWindow: 1000, ReservedOutput: 100})
	if err != nil {
		t.Fatal(err)
	}
	model := &compactProvider{count: 10_000, events: []core.Event{{Type: core.EventCompleted, FinishReason: "stop"}}}
	engine := NewEngine(model, Options{
		ContextBuilder: builder, Compactor: compactor,
		InitialHistory: textHistory(strings.Repeat("old history ", 1000), "old assistant"),
	})

	events := collectAgentEvents(t, engine.Run(context.Background(), "new question"))

	for _, event := range events {
		if event.Type == core.EventError {
			t.Fatalf("oversized history failed before compact: %#v", events)
		}
	}
	if len(events) < 2 || events[1].Type != core.EventCompacted {
		t.Fatalf("events = %#v", events)
	}
}

func TestEngineCompactFailureWarnsAndKeepsHistory(t *testing.T) {
	compactor, err := session.NewCompactor(session.CompactOptions{
		ThresholdTokens:    1,
		KeepRecentMessages: 1,
		Summarize: func(context.Context, []core.Message) (string, error) {
			return "", errors.New("summary service unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	initial := textHistory("old user", "old assistant")
	model := &compactProvider{count: 100, events: []core.Event{{Type: core.EventCompleted, FinishReason: "stop"}}}
	engine := NewEngine(model, Options{InitialHistory: initial, Compactor: compactor})
	events := collectAgentEvents(t, engine.Run(context.Background(), "new question"))

	if len(events) != 3 || events[1].Type != core.EventWarning || events[1].Text == "" {
		t.Fatalf("events = %#v", events)
	}
	model.mu.Lock()
	request := model.request
	model.mu.Unlock()
	if len(request.Messages) != 3 || request.Messages[0].Content[0].Text != "old user" {
		t.Fatalf("provider request lost original history: %#v", request)
	}
}

func textHistory(texts ...string) []core.Message {
	messages := make([]core.Message, len(texts))
	for index, text := range texts {
		role := core.RoleUser
		if index%2 == 1 {
			role = core.RoleAssistant
		}
		messages[index] = core.Message{Role: role, Content: []core.ContentBlock{{Type: core.ContentText, Text: text}}}
	}
	return messages
}

type compactProvider struct {
	mu      sync.Mutex
	count   int
	events  []core.Event
	request core.Request
}

func (model *compactProvider) Name() string { return "compact" }
func (model *compactProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true, TokenCounting: true}, nil
}
func (model *compactProvider) CountTokens(context.Context, core.Request) (int, error) {
	return model.count, nil
}
func (model *compactProvider) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	model.mu.Lock()
	model.request = request
	events := append([]core.Event(nil), model.events...)
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
