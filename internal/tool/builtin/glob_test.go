package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyber-code/internal/permissions"
)

func TestGlobFilesSupportsRecursivePatternsAndStableRelativePaths(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"root.go", "nested/a.go", "nested/deeper/b.go", "nested/skip.txt"} {
		path := filepath.Join(workspace, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	glob := NewGlobFiles(workspace)
	if spec := glob.Spec(); spec.Name != "glob_files" || !spec.ReadOnly || !spec.ConcurrencySafe {
		t.Fatalf("spec = %#v", spec)
	}
	result, err := glob.Run(context.Background(), json.RawMessage(`{"pattern":"**/*.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(result.Content[0].Text)
	want := []string{"nested/a.go", "nested/deeper/b.go", "root.go"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("glob matches = %#v, want %#v", got, want)
	}
}

func TestGlobFilesRejectsEscapesSymlinksAndHonorsLimitsAndCancellation(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	glob := NewGlobFiles(workspace)
	request, err := glob.Authorize(context.Background(), json.RawMessage(`{"pattern":"*.txt","path":"."}`))
	if err != nil || request.Action != permissions.ActionRead || len(request.Paths) != 1 {
		t.Fatalf("authorization = %#v, error = %v", request, err)
	}
	result, err := glob.Run(context.Background(), json.RawMessage(`{"pattern":"*.txt","limit":2}`))
	if err != nil || len(strings.Fields(result.Content[0].Text)) != 2 {
		t.Fatalf("limited result = %#v, error = %v", result, err)
	}
	if _, err := glob.Run(context.Background(), json.RawMessage(`{"pattern":"*","path":"../outside"}`)); err == nil {
		t.Fatal("glob accepted workspace traversal")
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err == nil {
		result, err := glob.Run(context.Background(), json.RawMessage(`{"pattern":"**/*.txt"}`))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(result.Content[0].Text, "secret") || strings.Contains(result.Content[0].Text, "escape/") {
			t.Fatalf("glob followed escaping symlink: %#v", result)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := glob.Run(canceled, json.RawMessage(`{"pattern":"**/*"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled glob error = %v", err)
	}
}
