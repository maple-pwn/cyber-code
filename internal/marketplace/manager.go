package marketplace

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxMarketplaceFileBytes  int64 = 16 << 20
	maxMarketplaceTotalBytes int64 = 64 << 20
	gitCloneTimeout                = 2 * time.Minute
)

var compatibleName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type Catalog struct {
	Schema      string        `json:"$schema,omitempty"`
	Name        string        `json:"name"`
	Version     string        `json:"version,omitempty"`
	Description string        `json:"description,omitempty"`
	Owner       any           `json:"owner,omitempty"`
	Plugins     []PluginEntry `json:"plugins"`
}

type PluginEntry struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Version      string            `json:"version,omitempty"`
	Source       string            `json:"source"`
	Digest       string            `json:"digest,omitempty"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
	Category     string            `json:"category,omitempty"`
	Author       any               `json:"author,omitempty"`
}

type Source struct {
	Alias    string `json:"alias"`
	Name     string `json:"name"`
	Origin   string `json:"origin"`
	Root     string `json:"root"`
	Digest   string `json:"digest"`
	Revision string `json:"revision,omitempty"`
}

type SearchResult struct {
	Marketplace Source      `json:"marketplace"`
	Plugin      PluginEntry `json:"plugin"`
}

type Installed struct {
	Name        string   `json:"name"`
	Marketplace string   `json:"marketplace"`
	Version     string   `json:"version,omitempty"`
	Digest      string   `json:"digest"`
	Revision    string   `json:"revision,omitempty"`
	Skills      []string `json:"skills,omitempty"`
}

type ManagerOptions struct {
	StateDir          string
	TrustedKeys       map[string]ed25519.PublicKey
	RequireSignatures bool
}

type Manager struct {
	stateDir          string
	trustedKeys       map[string]ed25519.PublicKey
	requireSignatures bool
	writeState        func(string, any) error
}

func NewManager(stateDir string) (*Manager, error) {
	return NewManagerWithOptions(ManagerOptions{StateDir: stateDir})
}

func NewManagerWithOptions(options ManagerOptions) (*Manager, error) {
	absolute, err := filepath.Abs(options.StateDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, err
	}
	trustedKeys := make(map[string]ed25519.PublicKey, len(options.TrustedKeys))
	for keyID, publicKey := range options.TrustedKeys {
		trustedKeys[keyID] = append(ed25519.PublicKey(nil), publicKey...)
	}
	if options.RequireSignatures && len(trustedKeys) == 0 {
		return nil, fmt.Errorf("trusted marketplace signing keys are required")
	}
	return &Manager{stateDir: absolute, trustedKeys: trustedKeys, requireSignatures: options.RequireSignatures, writeState: writeJSON}, nil
}

func (manager *Manager) Add(ctx context.Context, alias, source string) (Source, error) {
	if !compatibleName.MatchString(alias) {
		return Source{}, fmt.Errorf("invalid marketplace alias %q", alias)
	}
	sources, err := manager.sources()
	if err != nil {
		return Source{}, err
	}
	if _, exists := sources[alias]; exists {
		return Source{}, fmt.Errorf("marketplace %q already exists", alias)
	}
	root := filepath.Join(manager.stateDir, "marketplaces")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Source{}, err
	}
	staging, err := os.MkdirTemp(root, ".marketplace-*")
	if err != nil {
		return Source{}, err
	}
	defer os.RemoveAll(staging)
	snapshot := filepath.Join(staging, "repository")
	revision, err := materialize(ctx, source, snapshot)
	if err != nil {
		return Source{}, err
	}
	catalog, err := loadCatalog(snapshot)
	if err != nil {
		return Source{}, err
	}
	if err := validateCatalog(snapshot, catalog); err != nil {
		return Source{}, err
	}
	if err := manager.verifyCatalog(snapshot, catalog); err != nil {
		return Source{}, err
	}
	digest, err := treeDigest(snapshot)
	if err != nil {
		return Source{}, err
	}
	destination := filepath.Join(root, alias)
	if err := os.Rename(snapshot, destination); err != nil {
		return Source{}, fmt.Errorf("activate marketplace: %w", err)
	}
	record := Source{Alias: alias, Name: catalog.Name, Origin: source, Root: destination, Digest: digest, Revision: revision}
	sources[alias] = record
	if err := manager.writeState(manager.sourcesPath(), sources); err != nil {
		_ = os.RemoveAll(destination)
		return Source{}, err
	}
	return record, nil
}

func (manager *Manager) List() ([]Source, error) {
	sources, err := manager.sources()
	if err != nil {
		return nil, err
	}
	result := make([]Source, 0, len(sources))
	for _, source := range sources {
		result = append(result, source)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Alias < result[j].Alias })
	return result, nil
}

func (manager *Manager) Search(query string) ([]SearchResult, error) {
	sources, err := manager.List()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var result []SearchResult
	for _, source := range sources {
		catalog, err := loadCatalog(source.Root)
		if err != nil {
			return nil, err
		}
		for _, plugin := range catalog.Plugins {
			haystack := strings.ToLower(plugin.Name + " " + plugin.Description + " " + plugin.Category)
			if query == "" || strings.Contains(haystack, query) {
				result = append(result, SearchResult{Marketplace: source, Plugin: plugin})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Plugin.Name == result[j].Plugin.Name {
			return result[i].Marketplace.Alias < result[j].Marketplace.Alias
		}
		return result[i].Plugin.Name < result[j].Plugin.Name
	})
	return result, nil
}

func (manager *Manager) Install(_ context.Context, marketplaceAlias, pluginName string) (Installed, error) {
	if !compatibleName.MatchString(pluginName) {
		return Installed{}, fmt.Errorf("invalid plugin name %q", pluginName)
	}
	sources, err := manager.sources()
	if err != nil {
		return Installed{}, err
	}
	source, exists := sources[marketplaceAlias]
	if !exists {
		return Installed{}, fmt.Errorf("marketplace %q does not exist", marketplaceAlias)
	}
	catalog, err := loadCatalog(source.Root)
	if err != nil {
		return Installed{}, err
	}
	if err := manager.verifyCatalog(source.Root, catalog); err != nil {
		return Installed{}, err
	}
	var entry *PluginEntry
	for index := range catalog.Plugins {
		if catalog.Plugins[index].Name == pluginName {
			entry = &catalog.Plugins[index]
			break
		}
	}
	if entry == nil {
		return Installed{}, fmt.Errorf("plugin %q was not found", pluginName)
	}
	pluginRoot, err := resolveInside(source.Root, entry.Source)
	if err != nil {
		return Installed{}, err
	}
	manifest := pluginManifest{Name: entry.Name, Version: entry.Version, Description: entry.Description, Author: entry.Author}
	if _, statErr := os.Stat(filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")); statErr == nil {
		manifest, err = loadPluginManifest(pluginRoot)
		if err != nil {
			return Installed{}, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Installed{}, statErr
	}
	if manifest.Name != pluginName {
		return Installed{}, fmt.Errorf("plugin manifest name %q does not match %q", manifest.Name, pluginName)
	}
	digest, err := treeDigest(pluginRoot)
	if err != nil {
		return Installed{}, err
	}
	if manager.requireSignatures && entry.Digest == "" {
		return Installed{}, fmt.Errorf("plugin %q does not have a signed digest", pluginName)
	}
	if entry.Digest != "" && !strings.EqualFold(entry.Digest, digest) {
		return Installed{}, fmt.Errorf("plugin %q digest verification failed", pluginName)
	}
	installs, err := manager.installs()
	if err != nil {
		return Installed{}, err
	}
	for dependency, version := range entry.Dependencies {
		installed, exists := installs[dependency]
		if !exists || installed.Version != version {
			return Installed{}, fmt.Errorf("plugin %q requires %s@%s", pluginName, dependency, version)
		}
	}
	previous, exists := installs[pluginName]
	if exists && previous.Digest == digest {
		return Installed{}, fmt.Errorf("plugin %q is already installed", pluginName)
	}
	staging, err := os.MkdirTemp(manager.stateDir, ".marketplace-install-*")
	if err != nil {
		return Installed{}, err
	}
	defer os.RemoveAll(staging)
	if err := copyTree(pluginRoot, filepath.Join(staging, "plugin")); err != nil {
		return Installed{}, err
	}
	skills, err := manager.installPromptComponents(pluginName, pluginRoot, filepath.Join(staging, "skills"))
	if err != nil {
		return Installed{}, err
	}
	version := manifest.Version
	if version == "" {
		version = entry.Version
	}
	installed := Installed{Name: pluginName, Marketplace: marketplaceAlias, Version: version, Digest: digest, Revision: source.Revision, Skills: skills}
	installs[pluginName] = installed
	var old *Installed
	if exists {
		old = &previous
	}
	if err := manager.activateInstall(staging, installed, old, installs); err != nil {
		return Installed{}, err
	}
	return installed, nil
}

func (manager *Manager) Remove(pluginName string) error {
	installs, err := manager.installs()
	if err != nil {
		return err
	}
	installed, exists := installs[pluginName]
	if !exists {
		return fmt.Errorf("plugin %q is not installed", pluginName)
	}
	if err := validateInstalled(installed); err != nil {
		return err
	}
	delete(installs, pluginName)
	return manager.deactivateInstall(installed, installs)
}

type pluginManifest struct {
	Schema      string   `json:"$schema,omitempty"`
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      any      `json:"author,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Repository  any      `json:"repository,omitempty"`
	License     string   `json:"license,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Commands    any      `json:"commands,omitempty"`
	Agents      any      `json:"agents,omitempty"`
	Skills      any      `json:"skills,omitempty"`
	Hooks       any      `json:"hooks,omitempty"`
	MCPServers  any      `json:"mcpServers,omitempty"`
}

func loadCatalog(root string) (Catalog, error) {
	var catalog Catalog
	if err := decodeStrict(filepath.Join(root, ".claude-plugin", "marketplace.json"), &catalog); err != nil {
		return Catalog{}, fmt.Errorf("load Claude-compatible marketplace: %w", err)
	}
	return catalog, nil
}

func loadPluginManifest(root string) (pluginManifest, error) {
	var manifest pluginManifest
	if err := decodeStrict(filepath.Join(root, ".claude-plugin", "plugin.json"), &manifest); err != nil {
		return pluginManifest{}, fmt.Errorf("load Claude-compatible plugin: %w", err)
	}
	if !compatibleName.MatchString(manifest.Name) {
		return pluginManifest{}, fmt.Errorf("invalid plugin manifest name %q", manifest.Name)
	}
	return manifest, nil
}

func validateCatalog(root string, catalog Catalog) error {
	if !compatibleName.MatchString(catalog.Name) || len(catalog.Plugins) > 1024 {
		return fmt.Errorf("invalid marketplace catalog")
	}
	seen := make(map[string]struct{})
	for _, plugin := range catalog.Plugins {
		if !compatibleName.MatchString(plugin.Name) {
			return fmt.Errorf("invalid marketplace plugin name %q", plugin.Name)
		}
		if _, duplicate := seen[plugin.Name]; duplicate {
			return fmt.Errorf("duplicate marketplace plugin %q", plugin.Name)
		}
		seen[plugin.Name] = struct{}{}
		if plugin.Digest != "" && (len(plugin.Digest) != sha256.Size*2 || !isHex(plugin.Digest)) {
			return fmt.Errorf("invalid marketplace plugin digest for %q", plugin.Name)
		}
		path, err := resolveInside(root, plugin.Source)
		if err != nil {
			return err
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return fmt.Errorf("plugin %q source directory is unavailable", plugin.Name)
		}
	}
	if err := validateDependencies(catalog.Plugins); err != nil {
		return err
	}
	_, err := treeDigest(root)
	return err
}

func (manager *Manager) installPromptComponents(pluginName, root, destinationRoot string) ([]string, error) {
	type component struct{ prefix, directory, extension string }
	components := []component{{"", "skills", ""}, {"command-", "commands", ".md"}, {"agent-", "agents", ".md"}}
	var installed []string
	for _, component := range components {
		directory := filepath.Join(root, component.directory)
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), component.extension)
			if !compatibleName.MatchString(name) {
				continue
			}
			var source string
			if component.extension == "" && entry.IsDir() {
				source = filepath.Join(directory, entry.Name(), "SKILL.md")
			} else if component.extension != "" && !entry.IsDir() && filepath.Ext(entry.Name()) == component.extension {
				source = filepath.Join(directory, entry.Name())
			} else {
				continue
			}
			data, err := os.ReadFile(source)
			if err != nil || len(data) > int(maxMarketplaceFileBytes) {
				return nil, fmt.Errorf("read plugin component %q", source)
			}
			skillName := pluginName + "--" + component.prefix + name
			destination := filepath.Join(destinationRoot, skillName)
			if err := os.MkdirAll(destination, 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(destination, "SKILL.md"), data, 0o600); err != nil {
				return nil, err
			}
			installed = append(installed, skillName)
		}
	}
	sort.Strings(installed)
	return installed, nil
}

func (manager *Manager) verifyCatalog(root string, catalog Catalog) error {
	var signature CatalogSignature
	err := decodeStrict(filepath.Join(root, ".claude-plugin", "marketplace.sig.json"), &signature)
	if errors.Is(err, os.ErrNotExist) && !manager.requireSignatures {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load marketplace signature: %w", err)
	}
	return VerifyCatalogSignature(catalog, signature, manager.trustedKeys)
}

func validateDependencies(plugins []PluginEntry) error {
	entries := make(map[string]PluginEntry, len(plugins))
	for _, entry := range plugins {
		entries[entry.Name] = entry
	}
	for _, entry := range plugins {
		for dependency, version := range entry.Dependencies {
			pinned, exists := entries[dependency]
			if !exists || strings.TrimSpace(version) == "" || pinned.Version != version {
				return fmt.Errorf("plugin %q has invalid dependency pin %s@%s", entry.Name, dependency, version)
			}
		}
	}
	visiting := make(map[string]bool, len(entries))
	visited := make(map[string]bool, len(entries))
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("marketplace dependency cycle includes %q", name)
		}
		if visited[name] {
			return nil
		}
		visiting[name] = true
		for dependency := range entries[name].Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		return nil
	}
	for name := range entries {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func isHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func (manager *Manager) activateInstall(staging string, installed Installed, previous *Installed, installs map[string]Installed) error {
	backup, err := os.MkdirTemp(manager.stateDir, ".marketplace-rollback-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(backup)
	pluginPath := filepath.Join(manager.stateDir, "marketplace-plugins", installed.Name)
	pointerPath := filepath.Join(manager.stateDir, "marketplace-plugins", installed.Name+".active.json")
	moved := make(map[string]string)
	moveToBackup := func(path, relative string) error {
		if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
			return nil
		} else if statErr != nil {
			return statErr
		}
		destination := filepath.Join(backup, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		if err := os.Rename(path, destination); err != nil {
			return err
		}
		moved[path] = destination
		return nil
	}
	rollback := func() {
		_ = os.RemoveAll(pluginPath)
		_ = os.Remove(pointerPath)
		for _, skill := range installed.Skills {
			_ = os.RemoveAll(filepath.Join(manager.stateDir, "skills", skill))
		}
		for original, saved := range moved {
			_ = os.MkdirAll(filepath.Dir(original), 0o700)
			_ = os.Rename(saved, original)
		}
	}
	if err := moveToBackup(pluginPath, "plugin"); err != nil {
		return err
	}
	if err := moveToBackup(pointerPath, "active.json"); err != nil {
		rollback()
		return err
	}
	oldSkills := []string(nil)
	if previous != nil {
		oldSkills = previous.Skills
	}
	for _, skill := range oldSkills {
		if err := moveToBackup(filepath.Join(manager.stateDir, "skills", skill), filepath.Join("skills", skill)); err != nil {
			rollback()
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o700); err != nil {
		rollback()
		return err
	}
	if err := os.Rename(filepath.Join(staging, "plugin"), pluginPath); err != nil {
		rollback()
		return err
	}
	for _, skill := range installed.Skills {
		source := filepath.Join(staging, "skills", skill)
		destination := filepath.Join(manager.stateDir, "skills", skill)
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			rollback()
			return err
		}
		if err := os.Rename(source, destination); err != nil {
			rollback()
			return err
		}
	}
	if err := writeJSON(pointerPath, map[string]string{"digest": installed.Digest, "version": installed.Version}); err != nil {
		rollback()
		return err
	}
	if err := manager.writeState(manager.installsPath(), installs); err != nil {
		rollback()
		return err
	}
	return nil
}

func (manager *Manager) deactivateInstall(installed Installed, installs map[string]Installed) error {
	if err := validateInstalled(installed); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(manager.stateDir, ".marketplace-uninstall-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(backup)
	paths := []struct{ original, saved string }{
		{filepath.Join(manager.stateDir, "marketplace-plugins", installed.Name), filepath.Join(backup, "plugin")},
		{filepath.Join(manager.stateDir, "marketplace-plugins", installed.Name+".active.json"), filepath.Join(backup, "active.json")},
	}
	for _, skill := range installed.Skills {
		paths = append(paths, struct{ original, saved string }{filepath.Join(manager.stateDir, "skills", skill), filepath.Join(backup, "skills", skill)})
	}
	moved := make([]struct{ original, saved string }, 0, len(paths))
	rollback := func() {
		for _, path := range moved {
			_ = os.MkdirAll(filepath.Dir(path.original), 0o700)
			_ = os.Rename(path.saved, path.original)
		}
	}
	for _, path := range paths {
		if _, statErr := os.Stat(path.original); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			rollback()
			return statErr
		}
		if err := os.MkdirAll(filepath.Dir(path.saved), 0o700); err != nil {
			rollback()
			return err
		}
		if err := os.Rename(path.original, path.saved); err != nil {
			rollback()
			return err
		}
		moved = append(moved, path)
	}
	if err := manager.writeState(manager.installsPath(), installs); err != nil {
		rollback()
		return err
	}
	return nil
}

func validateInstalled(installed Installed) error {
	if !compatibleName.MatchString(installed.Name) {
		return fmt.Errorf("invalid installed plugin name")
	}
	for _, skill := range installed.Skills {
		if !strings.HasPrefix(skill, installed.Name+"--") || strings.ContainsAny(skill, `/\\`) {
			return fmt.Errorf("invalid installed skill path")
		}
	}
	return nil
}

func materialize(ctx context.Context, source, destination string) (string, error) {
	parsed, err := url.Parse(source)
	if !filepath.IsAbs(source) && err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "https" || parsed.Host == "" {
			return "", fmt.Errorf("marketplace Git source must use HTTPS")
		}
		ref := parsed.Fragment
		parsed.Fragment = ""
		cloneCtx, cancel := context.WithTimeout(ctx, gitCloneTimeout)
		defer cancel()
		args := []string{"clone", "--depth", "1"}
		if ref != "" {
			args = append(args, "--branch", ref)
		}
		args = append(args, "--", parsed.String(), destination)
		if output, err := exec.CommandContext(cloneCtx, "git", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("clone marketplace: %w: %s", err, strings.TrimSpace(string(output)))
		}
		revision, err := exec.CommandContext(cloneCtx, "git", "-C", destination, "rev-parse", "HEAD").Output()
		if err != nil {
			return "", fmt.Errorf("resolve marketplace revision: %w", err)
		}
		return strings.TrimSpace(string(revision)), nil
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return "", copyTree(resolved, destination)
}

func copyTree(source, destination string) error {
	var total int64
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("marketplace symlink is not allowed: %s", relative)
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported marketplace file: %s", relative)
		}
		if info.Size() > maxMarketplaceFileBytes || total+info.Size() > maxMarketplaceTotalBytes {
			return fmt.Errorf("marketplace content exceeds size limits")
		}
		total += info.Size()
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}

func treeDigest(root string) (string, error) {
	hash := sha256.New()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("marketplace symlink is not allowed: %s", relative)
		}
		if !entry.IsDir() {
			files = append(files, relative)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	var total int64
	for _, relative := range files {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return "", err
		}
		if int64(len(data)) > maxMarketplaceFileBytes || total+int64(len(data)) > maxMarketplaceTotalBytes {
			return "", fmt.Errorf("marketplace content exceeds size limits")
		}
		total += int64(len(data))
		_, _ = io.WriteString(hash, filepath.ToSlash(relative))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func resolveInside(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("marketplace source path must be relative")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(canonicalRoot, filepath.Clean(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	relativeToRoot, err := filepath.Rel(canonicalRoot, resolved)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("marketplace source escapes its root")
	}
	return resolved, nil
}

func decodeStrict(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if int64(len(data)) > maxMarketplaceFileBytes {
		return fmt.Errorf("manifest exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("manifest contains multiple JSON values")
	}
	return nil
}

func (manager *Manager) sourcesPath() string {
	return filepath.Join(manager.stateDir, "marketplaces.json")
}
func (manager *Manager) installsPath() string {
	return filepath.Join(manager.stateDir, "marketplace-installs.json")
}

func (manager *Manager) sources() (map[string]Source, error) {
	result := make(map[string]Source)
	return result, readJSON(manager.sourcesPath(), &result)
}

func (manager *Manager) installs() (map[string]Installed, error) {
	result := make(map[string]Installed)
	return result, readJSON(manager.installsPath(), &result)
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return replaceMarketplaceFile(temporary, path)
}
