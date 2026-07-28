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

func TestSearchFilesFindsWorkspaceMatchesAndAuthorizesPath(t *testing.T) {
	workspace := t.TempDir()
	matching := filepath.Join(workspace, "matching.txt")
	if err := os.WriteFile(matching, []byte("needle in file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "other.txt"), []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}
	search := NewSearchFiles(workspace)
	spec := search.Spec()
	if spec.Name != "search_files" || !spec.ReadOnly || !spec.ConcurrencySafe {
		t.Fatalf("spec = %#v", spec)
	}
	request, err := search.Authorize(context.Background(), json.RawMessage(`{"query":"needle"}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != permissions.ActionRead || len(request.Paths) != 1 || request.Paths[0] != workspace {
		t.Fatalf("authorization request = %#v", request)
	}
	result, err := search.Run(context.Background(), json.RawMessage(`{"query":"needle"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || strings.TrimSpace(result.Content[0].Text) != matching {
		t.Fatalf("search result = %#v", result)
	}
}

func TestSearchFilesSupportsExplicitPathAndCancellation(t *testing.T) {
	workspace := t.TempDir()
	nested := filepath.Join(workspace, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "match.txt"), []byte("needle"), 0o600); err != nil {
		t.Fatal(err)
	}
	search := NewSearchFiles(workspace)
	arguments, err := json.Marshal(map[string]string{"query": "needle", "path": nested})
	if err != nil {
		t.Fatal(err)
	}
	request, err := search.Authorize(context.Background(), arguments)
	if err != nil || request.Paths[0] != nested {
		t.Fatalf("request = %#v, error = %v", request, err)
	}
	result, err := search.Run(context.Background(), arguments)
	if err != nil || !strings.Contains(result.Content[0].Text, "match.txt") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := search.Run(canceled, arguments); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search error = %v", err)
	}
	if _, err := search.Authorize(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed search authorization was accepted")
	}
	if _, err := search.Run(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed search input was accepted")
	}
}
