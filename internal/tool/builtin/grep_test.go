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

func TestGrepFilesReturnsLineNumbersAndSupportsRegexAndGlobFilter(t *testing.T) {
	workspace := t.TempDir()
	files := map[string]string{
		"a.go":         "first\nneedle 123\nlast\n",
		"nested/b.go":  "needle 456\n",
		"nested/c.txt": "needle 789\n",
	}
	for name, content := range files {
		path := filepath.Join(workspace, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	grep := NewGrepFiles(workspace)
	if spec := grep.Spec(); spec.Name != "grep_files" || !spec.ReadOnly || !spec.ConcurrencySafe {
		t.Fatalf("spec = %#v", spec)
	}
	request, err := grep.Authorize(context.Background(), json.RawMessage(`{"pattern":"needle [0-9]+","glob":"**/*.go"}`))
	if err != nil || request.Action != permissions.ActionRead || len(request.Paths) != 1 {
		t.Fatalf("authorization = %#v, error = %v", request, err)
	}
	result, err := grep.Run(context.Background(), json.RawMessage(`{"pattern":"needle [0-9]+","glob":"**/*.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].Text
	for _, want := range []string{"a.go:2:needle 123", "nested/b.go:1:needle 456"} {
		if !strings.Contains(text, want) {
			t.Fatalf("grep missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "c.txt") {
		t.Fatalf("glob filter leaked txt match: %q", text)
	}
}

func TestGrepFilesSkipsBinaryOversizedAndEscapingFiles(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "text.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "binary.bin"), []byte("needle\x00binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "large.txt"), []byte(strings.Repeat("x", maxSearchFileBytes+1)+"needle"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("needle secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Logf("symlink unavailable: %v", err)
	}
	grep := NewGrepFiles(workspace)
	result, err := grep.Run(context.Background(), json.RawMessage(`{"pattern":"needle"}`))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "text.txt:1:needle") || strings.Contains(text, "binary") || strings.Contains(text, "large") || strings.Contains(text, "secret") {
		t.Fatalf("grep result = %q", text)
	}
	if _, err := grep.Run(context.Background(), json.RawMessage(`{"pattern":"needle","path":"../outside"}`)); err == nil {
		t.Fatal("grep accepted workspace traversal")
	}
}

func TestGrepFilesHonorsResultLimitCancellationAndInputValidation(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "many.txt"), []byte("match\nmatch\nmatch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	grep := NewGrepFiles(workspace)
	result, err := grep.Run(context.Background(), json.RawMessage(`{"pattern":"match","limit":2}`))
	if err != nil || len(strings.Split(strings.TrimSpace(result.Content[0].Text), "\n")) != 2 {
		t.Fatalf("limited result = %#v, error = %v", result, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := grep.Run(canceled, json.RawMessage(`{"pattern":"match"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled grep error = %v", err)
	}
	for _, input := range []string{`{`, `{"pattern":"["}`, `{"pattern":"x","limit":1001}`} {
		if _, err := grep.Run(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("grep accepted %s", input)
		}
	}
}
