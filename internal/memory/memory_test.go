package memory

import (
	"context"
	"strings"
	"testing"
)

func TestStoreScopesAndRetrievesRelevantMemory(t *testing.T) {
	store, err := NewStore(t.TempDir(), Options{MaxEntries: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(context.Background(), Entry{ID: "project-1", Scope: ScopeProject, Content: "Use Go modules and table driven tests", Tags: []string{"go"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(context.Background(), Entry{ID: "user-1", Scope: ScopeUser, Content: "Prefer concise Chinese output"}); err != nil {
		t.Fatal(err)
	}
	results, err := store.Retrieve(context.Background(), ScopeProject, "Go tests", 3)
	if err != nil || len(results) != 1 || results[0].ID != "project-1" {
		t.Fatalf("results=%#v err=%v", results, err)
	}
}

func TestStoreRejectsSecretsAndPersistsAtomically(t *testing.T) {
	store, err := NewStore(t.TempDir(), Options{MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"api_key=secret", "Authorization: Bearer abc"} {
		if err := store.Add(context.Background(), Entry{Scope: ScopeSession, Content: content}); err == nil {
			t.Fatalf("secret memory accepted: %q", content)
		}
	}
	if err := store.Add(context.Background(), Entry{ID: "one", Scope: ScopeSession, Content: strings.Repeat("x", 100)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(context.Background(), ScopeSession)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries=%#v err=%v", entries, err)
	}
}
