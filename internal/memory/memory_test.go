package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

func TestStoreInstancesSerializeConcurrentWrites(t *testing.T) {
	directory := t.TempDir()
	first, err := NewStore(directory, Options{MaxEntries: 50})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(directory, Options{MaxEntries: 50})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errors := make(chan error, 20)
	for index := range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			store := first
			if index%2 == 0 {
				store = second
			}
			errors <- store.Add(context.Background(), Entry{ID: fmt.Sprintf("entry-%d", index), Scope: ScopeProject, Content: fmt.Sprintf("fact %d", index)})
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := first.List(context.Background(), ScopeProject)
	if err != nil || len(entries) != 20 {
		t.Fatalf("entry count=%d err=%v", len(entries), err)
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
