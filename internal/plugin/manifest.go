// Package plugin implements isolated plugin discovery and execution.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrInvalidManifest       = errors.New("invalid plugin manifest")
	ErrEntrypointOutsideRoot = errors.New("plugin entrypoint is outside plugin root")
)

var pluginNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Manifest struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Description  string       `json:"description,omitempty"`
	Entrypoint   string       `json:"entrypoint"`
	Args         []string     `json:"args,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	Tools        []Tool       `json:"tools,omitempty"`
	Root         string       `json:"-"`
}

type Capabilities struct {
	Process bool             `json:"process"`
	Files   []FileCapability `json:"files,omitempty"`
	Network []string         `json:"network,omitempty"`
	Tools   []ToolCapability `json:"tools,omitempty"`
}

type FileCapability struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

type ToolCapability struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func LoadManifest(root string) (Manifest, error) {
	resolvedRoot, err := resolveDirectory(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve plugin root: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(resolvedRoot, "plugin.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("read plugin manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalidManifest, err)
	}
	manifest.Root = resolvedRoot
	entrypoint, err := resolveInsideRoot(resolvedRoot, manifest.Entrypoint, ErrEntrypointOutsideRoot)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Entrypoint = entrypoint
	for index := range manifest.Capabilities.Files {
		resolved, err := resolveInsideRoot(resolvedRoot, manifest.Capabilities.Files[index].Path, ErrInvalidManifest)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Capabilities.Files[index].Path = resolved
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return cloneManifest(manifest), nil
}

func validateManifest(manifest Manifest) error {
	if !pluginNamePattern.MatchString(manifest.Name) {
		return fmt.Errorf("%w: plugin name must match %s", ErrInvalidManifest, pluginNamePattern)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("%w: version is required", ErrInvalidManifest)
	}
	if !manifest.Capabilities.Process {
		return fmt.Errorf("%w: process capability is required for entrypoint", ErrInvalidManifest)
	}
	for _, file := range manifest.Capabilities.Files {
		if file.Access != "read" && file.Access != "write" {
			return fmt.Errorf("%w: unsupported file access %q", ErrInvalidManifest, file.Access)
		}
	}
	declared := make(map[string]string, len(manifest.Capabilities.Tools))
	for _, capability := range manifest.Capabilities.Tools {
		if !pluginNamePattern.MatchString(capability.Name) || !validToolAction(capability.Action) {
			return fmt.Errorf("%w: invalid tool capability %q", ErrInvalidManifest, capability.Name)
		}
		if _, duplicate := declared[capability.Name]; duplicate {
			return fmt.Errorf("%w: duplicate tool capability %q", ErrInvalidManifest, capability.Name)
		}
		declared[capability.Name] = capability.Action
	}
	seen := make(map[string]struct{}, len(manifest.Tools))
	for _, tool := range manifest.Tools {
		if _, duplicate := seen[tool.Name]; duplicate {
			return fmt.Errorf("%w: duplicate tool %q", ErrInvalidManifest, tool.Name)
		}
		seen[tool.Name] = struct{}{}
		if _, ok := declared[tool.Name]; !ok {
			return fmt.Errorf("%w: tool %q has no declared capability", ErrInvalidManifest, tool.Name)
		}
		var schema map[string]any
		if json.Unmarshal(tool.InputSchema, &schema) != nil || schema["type"] != "object" {
			return fmt.Errorf("%w: tool %q inputSchema must be an object schema", ErrInvalidManifest, tool.Name)
		}
	}
	if len(seen) != len(declared) {
		return fmt.Errorf("%w: tool capabilities and tools must match", ErrInvalidManifest)
	}
	return nil
}

func validToolAction(action string) bool {
	switch action {
	case "read", "write", "execute", "delete", "network":
		return true
	default:
		return false
	}
}

func resolveDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	return resolved, nil
}

func resolveInsideRoot(root, relative string, boundaryError error) (string, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: path must be relative", boundaryError)
	}
	candidate := filepath.Join(root, filepath.Clean(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: %v", boundaryError, err)
	}
	relativeToRoot, err := filepath.Rel(root, resolved)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", boundaryError
	}
	return resolved, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func cloneManifest(manifest Manifest) Manifest {
	manifest.Args = append([]string(nil), manifest.Args...)
	manifest.Capabilities.Files = append([]FileCapability(nil), manifest.Capabilities.Files...)
	manifest.Capabilities.Network = append([]string(nil), manifest.Capabilities.Network...)
	manifest.Capabilities.Tools = append([]ToolCapability(nil), manifest.Capabilities.Tools...)
	manifest.Tools = append([]Tool(nil), manifest.Tools...)
	for index := range manifest.Tools {
		manifest.Tools[index].InputSchema = append(json.RawMessage(nil), manifest.Tools[index].InputSchema...)
	}
	return manifest
}
