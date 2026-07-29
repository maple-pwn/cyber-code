package attachment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyber-code/internal/core"
)

func TestLoadImageValidatesWorkspaceMimeAndSize(t *testing.T) {
	workspace := t.TempDir()
	png := filepath.Join(workspace, "image.png")
	data := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := os.WriteFile(png, data, 0o600); err != nil {
		t.Fatal(err)
	}
	block, err := LoadImage(workspace, "image.png")
	if err != nil {
		t.Fatal(err)
	}
	if block.Type != core.ContentImage || block.MediaType != "image/png" || block.Data == "" || strings.Contains(block.Data, "PNG") {
		t.Fatalf("image block = %#v", block)
	}

	unsupported := filepath.Join(workspace, "image.svg")
	if err := os.WriteFile(unsupported, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImage(workspace, unsupported); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("unsupported image error = %v", err)
	}

	oversized := filepath.Join(workspace, "large.png")
	if err := os.WriteFile(oversized, append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, MaxImageBytes)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImage(workspace, oversized); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("oversized image error = %v", err)
	}
}

func TestLoadImageRejectsTraversalAndEscapingSymlink(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, filepath.Join("..", filepath.Base(filepath.Dir(outside)), filepath.Base(outside))} {
		if _, err := LoadImage(workspace, path); !errors.Is(err, ErrImageOutsideWorkspace) {
			t.Fatalf("outside path %q error = %v", path, err)
		}
	}
	link := filepath.Join(workspace, "linked.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := LoadImage(workspace, link); !errors.Is(err, ErrImageOutsideWorkspace) {
		t.Fatalf("escaping symlink error = %v", err)
	}
}
