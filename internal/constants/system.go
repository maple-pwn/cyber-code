package constants

import (
	"os"
	"runtime"

	"cyber-code/internal/product"
)

// System prompt prefix types
type CLISyspromptPrefix string

const (
	DefaultPrefix                 CLISyspromptPrefix = product.DefaultSystemPrompt
	AgentSDKCyberCodePresetPrefix CLISyspromptPrefix = product.DefaultSystemPrompt + " You are running through an agent SDK."
	AgentSDKPrefix                CLISyspromptPrefix = product.DefaultSystemPrompt + " You are running through a generic agent SDK integration."
)

// CLISyspromptPrefixes contains all possible CLI sysprompt prefix values
var CLISyspromptPrefixes = map[CLISyspromptPrefix]bool{
	DefaultPrefix:                 true,
	AgentSDKCyberCodePresetPrefix: true,
	AgentSDKPrefix:                true,
}

// GetCLISyspromptPrefix returns the appropriate CLI sysprompt prefix based on context
func GetCLISyspromptPrefix(isNonInteractive bool, hasAppendSystemPrompt bool) CLISyspromptPrefix {
	if isNonInteractive {
		if hasAppendSystemPrompt {
			return AgentSDKCyberCodePresetPrefix
		}
		return AgentSDKPrefix
	}
	return DefaultPrefix
}

// SystemPromptDynamicBoundary is a marker separating static from dynamic content
const SystemPromptDynamicBoundary = "__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__"

// GetAttributionHeader returns the attribution header for API requests
func GetAttributionHeader(fingerprint string) string {
	// Check if attribution header is enabled
	if isAttributionHeaderEnabled() {
		return ""
	}

	version := product.BuildVersion + "." + fingerprint
	entrypoint := getEnvOrDefault("CYBER_CODE_ENTRYPOINT", "unknown")

	// Native client attestation and workload context are not supported.
	header := "x-anthropic-billing-header: cc_version=" + version + "; cc_entrypoint=" + entrypoint + ";"

	return header
}

func isAttributionHeaderEnabled() bool {
	// Check environment variable
	val := os.Getenv("CYBER_CODE_ATTRIBUTION_HEADER")
	if val == "false" || val == "0" || val == "no" {
		return false
	}
	// Remote feature-flag services are intentionally not used here.
	return true
}

// ClaudeCodeDocsMapURL is the URL for the Claude Code docs map
const ClaudeCodeDocsMapURL = "https://code.claude.com/docs/en/claude_code_docs_map.md"

// GetUnameSR returns the stable build OS and architecture identity.
func GetUnameSR() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// Helper function - uses getEnvOrDefault from oauth.go
