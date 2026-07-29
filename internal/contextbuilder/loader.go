package contextbuilder

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cyber-code/internal/security"
)

type LoaderOptions struct {
	StateDir      string
	Workspace     string
	CurrentDir    string
	MaxFileBytes  int64
	MaxTotalBytes int64
}

type Loader struct {
	stateDir      string
	workspace     string
	currentDir    string
	maxFileBytes  int64
	maxTotalBytes int64
}

func NewLoader(options LoaderOptions) (*Loader, error) {
	workspace, err := canonicalDirectory(options.Workspace, true)
	if err != nil {
		return nil, fmt.Errorf("resolve instruction workspace: %w", err)
	}
	current := options.CurrentDir
	if strings.TrimSpace(current) == "" {
		current = workspace
	}
	current, err = canonicalDirectory(current, true)
	if err != nil {
		return nil, fmt.Errorf("resolve instruction current directory: %w", err)
	}
	if !withinRoot(workspace, current) {
		return nil, fmt.Errorf("%w: current directory %q", ErrPathOutsideRoot, current)
	}
	state := ""
	if strings.TrimSpace(options.StateDir) != "" {
		state, err = canonicalDirectory(options.StateDir, false)
		if err != nil {
			return nil, fmt.Errorf("resolve instruction state directory: %w", err)
		}
	}
	maxFile := options.MaxFileBytes
	if maxFile <= 0 {
		maxFile = DefaultMaxFileBytes
	}
	maxTotal := options.MaxTotalBytes
	if maxTotal <= 0 {
		maxTotal = DefaultMaxTotalBytes
	}
	if maxTotal < maxFile {
		return nil, fmt.Errorf("%w: total byte limit must be at least the file limit", ErrInstructionTooLarge)
	}
	return &Loader{stateDir: state, workspace: workspace, currentDir: current, maxFileBytes: maxFile, maxTotalBytes: maxTotal}, nil
}

func (loader *Loader) Load() ([]Source, error) {
	type candidate struct {
		id       string
		kind     SourceKind
		root     string
		path     string
		priority int
	}
	candidates := make([]candidate, 0, 8)
	if loader.stateDir != "" {
		candidates = append(candidates, candidate{
			id: "user:instructions", kind: SourceUser, root: loader.stateDir,
			path: filepath.Join(loader.stateDir, "instructions.md"), priority: 20,
		})
	}
	candidates = append(candidates, candidate{
		id: "project:CYBER.md", kind: SourceProject, root: loader.workspace,
		path: filepath.Join(loader.workspace, "CYBER.md"), priority: 30,
	})
	directories, err := pathFromRoot(loader.workspace, loader.currentDir)
	if err != nil {
		return nil, err
	}
	for index, directory := range directories {
		relative, err := filepath.Rel(loader.workspace, directory)
		if err != nil {
			return nil, err
		}
		if relative == "." {
			relative = "root"
		}
		candidates = append(candidates, candidate{
			id: "project:" + filepath.ToSlash(relative) + "/instructions", kind: SourceProject,
			root: loader.workspace, path: filepath.Join(directory, ".cyber-code", "instructions.md"), priority: 31 + index,
		})
	}

	var total int64
	result := make([]Source, 0, len(candidates))
	for _, item := range candidates {
		content, size, exists, err := loader.read(item.root, item.path)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		total += size
		if total > loader.maxTotalBytes {
			return nil, fmt.Errorf("%w: instruction total exceeds %d bytes", ErrInstructionTooLarge, loader.maxTotalBytes)
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		result = append(result, Source{
			ID: item.id, Kind: item.kind, Path: item.path, Priority: item.priority,
			Content: content,
		})
	}
	return result, nil
}

func (loader *Loader) read(root, path string) (string, int64, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, fmt.Errorf("inspect instruction %q: %w", path, err)
	}
	file, info, err := security.OpenVerified(root, path)
	if errors.Is(err, security.ErrOutsideRoot) || errors.Is(err, security.ErrPathChanged) {
		return "", 0, false, fmt.Errorf("%w: %q", ErrPathOutsideRoot, path)
	}
	if err != nil {
		return "", 0, false, fmt.Errorf("open instruction %q: %w", path, err)
	}
	defer file.Close()
	if info.Size() > loader.maxFileBytes {
		return "", 0, false, fmt.Errorf("%w: %q exceeds %d bytes", ErrInstructionTooLarge, path, loader.maxFileBytes)
	}
	encoded, err := readBounded(file, loader.maxFileBytes)
	if err != nil {
		return "", 0, false, fmt.Errorf("read instruction %q: %w", path, err)
	}
	if int64(len(encoded)) > loader.maxFileBytes {
		return "", 0, false, fmt.Errorf("%w: %q changed while reading", ErrInstructionTooLarge, path)
	}
	return string(encoded), int64(len(encoded)), true, nil
}

func readBounded(file *os.File, maximum int64) ([]byte, error) {
	if maximum < 0 {
		return nil, fmt.Errorf("negative read limit")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	return content, nil
}

func canonicalDirectory(path string, mustExist bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if !mustExist && errors.Is(err, os.ErrNotExist) {
			return filepath.Clean(absolute), nil
		}
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", path)
	}
	return filepath.Clean(resolved), nil
}

func withinRoot(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func pathFromRoot(root, current string) ([]string, error) {
	relative, err := filepath.Rel(root, current)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: %q", ErrPathOutsideRoot, current)
	}
	result := []string{root}
	if relative == "." {
		return result, nil
	}
	path := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		path = filepath.Join(path, part)
		result = append(result, path)
	}
	return result, nil
}
