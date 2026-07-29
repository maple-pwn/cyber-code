package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFilePreviewBoundsLongLinesToRequestedWidth(t *testing.T) {
	preview := FilePreview{
		Path: "internal/very-long-file-name.go", Content: strings.Repeat("界", 80), StartLine: 1, EndLine: 1,
	}
	rendered := preview.Render(32)
	for _, line := range strings.Split(rendered, "\n") {
		if width := ansi.StringWidth(line); width > 32 {
			t.Fatalf("preview line width = %d, want <= 32: %q", width, ansi.Strip(line))
		}
	}
}
