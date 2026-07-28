// Package provider defines the provider-independent model gateway contract.
package provider

import (
	"context"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
)

// Capabilities describes optional behavior supported by a provider.
type Capabilities struct {
	Streaming     bool
	ToolCalls     bool
	Thinking      bool
	TokenCounting bool
}

// Provider streams canonical events and never exposes provider SDK types.
// Implementations must close the returned event channel when ctx is canceled.
type Provider interface {
	Name() string
	Capabilities(context.Context) (Capabilities, error)
	Stream(context.Context, core.Request) (<-chan core.Event, error)
	CountTokens(context.Context, core.Request) (int, error)
}

// Factory constructs a fresh provider for a validated profile.
type Factory func(config.Profile) (Provider, error)
