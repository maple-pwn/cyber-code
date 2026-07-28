package utils

import (
	"strings"
	"testing"
)

func TestUserAgentsUseCyberCodeBrand(t *testing.T) {
	for name, value := range map[string]string{
		"api":    GetUserAgent(),
		"mcp":    GetMCPUserAgent(),
		"simple": GetCyberCodeUserAgent(),
	} {
		if !strings.Contains(strings.ToLower(value), "cyber-code") || strings.Contains(strings.ToLower(value), "claude-code") {
			t.Fatalf("%s user agent = %q", name, value)
		}
	}
}
