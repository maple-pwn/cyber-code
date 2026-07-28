package agent

import (
	"context"
	"encoding/json"
	"sync"

	"claude-code-go/internal/core"
	"claude-code-go/internal/provider"
)

// Engine owns canonical conversation history and runs provider turns.
type Engine struct {
	provider provider.Provider
	options  Options

	mu      sync.RWMutex
	history []core.Message
	turn    chan struct{}
}

func NewEngine(modelProvider provider.Provider, options Options) *Engine {
	turn := make(chan struct{}, 1)
	turn <- struct{}{}
	return &Engine{provider: modelProvider, options: options, turn: turn}
}

// Run starts one provider turn. The returned channel closes only after the
// provider stream has stopped, including on cancellation.
func (e *Engine) Run(ctx context.Context, prompt string) <-chan core.Event {
	output := make(chan core.Event)
	go e.run(ctx, prompt, output)
	return output
}

func (e *Engine) run(ctx context.Context, prompt string, output chan<- core.Event) {
	defer close(output)
	select {
	case <-ctx.Done():
		return
	case <-e.turn:
	}
	defer func() { e.turn <- struct{}{} }()

	user := core.Message{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: prompt}}}
	messages := e.appendAndSnapshot(user)
	if !sendEvent(ctx, output, core.Event{Type: core.EventUserMessage, Message: messagePointer(user)}) {
		return
	}

	if e.provider == nil {
		sendEvent(ctx, output, providerError("agent.stream", "provider is not configured", nil))
		return
	}

	stream, err := e.provider.Stream(ctx, core.Request{Model: e.options.Model, Messages: messages})
	if err != nil {
		if ctx.Err() == nil {
			sendEvent(ctx, output, providerError("agent.stream", "provider stream could not be started", err))
		}
		return
	}
	if stream == nil {
		sendEvent(ctx, output, providerError("agent.stream", "provider returned a nil stream", nil))
		return
	}

	var assistant []core.ContentBlock
	terminal := false
	for event := range stream {
		if terminal {
			continue
		}

		switch event.Type {
		case core.EventTextDelta:
			assistant = appendDelta(assistant, core.ContentText, event.Text)
		case core.EventThinkingDelta:
			assistant = appendDelta(assistant, core.ContentThinking, event.Text)
		case core.EventCompleted:
			e.appendHistory(core.Message{Role: core.RoleAssistant, Content: assistant})
			terminal = true
		case core.EventError:
			terminal = true
		}

		if !sendEvent(ctx, output, event) {
			terminal = true
		}
	}
}

func sendEvent(ctx context.Context, output chan<- core.Event, event core.Event) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case output <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func providerError(op, message string, cause error) core.Event {
	return core.Event{Type: core.EventError, Err: &core.Error{
		Kind:    core.ErrorKindProvider,
		Op:      op,
		Message: message,
		Cause:   cause,
	}}
}

func appendDelta(blocks []core.ContentBlock, contentType core.ContentType, text string) []core.ContentBlock {
	if len(blocks) > 0 && blocks[len(blocks)-1].Type == contentType {
		if contentType == core.ContentThinking {
			blocks[len(blocks)-1].Thinking += text
		} else {
			blocks[len(blocks)-1].Text += text
		}
		return blocks
	}

	block := core.ContentBlock{Type: contentType}
	if contentType == core.ContentThinking {
		block.Thinking = text
	} else {
		block.Text = text
	}
	return append(blocks, block)
}

func (e *Engine) appendAndSnapshot(message core.Message) []core.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = append(e.history, cloneMessage(message))
	return cloneMessages(e.history)
}

func (e *Engine) appendHistory(message core.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = append(e.history, cloneMessage(message))
}

// History returns a deep copy that callers may mutate freely.
func (e *Engine) History() []core.Message {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return cloneMessages(e.history)
}

func cloneMessages(messages []core.Message) []core.Message {
	if messages == nil {
		return nil
	}
	cloned := make([]core.Message, len(messages))
	for index := range messages {
		cloned[index] = cloneMessage(messages[index])
	}
	return cloned
}

func cloneMessage(message core.Message) core.Message {
	cloned := core.Message{Role: message.Role}
	if message.Content != nil {
		cloned.Content = make([]core.ContentBlock, len(message.Content))
		for index := range message.Content {
			cloned.Content[index] = cloneContentBlock(message.Content[index])
		}
	}
	return cloned
}

func cloneContentBlock(block core.ContentBlock) core.ContentBlock {
	cloned := core.ContentBlock{Type: block.Type, Text: block.Text, Thinking: block.Thinking}
	if block.ToolCall != nil {
		cloned.ToolCall = &core.ToolCall{
			ID:        block.ToolCall.ID,
			Name:      block.ToolCall.Name,
			Arguments: cloneRawMessage(block.ToolCall.Arguments),
		}
	}
	if block.ToolResult != nil {
		cloned.ToolResult = &core.ToolResult{
			ToolCallID: block.ToolResult.ToolCallID,
			IsError:    block.ToolResult.IsError,
		}
		if block.ToolResult.Content != nil {
			cloned.ToolResult.Content = make([]core.ContentBlock, len(block.ToolResult.Content))
			for index := range block.ToolResult.Content {
				cloned.ToolResult.Content[index] = cloneContentBlock(block.ToolResult.Content[index])
			}
		}
	}
	return cloned
}

func cloneRawMessage(message json.RawMessage) json.RawMessage {
	if message == nil {
		return nil
	}
	return append(json.RawMessage(nil), message...)
}

func messagePointer(message core.Message) *core.Message {
	cloned := cloneMessage(message)
	return &cloned
}
