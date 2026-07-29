package openai

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
	t.Setenv("OPENAI_COVERAGE_KEY", "test-secret")
	profile := config.Profile{BaseURL: "https://example.test/proxy", Model: "model", APIKeyEnv: "OPENAI_COVERAGE_KEY"}
	client, err := New(profile, WithToolCalls(false), WithRetryPolicy(1, time.Millisecond), WithResponseLimit(1024))
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "openai" {
		t.Fatalf("Name() = %q", client.Name())
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil || !capabilities.Streaming || capabilities.ToolCalls {
		t.Fatalf("capabilities = %#v, error = %v", capabilities, err)
	}
	if _, err := client.CountTokens(context.Background(), core.Request{}); err == nil {
		t.Fatal("portable token counting unexpectedly succeeded")
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
		{profile: config.Profile{BaseURL: profile.BaseURL, Model: profile.Model, APIKeyEnv: "MISSING_KEY"}},
		{profile: config.Profile{BaseURL: "://bad", Model: profile.Model, APIKeyEnv: profile.APIKeyEnv}},
		{profile: config.Profile{BaseURL: profile.BaseURL, APIKeyEnv: profile.APIKeyEnv}},
		{profile: profile, option: nil},
		{profile: profile, option: WithHTTPClient(nil)},
		{profile: profile, option: WithRetryPolicy(-1, 0)},
		{profile: profile, option: WithResponseLimit(0)},
	} {
		if _, err := New(test.profile, test.option); err == nil {
			t.Fatalf("invalid client settings were accepted: %#v", test)
		}
	}
}

func TestEncodeRequestSupportsMessageRolesAndRejectsInvalidContent(t *testing.T) {
	profile := config.Profile{Model: "model"}
	request := core.Request{
		System: []core.ContentBlock{{Type: core.ContentText, Text: "system"}},
		Messages: []core.Message{
			{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "one"}, {Type: core.ContentText, Text: "two"}}},
			{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentText, Text: "prefix"}, {Type: core.ContentText, Text: "suffix"}, {Type: core.ContentToolCall, ToolCall: &core.ToolCall{ID: "call", Name: "read", Arguments: json.RawMessage(`{}`)}}}},
			{Role: core.RoleTool, Content: []core.ContentBlock{{Type: core.ContentToolResult, ToolResult: &core.ToolResult{ToolCallID: "call", Content: []core.ContentBlock{{Type: core.ContentText, Text: "result"}}}}}},
		},
		Tools: []core.ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
	encoded, err := encodeRequest(profile, request)
	if err != nil || !json.Valid(encoded) || !bytes.Contains(encoded, []byte(`"tool_call_id":"call"`)) {
		t.Fatalf("encoded request = %s, error = %v", encoded, err)
	}
	invalidMessages := []core.Message{
		{Role: "unknown"},
		{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentThinking}}},
		{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentToolCall}}},
		{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentToolCall, ToolCall: &core.ToolCall{Arguments: json.RawMessage(`{`)}}}},
		{Role: core.RoleTool},
		{Role: core.RoleTool, Content: []core.ContentBlock{{Type: core.ContentToolResult, ToolResult: &core.ToolResult{Content: []core.ContentBlock{{Type: core.ContentThinking}}}}}},
	}
	for _, message := range invalidMessages {
		if _, err := encodeMessage(message); err == nil {
			t.Fatalf("invalid message was accepted: %#v", message)
		}
	}
	if _, err := encodeRequest(config.Profile{}, core.Request{}); err == nil {
		t.Fatal("missing model was accepted")
	}
	if _, err := encodeRequest(profile, core.Request{Tools: []core.ToolDefinition{{Name: "bad", InputSchema: json.RawMessage(`{`)}}}); err == nil {
		t.Fatal("invalid tool definition was accepted")
	}
}

func TestEncodeRequestSplitsGroupedToolResultsIntoWireMessages(t *testing.T) {
	request := core.Request{
		Model: "model",
		Messages: []core.Message{{Role: core.RoleTool, Content: []core.ContentBlock{
			{Type: core.ContentToolResult, ToolResult: &core.ToolResult{ToolCallID: "call-1", Content: []core.ContentBlock{{Type: core.ContentText, Text: "first"}}}},
			{Type: core.ContentToolResult, ToolResult: &core.ToolResult{ToolCallID: "call-2", Content: []core.ContentBlock{{Type: core.ContentText, Text: "second"}}}},
		}}},
	}
	encoded, err := encodeRequest(config.Profile{Model: "model"}, request)
	if err != nil {
		t.Fatalf("encode grouped tool results: %v", err)
	}
	var payload requestPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("wire messages = %#v", payload.Messages)
	}
	for index, want := range []string{"call-1", "call-2"} {
		if payload.Messages[index].Role != "tool" || payload.Messages[index].ToolCallID != want {
			t.Fatalf("wire message %d = %#v", index, payload.Messages[index])
		}
	}
}

func TestRetryBoundedAndResponseErrorHelpers(t *testing.T) {
	client := &Client{apiKey: "secret", retryBase: 4 * time.Second, retryMaximum: 5 * time.Second, responseLimit: 8}
	if client.delay(2, "") != 5*time.Second || client.delay(0, "10") != 5*time.Second {
		t.Fatal("retry delay was not capped")
	}
	if min(time.Second, 2*time.Second) != time.Second || min(2*time.Second, time.Second) != time.Second {
		t.Fatal("min returned the larger duration")
	}
	if err := wait(context.Background(), 0); err != nil {
		t.Fatalf("zero wait = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := wait(canceled, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait = %v", err)
	}
	if body, err := bounded(strings.NewReader("abc"), 3); err != nil || string(body) != "abc" {
		t.Fatalf("bounded body = %q, %v", body, err)
	}
	if _, err := bounded(strings.NewReader("abcd"), 3); err == nil {
		t.Fatal("oversized response was accepted")
	}
	if _, err := bounded(openAIErrorReader{}, 3); err == nil {
		t.Fatal("reader error was ignored")
	}
	if !retryableTransport(io.ErrUnexpectedEOF) || !retryableTransport(openAITemporaryError{}) || retryableTransport(errors.New("permanent")) {
		t.Fatal("transport retry classification is incorrect")
	}
	if canceledError("test", nil).Kind != core.ErrorKindCanceled {
		t.Fatal("nil cancellation cause was not classified")
	}

	for _, test := range []struct {
		status    int
		retryable bool
		kind      core.ErrorKind
	}{
		{status: http.StatusUnauthorized, kind: core.ErrorKindAuthentication},
		{status: http.StatusTooManyRequests, kind: core.ErrorKindRateLimit, retryable: true},
		{status: http.StatusServiceUnavailable, kind: core.ErrorKindProvider, retryable: true},
	} {
		response := &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"api_key=secret"}}`))}
		classified := client.responseError(response)
		if classified.Kind != test.kind || classified.Retryable != test.retryable || strings.Contains(classified.Message, "secret") {
			t.Fatalf("classified error = %#v", classified)
		}
	}
}

type openAIErrorReader struct{}

func (openAIErrorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

type openAITemporaryError struct{}

func (openAITemporaryError) Error() string   { return "temporary" }
func (openAITemporaryError) Timeout() bool   { return true }
func (openAITemporaryError) Temporary() bool { return false }
