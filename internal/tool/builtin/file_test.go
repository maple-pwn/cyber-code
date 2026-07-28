package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"claude-code-go/internal/permissions"
	"claude-code-go/internal/tool"
)

func TestWriteFileUsesRunnerPermissionBoundaryAndAtomicReplace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	if err := registry.Register(NewWriteFile(workspace)); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeAcceptEdits})
	if err != nil {
		t.Fatal(err)
	}
	runner := tool.NewRunner(registry, broker, tool.RunnerOptions{})
	target := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), "write_file", writeArgs(target, "new")); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "new" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
	matches, err := filepath.Glob(filepath.Join(workspace, ".claude-go-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %#v, err=%v", matches, err)
	}

	for _, escaped := range []string{outside, filepath.Join(workspace, "..", "outside", "secret.txt")} {
		if _, err := runner.Run(context.Background(), "write_file", writeArgs(escaped, "secret")); !errors.Is(err, tool.ErrPermissionDenied) {
			t.Fatalf("escaped path %q error = %v", escaped, err)
		}
	}
	link := filepath.Join(workspace, "link")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := runner.Run(context.Background(), "write_file", writeArgs(filepath.Join(link, "secret.txt"), "secret")); !errors.Is(err, tool.ErrPermissionDenied) {
			t.Fatalf("symlink escape error = %v", err)
		}
	}
}

func TestEditFileDoesNotWriteOnZeroOrMultipleMatches(t *testing.T) {
	workspace := t.TempDir()
	registry := tool.NewRegistry()
	if err := registry.Register(NewEditFile(workspace)); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeAcceptEdits})
	if err != nil {
		t.Fatal(err)
	}
	runner := tool.NewRunner(registry, broker, tool.RunnerOptions{})
	target := filepath.Join(workspace, "file.txt")
	original := "same\nsame\n"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"missing", "same"} {
		args, _ := json.Marshal(map[string]string{"path": target, "old_text": old, "new_text": "changed"})
		if _, err := runner.Run(context.Background(), "edit_file", args); err == nil {
			t.Fatalf("edit %q unexpectedly succeeded", old)
		}
		content, err := os.ReadFile(target)
		if err != nil || string(content) != original {
			t.Fatalf("file changed after failed edit: %q, %v", content, err)
		}
	}
}

func writeArgs(path, content string) json.RawMessage {
	encoded, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return encoded
}
