package core

// EventType identifies one item on the canonical runtime event stream.
type EventType string

const (
	EventUserMessage        EventType = "user_message"
	EventAssistantMessage   EventType = "assistant_message"
	EventTextDelta          EventType = "text_delta"
	EventThinkingDelta      EventType = "thinking_delta"
	EventToolCall           EventType = "tool_call"
	EventToolArgumentsDelta EventType = "tool_arguments_delta"
	EventToolResult         EventType = "tool_result"
	EventUsage              EventType = "usage"
	EventCompleted          EventType = "completed"
	EventError              EventType = "error"
)

// Usage reports provider token accounting without exposing provider types.
type Usage struct {
	InputTokens              int64 `json:"input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
}

// Event is the single stream shape consumed by the runtime and frontends.
type Event struct {
	Type           EventType   `json:"type"`
	Text           string      `json:"text,omitempty"`
	Message        *Message    `json:"message,omitempty"`
	ToolCall       *ToolCall   `json:"tool_call,omitempty"`
	ToolCallID     string      `json:"tool_call_id,omitempty"`
	ArgumentsDelta string      `json:"arguments_delta,omitempty"`
	ToolResult     *ToolResult `json:"tool_result,omitempty"`
	Usage          *Usage      `json:"usage,omitempty"`
	FinishReason   string      `json:"finish_reason,omitempty"`
	Err            *Error      `json:"error,omitempty"`
}
