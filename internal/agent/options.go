package agent

import (
	"claude-code-go/internal/core"
	"claude-code-go/internal/hooks"
	"claude-code-go/internal/session"
	toolpkg "claude-code-go/internal/tool"
)

// Options configures model selection and the future tool-turn budget.
type Options struct {
	Model      string
	MaxTurns   int
	Tools      *toolpkg.Registry
	ToolRunner *toolpkg.Runner
	Compactor  *session.Compactor
	Hooks      *hooks.Runner
	SessionID  string

	// InitialHistory is copied when the engine is created and is used when a
	// persisted session is resumed.
	InitialHistory []core.Message
}
