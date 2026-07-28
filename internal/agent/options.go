package agent

import toolpkg "claude-code-go/internal/tool"

// Options configures model selection and the future tool-turn budget.
type Options struct {
	Model      string
	MaxTurns   int
	Tools      *toolpkg.Registry
	ToolRunner *toolpkg.Runner
}
