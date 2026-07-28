package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
	"cyber-code/internal/provider/anthropic"
	"cyber-code/internal/provider/testkit"
)

func TestStreamMapsAnthropicRequestAndEvents(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_API_KEY", "anthropic-secret")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", request.URL.Path)
		}
		if got := request.Header.Get("X-Api-Key"); got != "anthropic-secret" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := request.Header.Get("Anthropic-Version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q", got)
		}
		var payload struct {
			Model     string            `json:"model"`
			MaxTokens int               `json:"max_tokens"`
			Stream    bool              `json:"stream"`
			System    []json.RawMessage `json:"system"`
			Messages  []json.RawMessage `json:"messages"`
			Tools     []struct {
				Name        string          `json:"name"`
				InputSchema json.RawMessage `json:"input_schema"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload.Model != "claude-test" || payload.MaxTokens != 512 || !payload.Stream {
			t.Errorf("unexpected request options: %#v", payload)
		}
		if len(payload.System) != 1 || len(payload.Messages) != 1 || len(payload.Tools) != 1 {
			t.Errorf("request omitted system/messages/tools: %#v", payload)
		}
		if payload.Tools[0].Name != "read_file" || string(payload.Tools[0].InputSchema) != `{"type":"object"}` {
			t.Errorf("tool mapping changed: %#v", payload.Tools[0])
		}

		response.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, response, "message_start", `{"type":"message_start","message":{"usage":{"input_tokens":11,"cache_read_input_tokens":2}}}`)
		writeSSE(t, response, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`)
		writeSSE(t, response, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool-1","name":"read_file","input":{}}}`)
		writeSSE(t, response, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"README.md\"}"}}`)
		writeSSE(t, response, "message_delta", `{"type":"message_delta","usage":{"output_tokens":7}}`)
		writeSSE(t, response, "message_stop", `{"type":"message_stop"}`)
	})
	defer server.Close()

	client := newTestClient(t, server, anthropic.WithRetryPolicy(1, time.Millisecond))
	stream, err := client.Stream(context.Background(), core.Request{
		MaxTokens: 512,
		System:    []core.ContentBlock{{Type: core.ContentText, Text: "be helpful"}},
		Messages: []core.Message{{
			Role:    core.RoleUser,
			Content: []core.ContentBlock{{Type: core.ContentText, Text: "read it"}},
		}},
		Tools: []core.ToolDefinition{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	events := collectEvents(t, stream)

	wantTypes := []core.EventType{
		core.EventUsage,
		core.EventTextDelta,
		core.EventToolCall,
		core.EventToolArgumentsDelta,
		core.EventUsage,
		core.EventCompleted,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("got %d events, want %d: %#v", len(events), len(wantTypes), events)
	}
	for index, wantType := range wantTypes {
		if events[index].Type != wantType {
			t.Fatalf("event %d type = %q, want %q", index, events[index].Type, wantType)
		}
	}
	if events[0].Usage.InputTokens != 11 || events[0].Usage.CacheReadInputTokens != 2 {
		t.Fatalf("input usage changed: %#v", events[0].Usage)
	}
	if events[1].Text != "hello" {
		t.Fatalf("text delta = %q", events[1].Text)
	}
	if events[2].ToolCall == nil || events[2].ToolCall.ID != "tool-1" || events[2].ToolCall.Name != "read_file" {
		t.Fatalf("tool call changed: %#v", events[2].ToolCall)
	}
	if events[3].ToolCallID != "tool-1" || events[3].ArgumentsDelta != `{"path":"README.md"}` {
		t.Fatalf("tool arguments delta changed: %#v", events[3])
	}
	if events[4].Usage.OutputTokens != 7 {
		t.Fatalf("output usage changed: %#v", events[4].Usage)
	}
}

func TestStreamDoesNotRetryAuthenticationFailure(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_API_KEY", "anthropic-secret")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(response, `{"type":"error","error":{"type":"authentication_error","message":"invalid key"}}`)
	})
	defer server.Close()

	client := newTestClient(t, server, anthropic.WithRetryPolicy(3, time.Millisecond))
	_, err := client.Stream(context.Background(), core.Request{})
	if err == nil {
		t.Fatal("expected authentication error")
	}
	var coreError *core.Error
	if !errors.As(err, &coreError) || coreError.Kind != core.ErrorKindAuthentication || coreError.Retryable {
		t.Fatalf("unexpected error classification: %#v", err)
	}
	if count := len(server.Requests()); count != 1 {
		t.Fatalf("authentication failure made %d requests, want 1", count)
	}
}

func TestStreamRetriesRateLimitAndHonorsRetryAfter(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_API_KEY", "anthropic-secret")
	server := testkit.NewServer(
		func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Retry-After", "0")
			response.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(response, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
		},
		func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/event-stream")
			writeSSE(t, response, "message_stop", `{"type":"message_stop"}`)
		},
	)
	defer server.Close()

	client := newTestClient(t, server, anthropic.WithRetryPolicy(1, time.Hour))
	stream, err := client.Stream(context.Background(), core.Request{})
	if err != nil {
		t.Fatalf("stream after retry: %v", err)
	}
	events := collectEvents(t, stream)
	if len(events) != 1 || events[0].Type != core.EventCompleted {
		t.Fatalf("unexpected retry events: %#v", events)
	}
	if count := len(server.Requests()); count != 2 {
		t.Fatalf("rate limit made %d requests, want 2", count)
	}
}

func TestStreamDoesNotRetryPermanentTransportError(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_API_KEY", "anthropic-secret")
	attempts := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("permanent transport configuration error")
	})}
	client, err := anthropic.New(config.Profile{
		Provider:  "anthropic",
		BaseURL:   "http://anthropic.invalid",
		Model:     "claude-test",
		APIKeyEnv: "TEST_ANTHROPIC_API_KEY",
	}, anthropic.WithHTTPClient(httpClient), anthropic.WithRetryPolicy(3, 0))
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	_, err = client.Stream(context.Background(), core.Request{})
	if err == nil {
		t.Fatal("expected transport error")
	}
	var coreError *core.Error
	if !errors.As(err, &coreError) || coreError.Retryable {
		t.Fatalf("permanent transport error was classified as retryable: %#v", err)
	}
	if attempts != 1 {
		t.Fatalf("permanent transport error made %d attempts, want 1", attempts)
	}
}

func TestStreamCancellationClosesChannel(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_API_KEY", "anthropic-secret")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		response.WriteHeader(http.StatusOK)
		response.(http.Flusher).Flush()
		<-request.Context().Done()
	})
	defer server.Close()

	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Stream(ctx, core.Request{})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	cancel()
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("stream emitted an event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not close after cancellation")
	}
}

func TestStreamRejectsOversizedResponse(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_API_KEY", "anthropic-secret")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, response, "content_block_delta", strings.Repeat("x", 256))
	})
	defer server.Close()

	client := newTestClient(t, server, anthropic.WithResponseLimit(64))
	stream, err := client.Stream(context.Background(), core.Request{})
	if err != nil {
		t.Fatalf("start stream: %v", err)
	}
	events := collectEvents(t, stream)
	if len(events) != 1 || events[0].Type != core.EventError || events[0].Err == nil || events[0].Err.Kind != core.ErrorKindProvider {
		t.Fatalf("oversized stream was not rejected: %#v", events)
	}
}

func TestCountTokensUsesBoundedAnthropicEndpoint(t *testing.T) {
	t.Setenv("TEST_ANTHROPIC_API_KEY", "anthropic-secret")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages/count_tokens" {
			t.Errorf("path = %q", request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		for _, unsupported := range []string{`"stream"`, `"max_tokens"`, `"temperature"`} {
			if strings.Contains(string(body), unsupported) {
				t.Errorf("count request contains unsupported field %s: %s", unsupported, body)
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"input_tokens":42}`)
	})
	defer server.Close()

	client := newTestClient(t, server)
	temperature := 0.5
	count, err := client.CountTokens(context.Background(), core.Request{
		Messages:    []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "hello"}}}},
		MaxTokens:   123,
		Temperature: &temperature,
	})
	if err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count != 42 {
		t.Fatalf("token count = %d, want 42", count)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func newTestClient(t *testing.T, server *testkit.Server, options ...anthropic.Option) *anthropic.Client {
	t.Helper()
	options = append([]anthropic.Option{anthropic.WithHTTPClient(server.Client())}, options...)
	client, err := anthropic.New(config.Profile{
		Provider:  "anthropic",
		BaseURL:   server.URL(),
		Model:     "claude-test",
		APIKeyEnv: "TEST_ANTHROPIC_API_KEY",
	}, options...)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return client
}

func collectEvents(t *testing.T, stream <-chan core.Event) []core.Event {
	t.Helper()
	var events []core.Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-deadline:
			t.Fatalf("timed out collecting events: %#v", events)
		}
	}
}

func writeSSE(t *testing.T, response http.ResponseWriter, event, data string) {
	t.Helper()
	if err := testkit.WriteSSE(response, event, data); err != nil {
		t.Errorf("write SSE event %s: %v", event, err)
	}
}
