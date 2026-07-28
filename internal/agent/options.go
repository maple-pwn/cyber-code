package agent

import (
	"claude-code-go/internal/core"
	toolpkg "claude-code-go/internal/tool"
)

// Options configures model selection and the future tool-turn budget.
type Options struct {
	Model      string
	MaxTurns   int
	Tools      *toolpkg.Registry
	ToolRunner *toolpkg.Runner

	// InitialHistory is copied when the engine is created and is used when a
	// persisted session is resumed.
	InitialHistory []core.Message
}
