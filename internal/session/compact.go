package session

import (
	"context"
	"fmt"

	"cyber-code/internal/core"
)

type SummarizeFunc func(context.Context, []core.Message) (string, error)

type CompactOptions struct {
	ThresholdTokens    int
	KeepRecentMessages int
	EstimateOnly       bool
	Estimate           func(core.Request) int
	Summarize          SummarizeFunc
}

type CompactResult struct {
	Applied         bool
	Messages        []core.Message
	Summary         *core.Message
	CoveredMessages int
	TokenCount      int
	Exact           bool
	Warning         string
}

type Compactor struct {
	options CompactOptions
}

func NewCompactor(options CompactOptions) (*Compactor, error) {
	if options.ThresholdTokens <= 0 {
		return nil, fmt.Errorf("compact token threshold must be positive")
	}
	if options.KeepRecentMessages <= 0 {
		options.KeepRecentMessages = 8
	}
	if options.Estimate == nil {
		options.Estimate = EstimateRequestTokens
	}
	if options.Summarize == nil {
		return nil, fmt.Errorf("compact summarizer is required")
	}
	return &Compactor{options: options}, nil
}

// ShouldCompact reports whether the configured token and history boundaries
// are currently crossed without invoking the summarizer.
func (compactor *Compactor) ShouldCompact(ctx context.Context, request core.Request, counter TokenCounter) bool {
	if compactor == nil {
		return false
	}
	if compactor.options.EstimateOnly {
		counter = nil
	}
	count := CountRequestTokens(ctx, counter, request, compactor.options.Estimate)
	return count.Tokens >= compactor.options.ThresholdTokens && compactor.CanCompact(request)
}

func (compactor *Compactor) Compact(ctx context.Context, request core.Request, counter TokenCounter) CompactResult {
	return compactor.compact(ctx, request, counter, false)
}

// CompactNow bypasses the legacy token threshold while retaining history
// boundaries. It is used after the context-window governance threshold crosses.
func (compactor *Compactor) CompactNow(ctx context.Context, request core.Request, counter TokenCounter) CompactResult {
	return compactor.compact(ctx, request, counter, true)
}

// CanCompact reports whether enough history exists to preserve the configured
// recent-message boundary.
func (compactor *Compactor) CanCompact(request core.Request) bool {
	return compactor != nil && compactBoundary(request.Messages, compactor.options.KeepRecentMessages) > 0
}

func (compactor *Compactor) compact(ctx context.Context, request core.Request, counter TokenCounter, force bool) CompactResult {
	original := cloneSessionMessages(request.Messages)
	if compactor.options.EstimateOnly {
		counter = nil
	}
	count := CountRequestTokens(ctx, counter, request, compactor.options.Estimate)
	result := CompactResult{Messages: original, TokenCount: count.Tokens, Exact: count.Exact}
	if !force && count.Tokens < compactor.options.ThresholdTokens {
		return result
	}
	boundary := compactBoundary(original, compactor.options.KeepRecentMessages)
	if boundary <= 0 {
		return result
	}
	covered := cloneSessionMessages(original[:boundary])
	summaryText, err := compactor.options.Summarize(ctx, covered)
	if err != nil {
		result.Warning = "conversation compaction failed: " + err.Error()
		return result
	}
	if summaryText == "" {
		result.Warning = "conversation compaction failed: summarizer returned an empty summary"
		return result
	}
	summary := core.Message{Role: core.RoleSystem, Content: []core.ContentBlock{{Type: core.ContentText, Text: summaryText}}}
	result.Applied = true
	result.Summary = &summary
	result.CoveredMessages = boundary
	result.Messages = append([]core.Message{summary}, cloneSessionMessages(original[boundary:])...)
	return result
}

func compactBoundary(messages []core.Message, keepRecent int) int {
	boundary := len(messages) - keepRecent
	if boundary <= 0 {
		return 0
	}
	for boundary > 0 && messages[boundary].Role == core.RoleTool {
		boundary--
	}
	return boundary
}

func cloneSessionMessages(messages []core.Message) []core.Message {
	if messages == nil {
		return nil
	}
	cloned := make([]core.Message, len(messages))
	for messageIndex, message := range messages {
		cloned[messageIndex].Role = message.Role
		cloned[messageIndex].Content = make([]core.ContentBlock, len(message.Content))
		for blockIndex, block := range message.Content {
			copy := core.ContentBlock{Type: block.Type, Text: block.Text, Thinking: block.Thinking}
			if block.ToolCall != nil {
				copy.ToolCall = &core.ToolCall{ID: block.ToolCall.ID, Name: block.ToolCall.Name, Arguments: append([]byte(nil), block.ToolCall.Arguments...)}
			}
			if block.ToolResult != nil {
				copy.ToolResult = cloneSessionToolResult(block.ToolResult)
			}
			cloned[messageIndex].Content[blockIndex] = copy
		}
	}
	return cloned
}

func cloneSessionToolResult(result *core.ToolResult) *core.ToolResult {
	cloned := &core.ToolResult{ToolCallID: result.ToolCallID, IsError: result.IsError, Content: make([]core.ContentBlock, len(result.Content))}
	for index, block := range result.Content {
		cloned.Content[index] = core.ContentBlock{Type: block.Type, Text: block.Text, Thinking: block.Thinking}
		if block.ToolResult != nil {
			cloned.Content[index].ToolResult = cloneSessionToolResult(block.ToolResult)
		}
	}
	return cloned
}
