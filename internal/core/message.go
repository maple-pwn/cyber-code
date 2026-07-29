package core

import "encoding/json"

// Role identifies the participant that produced a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentType identifies the canonical content carried by a message block.
type ContentType string

const (
	ContentText       ContentType = "text"
	ContentImage      ContentType = "image"
	ContentThinking   ContentType = "thinking"
	ContentToolCall   ContentType = "tool_call"
	ContentToolResult ContentType = "tool_result"
)

// Message is a provider-independent conversation message.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock is one typed unit in a message. Only the field associated with
// Type is populated.
type ContentBlock struct {
	Type       ContentType `json:"type"`
	Text       string      `json:"text,omitempty"`
	Thinking   string      `json:"thinking,omitempty"`
	MediaType  string      `json:"media_type,omitempty"`
	Data       string      `json:"data,omitempty"`
	ToolCall   *ToolCall   `json:"tool_call,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
}

// ToolCall requests execution of a named tool with JSON arguments.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult contains the canonical response to one tool call.
type ToolResult struct {
	ToolCallID string         `json:"tool_call_id"`
	Content    []ContentBlock `json:"content"`
	IsError    bool           `json:"is_error,omitempty"`
}

// ToolDefinition describes a tool without using a provider SDK type.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Request is the canonical input accepted by providers.
type Request struct {
	Model       string           `json:"model"`
	System      []ContentBlock   `json:"system,omitempty"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
}
