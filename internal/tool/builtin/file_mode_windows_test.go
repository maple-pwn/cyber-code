//go:build windows

package builtin

import (
	"os"
	"testing"
)

func assertPreservedFileMode(t *testing.T, path string, _ os.FileMode) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("atomically replaced file is not accessible to the current user: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close atomically replaced file: %v", err)
	}
}
