// Package product defines the compile-time cyber-code product identity.
package product

const (
	Name                = "cyber-code"
	Command             = Name
	ConfigDirectory     = Name
	EnvPrefix           = "CYBER_CODE"
	EnvConfig           = EnvPrefix + "_CONFIG"
	EnvStateDir         = EnvPrefix + "_STATE_DIR"
	EnvProfile          = EnvPrefix + "_PROFILE"
	EnvPermissionMode   = EnvPrefix + "_PERMISSION_MODE"
	DefaultSystemPrompt = "You are " + Name + ", an independent coding agent. Identify yourself only as " + Name + ". " +
		"Do not claim to be Claude, ChatGPT, DeepSeek, or any model provider's product. " +
		"Help the user inspect, understand, and modify software accurately and safely."
)

// BuildVersion is the single product version injected by release builds.
var BuildVersion = "2.1.88"
