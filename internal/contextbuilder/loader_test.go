package contextbuilder

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestLoaderDiscoversUserAndLayeredProjectInstructions(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	workspace := filepath.Join(root, "workspace")
	current := filepath.Join(workspace, "service", "api")
	writeInstruction(t, filepath.Join(state, "instructions.md"), "user")
	writeInstruction(t, filepath.Join(workspace, "CYBER.md"), "root")
	writeInstruction(t, filepath.Join(workspace, ".cyber-code", "instructions.md"), "workspace")
	writeInstruction(t, filepath.Join(workspace, "service", ".cyber-code", "instructions.md"), "service")
	writeInstruction(t, filepath.Join(current, ".cyber-code", "instructions.md"), "api")

	loader, err := NewLoader(LoaderOptions{StateDir: state, Workspace: workspace, CurrentDir: current})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := loader.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(sources))
	for index := range sources {
		got[index] = sources[index].Content
		if sources[index].Trusted {
			t.Fatalf("discovered file was trusted: %+v", sources[index])
		}
	}
	want := []string{"user", "root", "workspace", "service", "api"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("contents = %v, want %v", got, want)
	}
}

func TestLoaderIgnoresMissingFiles(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	loader, err := NewLoader(LoaderOptions{StateDir: filepath.Join(root, "missing-state"), Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := loader.Load()
	if err != nil || len(sources) != 0 {
		t.Fatalf("sources/error = %+v/%v", sources, err)
	}
}

func TestLoaderRejectsCurrentDirectoryOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := NewLoader(LoaderOptions{Workspace: workspace, CurrentDir: outside})
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("error = %v, want ErrPathOutsideRoot", err)
	}
}

func TestLoaderRejectsOversizedInstructionFiles(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	writeInstruction(t, filepath.Join(workspace, "CYBER.md"), strings.Repeat("x", 33))
	loader, err := NewLoader(LoaderOptions{Workspace: workspace, MaxFileBytes: 32, MaxTotalBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	_, err = loader.Load()
	if !errors.Is(err, ErrInstructionTooLarge) {
		t.Fatalf("error = %v, want ErrInstructionTooLarge", err)
	}
}

func TestLoaderRejectsSymlinkEscapingInstructionRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows hosts")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside.md")
	writeInstruction(t, outside, "outside")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "CYBER.md")); err != nil {
		t.Fatal(err)
	}
	loader, err := NewLoader(LoaderOptions{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	_, err = loader.Load()
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("error = %v, want ErrPathOutsideRoot", err)
	}
}

func writeInstruction(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
