package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTailLinesKeepsNewestCompleteLines(t *testing.T) {
	got := tailLines([]string{"one", "two", "three"}, 2)
	if !reflect.DeepEqual(got, []string{"two", "three"}) {
		t.Fatalf("tail = %#v", got)
	}
}

func TestMarkdownRenderingPreservesStructureBoundsWidthAndSanitizesControlSequences(t *testing.T) {
	source := "# Heading\n\n- first\n- second\n\n```go\nfmt.Println(\"hello\")\n```\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n\x1b]52;c;secret\aunsafe"
	rendered, err := renderMarkdown(source, 32)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(rendered)
	for _, want := range []string{"Heading", "first", "fmt.Println", "A", "B", "unsafe"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered markdown missing %q: %q", want, plain)
		}
	}
	if strings.Contains(rendered, "]52;") || strings.Contains(rendered, "secret") {
		t.Fatalf("rendered markdown retained terminal injection: %q", rendered)
	}
	assertRenderedWidth(t, rendered, 32)
}

func TestUnifiedDiffRenderingMarksAdditionsAndDeletionsAndSanitizesInput(t *testing.T) {
	source := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\x1b]52;c;secret\a"
	rendered := renderUnifiedDiff(source, 40)
	plain := ansi.Strip(rendered)
	for _, want := range []string{"-old", "+new", "@@ -1 +1 @@"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered diff missing %q: %q", want, plain)
		}
	}
	if strings.Contains(rendered, "]52;") || strings.Contains(rendered, "secret") {
		t.Fatalf("rendered diff retained terminal injection: %q", rendered)
	}
	assertRenderedWidth(t, rendered, 40)
}

func TestModelViewRendersAssistantMarkdown(t *testing.T) {
	model := NewModel(&uiTestRunner{}, ModelOptions{Width: 60, Height: 16})
	model.AddMessage("assistant", "**bold text**")
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "bold text") || strings.Contains(view, "**bold text**") {
		t.Fatalf("assistant markdown was not rendered: %q", view)
	}
}

func assertRenderedWidth(t *testing.T, rendered string, maximum int) {
	t.Helper()
	for _, line := range strings.Split(rendered, "\n") {
		if width := ansi.StringWidth(line); width > maximum {
			t.Fatalf("rendered line width = %d, maximum = %d: %q", width, maximum, line)
		}
	}
}
