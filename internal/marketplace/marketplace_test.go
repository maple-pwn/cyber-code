package marketplace

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerAddsSearchesInstallsAndRemovesClaudeCompatibleMarketplace(t *testing.T) {
	source := marketplaceFixture(t)
	state := t.TempDir()
	manager, err := NewManager(state)
	if err != nil {
		t.Fatal(err)
	}
	added, err := manager.Add(context.Background(), "fixture", source)
	if err != nil {
		t.Fatal(err)
	}
	if added.Name != "fixture-market" || added.Digest == "" {
		t.Fatalf("added marketplace = %#v", added)
	}
	results, err := manager.Search("review")
	if err != nil || len(results) != 1 || results[0].Plugin.Name != "review-kit" {
		t.Fatalf("search results = %#v, error = %v", results, err)
	}
	installed, err := manager.Install(context.Background(), "fixture", "review-kit")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Digest == "" || installed.Version != "1.2.3" || len(installed.Skills) != 3 {
		t.Fatalf("installed plugin = %#v", installed)
	}
	for _, skill := range installed.Skills {
		data, err := os.ReadFile(filepath.Join(state, "skills", skill, "SKILL.md"))
		if err != nil || !strings.Contains(string(data), "review") {
			t.Fatalf("installed skill %q: %q, %v", skill, data, err)
		}
	}
	if err := manager.Remove("review-kit"); err != nil {
		t.Fatal(err)
	}
	for _, skill := range installed.Skills {
		if _, err := os.Stat(filepath.Join(state, "skills", skill)); !os.IsNotExist(err) {
			t.Fatalf("removed skill still exists: %s", skill)
		}
	}
}

func TestSignedManagerRequiresCatalogSignatureAndPinnedPluginDigest(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	source := marketplaceFixture(t)
	signMarketplaceFixture(t, source, "release-1", privateKey)
	manager, err := NewManagerWithOptions(ManagerOptions{StateDir: t.TempDir(), RequireSignatures: true, TrustedKeys: map[string]ed25519.PublicKey{"release-1": publicKey}})
	if err != nil {
		t.Fatal(err)
	}
	added, err := manager.Add(context.Background(), "signed", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background(), added.Alias, "review-kit"); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(added.Root, "plugins", "review-kit", "skills", "review", "SKILL.md"), "tampered")
	if _, err := manager.Install(context.Background(), added.Alias, "review-kit"); err == nil {
		t.Fatal("tampered plugin digest was accepted")
	}
}

func TestManagerRejectsUnpinnedOrCyclicDependencies(t *testing.T) {
	for name, plugins := range map[string]string{
		"unpinned": `[{"name":"base","version":"1.0.0","source":"plugins/base"},{"name":"app","version":"1.0.0","source":"plugins/app","dependencies":{"base":""}}]`,
		"cycle":    `[{"name":"base","version":"1.0.0","source":"plugins/base","dependencies":{"app":"1.0.0"}},{"name":"app","version":"1.0.0","source":"plugins/app","dependencies":{"base":"1.0.0"}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFixtureFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), `{"name":"dependency-market","plugins":`+plugins+`}`)
			for _, pluginName := range []string{"base", "app"} {
				writeFixtureFile(t, filepath.Join(root, "plugins", pluginName, ".claude-plugin", "plugin.json"), `{"name":"`+pluginName+`","version":"1.0.0"}`)
			}
			manager, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Add(context.Background(), name, root); err == nil {
				t.Fatal("invalid dependency graph was accepted")
			}
		})
	}
}

func TestManagerRollsBackFailedUpgradeAndUninstall(t *testing.T) {
	source := marketplaceFixture(t)
	state := t.TempDir()
	manager, err := NewManager(state)
	if err != nil {
		t.Fatal(err)
	}
	added, err := manager.Add(context.Background(), "fixture", source)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := manager.Install(context.Background(), "fixture", "review-kit")
	if err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(state, "skills", installed.Skills[0], "SKILL.md")
	before, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(added.Root, "plugins", "review-kit", "skills", "review", "SKILL.md"), "# review\nupdated")
	writeFixtureFile(t, filepath.Join(added.Root, "plugins", "review-kit", ".claude-plugin", "plugin.json"), `{"name":"review-kit","version":"2.0.0"}`)
	writeFixtureFile(t, filepath.Join(added.Root, ".claude-plugin", "marketplace.json"), `{"name":"fixture-market","version":"2.0.0","plugins":[{"name":"review-kit","version":"2.0.0","source":"./plugins/review-kit"}]}`)
	manager.writeState = func(string, any) error { return errors.New("state unavailable") }
	if _, err := manager.Install(context.Background(), "fixture", "review-kit"); err == nil {
		t.Fatal("failed upgrade was accepted")
	}
	after, err := os.ReadFile(skillPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("failed upgrade replaced active skill: %q, %v", after, err)
	}
	if err := manager.Remove("review-kit"); err == nil {
		t.Fatal("failed uninstall was accepted")
	}
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("failed uninstall removed active skill: %v", err)
	}
}

func TestManagerRejectsTamperedInstalledPathsBeforeUninstall(t *testing.T) {
	state := t.TempDir()
	manager, err := NewManager(state)
	if err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(state, "escape")
	writeFixtureFile(t, filepath.Join(escape, "SKILL.md"), "keep")
	writeFixtureFile(t, manager.installsPath(), `{"review-kit":{"name":"review-kit","marketplace":"fixture","digest":"abc","skills":["../escape"]}}`)
	if err := manager.Remove("review-kit"); err == nil {
		t.Fatal("tampered installed skill path was accepted")
	}
	if _, err := os.Stat(filepath.Join(escape, "SKILL.md")); err != nil {
		t.Fatalf("tampered uninstall touched escaped path: %v", err)
	}
}

func TestManagerRejectsUnknownFieldsTraversalAndSymlinks(t *testing.T) {
	for name, mutate := range map[string]func(string){
		"unknown field": func(root string) {
			writeFixtureFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), `{"name":"bad","plugins":[],"dangerous":true}`)
		},
		"source traversal": func(root string) {
			writeFixtureFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), `{"name":"bad","plugins":[{"name":"bad","source":"../outside"}]}`)
		},
		"plugin symlink": func(root string) {
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(root, "plugins", "review-kit", "escape")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := marketplaceFixture(t)
			mutate(root)
			manager, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Add(context.Background(), "bad", root); err == nil {
				t.Fatal("unsafe marketplace was accepted")
			}
		})
	}
}

func marketplaceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), `{
  "name":"fixture-market",
  "version":"1.0.0",
  "plugins":[{"name":"review-kit","description":"Review code safely","version":"1.2.3","source":"./plugins/review-kit","category":"productivity"}]
}`)
	plugin := filepath.Join(root, "plugins", "review-kit")
	writeFixtureFile(t, filepath.Join(plugin, ".claude-plugin", "plugin.json"), `{"name":"review-kit","version":"1.2.3","description":"Review code"}`)
	writeFixtureFile(t, filepath.Join(plugin, "skills", "review", "SKILL.md"), "# review\nReview the selected code.")
	writeFixtureFile(t, filepath.Join(plugin, "commands", "audit.md"), "---\ndescription: review command\n---\nreview the repository")
	writeFixtureFile(t, filepath.Join(plugin, "agents", "reviewer.md"), "---\ndescription: review agent\n---\nreview changes")
	return root
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func signMarketplaceFixture(t *testing.T, root, keyID string, privateKey ed25519.PrivateKey) {
	t.Helper()
	catalog, err := loadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Plugins {
		pluginRoot, err := resolveInside(root, catalog.Plugins[index].Source)
		if err != nil {
			t.Fatal(err)
		}
		catalog.Plugins[index].Digest, err = treeDigest(pluginRoot)
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), string(data))
	signature, err := SignCatalog(catalog, keyID, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.MarshalIndent(signature, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(root, ".claude-plugin", "marketplace.sig.json"), string(data))
}
