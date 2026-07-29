package cli

import (
	"testing"

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
