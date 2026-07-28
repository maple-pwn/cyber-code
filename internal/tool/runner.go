package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
)

var (
	ErrToolNotFound     = errors.New("tool not found")
	ErrPermissionDenied = errors.New("tool permission denied")
)

type Authorizer interface {
	Decide(context.Context, permissions.Request) (permissions.Decision, error)
}

type RunnerOptions struct {
	MaxResultBytes int
}

type Runner struct {
	registry   *Registry
	authorizer Authorizer
	maxBytes   int
}

func NewRunner(registry *Registry, authorizer Authorizer, options RunnerOptions) *Runner {
	if options.MaxResultBytes <= 0 {
		options.MaxResultBytes = 1 << 20
	}
	return &Runner{registry: registry, authorizer: authorizer, maxBytes: options.MaxResultBytes}
}

func (runner *Runner) Run(ctx context.Context, name string, arguments json.RawMessage) (core.ToolResult, error) {
	if runner.registry == nil {
		return core.ToolResult{}, fmt.Errorf("%w: registry unavailable", ErrToolNotFound)
	}
	registered, ok := runner.registry.lookup(name)
	if !ok {
		return core.ToolResult{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	if err := validateArguments(registered.spec.Schema, arguments); err != nil {
		return core.ToolResult{}, fmt.Errorf("validate %s arguments: %w", name, err)
	}
	request, err := registered.tool.Authorize(ctx, arguments)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("authorize %s: %w", name, err)
	}
	if runner.authorizer == nil {
		return core.ToolResult{}, fmt.Errorf("%w: permission broker unavailable", ErrPermissionDenied)
	}
	decision, err := runner.authorizer.Decide(ctx, request)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("decide %s permission: %w", name, err)
	}
	if decision.Behavior != permissions.PermissionBehaviorAllow {
		return core.ToolResult{}, fmt.Errorf("%w: %s", ErrPermissionDenied, decision.Reason)
	}
	result, err := registered.tool.Run(ctx, arguments)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("run %s: %w", name, err)
	}
	truncateResult(&result, runner.maxBytes)
	return result, nil
}

func validateSchemaDefinition(schema json.RawMessage) error {
	var definition map[string]any
	if len(schema) == 0 || json.Unmarshal(schema, &definition) != nil {
		return fmt.Errorf("must be valid JSON")
	}
	if definition["type"] != "object" {
		return fmt.Errorf("root type must be object")
	}
	if required, ok := definition["required"]; ok {
		if _, ok := required.([]any); !ok {
			return fmt.Errorf("required must be an array")
		}
	}
	if properties, ok := definition["properties"]; ok {
		if _, ok := properties.(map[string]any); !ok {
			return fmt.Errorf("properties must be an object")
		}
	}
	return nil
}

func validateArguments(schema, arguments json.RawMessage) error {
	var definition map[string]any
	if err := json.Unmarshal(schema, &definition); err != nil {
		return err
	}
	var input map[string]any
	if err := json.Unmarshal(arguments, &input); err != nil {
		return fmt.Errorf("input must be a JSON object: %w", err)
	}
	if input == nil {
		return fmt.Errorf("input must be a JSON object")
	}
	if required, ok := definition["required"].([]any); ok {
		for _, value := range required {
			name, ok := value.(string)
			if !ok {
				return fmt.Errorf("schema required entry is not a string")
			}
			if _, present := input[name]; !present {
				return fmt.Errorf("required property %q is missing", name)
			}
		}
	}
	properties, _ := definition["properties"].(map[string]any)
	if additional, ok := definition["additionalProperties"].(bool); ok && !additional {
		for name := range input {
			if _, known := properties[name]; !known {
				return fmt.Errorf("property %q is not allowed", name)
			}
		}
	}
	for name, value := range input {
		property, ok := properties[name].(map[string]any)
		if !ok {
			continue
		}
		if expected, ok := property["type"].(string); ok && !matchesJSONType(value, expected) {
			return fmt.Errorf("property %q must be %s", name, expected)
		}
	}
	return nil
}

func matchesJSONType(value any, expected string) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number", "integer":
		_, ok := value.(float64)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	default:
		return true
	}
}

func truncateResult(result *core.ToolResult, maximum int) {
	remaining := maximum
	truncateBlocks(result.Content, &remaining)
}

func truncateBlocks(blocks []core.ContentBlock, remaining *int) {
	for index := range blocks {
		block := &blocks[index]
		if block.ToolResult != nil {
			truncateBlocks(block.ToolResult.Content, remaining)
		}
		if block.Type != core.ContentText && block.Type != core.ContentThinking {
			continue
		}
		text := block.Text
		if block.Type == core.ContentThinking {
			text = block.Thinking
		}
		if len(text) <= *remaining {
			*remaining -= len(text)
			continue
		}
		cut := *remaining
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
		if block.Type == core.ContentThinking {
			block.Thinking = text
		} else {
			block.Text = text
		}
		*remaining = 0
	}
}
