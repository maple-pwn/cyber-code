package mcp

import (
	"fmt"

	runtimemcp "claude-code-go/internal/mcp"
)

// ToRuntimeConfig converts legacy configuration types without reusing the
// legacy Client, whose process and network paths are not permission gated.
func ToRuntimeConfig(name string, config McpServerConfig, workspace string) (runtimemcp.ServerConfig, error) {
	if config == nil {
		return runtimemcp.ServerConfig{}, fmt.Errorf("MCP server config is required")
	}
	converted := runtimemcp.ServerConfig{Name: name, Workspace: workspace}
	switch legacy := config.(type) {
	case *McpStdioServerConfig:
		converted.Transport = runtimemcp.TransportStdio
		converted.Command = legacy.Command
		converted.Args = append([]string(nil), legacy.Args...)
		converted.Environment = cloneRuntimeStrings(legacy.Env)
	case *McpHTTPServerConfig:
		converted.Transport = runtimemcp.TransportHTTP
		converted.URL = legacy.URL
		converted.Headers = cloneRuntimeStrings(legacy.Headers)
	default:
		return runtimemcp.ServerConfig{}, fmt.Errorf("legacy MCP transport %q is not supported by the runtime", config.GetType())
	}
	return converted, nil
}

func cloneRuntimeStrings(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
