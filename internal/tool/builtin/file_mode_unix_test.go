//go:build !windows

package builtin

import (
	"os"
	"testing"
)

func assertPreservedFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != want {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}
