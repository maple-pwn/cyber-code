package anthropic

import (
	"bytes"
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
)

func TestClientMetadataAndOptionValidation(t *testing.T) {
	profile := config.Profile{BaseURL: "https://example.test", Model: "claude-test"}
	client, err := New(profile, WithCredential("managed-secret"), WithRetryPolicy(1, time.Millisecond), WithResponseLimit(1024))
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "anthropic" {
		t.Fatalf("Name() = %q", client.Name())
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil || !capabilities.Streaming || !capabilities.ToolCalls || !capabilities.Thinking || !capabilities.TokenCounting {
		t.Fatalf("capabilities = %#v, error = %v", capabilities, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Capabilities(canceled); err == nil {
		t.Fatal("canceled capability request succeeded")
	}

	for _, test := range []struct {
		profile config.Profile
		option  Option
	}{
		{profile: config.Profile{BaseURL: "://bad", Model: "model"}, option: WithCredential("key")},
		{profile: config.Profile{BaseURL: "https://example.test"}, option: WithCredential("key")},
		{profile: profile, option: nil},
		{profile: profile, option: WithCredential(" ")},
		{profile: profile, option: WithHTTPClient(nil)},
		{profile: profile, option: WithRetryPolicy(-1, 0)},
		{profile: profile, option: WithResponseLimit(0)},
	} {
		if _, err := New(test.profile, test.option); err == nil {
			t.Fatalf("invalid client settings were accepted: %#v", test)
		}
	}
}

func TestEncodeRequestSupportsCanonicalContentAndRejectsInvalidBlocks(t *testing.T) {
	profile := config.Profile{Model: "claude-test"}
	request := core.Request{
		System: []core.ContentBlock{{Type: core.ContentText, Text: "system"}},
		Messages: []core.Message{
			{Role: core.RoleSystem, Content: []core.ContentBlock{{Type: core.ContentText, Text: "more system"}}},
			{Role: core.RoleAssistant, Content: []core.ContentBlock{
				{Type: core.ContentThinking, Thinking: "thought"},
				{Type: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "call", Name: "read", Arguments: nil}},
			}},
			{Role: core.RoleTool, Content: []core.ContentBlock{{Type: core.ContentToolResult, ToolResult: &core.ToolResult{
				ToolCallID: "call", IsError: true, Content: []core.ContentBlock{{Type: core.ContentText, Text: "result"}},
			}}}},
		},
		Tools: []core.ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
	encoded, err := encodeRequest(profile, request, true)
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("encoded request = %s, error = %v", encoded, err)
	}
	if !bytes.Contains(encoded, []byte(`"type":"thinking"`)) || !bytes.Contains(encoded, []byte(`"type":"tool_result"`)) {
		t.Fatalf("encoded request omitted content: %s", encoded)
	}

	invalidBlocks := []core.ContentBlock{
		{Type: core.ContentToolCall},
		{Type: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "call", Arguments: json.RawMessage(`{`)}},
		{Type: core.ContentToolResult},
		{Type: core.ContentToolResult, ToolResult: &core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentThinking}}}},
		{Type: "unknown"},
	}
	for _, block := range invalidBlocks {
		if _, err := encodeContent(block); err == nil {
			t.Fatalf("invalid block was accepted: %#v", block)
		}
	}
	if _, err := anthropicRole("unknown"); err == nil {
		t.Fatal("unknown role was accepted")
	}
	if _, err := encodeRequest(config.Profile{}, core.Request{}, false); err == nil {
		t.Fatal("missing model was accepted")
	}
	for _, definition := range []core.ToolDefinition{{}, {Name: "bad", InputSchema: json.RawMessage(`{`)}} {
		if _, err := encodeRequest(profile, core.Request{Tools: []core.ToolDefinition{definition}}, false); err == nil {
			t.Fatalf("invalid tool definition was accepted: %#v", definition)
		}
	}
}

func TestRetryAndBoundedReadHelpers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	if delay, ok := parseRetryAfter("2", now); !ok || delay != 2*time.Second {
		t.Fatalf("numeric Retry-After = %v, %v", delay, ok)
	}
	if delay, ok := parseRetryAfter(now.Add(-time.Second).Format(http.TimeFormat), now); !ok || delay != 0 {
		t.Fatalf("past Retry-After = %v, %v", delay, ok)
	}
	if delay, ok := parseRetryAfter(now.Add(2*time.Second).Format(http.TimeFormat), now); !ok || delay != 2*time.Second {
		t.Fatalf("date Retry-After = %v, %v", delay, ok)
	}
	for _, value := range []string{"", "invalid", "-1"} {
		if _, ok := parseRetryAfter(value, now); ok {
			t.Fatalf("invalid Retry-After %q was accepted", value)
		}
	}
	client := &Client{retryBase: 4 * time.Second, retryMaximum: 5 * time.Second}
	if got := client.retryDelay(2, ""); got != 5*time.Second {
		t.Fatalf("capped retry delay = %v", got)
	}
	if minDuration(time.Second, 2*time.Second) != time.Second || minDuration(2*time.Second, time.Second) != time.Second {
		t.Fatal("minDuration returned the larger duration")
	}
	if err := waitForRetry(context.Background(), 0); err != nil {
		t.Fatalf("zero retry wait = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForRetry(canceled, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry wait = %v", err)
	}
	if body, err := readBounded(strings.NewReader("abc"), 3); err != nil || string(body) != "abc" {
		t.Fatalf("bounded body = %q, %v", body, err)
	}
	if _, err := readBounded(strings.NewReader("abcd"), 3); err == nil {
		t.Fatal("oversized body was accepted")
	}
	if _, err := readBounded(errorReader{}, 3); err == nil {
		t.Fatal("reader error was ignored")
	}
	if !isRetryableTransportError(io.EOF) || !isRetryableTransportError(temporaryError{}) || isRetryableTransportError(errors.New("permanent")) {
		t.Fatal("transport retry classification is incorrect")
	}
	if canceledError("test", nil).Kind != core.ErrorKindCanceled {
		t.Fatal("nil cancellation cause was not classified")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }
