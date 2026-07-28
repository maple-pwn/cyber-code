package services

import managedlsp "claude-code-go/internal/lsp"

// LSPServerManager and its configuration aliases keep the former services
// boundary source-compatible with managed callers while removing the legacy
// permissionless process implementation.
type LSPServerManager = managedlsp.Manager
type LSPServerManagerOptions = managedlsp.ManagerOptions
type ScopedLspServerConfig = managedlsp.ServerConfig
type LSPQuery = managedlsp.Query

func NewLSPServerManager(options managedlsp.ManagerOptions) (*managedlsp.Manager, error) {
	return managedlsp.NewManager(options)
}
