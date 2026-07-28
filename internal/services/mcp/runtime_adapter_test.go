package mcp

import (
	"testing"

	runtimemcp "cyber-code/internal/mcp"
)

func TestToRuntimeConfigAdaptsSupportedExistingConfigs(t *testing.T) {
	t.Run("stdio", func(t *testing.T) {
		legacy := &McpStdioServerConfig{Command: "server", Args: []string{"--stdio"}, Env: map[string]string{"LANG": "C"}}
		converted, err := ToRuntimeConfig("local", legacy, "/workspace")
		if err != nil {
			t.Fatal(err)
		}
		if converted.Name != "local" || converted.Transport != runtimemcp.TransportStdio || converted.Command != "server" || converted.Workspace != "/workspace" {
			t.Fatalf("converted = %#v", converted)
		}
		if len(converted.Args) != 1 || converted.Args[0] != "--stdio" || converted.Environment["LANG"] != "C" {
			t.Fatalf("converted = %#v", converted)
		}
		legacy.Args[0] = "changed"
		legacy.Env["LANG"] = "changed"
		if converted.Args[0] != "--stdio" || converted.Environment["LANG"] != "C" {
			t.Fatal("converted stdio config aliases legacy state")
		}
	})

	t.Run("http", func(t *testing.T) {
		legacy := &McpHTTPServerConfig{URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "Bearer token"}}
		converted, err := ToRuntimeConfig("remote", legacy, "/workspace")
		if err != nil {
			t.Fatal(err)
		}
		if converted.Transport != runtimemcp.TransportHTTP || converted.URL != legacy.URL || converted.Headers["Authorization"] != "Bearer token" {
			t.Fatalf("converted = %#v", converted)
		}
		legacy.Headers["Authorization"] = "changed"
		if converted.Headers["Authorization"] != "Bearer token" {
			t.Fatal("converted HTTP headers alias legacy state")
		}
	})
}

func TestToRuntimeConfigRejectsLegacyPlaceholderTransports(t *testing.T) {
	if _, err := ToRuntimeConfig("sse", &McpSSEServerConfig{URL: "https://example.test/events"}, "/workspace"); err == nil {
		t.Fatal("unsupported legacy SSE transport was accepted")
	}
}
