package marketplace

import (
	"context"
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
