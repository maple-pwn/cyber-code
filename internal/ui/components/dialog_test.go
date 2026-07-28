package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputDialogUsesRuneCursorForUnicodeEditing(t *testing.T) {
	dialog := NewInputDialog("Question", "回答", "输入")
	dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("你好")})
	if dialog.Value != "你好" || dialog.CursorPos != 2 {
		t.Fatalf("after insert: value=%q cursor=%d", dialog.Value, dialog.CursorPos)
	}
	dialog.Update(tea.KeyMsg{Type: tea.KeyLeft})
	dialog.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if dialog.Value != "好" || dialog.CursorPos != 0 {
		t.Fatalf("after delete: value=%q cursor=%d", dialog.Value, dialog.CursorPos)
	}
}
