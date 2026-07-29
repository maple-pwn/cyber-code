package openai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
)

type requestPayload struct {
	Model       string           `json:"model"`
	Stream      bool             `json:"stream"`
	Messages    []messagePayload `json:"messages"`
	Tools       []toolPayload    `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
}
type messagePayload struct {
	Role       string            `json:"role"`
	Content    any               `json:"content,omitempty"`
	ToolCalls  []toolCallPayload `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}
type contentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *imageURLPayload `json:"image_url,omitempty"`
}
type imageURLPayload struct {
	URL string `json:"url"`
}
type toolPayload struct {
	Type     string          `json:"type"`
	Function functionPayload `json:"function"`
}
type toolCallPayload struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function functionPayload `json:"function"`
}
type functionPayload struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}

func encodeRequest(profile config.Profile, request core.Request) ([]byte, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = strings.TrimSpace(profile.Model)
	}
	if model == "" {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "openai.encode", Message: "OpenAI-compatible model is required"}
	}
	payload := requestPayload{Model: model, Stream: true, Messages: make([]messagePayload, 0, len(request.System)+len(request.Messages)), Tools: make([]toolPayload, 0, len(request.Tools)), MaxTokens: request.MaxTokens, Temperature: request.Temperature}
	for _, block := range request.System {
		text, err := textBlock(block)
		if err != nil {
			return nil, err
		}
		payload.Messages = append(payload.Messages, messagePayload{Role: "system", Content: text})
	}
	for _, message := range request.Messages {
		encoded, err := encodeMessage(message)
		if err != nil {
			return nil, err
		}
		payload.Messages = append(payload.Messages, encoded)
	}
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" || len(tool.InputSchema) == 0 || !json.Valid(tool.InputSchema) {
			return nil, &core.Error{Kind: core.ErrorKindTool, Op: "openai.encode", Message: "tool definition requires a name and valid JSON input schema"}
		}
		payload.Tools = append(payload.Tools, toolPayload{Type: "function", Function: functionPayload{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema}})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, &core.Error{Kind: core.ErrorKindInternal, Op: "openai.encode", Message: "failed to encode OpenAI-compatible request", Cause: err}
	}
	return encoded, nil
}

func encodeMessage(message core.Message) (messagePayload, error) {
	switch message.Role {
	case core.RoleSystem:
		texts := make([]string, 0, len(message.Content))
		for _, block := range message.Content {
			text, err := textBlock(block)
			if err != nil {
				return messagePayload{}, err
			}
			texts = append(texts, text)
		}
		return messagePayload{Role: string(message.Role), Content: strings.Join(texts, "\n")}, nil
	case core.RoleUser:
		parts := make([]contentPart, 0, len(message.Content))
		hasImage := false
		for _, block := range message.Content {
			switch block.Type {
			case core.ContentText:
				parts = append(parts, contentPart{Type: "text", Text: block.Text})
			case core.ContentImage:
				if err := validateImageBlock(block); err != nil {
					return messagePayload{}, err
				}
				hasImage = true
				parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURLPayload{URL: "data:" + block.MediaType + ";base64," + block.Data}})
			default:
				return messagePayload{}, &core.Error{Kind: core.ErrorKindProvider, Op: "openai.encode", Message: fmt.Sprintf("unsupported user content type %q", block.Type)}
			}
		}
		if hasImage {
			return messagePayload{Role: string(message.Role), Content: parts}, nil
		}
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			texts = append(texts, part.Text)
		}
		return messagePayload{Role: string(message.Role), Content: strings.Join(texts, "\n")}, nil
	case core.RoleAssistant:
		result := messagePayload{Role: "assistant"}
		for _, block := range message.Content {
			if block.Type == core.ContentText {
				result.Content = appendText(result.Content, block.Text)
				continue
			}
			if block.Type != core.ContentToolCall || block.ToolCall == nil || !json.Valid(block.ToolCall.Arguments) {
				return messagePayload{}, &core.Error{Kind: core.ErrorKindTool, Op: "openai.encode", Message: "assistant content is not supported by OpenAI-compatible Chat Completions"}
			}
			result.ToolCalls = append(result.ToolCalls, toolCallPayload{ID: block.ToolCall.ID, Type: "function", Function: functionPayload{Name: block.ToolCall.Name, Arguments: string(block.ToolCall.Arguments)}})
		}
		return result, nil
	case core.RoleTool:
		if len(message.Content) != 1 || message.Content[0].ToolResult == nil {
			return messagePayload{}, &core.Error{Kind: core.ErrorKindTool, Op: "openai.encode", Message: "tool message requires exactly one tool result"}
		}
		result := message.Content[0].ToolResult
		texts := make([]string, 0, len(result.Content))
		for _, block := range result.Content {
			text, err := textBlock(block)
			if err != nil {
				return messagePayload{}, err
			}
			texts = append(texts, text)
		}
		return messagePayload{Role: "tool", ToolCallID: result.ToolCallID, Content: strings.Join(texts, "\n")}, nil
	default:
		return messagePayload{}, &core.Error{Kind: core.ErrorKindProvider, Op: "openai.encode", Message: fmt.Sprintf("unsupported message role %q", message.Role)}
	}
}

func validateImageBlock(block core.ContentBlock) error {
	switch block.MediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return &core.Error{Kind: core.ErrorKindProvider, Op: "openai.encode", Message: "unsupported image media type"}
	}
	if decoded, err := base64.StdEncoding.DecodeString(block.Data); err != nil || len(decoded) == 0 {
		return &core.Error{Kind: core.ErrorKindProvider, Op: "openai.encode", Message: "image data is not valid base64"}
	}
	return nil
}
func textBlock(block core.ContentBlock) (string, error) {
	if block.Type != core.ContentText {
		return "", &core.Error{Kind: core.ErrorKindProvider, Op: "openai.encode", Message: fmt.Sprintf("unsupported content type %q", block.Type)}
	}
	return block.Text, nil
}
func appendText(content any, text string) string {
	if content == nil {
		return text
	}
	return content.(string) + text
}
