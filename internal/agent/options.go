package agent

import (
	"cyber-code/internal/core"
	"cyber-code/internal/hooks"
	"cyber-code/internal/product"
	"cyber-code/internal/session"
	toolpkg "cyber-code/internal/tool"
)

// Options configures model selection and the future tool-turn budget.
type Options struct {
	Model        string
	SystemPrompt string
	MaxTurns     int
	Tools        *toolpkg.Registry
	ToolRunner   *toolpkg.Runner
	Compactor    *session.Compactor
	Hooks        *hooks.Runner
	SessionID    string

	// InitialHistory is copied when the engine is created and is used when a
	// persisted session is resumed.
	InitialHistory []core.Message
}

const DefaultSystemPrompt = product.DefaultSystemPrompt
