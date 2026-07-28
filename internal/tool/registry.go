package tool

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]registration
}

type registration struct {
	tool Tool
	spec Spec
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]registration)}
}

func (registry *Registry) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("tool is nil")
	}
	spec := tool.Spec()
	if strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("tool name is required")
	}
	if err := validateSchemaDefinition(spec.Schema); err != nil {
		return fmt.Errorf("tool %q schema: %w", spec.Name, err)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.tools[spec.Name]; exists {
		return fmt.Errorf("tool %q is already registered", spec.Name)
	}
	spec.Schema = append(json.RawMessage(nil), spec.Schema...)
	registry.tools[spec.Name] = registration{tool: tool, spec: spec}
	return nil
}

func (registry *Registry) Get(name string) (Tool, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	registered, ok := registry.tools[name]
	return registered.tool, ok
}

func (registry *Registry) lookup(name string) (registration, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	registered, ok := registry.tools[name]
	registered.spec.Schema = append(json.RawMessage(nil), registered.spec.Schema...)
	return registered, ok
}

func (registry *Registry) Specs() []Spec {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	specs := make([]Spec, 0, len(registry.tools))
	for _, registered := range registry.tools {
		spec := registered.spec
		spec.Schema = append(json.RawMessage(nil), spec.Schema...)
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}
