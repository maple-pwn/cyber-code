// Package skill discovers immutable instruction bundles.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxInstructionsBytes = 1 << 20
const (
	maxDiscoveredSkills = 256
	maxTotalSkillBytes  = 8 << 20
)

type Root struct {
	Name string
	Path string
}

type Skill struct {
	Name         string
	Source       string
	Path         string
	Instructions string
}

type Loader struct{ roots []Root }

func NewLoader(roots []Root) (*Loader, error) {
	resolved := make([]Root, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root.Name) == "" {
			return nil, fmt.Errorf("skill root name is required")
		}
		absolute, err := filepath.Abs(root.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve skill root %q: %w", root.Name, err)
		}
		linked, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, fmt.Errorf("resolve skill root %q links: %w", root.Name, err)
		}
		info, err := os.Stat(linked)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("skill root %q is not a directory", root.Name)
		}
		resolved = append(resolved, Root{Name: root.Name, Path: linked})
	}
	return &Loader{roots: resolved}, nil
}

func (loader *Loader) Discover() ([]Skill, error) {
	seen := make(map[string]struct{})
	var result []Skill
	var totalBytes int64
	for _, root := range loader.roots {
		entries, err := os.ReadDir(root.Path)
		if err != nil {
			return nil, fmt.Errorf("read skill root %q: %w", root.Name, err)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("skill %q may not be a symbolic link", entry.Name())
			}
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if _, duplicate := seen[name]; duplicate {
				continue
			}
			directory := filepath.Join(root.Path, name)
			resolved, err := filepath.EvalSymlinks(directory)
			if err != nil || !insideRoot(root.Path, resolved) {
				return nil, fmt.Errorf("skill %q escapes root %q", name, root.Name)
			}
			path := filepath.Join(resolved, "SKILL.md")
			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxInstructionsBytes {
				return nil, fmt.Errorf("skill %q instructions are invalid", name)
			}
			if len(result) >= maxDiscoveredSkills || totalBytes+info.Size() > maxTotalSkillBytes {
				return nil, fmt.Errorf("skill discovery exceeds configured limits")
			}
			seen[name] = struct{}{}
			totalBytes += info.Size()
			result = append(result, Skill{Name: name, Source: root.Name, Path: path})
		}
	}
	return result, nil
}

func insideRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
