package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticSearchRejectsWorkspaceAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	for _, path := range []string{workspace, outside} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("private deployment credential"), 0o600); err != nil {
		t.Fatal(err)
	}
	search := NewSemanticSearch(workspace)
	if _, err := search.Run(context.Background(), json.RawMessage(`{"query":"credential","path":"../outside"}`)); err == nil {
		t.Fatal("semantic search accepted workspace traversal")
	}
	link := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := search.Run(context.Background(), json.RawMessage(`{"query":"credential","path":"escape"}`)); err == nil {
		t.Fatal("semantic search accepted a symlink escape")
	}
}

func TestSemanticSearchIsBoundedCancelableAndNotConcurrencySafe(t *testing.T) {
	workspace := t.TempDir()
	search := NewSemanticSearch(workspace)
	if spec := search.Spec(); spec.ConcurrencySafe {
		t.Fatal("memory-intensive local relevance search must not run concurrently")
	}
	if err := os.WriteFile(filepath.Join(workspace, "oversized.txt"), []byte(strings.Repeat("concept ", maxSemanticFileBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := search.Run(context.Background(), json.RawMessage(`{"query":"concept"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content[0].Text, "oversized.txt") {
		t.Fatal("semantic search read a file beyond its per-file budget")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := search.Run(canceled, json.RawMessage(`{"query":"concept"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled semantic search error = %v", err)
	}
}

func TestSemanticSearchStopsAtTotalByteBudget(t *testing.T) {
	workspace := t.TempDir()
	chunk := strings.Repeat("ordinary ", maxSemanticFileBytes/len("ordinary "))
	fileCount := maxSemanticTotalBytes/maxSemanticFileBytes + 1
	for index := 0; index < fileCount; index++ {
		name := filepath.Join(workspace, string(rune('a'+index))+".txt")
		if err := os.WriteFile(name, []byte(chunk), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "z-target.txt"), []byte("uniquetargetterm"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewSemanticSearch(workspace).Run(context.Background(), json.RawMessage(`{"query":"uniquetargetterm"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content[0].Text, "z-target.txt") {
		t.Fatal("semantic search continued reading after its total byte budget")
	}
}

func TestSemanticSearchStopsAtFileCountBudget(t *testing.T) {
	workspace := t.TempDir()
	for index := 0; index < maxSemanticFiles; index++ {
		name := filepath.Join(workspace, fmt.Sprintf("%05d-empty.txt", index))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "z-target.txt"), []byte("uniquetargetterm"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewSemanticSearch(workspace).Run(context.Background(), json.RawMessage(`{"query":"uniquetargetterm"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content[0].Text, "z-target.txt") {
		t.Fatal("semantic search continued walking after its file count budget")
	}
}

func TestSemanticSearchRankingAndOutputAreDeterministic(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"b.txt", "a.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("stable ranking phrase"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	search := NewSemanticSearch(workspace)
	var previous string
	for attempt := 0; attempt < 10; attempt++ {
		result, err := search.Run(context.Background(), json.RawMessage(`{"query":"stable ranking","limit":2}`))
		if err != nil {
			t.Fatal(err)
		}
		text := result.Content[0].Text
		if !strings.Contains(text, "Local relevance search") || strings.Contains(strings.ToLower(text), "semantic search") {
			t.Fatalf("output overstates TF-IDF capability: %q", text)
		}
		if !strings.Contains(text, "a.txt") || !strings.Contains(text, "b.txt") || strings.Contains(text, "c.txt") {
			t.Fatalf("tie ordering/output limit is not deterministic: %q", text)
		}
		if previous != "" && previous != text {
			t.Fatalf("output changed between runs:\n%s\n%s", previous, text)
		}
		previous = text
	}
}
