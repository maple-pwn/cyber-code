// Package controlplane defines the provider-independent command control plane.
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"cyber-code/internal/core"
)

var (
	ErrNotCommand     = errors.New("input is not a command")
	ErrUnknownCommand = errors.New("unknown command")
	ErrInvalidCommand = errors.New("invalid command")
)

type Invocation struct {
	Name string
	Args []string
	Raw  string
}

type Handler func(context.Context, Invocation) ([]core.Event, error)

type Spec struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Handler     Handler
}

type Registry struct {
	commands map[string]Spec
}

func NewRegistry() *Registry { return &Registry{commands: make(map[string]Spec)} }

func (registry *Registry) Register(spec Spec) error {
	if registry == nil {
		return fmt.Errorf("control plane registry is nil")
	}
	spec.Name = normalizeName(spec.Name)
	if spec.Name == "" || strings.ContainsAny(spec.Name, " \t\r\n") || spec.Handler == nil {
		return fmt.Errorf("%w: command name and handler are required", ErrInvalidCommand)
	}
	aliases := make([]string, 0, len(spec.Aliases))
	seen := map[string]struct{}{spec.Name: {}}
	for _, alias := range spec.Aliases {
		alias = normalizeName(alias)
		if alias == "" || strings.ContainsAny(alias, " \t\r\n") {
			return fmt.Errorf("%w: invalid alias", ErrInvalidCommand)
		}
		if _, duplicate := seen[alias]; duplicate {
			return fmt.Errorf("%w: duplicate command alias %q", ErrInvalidCommand, alias)
		}
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}
	for name := range seen {
		if _, exists := registry.commands[name]; exists {
			return fmt.Errorf("%w: command %q is already registered", ErrInvalidCommand, name)
		}
	}
	spec.Aliases = aliases
	for name := range seen {
		registry.commands[name] = spec
	}
	return nil
}

func (registry *Registry) Lookup(name string) (Spec, bool) {
	if registry == nil {
		return Spec{}, false
	}
	spec, ok := registry.commands[normalizeName(name)]
	return spec, ok
}

func (registry *Registry) Dispatch(ctx context.Context, input string) ([]core.Event, error) {
	invocation, err := Parse(input)
	if err != nil {
		return nil, err
	}
	spec, ok := registry.Lookup(invocation.Name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownCommand, invocation.Name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return spec.Handler(ctx, invocation)
}

func (registry *Registry) Specs() []Spec {
	if registry == nil {
		return nil
	}
	unique := make(map[string]Spec)
	for _, spec := range registry.commands {
		unique[spec.Name] = spec
	}
	result := make([]Spec, 0, len(unique))
	for _, spec := range unique {
		spec.Aliases = append([]string(nil), spec.Aliases...)
		spec.Handler = nil
		result = append(result, spec)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

func (registry *Registry) Help(ctx context.Context, _ Invocation) ([]core.Event, error) {
	var lines []string
	for _, spec := range registry.Specs() {
		usage := spec.Usage
		if usage == "" {
			usage = "/" + spec.Name
		}
		lines = append(lines, fmt.Sprintf("%-24s %s", usage, spec.Description))
	}
	return TextEvents(strings.Join(lines, "\n")), nil
}

func Parse(input string) (Invocation, error) {
	raw := strings.TrimSpace(input)
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return Invocation{}, ErrNotCommand
	}
	if len(raw) == 1 {
		return Invocation{}, fmt.Errorf("%w: command name is required", ErrInvalidCommand)
	}
	parts, err := tokenize(raw[1:])
	if err != nil {
		return Invocation{}, err
	}
	if len(parts) == 0 || parts[0] == "" {
		return Invocation{}, fmt.Errorf("%w: command name is required", ErrInvalidCommand)
	}
	return Invocation{Name: normalizeName(parts[0]), Args: append([]string(nil), parts[1:]...), Raw: raw}, nil
}

func TextEvents(text string) []core.Event {
	if text == "" {
		return []core.Event{{Type: core.EventCompleted, FinishReason: "command"}}
	}
	return []core.Event{{Type: core.EventTextDelta, Text: text}, {Type: core.EventCompleted, FinishReason: "command"}}
}

func normalizeName(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "/") {
		name = strings.TrimPrefix(name, "/")
	}
	return strings.ToLower(name)
}

func tokenize(input string) ([]string, error) {
	var result []string
	var current []rune
	var quote rune
	escaped := false
	flush := func() {
		if len(current) > 0 {
			result = append(result, string(current))
			current = nil
		}
	}
	for _, character := range input {
		if escaped {
			current = append(current, character)
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				current = append(current, character)
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if unicode.IsSpace(character) {
			flush()
			continue
		}
		current = append(current, character)
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("%w: unterminated quote or escape", ErrInvalidCommand)
	}
	flush()
	return result, nil
}
