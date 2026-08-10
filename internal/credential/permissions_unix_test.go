//go:build !windows

package credential

import (
	"os"
	"testing"
)

func assertPrivateStorePermissions(t *testing.T, directory, path string) {
	t.Helper()
	for target, want := range map[string]os.FileMode{directory: 0o700, path: 0o600} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat private path %q: %v", target, err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("private path %q mode=%v", target, info.Mode())
		}
	}
}
