package agent

import (
	"claude-code-go/internal/core"
	"claude-code-go/internal/hooks"
	"claude-code-go/internal/session"
	toolpkg "claude-code-go/internal/tool"
)

const DefaultSystemPrompt = "You are cyber-code, an independent coding agent. Identify yourself only as cyber-code. Do not claim to be Claude, ChatGPT, DeepSeek, or any model provider's product. Help the user inspect, understand, and modify software accurately and safely."

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
