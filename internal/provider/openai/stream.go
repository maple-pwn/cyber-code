package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"claude-code-go/internal/core"
)

type streamEnvelope struct {
	Choices []choicePayload `json:"choices"`
	Usage   *usagePayload   `json:"usage"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}
type choicePayload struct {
	Delta        deltaPayload `json:"delta"`
	FinishReason string       `json:"finish_reason"`
}
type deltaPayload struct {
	Content   string          `json:"content"`
	ToolCalls []deltaToolCall `json:"tool_calls"`
}
type deltaToolCall struct {
	Index    int           `json:"index"`
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function deltaFunction `json:"function"`
}
type deltaFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type usagePayload struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}
type toolState struct {
	id, name  strings.Builder
	arguments strings.Builder
	emitted   bool
}

func (c *Client) consumeStream(ctx context.Context, body io.ReadCloser, events chan<- core.Event) {
	defer close(events)
	defer body.Close()
	tools := map[int]*toolState{}
	completed := false
	finishReason := ""
	emit := func(event core.Event) error {
		select {
		case events <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := scan(ctx, body, c.responseLimit, func(data string) error {
		if strings.TrimSpace(data) == "[DONE]" {
			completed = true
			return validateTools(tools)
		}
		var envelope streamEnvelope
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			return fmt.Errorf("decode stream event: %w", err)
		}
		if envelope.Error != nil {
			return emit(core.Event{Type: core.EventError, Err: &core.Error{Kind: core.ErrorKindProvider, Op: "openai.stream", Message: envelope.Error.Message}})
		}
		if envelope.Usage != nil {
			if err := emit(core.Event{Type: core.EventUsage, Usage: &core.Usage{InputTokens: envelope.Usage.PromptTokens, OutputTokens: envelope.Usage.CompletionTokens}}); err != nil {
				return err
			}
		}
		for _, choice := range envelope.Choices {
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			if choice.Delta.Content != "" {
				if err := emit(core.Event{Type: core.EventTextDelta, Text: choice.Delta.Content}); err != nil {
					return err
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				state := tools[call.Index]
				if state == nil {
					state = &toolState{}
					tools[call.Index] = state
				}
				state.id.WriteString(call.ID)
				state.name.WriteString(call.Function.Name)
				state.arguments.WriteString(call.Function.Arguments)
				if !state.emitted && state.id.Len() != 0 && state.name.Len() != 0 {
					state.emitted = true
					if err := emit(core.Event{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: state.id.String(), Name: state.name.String(), Arguments: json.RawMessage(`{}`)}}); err != nil {
						return err
					}
				}
				if call.Function.Arguments != "" {
					if !state.emitted {
						return fmt.Errorf("tool arguments arrived before ID and name for index %d", call.Index)
					}
					if err := emit(core.Event{Type: core.EventToolArgumentsDelta, ToolCallID: state.id.String(), ArgumentsDelta: call.Function.Arguments}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if ctx.Err() != nil {
		return
	}
	if err == nil && !completed {
		err = fmt.Errorf("OpenAI-compatible stream ended before [DONE]")
	}
	if err != nil {
		kind := core.ErrorKindProvider
		if strings.Contains(err.Error(), "invalid JSON") {
			kind = core.ErrorKindTool
		}
		_ = emit(core.Event{Type: core.EventError, Err: &core.Error{Kind: kind, Op: "openai.stream", Message: "OpenAI-compatible stream was invalid or incomplete", Cause: err}})
		return
	}
	_ = emit(core.Event{Type: core.EventCompleted, FinishReason: finishReason})
}

func validateTools(tools map[int]*toolState) error {
	for index, tool := range tools {
		if !tool.emitted || !json.Valid([]byte(tool.arguments.String())) {
			return fmt.Errorf("tool call %d has invalid JSON arguments", index)
		}
	}
	return nil
}
func scan(ctx context.Context, reader io.Reader, limit int64, handle func(string) error) error {
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	max := limit
	if max > 1<<20 {
		max = 1 << 20
	}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 64<<10), int(max))
	var data []string
	dispatch := func() error {
		if len(data) == 0 {
			return nil
		}
		result := handle(strings.Join(data, "\n"))
		data = data[:0]
		return result
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
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if limited.N <= 0 {
		return fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return dispatch()
}
