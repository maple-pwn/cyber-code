package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoaderDiscoversInstructionsInRootThenLexicalOrder(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeSkill(t, first, "zeta", "# Zeta\nFirst zeta")
	writeSkill(t, first, "alpha", "# Alpha\nFirst alpha")
	writeSkill(t, second, "beta", "# Beta\nSecond beta")
	loader, err := NewLoader([]Root{{Name: "project", Path: first}, {Name: "user", Path: second}})
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := loader.Discover()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range discovered {
		got = append(got, item.Source+":"+item.Name)
	}
	want := []string{"project:alpha", "project:zeta", "user:beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %#v", got)
	}
}

func TestLoaderUsesFirstRootForDuplicateSkillName(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeSkill(t, first, "review", "first instructions")
	writeSkill(t, second, "review", "second instructions")
	loader, err := NewLoader([]Root{{Name: "project", Path: first}, {Name: "user", Path: second}})
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := loader.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 || discovered[0].Instructions != "first instructions" || discovered[0].Source != "project" {
		t.Fatalf("skills = %#v", discovered)
	}
}

func TestLoaderRejectsSymlinkedSkillOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeSkill(t, outside, "external", "outside instructions")
	if err := os.Symlink(filepath.Join(outside, "external"), filepath.Join(root, "external")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	loader, err := NewLoader([]Root{{Name: "project", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Discover(); err == nil {
		t.Fatal("outside symlinked skill was accepted")
	}
}

func writeSkill(t *testing.T, root, name, instructions string) {
	t.Helper()
	directory := filepath.Join(root, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(instructions), 0o600); err != nil {
		t.Fatal(err)
	}
}
