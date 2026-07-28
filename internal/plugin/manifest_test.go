package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin.json", `{
  "name":"example","version":"1.0.0","entrypoint":"plugin",
  "capabilities":{"process":true,"tools":[]},"tools":[],"unexpected":true
}`)
	writePluginFile(t, root, "plugin", "binary")
	if _, err := LoadManifest(root); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadManifestRejectsEntrypointOutsidePluginRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "plugin")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writePluginFile(t, parent, "outside", "binary")
	writePluginFile(t, root, "plugin.json", `{
  "name":"example","version":"1.0.0","entrypoint":"../outside",
  "capabilities":{"process":true,"tools":[]},"tools":[]
}`)
	if _, err := LoadManifest(root); !errors.Is(err, ErrEntrypointOutsideRoot) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadManifestRejectsSymlinkEntrypointOutsidePluginRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "plugin")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writePluginFile(t, parent, "outside", "binary")
	if err := os.Symlink(filepath.Join(parent, "outside"), filepath.Join(root, "entry")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writePluginFile(t, root, "plugin.json", `{
  "name":"example","version":"1.0.0","entrypoint":"entry",
  "capabilities":{"process":true,"tools":[]},"tools":[]
}`)
	if _, err := LoadManifest(root); !errors.Is(err, ErrEntrypointOutsideRoot) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadManifestRejectsDuplicateAndUndeclaredTools(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{name: "duplicate", manifest: `{
  "name":"example","version":"1.0.0","entrypoint":"plugin",
  "capabilities":{"process":true,"tools":[{"name":"echo","action":"execute"}]},
  "tools":[{"name":"echo","inputSchema":{"type":"object"}},{"name":"echo","inputSchema":{"type":"object"}}]
}`},
		{name: "undeclared", manifest: `{
  "name":"example","version":"1.0.0","entrypoint":"plugin",
  "capabilities":{"process":true,"tools":[]},
  "tools":[{"name":"echo","inputSchema":{"type":"object"}}]
}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writePluginFile(t, root, "plugin", "binary")
			writePluginFile(t, root, "plugin.json", test.manifest)
			if _, err := LoadManifest(root); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLoadManifestReturnsResolvedImmutableManifest(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "plugin", "binary")
	writePluginFile(t, root, "data.txt", "data")
	writePluginFile(t, root, "plugin.json", `{
  "name":"example","version":"1.0.0","entrypoint":"plugin",
  "capabilities":{
    "process":true,
    "files":[{"path":"data.txt","access":"read"}],
    "network":["api.example.test:443"],
    "tools":[{"name":"echo","action":"execute"}]
  },
  "tools":[{"name":"echo","description":"Echo","inputSchema":{"type":"object"}}]
}`)
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(manifest.Entrypoint) || manifest.Name != "example" || len(manifest.Tools) != 1 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if !filepath.IsAbs(manifest.Capabilities.Files[0].Path) {
		t.Fatalf("file capability was not resolved: %#v", manifest.Capabilities.Files)
	}
}

func writePluginFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
