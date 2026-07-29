package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
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
	result, err := runner.Run(context.Background(), "write_file", writeArgs(target, "new"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Diff == nil || result.Diff.Path != "file.txt" || result.Diff.OldText != "old" || result.Diff.NewText != "new" {
		t.Fatalf("write diff = %#v", result.Diff)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "new" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
	matches, err := filepath.Glob(filepath.Join(workspace, ".cyber-code-*"))
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

func TestReadAndEditFileSuccessfulPaths(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(target, []byte("before value"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	for _, model := range []tool.Tool{NewReadFile(workspace), NewEditFile(workspace)} {
		if err := registry.Register(model); err != nil {
			t.Fatal(err)
		}
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeAcceptEdits})
	if err != nil {
		t.Fatal(err)
	}
	runner := tool.NewRunner(registry, broker, tool.RunnerOptions{})
	readArguments, _ := json.Marshal(map[string]string{"path": target})
	read, err := runner.Run(context.Background(), "read_file", readArguments)
	if err != nil || read.Content[0].Text != "before value" {
		t.Fatalf("read result = %#v, error = %v", read, err)
	}
	editArguments, _ := json.Marshal(map[string]string{"path": target, "old_text": "before", "new_text": "after"})
	result, err := runner.Run(context.Background(), "edit_file", editArguments)
	if err != nil {
		t.Fatal(err)
	}
	if result.Diff == nil || result.Diff.Path != "file.txt" || result.Diff.OldText != "before value" || result.Diff.NewText != "after value" {
		t.Fatalf("edit diff = %#v", result.Diff)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "after value" {
		t.Fatalf("edited content = %q, error = %v", content, err)
	}
}

func TestEditFileRejectsUnexpectedContentVersion(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	edit := NewEditFile(workspace)
	args := json.RawMessage(`{"path":"file.txt","old_text":"before","new_text":"after","expected_sha256":"deadbeef"}`)
	if _, err := edit.Run(context.Background(), args); err == nil {
		t.Fatal("stale edit was accepted")
	}
}

func TestFileToolRejectsMalformedUnsupportedAndCanceledOperations(t *testing.T) {
	workspace := t.TempDir()
	read := NewReadFile(workspace)
	if _, err := read.Authorize(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed authorization input was accepted")
	}
	if _, err := read.Run(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed read input was accepted")
	}
	if _, err := read.Run(context.Background(), json.RawMessage(`{"path":"missing.txt"}`)); err == nil {
		t.Fatal("missing file read succeeded")
	}
	unknown := &fileTool{workspace: workspace, spec: tool.Spec{Name: "unknown"}}
	if _, err := unknown.Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("unsupported file operation succeeded")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := atomicWrite(canceled, filepath.Join(workspace, "canceled.txt"), []byte("content")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write error = %v", err)
	}
}

func TestWriteFileCreatesMissingDirectories(t *testing.T) {
	workspace := t.TempDir()
	registry := tool.NewRegistry()
	if err := registry.Register(NewWriteFile(workspace)); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeAcceptEdits})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "nested", "file.txt")
	result, err := tool.NewRunner(registry, broker, tool.RunnerOptions{}).Run(context.Background(), "write_file", writeArgs(target, "created"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Diff == nil || result.Diff.Path != "nested/file.txt" || result.Diff.OldText != "" || result.Diff.NewText != "created" {
		t.Fatalf("create diff = %#v", result.Diff)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "created" {
		t.Fatalf("created content = %q, error = %v", content, err)
	}
}

func writeArgs(path, content string) json.RawMessage {
	encoded, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return encoded
}
