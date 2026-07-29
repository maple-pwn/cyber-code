package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrOutsideRoot = errors.New("path is outside root")
	ErrPathChanged = errors.New("path changed while opening")
)

// OpenVerified opens path and verifies the opened handle still identifies the
// same regular file reached through path inside root. Callers may read only
// after this function returns successfully.
func OpenVerified(root, path string) (*os.File, os.FileInfo, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve root: %w", err)
	}
	canonicalRoot, err = filepath.Abs(canonicalRoot)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, nil, err
	}
	if !pathWithinRoot(canonicalRoot, resolved) {
		return nil, nil, fmt.Errorf("%w: %q", ErrOutsideRoot, path)
	}
	file, err := os.Open(filepath.Clean(resolved))
	if err != nil {
		return nil, nil, err
	}
	info, err := verifyOpenedPath(canonicalRoot, path, resolved, file)
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, fmt.Errorf("%q is not a regular file", path)
	}
	return file, info, nil
}

func verifyOpenedPath(root, original, resolved string, file *os.File) (os.FileInfo, error) {
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	after, err := filepath.EvalSymlinks(original)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPathChanged, err)
	}
	after, err = filepath.Abs(after)
	if err != nil {
		return nil, err
	}
	if !pathWithinRoot(root, after) {
		return nil, fmt.Errorf("%w: %q", ErrOutsideRoot, original)
	}
	if !samePath(after, resolved) {
		return nil, fmt.Errorf("%w: %q", ErrPathChanged, original)
	}
	current, err := os.Stat(after)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPathChanged, err)
	}
	if !os.SameFile(opened, current) {
		return nil, fmt.Errorf("%w: file identity differs for %q", ErrPathChanged, original)
	}
	return opened, nil
}

func pathWithinRoot(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func samePath(first, second string) bool {
	first, second = filepath.Clean(first), filepath.Clean(second)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(first, second)
	}
	return first == second
}
