package services

import managedlsp "cyber-code/internal/lsp"

// LSPClient is retained as an internal compatibility alias. New code should
// depend on internal/lsp directly.
type LSPClient = managedlsp.Client
type LSPClientOptions = managedlsp.ClientOptions
type LSPPosition = managedlsp.Position
type LSPRange = managedlsp.Range
type LSPLocation = managedlsp.Location
type LSPHover = managedlsp.Hover
type LSPCompletionItem = managedlsp.CompletionItem
type LSPCompletionList = managedlsp.CompletionList

func NewLSPClient(options managedlsp.ClientOptions) (*managedlsp.Client, error) {
	return managedlsp.NewClient(options)
}
