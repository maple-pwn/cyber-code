package credential

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileStoreWritesPrivateAtomicValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Save(context.Background(), []byte(`{"access_token":"secret"}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got) != `{"access_token":"secret"}` {
		t.Fatalf("value = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("credential file permissions are too broad: %o", info.Mode().Perm())
	}
}

func TestFileStoreReplacesExistingValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), []byte("second")); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("value = %q, want second", got)
	}
}

func TestFileStoreConcurrentSaveNeverProducesPartialValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	values := [][]byte{[]byte(`{"token":"one"}`), []byte(`{"token":"two"}`), []byte(`{"token":"three"}`)}
	var group sync.WaitGroup
	for _, value := range values {
		group.Add(1)
		go func(value []byte) {
			defer group.Done()
			if err := store.Save(context.Background(), value); err != nil {
				t.Errorf("save: %v", err)
			}
		}(value)
	}
	group.Wait()
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	valid := false
	for _, value := range values {
		if string(got) == string(value) {
			valid = true
		}
	}
	if !valid {
		t.Fatalf("store wrote a partial or unknown value: %q", got)
	}
}

func TestFileStoreReportsAbsentValue(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); err != ErrNotFound {
		t.Fatalf("load absent value error = %v, want ErrNotFound", err)
	}
}
