package permissions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateRequestPaths(workspace string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("workspace is required for filesystem access")
	}
	for _, requestedPath := range paths {
		if _, err := ResolvePath(workspace, requestedPath); err != nil {
			return err
		}
	}
	return nil
}

// ResolvePath resolves a path through its existing ancestors and rejects paths
// outside workspace. Callers that perform filesystem I/O should invoke it as
// close to the operation as possible to recheck authorization-time paths.
func ResolvePath(workspace, requestedPath string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("workspace is required for filesystem access")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace links: %w", err)
	}
	resolved, err := resolvePath(root, requestedPath)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("compare path to workspace: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return resolved, nil
}

func resolvePath(workspace, requestedPath string) (string, error) {
	if strings.TrimSpace(requestedPath) == "" {
		return "", fmt.Errorf("path is empty")
	}
	path := requestedPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	absolute = filepath.Clean(absolute)

	existing := absolute
	var missing []string
	for {
		_, err = os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect path: %w", err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("path has no existing ancestor")
		}
		missing = append(missing, filepath.Base(existing))
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("resolve path links: %w", err)
	}
	for index := len(missing) - 1; index >= 0; index-- {
		resolved = filepath.Join(resolved, missing[index])
	}
	return filepath.Clean(resolved), nil
}
