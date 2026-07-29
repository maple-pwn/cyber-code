package collaboration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefinitionLoaderDiscoversUserAndProjectAgents(t *testing.T) {
	state, workspace := t.TempDir(), t.TempDir()
	userDir := filepath.Join(state, "agents")
	projectDir := filepath.Join(workspace, ".cyber-code", "agents")
	for _, directory := range []string{userDir, projectDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(userDir, "reviewer.json"), []byte(`{"name":"reviewer","description":"review code","max_turns":4,"permission_mode":"plan","tools":["read_file"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "tester.json"), []byte(`{"name":"tester","description":"run tests","max_turns":3,"permission_mode":"default","tools":["shell"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loader, err := NewDefinitionLoader(state, workspace)
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := loader.Load()
	if err != nil || len(definitions) != 2 || definitions[0].Name != "reviewer" || definitions[1].Name != "tester" {
		t.Fatalf("definitions=%#v err=%v", definitions, err)
	}
}

func TestDefinitionLoaderRejectsEscalatingAndDuplicateDefinitions(t *testing.T) {
	state, workspace := t.TempDir(), t.TempDir()
	for _, directory := range []string{filepath.Join(state, "agents"), filepath.Join(workspace, ".cyber-code", "agents")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "same.json"), []byte(`{"name":"same","max_turns":1,"permission_mode":"bypass"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loader, err := NewDefinitionLoader(state, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(); err == nil {
		t.Fatal("unsafe duplicate definitions were accepted")
	}
}

func TestDefinitionLoaderRejectsTrailingJSON(t *testing.T) {
	state, workspace := t.TempDir(), t.TempDir()
	directory := filepath.Join(state, "agents")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"name":"reviewer","max_turns":1,"permission_mode":"plan"}{}`
	if err := os.WriteFile(filepath.Join(directory, "bad.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	loader, _ := NewDefinitionLoader(state, workspace)
	if _, err := loader.Load(); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}
