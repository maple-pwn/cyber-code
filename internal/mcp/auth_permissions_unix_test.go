//go:build !windows

package mcp

import (
	"os"
	"testing"
)

func assertCredentialStorePermissions(t *testing.T, store *CredentialStore) {
	t.Helper()
	for target, want := range map[string]os.FileMode{store.directory: 0o700, store.path: 0o600} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat private path %q: %v", target, err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("private path %q mode=%v", target, info.Mode())
		}
	}
}
