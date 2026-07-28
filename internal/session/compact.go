package session

import (
	"context"
	"fmt"

	"claude-code-go/internal/core"
)

type SummarizeFunc func(context.Context, []core.Message) (string, error)

type CompactOptions struct {
	ThresholdTokens    int
	KeepRecentMessages int
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

func (compactor *Compactor) Compact(ctx context.Context, request core.Request, counter TokenCounter) CompactResult {
	original := cloneSessionMessages(request.Messages)
	count := CountRequestTokens(ctx, counter, request, compactor.options.Estimate)
	result := CompactResult{Messages: original, TokenCount: count.Tokens, Exact: count.Exact}
	if count.Tokens < compactor.options.ThresholdTokens {
		return result
	}
	boundary := len(original) - compactor.options.KeepRecentMessages
	if boundary <= 0 {
		return result
	}
	for boundary > 0 && original[boundary].Role == core.RoleTool {
		boundary--
	}
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
