package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"claude-code-go/internal/config"
	"claude-code-go/internal/core"
)

type requestPayload struct {
	Model       string           `json:"model"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      *bool            `json:"stream,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	System      []contentPayload `json:"system,omitempty"`
	Messages    []messagePayload `json:"messages"`
	Tools       []toolPayload    `json:"tools,omitempty"`
}

type messagePayload struct {
	Role    string           `json:"role"`
	Content []contentPayload `json:"content"`
}

type contentPayload struct {
	Type      string           `json:"type"`
	Text      string           `json:"text,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
	ID        string           `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Input     json.RawMessage  `json:"input,omitempty"`
	ToolUseID string           `json:"tool_use_id,omitempty"`
	Content   []contentPayload `json:"content,omitempty"`
	IsError   bool             `json:"is_error,omitempty"`
}

type toolPayload struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func encodeRequest(profile config.Profile, request core.Request, stream bool) ([]byte, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = strings.TrimSpace(profile.Model)
	}
	payload := requestPayload{
		Model:    model,
		Messages: make([]messagePayload, 0, len(request.Messages)),
		System:   make([]contentPayload, 0, len(request.System)),
		Tools:    make([]toolPayload, 0, len(request.Tools)),
	}
	if model == "" {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "anthropic.encode", Message: "Anthropic model is required"}
	}
	if stream {
		streamEnabled := true
		payload.Stream = &streamEnabled
		payload.Temperature = request.Temperature
		payload.MaxTokens = request.MaxTokens
		if payload.MaxTokens <= 0 {
			payload.MaxTokens = defaultMaxTokens
		}
	}

	for _, block := range request.System {
		encoded, err := encodeContent(block)
		if err != nil {
			return nil, err
		}
		payload.System = append(payload.System, encoded)
	}
	for _, message := range request.Messages {
		role, err := anthropicRole(message.Role)
		if err != nil {
			return nil, err
		}
		blocks := make([]contentPayload, 0, len(message.Content))
		for _, block := range message.Content {
			encoded, err := encodeContent(block)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, encoded)
		}
		if message.Role == core.RoleSystem {
			payload.System = append(payload.System, blocks...)
			continue
		}
		payload.Messages = append(payload.Messages, messagePayload{Role: role, Content: blocks})
	}
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return nil, &core.Error{Kind: core.ErrorKindTool, Op: "anthropic.encode", Message: "tool name is required"}
		}
		if len(tool.InputSchema) == 0 || !json.Valid(tool.InputSchema) {
			return nil, &core.Error{Kind: core.ErrorKindTool, Op: "anthropic.encode", Message: fmt.Sprintf("tool %q input schema is not valid JSON", tool.Name)}
		}
		payload.Tools = append(payload.Tools, toolPayload(tool))
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, &core.Error{Kind: core.ErrorKindInternal, Op: "anthropic.encode", Message: "failed to encode Anthropic request", Cause: err}
	}
	return encoded, nil
}

func anthropicRole(role core.Role) (string, error) {
	switch role {
	case core.RoleUser, core.RoleSystem:
		return "user", nil
	case core.RoleAssistant:
		return "assistant", nil
	case core.RoleTool:
		return "user", nil
	default:
		return "", &core.Error{Kind: core.ErrorKindProvider, Op: "anthropic.encode", Message: fmt.Sprintf("unsupported message role %q", role)}
	}
}

func encodeContent(block core.ContentBlock) (contentPayload, error) {
	switch block.Type {
	case core.ContentText:
		return contentPayload{Type: "text", Text: block.Text}, nil
	case core.ContentThinking:
		return contentPayload{Type: "thinking", Thinking: block.Thinking}, nil
	case core.ContentToolCall:
		if block.ToolCall == nil {
			return contentPayload{}, &core.Error{Kind: core.ErrorKindTool, Op: "anthropic.encode", Message: "tool call block has no tool call"}
		}
		arguments := block.ToolCall.Arguments
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		if !json.Valid(arguments) {
			return contentPayload{}, &core.Error{Kind: core.ErrorKindTool, Op: "anthropic.encode", Message: fmt.Sprintf("tool call %q arguments are not valid JSON", block.ToolCall.ID)}
		}
		return contentPayload{Type: "tool_use", ID: block.ToolCall.ID, Name: block.ToolCall.Name, Input: arguments}, nil
	case core.ContentToolResult:
		if block.ToolResult == nil {
			return contentPayload{}, &core.Error{Kind: core.ErrorKindTool, Op: "anthropic.encode", Message: "tool result block has no tool result"}
		}
		content := make([]contentPayload, 0, len(block.ToolResult.Content))
		for _, resultBlock := range block.ToolResult.Content {
			switch resultBlock.Type {
			case core.ContentText:
				content = append(content, contentPayload{Type: "text", Text: resultBlock.Text})
			default:
				return contentPayload{}, &core.Error{Kind: core.ErrorKindTool, Op: "anthropic.encode", Message: fmt.Sprintf("unsupported tool result content type %q", resultBlock.Type)}
			}
		}
		return contentPayload{Type: "tool_result", ToolUseID: block.ToolResult.ToolCallID, Content: content, IsError: block.ToolResult.IsError}, nil
	default:
		return contentPayload{}, &core.Error{Kind: core.ErrorKindProvider, Op: "anthropic.encode", Message: fmt.Sprintf("unsupported content type %q", block.Type)}
	}
}
