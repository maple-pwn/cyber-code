package core

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMessageJSONRoundTripPreservesContentBlocks(t *testing.T) {
	want := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: ContentText, Text: "hello"},
			{Type: ContentThinking, Thinking: "reasoning"},
			{
				Type: ContentToolCall,
				ToolCall: &ToolCall{
					ID:        "call-1",
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"README.md"}`),
				},
			},
			{
				Type: ContentToolResult,
				ToolResult: &ToolResult{
					ToolCallID: "call-1",
					Content:    []ContentBlock{{Type: ContentText, Text: "contents"}},
					IsError:    false,
				},
			},
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}

	if got.Role != want.Role || len(got.Content) != len(want.Content) {
		t.Fatalf("message shape changed: %#v", got)
	}
	if got.Content[0].Type != ContentText || got.Content[0].Text != "hello" {
		t.Fatalf("text block changed: %#v", got.Content[0])
	}
	if got.Content[1].Type != ContentThinking || got.Content[1].Thinking != "reasoning" {
		t.Fatalf("thinking block changed: %#v", got.Content[1])
	}
	call := got.Content[2].ToolCall
	if call == nil || call.ID != "call-1" || call.Name != "read_file" || string(call.Arguments) != `{"path":"README.md"}` {
		t.Fatalf("tool call changed: %#v", call)
	}
	result := got.Content[3].ToolResult
	if result == nil || result.ToolCallID != "call-1" || result.IsError || len(result.Content) != 1 || result.Content[0].Text != "contents" {
		t.Fatalf("tool result changed: %#v", result)
	}
}

func TestFileDiffIsTransientRuntimeMetadata(t *testing.T) {
	encoded, err := json.Marshal(ToolResult{
		Content: []ContentBlock{{Type: ContentText, Text: "file written"}},
		Diff:    &FileDiff{Path: "main.go", OldText: "old", NewText: "new"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"diff"`) || strings.Contains(string(encoded), `"old"`) {
		t.Fatalf("transient diff leaked into persisted tool result: %s", encoded)
	}
}

func TestEventJSONRoundTripToolCall(t *testing.T) {
	want := Event{
		Type: EventToolCall,
		ToolCall: &ToolCall{
			ID:        "call-1",
			Name:      "read_file",
			Arguments: json.RawMessage(`{"path":"README.md"}`),
		},
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if got.Type != want.Type || got.ToolCall == nil {
		t.Fatalf("event shape changed: %#v", got)
	}
	if got.ToolCall.ID != want.ToolCall.ID || got.ToolCall.Name != want.ToolCall.Name || string(got.ToolCall.Arguments) != string(want.ToolCall.Arguments) {
		t.Fatalf("tool call changed: %#v", got.ToolCall)
	}
}

func TestEventJSONRoundTripToolArgumentsDelta(t *testing.T) {
	want := Event{
		Type:           EventToolArgumentsDelta,
		ToolCallID:     "call-1",
		ArgumentsDelta: `{"path"`,
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if got.Type != want.Type || got.ToolCallID != want.ToolCallID || got.ArgumentsDelta != want.ArgumentsDelta {
		t.Fatalf("tool arguments delta changed: %#v", got)
	}
}

func TestSubagentEventJSONRoundTrip(t *testing.T) {
	want := Event{
		Type: EventSubagentEvent,
		Subagent: &SubagentEvent{
			TaskID: "task-1", Agent: "reviewer", Description: "review changes", Status: "running",
			Usage: &Usage{InputTokens: 7, OutputTokens: 3}, RecentTool: "read_file", Truncated: true,
			Event: &Event{Type: EventTextDelta, Text: "checking"},
		},
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != EventSubagentEvent || got.Subagent == nil || got.Subagent.TaskID != "task-1" || got.Subagent.Agent != "reviewer" {
		t.Fatalf("subagent event shape changed: %#v", got)
	}
	if got.Subagent.Event == nil || got.Subagent.Event.Type != EventTextDelta || got.Subagent.Event.Text != "checking" {
		t.Fatalf("nested event changed: %#v", got.Subagent.Event)
	}
	if got.Subagent.Usage == nil || got.Subagent.Usage.InputTokens != 7 || got.Subagent.RecentTool != "read_file" || !got.Subagent.Truncated {
		t.Fatalf("subagent metadata changed: %#v", got.Subagent)
	}
}

func TestErrorPreservesCauseAndRedactsUserMessage(t *testing.T) {
	cause := errors.New("upstream authorization failed with Bearer sk-secret-value")
	err := &Error{
		Kind:      ErrorKindAuthentication,
		Op:        "provider.stream",
		Message:   "request rejected; api_key=sk-visible-secret",
		Retryable: false,
		Cause:     cause,
	}

	if !errors.Is(err, cause) {
		t.Fatal("error does not preserve its cause")
	}
	if !strings.Contains(err.Error(), "provider.stream") {
		t.Fatalf("diagnostic error omits operation: %q", err.Error())
	}
	userMessage := err.UserMessage()
	if strings.Contains(userMessage, "sk-visible-secret") || strings.Contains(userMessage, "sk-secret-value") {
		t.Fatalf("user message leaked a credential: %q", userMessage)
	}
	if !strings.Contains(userMessage, "request rejected") {
		t.Fatalf("user message lost actionable context: %q", userMessage)
	}
}

func TestErrorUserMessageRedactsAuthorizationBearerValue(t *testing.T) {
	err := &Error{
		Kind:    ErrorKindAuthentication,
		Message: "request rejected; Authorization: Bearer header-secret.jwt",
	}

	userMessage := err.UserMessage()
	if strings.Contains(userMessage, "header-secret.jwt") {
		t.Fatalf("user message leaked bearer credential: %q", userMessage)
	}
}

func TestErrorNilReceiversAndDefaultMessages(t *testing.T) {
	var nilError *Error
	if got := nilError.Error(); got != "<nil>" {
		t.Fatalf("nil Error() = %q", got)
	}
	if nilError.Unwrap() != nil || nilError.UserMessage() != "" {
		t.Fatal("nil error receiver returned non-empty state")
	}

	want := map[ErrorKind]string{
		ErrorKindConfiguration:  "configuration is invalid",
		ErrorKindAuthentication: "authentication failed",
		ErrorKindRateLimit:      "request rate limit exceeded",
		ErrorKindPermission:     "operation was not permitted",
		ErrorKindCanceled:       "operation was canceled",
		ErrorKindProvider:       "operation failed",
	}
	for kind, message := range want {
		err := &Error{Kind: kind}
		if got := err.UserMessage(); got != message {
			t.Errorf("UserMessage(%q) = %q, want %q", kind, got, message)
		}
	}
}

func TestErrorDiagnosticFallsBackToKindAndUnwrapsNilCause(t *testing.T) {
	err := &Error{Kind: ErrorKindTool, Op: "tool.run"}
	if got := err.Error(); got != "tool.run: tool" {
		t.Fatalf("Error() = %q", got)
	}
	if err.Unwrap() != nil {
		t.Fatal("error without cause returned a wrapped error")
	}
}

func TestRequestJSONRoundTripPreservesProviderIndependentFields(t *testing.T) {
	want := Request{
		Model:  "model-name",
		System: []ContentBlock{{Type: ContentText, Text: "be precise"}},
		Messages: []Message{{
			Role:    RoleUser,
			Content: []ContentBlock{{Type: ContentText, Text: "hello"}},
		}},
		Tools: []ToolDefinition{{
			Name:        "read_file",
			Description: "Read a workspace file",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		MaxTokens: 1024,
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if got.Model != want.Model || got.MaxTokens != want.MaxTokens || len(got.System) != 1 || len(got.Messages) != 1 || len(got.Tools) != 1 {
		t.Fatalf("request shape changed: %#v", got)
	}
	if got.Tools[0].Name != "read_file" || string(got.Tools[0].InputSchema) != `{"type":"object"}` {
		t.Fatalf("tool definition changed: %#v", got.Tools[0])
	}
}
