package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
	if len(events) < 3 || events[1].Type != core.EventWarning || events[2].Type != core.EventCompacted {
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

func TestEngineEmitsContextWarningBeforeCompactThreshold(t *testing.T) {
	builder := &governanceContextBuilder{ratio: func(int) float64 { return 0.75 }}
	model := &compactProvider{events: []core.Event{{Type: core.EventCompleted, FinishReason: "stop"}}}
	engine := NewEngine(model, Options{ContextBuilder: builder})
	events := collectAgentEvents(t, engine.Run(context.Background(), "near limit"))
	if len(events) != 3 || events[0].Type != core.EventUserMessage || events[1].Type != core.EventWarning || events[2].Type != core.EventCompleted {
		t.Fatalf("warning events = %#v", events)
	}
	if !strings.Contains(events[1].Text, "context") || !strings.Contains(events[1].Text, "75") {
		t.Fatalf("warning text = %q", events[1].Text)
	}
}

func TestEngineAutoCompactAttemptsOncePerThresholdCrossing(t *testing.T) {
	attempts := 0
	compactor, err := session.NewCompactor(session.CompactOptions{
		ThresholdTokens: 1, KeepRecentMessages: 1,
		Summarize: func(context.Context, []core.Message) (string, error) {
			attempts++
			return "", errors.New("summary unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	builder := &governanceContextBuilder{ratio: func(messages int) float64 {
		if messages >= 3 {
			return 0.95
		}
		return 0.20
	}}
	model := &compactProvider{count: 100, events: []core.Event{{Type: core.EventCompleted, FinishReason: "stop"}}}
	engine := NewEngine(model, Options{InitialHistory: textHistory("old user", "old assistant"), ContextBuilder: builder, Compactor: compactor})
	collectAgentEvents(t, engine.Run(context.Background(), "first crossing"))
	collectAgentEvents(t, engine.Run(context.Background(), "still above"))
	if attempts != 1 {
		t.Fatalf("compact attempts while continuously above threshold = %d", attempts)
	}
	if err := engine.ReplaceHistory(context.Background(), textHistory("replacement user", "replacement assistant")); err != nil {
		t.Fatal(err)
	}
	collectAgentEvents(t, engine.Run(context.Background(), "second crossing"))
	if attempts != 2 {
		t.Fatalf("compact attempts after a second crossing = %d", attempts)
	}
}

func TestEngineAutoCompactIsCanceledWithTurn(t *testing.T) {
	started := make(chan struct{})
	compactor, err := session.NewCompactor(session.CompactOptions{
		ThresholdTokens: 1, KeepRecentMessages: 1,
		Summarize: func(ctx context.Context, _ []core.Message) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	builder := &governanceContextBuilder{ratio: func(int) float64 { return 0.95 }}
	engine := NewEngine(&compactProvider{count: 100}, Options{
		InitialHistory: textHistory("old user", "old assistant"), ContextBuilder: builder, Compactor: compactor,
	})
	ctx, cancel := context.WithCancel(context.Background())
	output := engine.Run(ctx, "cancel compact")
	if event := <-output; event.Type != core.EventUserMessage {
		t.Fatalf("first event = %#v", event)
	}
	if event := <-output; event.Type != core.EventWarning {
		t.Fatalf("second event = %#v", event)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("automatic compaction did not start")
	}
	cancel()
	for range output {
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("turn context error = %v", ctx.Err())
	}
}

type governanceContextBuilder struct {
	ratio func(int) float64
}

func (builder *governanceContextBuilder) Build(_ context.Context, input contextbuilder.BuildInput) (contextbuilder.Plan, error) {
	ratio := builder.ratio(len(input.Messages))
	return contextbuilder.Plan{
		Messages:        input.Messages,
		Tools:           input.Tools,
		MaxOutputTokens: 128,
		EstimatedTokens: int(ratio * 1000),
		Budget: contextbuilder.BudgetMetadata{
			ContextWindow: 1100, ReservedOutput: 100, InputLimit: 1000,
			UtilizationRatio: ratio, WarningThreshold: 0.70, CompactThreshold: 0.90,
			WarningExceeded: ratio >= 0.70, CompactExceeded: ratio >= 0.90,
		},
	}, nil
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
