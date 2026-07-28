package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"claude-code-go/internal/core"
)

func TestCompactUsesExactCountAndPreservesRecentToolContext(t *testing.T) {
	estimatorCalls := 0
	counter := &tokenCounter{count: 120}
	compactor, err := NewCompactor(CompactOptions{
		ThresholdTokens:    100,
		KeepRecentMessages: 2,
		Estimate: func(core.Request) int {
			estimatorCalls++
			return 999
		},
		Summarize: func(_ context.Context, messages []core.Message) (string, error) {
			if len(messages) != 2 {
				t.Fatalf("messages to summarize = %#v", messages)
			}
			messages[0].Content[0].Text = "summarizer mutation"
			return "old conversation summary", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	original := compactFixtureMessages()
	result := compactor.Compact(context.Background(), core.Request{Model: "priced-model", Messages: original}, counter)

	if !result.Applied || result.TokenCount != 120 || !result.Exact || estimatorCalls != 0 || counter.calls != 1 {
		t.Fatalf("compact result = %#v, estimator calls = %d, counter calls = %d", result, estimatorCalls, counter.calls)
	}
	if result.CoveredMessages != 2 || len(result.Messages) != 4 {
		t.Fatalf("compact result messages = %#v", result)
	}
	if result.Messages[0].Role != core.RoleSystem || result.Messages[0].Content[0].Text != "old conversation summary" {
		t.Fatalf("summary message = %#v", result.Messages[0])
	}
	if result.Messages[1].Content[0].ToolCall == nil || result.Messages[2].Role != core.RoleTool || result.Messages[3].Content[0].Text != "recent question" {
		t.Fatalf("recent tool context was not preserved: %#v", result.Messages)
	}
	if original[0].Content[0].Text != "old question" {
		t.Fatalf("compactor mutated input: %#v", original)
	}
}

func TestCompactFailureKeepsOriginalHistoryAndReturnsWarning(t *testing.T) {
	compactor, err := NewCompactor(CompactOptions{
		ThresholdTokens:    1,
		KeepRecentMessages: 2,
		Estimate:           func(core.Request) int { return 10 },
		Summarize: func(context.Context, []core.Message) (string, error) {
			return "", errors.New("summary unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	original := compactFixtureMessages()
	result := compactor.Compact(context.Background(), core.Request{Messages: original}, &tokenCounter{err: errors.New("no exact count")})
	if result.Applied || result.Warning == "" || !reflect.DeepEqual(result.Messages, original) {
		t.Fatalf("compact failure result = %#v", result)
	}
}

func TestCompactEventIsDerivedWithoutDeletingRawLog(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, text := range []string{"original one", "original two"} {
		if _, err := store.Append(ctx, "compact-session", core.Event{Type: core.EventTextDelta, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	summary := core.Message{Role: core.RoleSystem, Content: []core.ContentBlock{{Type: core.ContentText, Text: "derived summary"}}}
	record, err := store.Append(ctx, "compact-session", core.Event{Type: core.EventCompacted, Message: &summary})
	if err != nil {
		t.Fatal(err)
	}
	if record.Event.CoveredSequence != 2 {
		t.Fatalf("covered sequence = %d", record.Event.CoveredSequence)
	}
	records, err := store.Events(ctx, "compact-session")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || records[0].Event.Text != "original one" || records[1].Event.Text != "original two" {
		t.Fatalf("raw log changed during compaction: %#v", records)
	}
}

func TestUsagePrefersExactCountAndCalculatesConfiguredPrice(t *testing.T) {
	estimatorCalls := 0
	counter := &tokenCounter{count: 42}
	count := CountRequestTokens(context.Background(), counter, core.Request{}, func(core.Request) int {
		estimatorCalls++
		return 900
	})
	if count.Tokens != 42 || !count.Exact || estimatorCalls != 0 {
		t.Fatalf("token count = %#v, estimator calls = %d", count, estimatorCalls)
	}
	pricing := ModelPricing{InputPerMillion: 3, OutputPerMillion: 15, CacheReadPerMillion: 0.30, CacheWritePerMillion: 3.75}
	table := PricingTable{"priced-model": pricing}
	cost, err := table.Cost("priced-model", core.Usage{InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadInputTokens: 500_000, CacheCreationInputTokens: 200_000})
	if err != nil {
		t.Fatal(err)
	}
	if cost != 5.4 {
		t.Fatalf("cost = %v", cost)
	}
	if _, err := table.Cost("unknown-model", core.Usage{}); err == nil {
		t.Fatal("unknown model pricing was accepted")
	}
}

func compactFixtureMessages() []core.Message {
	return []core.Message{
		{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "old question"}}},
		{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentText, Text: "old answer"}}},
		{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{}`)}}}},
		{Role: core.RoleTool, Content: []core.ContentBlock{{Type: core.ContentToolResult, ToolResult: &core.ToolResult{ToolCallID: "call-1", Content: []core.ContentBlock{{Type: core.ContentText, Text: "result"}}}}}},
		{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "recent question"}}},
	}
}

type tokenCounter struct {
	count int
	err   error
	calls int
}

func (counter *tokenCounter) CountTokens(context.Context, core.Request) (int, error) {
	counter.calls++
	return counter.count, counter.err
}
