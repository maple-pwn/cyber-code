package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"cyber-code/internal/contextbuilder"
	"cyber-code/internal/core"
	"cyber-code/internal/provider"
	"cyber-code/internal/session"
)

// Engine owns canonical conversation history and runs provider turns.
type Engine struct {
	provider provider.Provider
	options  Options
	context  ContextBuilder

	mu      sync.RWMutex
	history []core.Message
	turn    chan struct{}
}

func NewEngine(modelProvider provider.Provider, options Options) *Engine {
	turn := make(chan struct{}, 1)
	turn <- struct{}{}
	builder := options.ContextBuilder
	if builder == nil {
		builder, _ = contextbuilder.New(contextbuilder.Options{})
	}
	return &Engine{provider: modelProvider, options: options, context: builder, history: cloneMessages(options.InitialHistory), turn: turn}
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

	tools := e.toolDefinitions()
	if e.options.Compactor != nil {
		request, err := e.request(ctx, messages, tools, true)
		if err != nil {
			sendEvent(ctx, output, contextBuildError(err))
			return
		}
		result := e.options.Compactor.Compact(ctx, request, e.provider)
		if result.Warning != "" {
			if !sendEvent(ctx, output, core.Event{Type: core.EventWarning, Text: result.Warning}) {
				return
			}
		} else if result.Applied {
			e.replaceHistory(result.Messages)
			messages = e.History()
			if !sendEvent(ctx, output, core.Event{
				Type: core.EventCompacted, Message: messagePointer(*result.Summary), CoveredMessages: result.CoveredMessages,
			}) {
				return
			}
		}
	}

	maximumTurns := e.options.MaxTurns
	if maximumTurns <= 0 {
		maximumTurns = 1
	}
	for turn := 1; turn <= maximumTurns; turn++ {
		request, err := e.request(ctx, messages, tools, false)
		if err != nil {
			sendEvent(ctx, output, contextBuildError(err))
			return
		}
		round, ok := e.providerRound(ctx, request, output)
		if !ok {
			return
		}
		e.appendHistory(core.Message{Role: core.RoleAssistant, Content: round.content})
		if len(round.calls) == 0 {
			sendEvent(ctx, output, round.completed)
			return
		}

		results := e.executeToolCalls(ctx, round.calls)
		toolContent := make([]core.ContentBlock, len(results))
		for index := range results {
			result := results[index]
			toolContent[index] = core.ContentBlock{Type: core.ContentToolResult, ToolResult: &result}
			if !sendEvent(ctx, output, core.Event{Type: core.EventToolResult, ToolCallID: result.ToolCallID, ToolResult: &result}) {
				return
			}
		}
		e.appendHistory(core.Message{Role: core.RoleTool, Content: toolContent})
		if turn == maximumTurns {
			sendEvent(ctx, output, core.Event{Type: core.EventCompleted, FinishReason: "max_turns"})
			return
		}
		messages = e.History()
	}
}

func (e *Engine) request(ctx context.Context, messages []core.Message, tools []core.ToolDefinition, allowOverBudget bool) (core.Request, error) {
	plan, err := e.context.Build(ctx, contextbuilder.BuildInput{
		Model: e.options.Model, Messages: messages, Tools: tools, AllowOverBudget: allowOverBudget,
	})
	if err != nil {
		return core.Request{}, err
	}
	return core.Request{
		Model: e.options.Model, System: plan.System, Messages: plan.Messages,
		Tools: plan.Tools, MaxTokens: plan.MaxOutputTokens,
	}, nil
}

func contextBuildError(cause error) core.Event {
	return core.Event{Type: core.EventError, Err: &core.Error{
		Kind: core.ErrorKindConfiguration, Op: "agent.context", Message: "context could not be assembled", Cause: cause,
	}}
}

type providerRound struct {
	content   []core.ContentBlock
	calls     []*toolCallState
	completed core.Event
}

type toolCallState struct {
	call        core.ToolCall
	initial     json.RawMessage
	arguments   strings.Builder
	hasArgument bool
}

func (e *Engine) providerRound(ctx context.Context, request core.Request, output chan<- core.Event) (providerRound, bool) {
	stream, err := e.provider.Stream(ctx, request)
	if err != nil {
		if ctx.Err() == nil {
			sendEvent(ctx, output, providerError("agent.stream", "provider stream could not be started", err))
		}
		return providerRound{}, false
	}
	if stream == nil {
		sendEvent(ctx, output, providerError("agent.stream", "provider returned a nil stream", nil))
		return providerRound{}, false
	}

	round := providerRound{}
	byID := make(map[string]*toolCallState)
	terminal := false
	failed := false
	for event := range stream {
		if terminal {
			continue
		}
		switch event.Type {
		case core.EventTextDelta:
			round.content = appendDelta(round.content, core.ContentText, event.Text)
		case core.EventThinkingDelta:
			round.content = appendDelta(round.content, core.ContentThinking, event.Text)
		case core.EventToolCall:
			if event.ToolCall == nil || event.ToolCall.ID == "" || event.ToolCall.Name == "" || byID[event.ToolCall.ID] != nil {
				sendEvent(ctx, output, toolStreamError("invalid or duplicate tool call"))
				terminal = true
				failed = true
				continue
			}
			state := &toolCallState{call: core.ToolCall{ID: event.ToolCall.ID, Name: event.ToolCall.Name}, initial: cloneRawMessage(event.ToolCall.Arguments)}
			byID[state.call.ID] = state
			round.calls = append(round.calls, state)
			round.content = append(round.content, core.ContentBlock{Type: core.ContentToolCall, ToolCall: &state.call})
		case core.EventToolArgumentsDelta:
			state := byID[event.ToolCallID]
			if state == nil {
				sendEvent(ctx, output, toolStreamError("tool arguments referenced an unknown call"))
				terminal = true
				failed = true
				continue
			}
			state.hasArgument = true
			state.arguments.WriteString(event.ArgumentsDelta)
		case core.EventCompleted:
			round.completed = event
			terminal = true
		case core.EventError:
			sendEvent(ctx, output, event)
			terminal = true
			failed = true
			continue
		}
		if event.Type != core.EventCompleted && !sendEvent(ctx, output, event) {
			terminal = true
		}
	}
	if ctx.Err() != nil || failed || round.completed.Type != core.EventCompleted {
		if ctx.Err() == nil && round.completed.Type == "" {
			if failed {
				return providerRound{}, false
			}
			sendEvent(ctx, output, providerError("agent.stream", "provider stream ended without completion", nil))
		}
		return providerRound{}, false
	}
	for _, state := range round.calls {
		arguments := state.initial
		if state.hasArgument {
			arguments = json.RawMessage(state.arguments.String())
		}
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		if !json.Valid(arguments) {
			sendEvent(ctx, output, toolStreamError("tool call arguments were invalid JSON"))
			return providerRound{}, false
		}
		state.call.Arguments = cloneRawMessage(arguments)
	}
	return round, true
}

func toolStreamError(message string) core.Event {
	return core.Event{Type: core.EventError, Err: &core.Error{Kind: core.ErrorKindTool, Op: "agent.tool_stream", Message: message}}
}

func (e *Engine) toolDefinitions() []core.ToolDefinition {
	if e.options.Tools == nil {
		return nil
	}
	specs := e.options.Tools.Specs()
	definitions := make([]core.ToolDefinition, len(specs))
	for index, spec := range specs {
		definitions[index] = core.ToolDefinition{Name: spec.Name, Description: spec.Description, InputSchema: cloneRawMessage(spec.Schema)}
	}
	return definitions
}

func (e *Engine) executeToolCalls(ctx context.Context, calls []*toolCallState) []core.ToolResult {
	results := make([]core.ToolResult, len(calls))
	for start := 0; start < len(calls); {
		end := start
		for end < len(calls) && e.concurrentTool(calls[end].call.Name) {
			end++
		}
		if end > start {
			var wait sync.WaitGroup
			for index := start; index < end; index++ {
				wait.Add(1)
				go func(index int) {
					defer wait.Done()
					results[index] = e.executeTool(ctx, calls[index].call)
				}(index)
			}
			wait.Wait()
			start = end
			continue
		}
		results[start] = e.executeTool(ctx, calls[start].call)
		start++
	}
	return results
}

func (e *Engine) concurrentTool(name string) bool {
	if e.options.Tools == nil {
		return false
	}
	registered, ok := e.options.Tools.Get(name)
	if !ok {
		return false
	}
	spec := registered.Spec()
	return spec.ReadOnly && spec.ConcurrencySafe
}

func (e *Engine) executeTool(ctx context.Context, call core.ToolCall) core.ToolResult {
	result := core.ToolResult{ToolCallID: call.ID}
	if e.options.ToolRunner == nil {
		result.IsError = true
		result.Content = []core.ContentBlock{{Type: core.ContentText, Text: "tool runner is unavailable"}}
		return result
	}
	executed, err := e.options.ToolRunner.Run(ctx, call.Name, call.Arguments)
	if err != nil {
		result.IsError = true
		result.Content = []core.ContentBlock{{Type: core.ContentText, Text: err.Error()}}
		return result
	}
	executed.ToolCallID = call.ID
	return executed
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

func (e *Engine) replaceHistory(messages []core.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history = cloneMessages(messages)
}

// History returns a deep copy that callers may mutate freely.
func (e *Engine) History() []core.Message {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return cloneMessages(e.history)
}

// Compact applies the configured conversation compactor outside a provider
// turn. The engine turn lease prevents concurrent history replacement.
func (e *Engine) Compact(ctx context.Context) (session.CompactResult, error) {
	if e.options.Compactor == nil {
		return session.CompactResult{}, fmt.Errorf("conversation compactor is not configured")
	}
	select {
	case <-ctx.Done():
		return session.CompactResult{}, ctx.Err()
	case <-e.turn:
	}
	defer func() { e.turn <- struct{}{} }()
	messages := e.History()
	request, err := e.request(ctx, messages, e.toolDefinitions(), true)
	if err != nil {
		return session.CompactResult{}, err
	}
	result := e.options.Compactor.Compact(ctx, request, e.provider)
	if result.Warning != "" {
		return result, nil
	}
	if result.Applied {
		e.replaceHistory(result.Messages)
	}
	return result, nil
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
