package cli

import (
	"errors"
	"path/filepath"
	"testing"

	"cyber-code/internal/filelock"
)

func TestRuntimeOwnerLeaseRejectsASecondOwnerWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.lock")
	first, err := acquireRuntimeOwner(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	if _, err := acquireRuntimeOwner(path); !errors.Is(err, filelock.ErrLocked) {
		t.Fatalf("second owner error = %v", err)
	}
}
