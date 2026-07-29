package collaboration

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"cyber-code/internal/permissions"
)

const (
	maxAgentDefinitions = 128
	maxDefinitionBytes  = 256 << 10
)

var definitionNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type Definition struct {
	Name           string                     `json:"name"`
	Description    string                     `json:"description,omitempty"`
	Instructions   string                     `json:"instructions,omitempty"`
	Model          string                     `json:"model,omitempty"`
	Tools          []string                   `json:"tools,omitempty"`
	MaxTurns       int                        `json:"max_turns"`
	PermissionMode permissions.PermissionMode `json:"permission_mode"`
	Source         string                     `json:"-"`
}

type DefinitionLoader struct {
	roots []string
}

func NewDefinitionLoader(stateDir, workspace string) (*DefinitionLoader, error) {
	if strings.TrimSpace(stateDir) == "" || strings.TrimSpace(workspace) == "" {
		return nil, fmt.Errorf("state directory and workspace are required")
	}
	return &DefinitionLoader{roots: []string{
		filepath.Join(stateDir, "agents"),
		filepath.Join(workspace, ".cyber-code", "agents"),
	}}, nil
}

func (loader *DefinitionLoader) Load() ([]Definition, error) {
	definitions := make([]Definition, 0)
	seen := make(map[string]string)
	for _, root := range loader.roots {
		loaded, err := loadDefinitionRoot(root)
		if err != nil {
			return nil, err
		}
		for _, definition := range loaded {
			if previous, exists := seen[definition.Name]; exists {
				return nil, fmt.Errorf("agent definition %q is duplicated in %s and %s", definition.Name, previous, definition.Source)
			}
			seen[definition.Name] = definition.Source
			definitions = append(definitions, definition)
			if len(definitions) > maxAgentDefinitions {
				return nil, fmt.Errorf("agent definitions exceed %d entries", maxAgentDefinitions)
			}
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions, nil
}

func loadDefinitionRoot(root string) ([]Definition, error) {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect agent definition root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("agent definition root %s is not a directory", root)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var definitions []Definition
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !pathWithin(resolvedRoot, resolved) {
			return nil, fmt.Errorf("agent definition path escapes its root: %s", path)
		}
		definition, err := readDefinition(resolved)
		if err != nil {
			return nil, fmt.Errorf("load agent definition %s: %w", path, err)
		}
		definition.Source = resolved
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func readDefinition(path string) (Definition, error) {
	opened, err := os.Open(path)
	if err != nil {
		return Definition{}, err
	}
	defer opened.Close()
	data, err := io.ReadAll(io.LimitReader(opened, maxDefinitionBytes+1))
	if err != nil {
		return Definition{}, err
	}
	if len(data) > maxDefinitionBytes {
		return Definition{}, fmt.Errorf("definition exceeds %d bytes", maxDefinitionBytes)
	}
	var definition Definition
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return Definition{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Definition{}, fmt.Errorf("definition contains trailing JSON")
		}
		return Definition{}, err
	}
	if !definitionNamePattern.MatchString(definition.Name) {
		return Definition{}, fmt.Errorf("invalid agent name %q", definition.Name)
	}
	if definition.MaxTurns <= 0 || definition.MaxTurns > 100 {
		return Definition{}, fmt.Errorf("max_turns must be between 1 and 100")
	}
	switch definition.PermissionMode {
	case permissions.PermissionModePlan, permissions.PermissionModeDefault, permissions.PermissionModeAcceptEdits:
	default:
		return Definition{}, fmt.Errorf("unsafe permission mode %q", definition.PermissionMode)
	}
	toolSeen := make(map[string]struct{}, len(definition.Tools))
	for _, name := range definition.Tools {
		if !definitionNamePattern.MatchString(name) {
			return Definition{}, fmt.Errorf("invalid tool name %q", name)
		}
		if _, exists := toolSeen[name]; exists {
			return Definition{}, fmt.Errorf("duplicate tool name %q", name)
		}
		toolSeen[name] = struct{}{}
	}
	definition.Tools = append([]string(nil), definition.Tools...)
	return definition, nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
