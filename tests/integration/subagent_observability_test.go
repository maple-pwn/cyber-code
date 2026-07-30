package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cyber-code/internal/agent"
	"cyber-code/internal/config"
	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/provider"
	"cyber-code/internal/provider/anthropic"
	"cyber-code/internal/provider/openai"
	"cyber-code/internal/provider/testkit"
	runtimepkg "cyber-code/internal/runtime"
	"cyber-code/internal/tasks"
	toolpkg "cyber-code/internal/tool"
)

func TestSubagentObservabilityOpenAICompatible(t *testing.T) {
	t.Setenv("SUBAGENT_OPENAI_KEY", "secret")
	server := testkit.NewServer(
		openAIFrame(t, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"task-call","type":"function","function":{"name":"task_run","arguments":"{\"prompt\":\"inspect child\",\"description\":\"child trace\",\"max_turns\":2,\"permission_mode\":\"plan\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`),
		openAIFrame(t, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"probe-a","type":"function","function":{"name":"probe","arguments":"{\"name\":\"a\"}"}},{"index":1,"id":"probe-b","type":"function","function":{"name":"probe","arguments":"{\"name\":\"b\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":2}}`),
		openAIFrame(t, `{"choices":[{"delta":{"content":"child result"},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":3}}`),
		openAIFrame(t, `{"choices":[{"delta":{"content":"parent done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":2}}`),
	)
	defer server.Close()
	client, err := openai.New(config.Profile{
		Provider: "openai-compatible", BaseURL: server.URL(), Model: "test", APIKeyEnv: "SUBAGENT_OPENAI_KEY",
	}, openai.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	runProviderSubagentObservation(t, client)
	assertSubagentRequestBoundaries(t, server.Requests())
}

func TestSubagentObservabilityAnthropic(t *testing.T) {
	t.Setenv("SUBAGENT_ANTHROPIC_KEY", "secret")
	server := testkit.NewServer(
		anthropicFrames(t,
			[]string{"message_start", "content_block_start", "message_delta", "message_stop"},
			[]string{
				`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"task-call","name":"task_run","input":{"prompt":"inspect child","description":"child trace","max_turns":2,"permission_mode":"plan"}}}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}}`,
				`{"type":"message_stop"}`,
			}),
		anthropicFrames(t,
			[]string{"message_start", "content_block_start", "content_block_start", "message_delta", "message_stop"},
			[]string{
				`{"type":"message_start","message":{"usage":{"input_tokens":7}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"probe-a","name":"probe","input":{"name":"a"}}}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"probe-b","name":"probe","input":{"name":"b"}}}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
				`{"type":"message_stop"}`,
			}),
		anthropicFrames(t,
			[]string{"message_start", "content_block_delta", "message_delta", "message_stop"},
			[]string{
				`{"type":"message_start","message":{"usage":{"input_tokens":9}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"child result"}}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
				`{"type":"message_stop"}`,
			}),
		anthropicFrames(t,
			[]string{"message_start", "content_block_delta", "message_delta", "message_stop"},
			[]string{
				`{"type":"message_start","message":{"usage":{"input_tokens":11}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"parent done"}}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
				`{"type":"message_stop"}`,
			}),
	)
	defer server.Close()
	client, err := anthropic.New(config.Profile{
		Provider: "anthropic", BaseURL: server.URL(), Model: "test", APIKeyEnv: "SUBAGENT_ANTHROPIC_KEY",
	}, anthropic.WithHTTPClient(server.Client()), anthropic.WithRetryPolicy(1, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	runProviderSubagentObservation(t, client)
	assertSubagentRequestBoundaries(t, server.Requests())
}

func runProviderSubagentObservation(t *testing.T, modelProvider provider.Provider) {
	t.Helper()
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeBypass, ModeSource: permissions.SourceCliArg})
	if err != nil {
		t.Fatal(err)
	}
	probe := newObservationProbe()
	childRegistry := toolpkg.NewRegistry()
	if err := childRegistry.Register(probe); err != nil {
		t.Fatal(err)
	}
	parentRegistry := childRegistry.Clone()
	service, err := tasks.NewToolService(tasks.ToolServiceOptions{
		ParentMode: permissions.PermissionModeBypass, ParentMaxTurns: 2,
		Execute: func(ctx context.Context, request tasks.AgentRequest) (any, error) {
			childRunner := toolpkg.NewRunner(childRegistry, broker, toolpkg.RunnerOptions{})
			child, err := tasks.NewSubAgent(tasks.SubAgentOptions{
				Provider: modelProvider,
				AgentOptions: agent.Options{
					Model: "test", MaxTurns: request.MaxTurns, Tools: childRegistry, ToolRunner: childRunner, SessionID: request.TaskID,
				},
				ParentMode: permissions.PermissionModeBypass, Mode: request.Mode,
				ParentMaxTurns: 2, MaxTurns: request.MaxTurns, ParentBroker: broker,
			})
			if err != nil {
				return nil, err
			}
			var output strings.Builder
			for event := range child.Engine.Run(ctx, request.Prompt) {
				if err := request.Emit(event); err != nil {
					return nil, err
				}
				if event.Type == core.EventTextDelta {
					output.WriteString(event.Text)
				}
				if event.Type == core.EventError {
					return nil, event.Err
				}
			}
			return output.String(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.RegisterTools(parentRegistry, service); err != nil {
		t.Fatal(err)
	}
	parentRunner := toolpkg.NewRunner(parentRegistry, broker, toolpkg.RunnerOptions{})
	runtime := runtimepkg.New(modelProvider, agent.Options{
		Model: "test", MaxTurns: 2, Tools: parentRegistry, ToolRunner: parentRunner,
	}, service)
	defer runtime.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observed := make(chan []core.Event, 1)
	go func() { observed <- collectSubagentObservations(runtime.Observe(ctx)) }()
	var parentText strings.Builder
	for event := range runtime.Run(ctx, "delegate") {
		if event.Type == core.EventTextDelta {
			parentText.WriteString(event.Text)
		}
		if event.Type == core.EventError {
			t.Errorf("parent event error: %v", event.Err)
		}
	}
	if parentText.String() != "parent done" {
		t.Fatalf("parent result = %q", parentText.String())
	}
	var observations []core.Event
	select {
	case observations = <-observed:
	case <-ctx.Done():
		t.Fatalf("observation stream timed out: %v", ctx.Err())
	}
	assertCompleteSubagentObservation(t, observations)
	if probe.maximum.Load() != 2 {
		t.Fatalf("parallel child tool activity = %d, want 2", probe.maximum.Load())
	}
}

func collectSubagentObservations(stream <-chan core.Event) []core.Event {
	var events []core.Event
	for event := range stream {
		events = append(events, event)
		if event.Type == core.EventSubagentStatus && event.Subagent != nil {
			switch event.Subagent.Status {
			case string(tasks.TaskStatusCompleted), string(tasks.TaskStatusFailed), string(tasks.TaskStatusCancelled):
				return events
			}
		}
	}
	return events
}

func assertCompleteSubagentObservation(t *testing.T, events []core.Event) {
	t.Helper()
	if len(events) < 3 || events[0].Type != core.EventSubagentStarted || events[1].Type != core.EventSubagentStatus {
		t.Fatalf("lifecycle prefix = %#v", events)
	}
	last := events[len(events)-1]
	if last.Type != core.EventSubagentStatus || last.Subagent == nil || last.Subagent.Status != string(tasks.TaskStatusCompleted) {
		t.Fatalf("terminal observation = %#v", last)
	}
	counts := make(map[core.EventType]int)
	var childText strings.Builder
	lastToolCall, firstToolResult, textIndex, completedIndex := -1, -1, -1, -1
	for index, observation := range events {
		if observation.Type != core.EventSubagentEvent || observation.Subagent == nil || observation.Subagent.Event == nil {
			continue
		}
		event := observation.Subagent.Event
		counts[event.Type]++
		switch event.Type {
		case core.EventToolCall:
			lastToolCall = index
		case core.EventToolResult:
			if firstToolResult < 0 {
				firstToolResult = index
			}
		case core.EventTextDelta:
			childText.WriteString(event.Text)
			textIndex = index
		case core.EventCompleted:
			completedIndex = index
		}
	}
	if counts[core.EventUserMessage] != 1 || counts[core.EventToolCall] != 2 || counts[core.EventToolResult] != 2 || counts[core.EventUsage] < 2 || childText.String() != "child result" {
		t.Fatalf("incomplete nested observations: counts=%#v text=%q events=%#v", counts, childText.String(), events)
	}
	if !(lastToolCall < firstToolResult && firstToolResult < textIndex && textIndex < completedIndex) {
		t.Fatalf("nested event order changed: call=%d result=%d text=%d completed=%d", lastToolCall, firstToolResult, textIndex, completedIndex)
	}
}

func assertSubagentRequestBoundaries(t *testing.T, requests []testkit.CapturedRequest) {
	t.Helper()
	if len(requests) != 4 {
		t.Fatalf("provider requests = %d, want 4", len(requests))
	}
	if !bytes.Contains(requests[0].Body, []byte("task_run")) || !bytes.Contains(requests[1].Body, []byte("inspect child")) {
		t.Fatalf("parent/child request boundary missing: %s / %s", requests[0].Body, requests[1].Body)
	}
	if !bytes.Contains(requests[2].Body, []byte("probe-a")) || !bytes.Contains(requests[2].Body, []byte("probe-b")) || !bytes.Contains(requests[3].Body, []byte("child result")) {
		t.Fatalf("tool/result request boundary missing: %s / %s", requests[2].Body, requests[3].Body)
	}
}

func openAIFrame(t *testing.T, payload string) http.HandlerFunc {
	t.Helper()
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		if err := testkit.WriteSSE(response, "", payload); err != nil {
			t.Error(err)
		}
		if err := testkit.WriteSSE(response, "", "[DONE]"); err != nil {
			t.Error(err)
		}
	}
}

func anthropicFrames(t *testing.T, names, payloads []string) http.HandlerFunc {
	t.Helper()
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		for index := range names {
			if err := testkit.WriteSSE(response, names[index], payloads[index]); err != nil {
				t.Error(err)
			}
		}
	}
}

type observationProbe struct {
	started atomic.Int32
	active  atomic.Int32
	maximum atomic.Int32
	release chan struct{}
	once    sync.Once
}

func newObservationProbe() *observationProbe { return &observationProbe{release: make(chan struct{})} }

func (*observationProbe) Spec() toolpkg.Spec {
	return toolpkg.Spec{
		Name: "probe", Description: "bounded integration probe", Schema: json.RawMessage(`{"type":"object"}`),
		ReadOnly: true, ConcurrencySafe: true,
	}
}

func (*observationProbe) Authorize(context.Context, json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Tool: "probe", Action: permissions.ActionRead}, nil
}

func (probe *observationProbe) Run(ctx context.Context, _ json.RawMessage) (core.ToolResult, error) {
	active := probe.active.Add(1)
	defer probe.active.Add(-1)
	for {
		maximum := probe.maximum.Load()
		if active <= maximum || probe.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	if probe.started.Add(1) == 2 {
		probe.once.Do(func() { close(probe.release) })
	}
	select {
	case <-probe.release:
		return core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentText, Text: fmt.Sprintf("probe %d ok", active)}}}, nil
	case <-ctx.Done():
		return core.ToolResult{}, ctx.Err()
	}
}
