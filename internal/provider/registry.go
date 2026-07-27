package provider

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"claude-code-go/internal/config"
)

// Registry stores provider factories rather than live provider instances.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a provider factory. Names are case-insensitive and must be
// unique after normalization.
func (registry *Registry) Register(name string, factory Factory) error {
	name = normalizeName(name)
	if name == "" {
		return fmt.Errorf("provider name is required")
	}
	if factory == nil {
		return fmt.Errorf("provider %q factory is nil", name)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.factories == nil {
		registry.factories = make(map[string]Factory)
	}
	if _, exists := registry.factories[name]; exists {
		return fmt.Errorf("provider %q is already registered", name)
	}
	registry.factories[name] = factory
	return nil
}

// Create constructs a fresh provider using the registered factory.
func (registry *Registry) Create(name string, profile config.Profile) (Provider, error) {
	name = normalizeName(name)
	registry.mu.RLock()
	factory, exists := registry.factories[name]
	registry.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("provider %q is not registered", name)
	}

	created, err := factory(profile)
	if err != nil {
		return nil, fmt.Errorf("create provider %q: %w", name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("create provider %q: factory returned nil", name)
	}
	return created, nil
}

// Names returns registered names in deterministic order.
func (registry *Registry) Names() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	names := make([]string, 0, len(registry.factories))
	for name := range registry.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
