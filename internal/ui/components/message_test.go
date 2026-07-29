package components

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapTextPreservesUnicodeAndDisplayWidth(t *testing.T) {
	for _, value := range []string{"東方的東不应被截断", "hello world again", "👩‍💻👩‍💻"} {
		wrapped := wrapText(value, 8)
		if !utf8.ValidString(wrapped) {
			t.Fatalf("invalid UTF-8 for %q: %q", value, wrapped)
		}
		for _, line := range strings.Split(wrapped, "\n") {
			if ansi.StringWidth(line) > 8 {
				t.Fatalf("line exceeds display width: %q (%d)", line, ansi.StringWidth(line))
			}
		}
	}
	if got := wrapText("hello world", 7); got != "hello\nworld" {
		t.Fatalf("word wrapping = %q", got)
	}
}
