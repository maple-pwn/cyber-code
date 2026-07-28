package agent

import (
	"context"

	"cyber-code/internal/contextbuilder"
	"cyber-code/internal/core"
	"cyber-code/internal/hooks"
	"cyber-code/internal/session"
	toolpkg "cyber-code/internal/tool"
)

type ContextBuilder interface {
	Build(context.Context, contextbuilder.BuildInput) (contextbuilder.Plan, error)
}

// Options configures model selection and the future tool-turn budget.
type Options struct {
	Model          string
	ContextBuilder ContextBuilder
	MaxTurns       int
	Tools          *toolpkg.Registry
	ToolRunner     *toolpkg.Runner
	Compactor      *session.Compactor
	Hooks          *hooks.Runner
	SessionID      string

	// InitialHistory is copied when the engine is created and is used when a
	// persisted session is resumed.
	InitialHistory []core.Message
}
