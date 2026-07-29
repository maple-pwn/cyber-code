package security

import (
	"errors"
	"net"
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

func TestOpenVerifiedRejectsPathThatCannotBeOpenedAsFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix domain socket fixture is only used for Unix coverage")
	}
	root, err := os.MkdirTemp("/tmp", "cc-safefile-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	socket := filepath.Join(root, "service.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, _, err := OpenVerified(root, socket); err == nil {
		t.Fatal("socket was opened as a regular file")
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

func TestOpenVerifiedRejectsMissingPathsAndDirectories(t *testing.T) {
	root := t.TempDir()
	if _, _, err := OpenVerified(filepath.Join(root, "missing-root"), filepath.Join(root, "file")); err == nil {
		t.Fatal("missing root was accepted")
	}
	if _, _, err := OpenVerified(root, filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing file was accepted")
	}
	if _, _, err := OpenVerified(root, root); err == nil {
		t.Fatal("directory was accepted as a regular file")
	}
}

func TestVerifyOpenedPathRejectsInvalidHandleAndRecheckedPaths(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	for _, file := range []string{first, second} {
		if err := os.WriteFile(file, []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalFirst, err := filepath.EvalSymlinks(first)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyOpenedPath(canonicalRoot, first, canonicalFirst, file); err == nil {
		t.Fatal("closed handle was accepted")
	}

	file, err = os.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := verifyOpenedPath(canonicalRoot, filepath.Join(root, "missing"), canonicalFirst, file); !errors.Is(err, ErrPathChanged) {
		t.Fatalf("missing recheck error = %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyOpenedPath(canonicalRoot, outside, canonicalOutside, file); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("outside recheck error = %v", err)
	}
	if _, err := verifyOpenedPath(canonicalRoot, second, canonicalFirst, file); !errors.Is(err, ErrPathChanged) {
		t.Fatalf("changed path error = %v", err)
	}
}
