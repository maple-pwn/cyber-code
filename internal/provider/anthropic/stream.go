package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"claude-code-go/internal/core"
)

type streamState struct {
	toolIDs   map[int]string
	completed bool
}

type streamEnvelope struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Usage usagePayload `json:"usage"`
	} `json:"message"`
	Usage        usagePayload `json:"usage"`
	ContentBlock struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Thinking string          `json:"thinking"`
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type usagePayload struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

func (client *Client) consumeStream(ctx context.Context, body io.ReadCloser, events chan<- core.Event) {
	defer close(events)
	defer body.Close()

	state := streamState{toolIDs: make(map[int]string)}
	emit := func(event core.Event) error {
		select {
		case events <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := scanSSE(ctx, body, client.responseLimit, func(eventName, data string) error {
		return state.handle(eventName, data, emit)
	})
	if ctx.Err() != nil || isContextError(err) {
		return
	}
	if err == nil && !state.completed {
		err = fmt.Errorf("Anthropic stream ended before message_stop")
	}
	if err != nil {
		_ = emit(core.Event{
			Type: core.EventError,
			Err: &core.Error{
				Kind:    core.ErrorKindProvider,
				Op:      "anthropic.stream",
				Message: "Anthropic stream was invalid or incomplete",
				Cause:   err,
			},
		})
	}
}

func (state *streamState) handle(eventName, data string, emit func(core.Event) error) error {
	if strings.TrimSpace(data) == "" {
		return nil
	}
	var envelope streamEnvelope
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return fmt.Errorf("decode %s event: %w", eventName, err)
	}
	eventType := envelope.Type
	if eventType == "" {
		eventType = eventName
	}

	switch eventType {
	case "ping", "content_block_stop":
		return nil
	case "message_start":
		return emit(core.Event{Type: core.EventUsage, Usage: envelope.Message.Usage.coreUsage()})
	case "content_block_start":
		switch envelope.ContentBlock.Type {
		case "text":
			if envelope.ContentBlock.Text != "" {
				return emit(core.Event{Type: core.EventTextDelta, Text: envelope.ContentBlock.Text})
			}
		case "thinking":
			if envelope.ContentBlock.Thinking != "" {
				return emit(core.Event{Type: core.EventThinkingDelta, Text: envelope.ContentBlock.Thinking})
			}
		case "tool_use":
			state.toolIDs[envelope.Index] = envelope.ContentBlock.ID
			arguments := envelope.ContentBlock.Input
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			return emit(core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{
				ID:        envelope.ContentBlock.ID,
				Name:      envelope.ContentBlock.Name,
				Arguments: arguments,
			}})
		}
		return nil
	case "content_block_delta":
		switch envelope.Delta.Type {
		case "text_delta":
			return emit(core.Event{Type: core.EventTextDelta, Text: envelope.Delta.Text})
		case "thinking_delta":
			return emit(core.Event{Type: core.EventThinkingDelta, Text: envelope.Delta.Thinking})
		case "input_json_delta":
			toolCallID, ok := state.toolIDs[envelope.Index]
			if !ok {
				return fmt.Errorf("tool arguments delta references unknown content block %d", envelope.Index)
			}
			return emit(core.Event{Type: core.EventToolArgumentsDelta, ToolCallID: toolCallID, ArgumentsDelta: envelope.Delta.PartialJSON})
		}
		return nil
	case "message_delta":
		return emit(core.Event{Type: core.EventUsage, Usage: envelope.Usage.coreUsage()})
	case "message_stop":
		state.completed = true
		return emit(core.Event{Type: core.EventCompleted})
	case "error":
		state.completed = true
		kind := core.ErrorKindProvider
		retryable := false
		switch envelope.Error.Type {
		case "authentication_error", "permission_error":
			kind = core.ErrorKindAuthentication
		case "rate_limit_error":
			kind = core.ErrorKindRateLimit
			retryable = true
		case "overloaded_error":
			retryable = true
		}
		return emit(core.Event{Type: core.EventError, Err: &core.Error{
			Kind:      kind,
			Op:        "anthropic.stream",
			Message:   envelope.Error.Message,
			Retryable: retryable,
		}})
	default:
		return nil
	}
}

func (usage usagePayload) coreUsage() *core.Usage {
	return &core.Usage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
	}
}

func scanSSE(ctx context.Context, reader io.Reader, responseLimit int64, handle func(eventName, data string) error) error {
	limited := &io.LimitedReader{R: reader, N: responseLimit + 1}
	maxToken := responseLimit
	if maxToken > 1<<20 {
		maxToken = 1 << 20
	}
	if maxToken < 1 {
		maxToken = 1
	}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, minInt64(maxToken, 64<<10)), int(maxToken))

	var eventName string
	var dataLines []string
	dispatch := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		err := handle(eventName, strings.Join(dataLines, "\n"))
		eventName = ""
		dataLines = dataLines[:0]
		return err
	}

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if limited.N <= 0 {
		return fmt.Errorf("response body exceeds %d bytes", responseLimit)
	}
	return dispatch()
}

func minInt64(left, right int64) int {
	if left < right {
		return int(left)
	}
	return int(right)
}
