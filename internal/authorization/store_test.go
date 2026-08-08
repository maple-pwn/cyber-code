package authorization

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileStorePersistsAndValidatesAuthorizationSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team", "authorization.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "owner", Role: RoleOwner, Active: true}})
	if err := store.Save(policy); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Members("tenant-a")) != 1 {
		t.Fatal("member was not restored")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	if err := os.WriteFile(path, []byte(`{"members":[],"invitations":[],"sessions":[],"decisions":[{"hash":"forged"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err != ErrAuditTampered {
		t.Fatalf("tampered load = %v", err)
	}
}

func TestFileStoreSerializesConcurrentWritersAndIgnoresCrashTempFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "team", "authorization.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	const writers = 12
	var group sync.WaitGroup
	errorsCh := make(chan error, writers)
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "writer-" + string(rune('a'+index)), Role: RoleViewer, Active: true}})
			errorsCh <- store.Save(policy)
		}(index)
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent save: %v", err)
		}
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("load after concurrent saves: %v", err)
	}
	if err := os.WriteFile(path+".crashed.tmp", []byte("partial snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("load with crash residue: %v", err)
	}
}
