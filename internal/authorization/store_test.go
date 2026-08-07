package authorization

import (
	"os"
	"path/filepath"
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
