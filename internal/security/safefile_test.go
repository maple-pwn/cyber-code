package security

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVerifyOpenedPathRejectsFileIdentityChangedBeforeRead(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside.md")
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(inside, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(outside)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalInside, err := filepath.EvalSymlinks(inside)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyOpenedPath(canonicalRoot, inside, canonicalInside, file); !errors.Is(err, ErrPathChanged) {
		t.Fatalf("error = %v, want ErrPathChanged", err)
	}
}

func TestOpenVerifiedAllowsInternalFileAndRejectsExternalSymlink(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside.md")
	if err := os.WriteFile(inside, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, info, err := OpenVerified(root, inside)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if !info.Mode().IsRegular() {
		t.Fatalf("mode = %v", info.Mode())
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows hosts")
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked.md")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenVerified(root, linked); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("error = %v, want ErrOutsideRoot", err)
	}
}
