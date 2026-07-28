package cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

func validatePluginName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("plugin name must be a non-empty safe identifier")
	}
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return fmt.Errorf("plugin name %q contains unsupported characters", name)
	}
	return nil
}

func resolvePluginRoot(stateDir, name string) (string, error) {
	if err := validatePluginName(name); err != nil {
		return "", err
	}
	root, err := filepath.Abs(filepath.Join(stateDir, "plugins"))
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve plugin directory: %w", err)
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return "", fmt.Errorf("resolve plugin %q: %w", name, err)
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("plugin %q escapes the plugin directory", name)
	}
	return candidate, nil
}
