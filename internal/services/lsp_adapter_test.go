package services

import (
	"testing"

	managedlsp "cyber-code/internal/lsp"
)

func TestLSPServiceAdaptersUseManagedConstructors(t *testing.T) {
	if _, err := NewLSPClient(managedlsp.ClientOptions{}); err == nil {
		t.Fatal("invalid managed client options were accepted")
	}
	manager, err := NewLSPServerManager(managedlsp.ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if manager == nil {
		t.Fatal("managed LSP manager is nil")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}
