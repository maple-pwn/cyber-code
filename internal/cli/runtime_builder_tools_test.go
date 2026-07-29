package cli

import (
	"context"
	"testing"

	"cyber-code/internal/permissions"
	"cyber-code/internal/platform"
	"cyber-code/internal/tool"
)

func TestRegisterWorkspaceToolsKeepsSearchCompatibilityAndAddsGrepGlob(t *testing.T) {
	registry := tool.NewRegistry()
	if err := registerWorkspaceTools(registry, t.TempDir(), nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"search_files", "grep_files", "glob_files", "edit_file"} {
		registered, ok := registry.Get(name)
		if !ok {
			t.Fatalf("workspace tool %q is not registered", name)
		}
		if (name == "grep_files" || name == "glob_files") && (!registered.Spec().ReadOnly || !registered.Spec().ConcurrencySafe) {
			t.Fatalf("workspace tool %q spec = %#v", name, registered.Spec())
		}
	}
}

func TestConnectConfiguredMCPReturnsManagerForInteractiveAddWithoutExistingEntries(t *testing.T) {
	stateDir := t.TempDir()
	workspace := t.TempDir()
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeDefault, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := connectConfiguredMCP(context.Background(), stateDir, workspace, tool.NewRegistry(), broker, platform.NewRunner(platform.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	if manager == nil {
		t.Fatal("MCP manager is unavailable when no servers are configured")
	}
	t.Cleanup(func() { _ = manager.Close() })
}
