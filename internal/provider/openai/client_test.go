package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"claude-code-go/internal/config"
	"claude-code-go/internal/core"
	"claude-code-go/internal/provider/openai"
	"claude-code-go/internal/provider/testkit"
)

func TestStreamMapsOpenAIRequestAndParallelToolCalls(t *testing.T) {
	t.Setenv("TEST_OPENAI_API_KEY", "openai-secret")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/proxy/v1/chat/completions" {
			t.Errorf("path = %q, want reverse-proxy prefix preserved", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer openai-secret" {
			t.Errorf("authorization = %q", got)
		}
		var payload struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Type     string `json:"type"`
				Function struct {
					Name       string          `json:"name"`
					Parameters json.RawMessage `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload.Model != "gpt-test" || !payload.Stream || len(payload.Messages) != 2 || len(payload.Tools) != 1 {
			t.Errorf("unexpected request: %#v", payload)
		}
		if payload.Messages[0].Role != "system" || payload.Messages[0].Content != "be helpful" || payload.Tools[0].Type != "function" || payload.Tools[0].Function.Name != "read_file" {
			t.Errorf("request mapping changed: %#v", payload)
		}

		response.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, response, `{"choices":[{"delta":{"content":"hello "}}]}`)
		writeSSE(t, response, `{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call-b","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"b"}},{"index":0,"id":"call-a","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a"}}]}}]}`)
		writeSSE(t, response, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":".md\"}"}},{"index":1,"function":{"arguments":".md\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9,"completion_tokens":4}}`)
		writeSSE(t, response, `[DONE]`)
	})
	defer server.Close()

	client := newTestClient(t, server)
	stream, err := client.Stream(context.Background(), core.Request{
		System:   []core.ContentBlock{{Type: core.ContentText, Text: "be helpful"}},
		Messages: []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "read it"}}}},
		Tools:    []core.ToolDefinition{{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	events := collectEvents(t, stream)
	want := []core.EventType{core.EventTextDelta, core.EventToolCall, core.EventToolArgumentsDelta, core.EventToolCall, core.EventToolArgumentsDelta, core.EventUsage, core.EventToolArgumentsDelta, core.EventToolArgumentsDelta, core.EventCompleted}
	if len(events) != len(want) {
		t.Fatalf("events = %#v", events)
	}
	for i, eventType := range want {
		if events[i].Type != eventType {
			t.Fatalf("event %d = %q, want %q", i, events[i].Type, eventType)
		}
	}
	if events[1].ToolCall.ID != "call-b" || events[3].ToolCall.ID != "call-a" {
		t.Fatalf("tool call ordering/IDs changed: %#v", events)
	}
	if events[5].Usage.InputTokens != 9 || events[5].Usage.OutputTokens != 4 {
		t.Fatalf("usage = %#v", events[5].Usage)
	}
	if events[8].FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", events[8].FinishReason)
	}
}

func TestStreamRejectsInvalidFinalToolArguments(t *testing.T) {
	t.Setenv("TEST_OPENAI_API_KEY", "openai-secret")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, response, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"read_file","arguments":"not-json"}}]}}]}`)
		writeSSE(t, response, `[DONE]`)
	})
	defer server.Close()

	stream, err := newTestClient(t, server).Stream(context.Background(), core.Request{})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	events := collectEvents(t, stream)
	if len(events) != 3 || events[2].Type != core.EventError || events[2].Err == nil || events[2].Err.Kind != core.ErrorKindTool {
		t.Fatalf("invalid tool arguments were accepted: %#v", events)
	}
}

func TestCapabilitiesRejectsToolRequestsWhenDisabled(t *testing.T) {
	t.Setenv("TEST_OPENAI_API_KEY", "openai-secret")
	client, err := openai.New(config.Profile{Provider: "openai", BaseURL: "http://example.test", Model: "gpt-test", APIKeyEnv: "TEST_OPENAI_API_KEY"}, openai.WithToolCalls(false))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Stream(context.Background(), core.Request{Tools: []core.ToolDefinition{{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}}}); err == nil {
		t.Fatal("expected explicit unsupported-tool error")
	}
}

func newTestClient(t *testing.T, server *testkit.Server) *openai.Client {
	t.Helper()
	client, err := openai.New(config.Profile{Provider: "openai", BaseURL: server.URL() + "/proxy", Model: "gpt-test", APIKeyEnv: "TEST_OPENAI_API_KEY"}, openai.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func writeSSE(t *testing.T, response http.ResponseWriter, data string) {
	t.Helper()
	if err := testkit.WriteSSE(response, "", data); err != nil {
		t.Fatal(err)
	}
}

func collectEvents(t *testing.T, stream <-chan core.Event) []core.Event {
	t.Helper()
	var result []core.Event
	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				return result
			}
			result = append(result, event)
		case <-deadline:
			t.Fatalf("stream did not close: %#v", result)
		}
	}
}
