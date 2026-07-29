package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputMultilineUsesAltEnterAndCtrlJ(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter, Alt: true}, {Type: tea.KeyCtrlJ}} {
		input := NewInput(">", "", 40)
		input.SetValue("first")
		input.Update(key)
		input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
		if input.Value != "first\nsecond" || !input.Multiline {
			t.Fatalf("key = %s, value = %q, multiline = %v", key.String(), input.Value, input.Multiline)
		}
	}
}

func TestInputMultilinePreservesPastedNewlines(t *testing.T) {
	input := NewInput(">", "", 40)
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first\nsecond")})
	if input.Value != "first\nsecond" || !input.Multiline {
		t.Fatalf("pasted value = %q, multiline = %v", input.Value, input.Multiline)
	}
}

func TestInputAcceptsSpaceInStandardAndVimInsertModes(t *testing.T) {
	standard := NewInput(">", "", 40)
	standard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mcp")})
	standard.Update(tea.KeyMsg{Type: tea.KeySpace})
	standard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("add")})
	if standard.Value != "mcp add" {
		t.Fatalf("standard input = %q", standard.Value)
	}

	vimInput := NewInput(">", "", 40)
	vimInput.SetVimEnabled(true)
	vimInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mcp")})
	vimInput.Update(tea.KeyMsg{Type: tea.KeySpace})
	vimInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("add")})
	if vimInput.Value != "mcp add" {
		t.Fatalf("Vim insert input = %q", vimInput.Value)
	}
}

func TestInputReverseHistorySearchCyclesMatchingEntries(t *testing.T) {
	input := NewInput(">", "", 40)
	for _, value := range []string{"inspect repo", "deploy staging", "deploy production"} {
		input.SetValue(value)
		input.Clear()
	}
	input.SetValue("deploy")
	input.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if input.Value != "deploy production" {
		t.Fatalf("first reverse search = %q", input.Value)
	}
	input.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if input.Value != "deploy staging" {
		t.Fatalf("second reverse search = %q", input.Value)
	}
	if !strings.Contains(input.View(), "reverse search") {
		t.Fatalf("search state is not visible: %q", input.View())
	}
}

func TestInputAcceptsDecoderSpaceVariantsAndPastedSpaces(t *testing.T) {
	for _, vimMode := range []bool{false, true} {
		input := NewInput(">", "", 40)
		input.SetVimEnabled(vimMode)
		input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello ")})
		input.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
		input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("world sentence")})
		if input.Value != "hello  world sentence" {
			t.Fatalf("vim=%v input=%q", vimMode, input.Value)
		}
	}
}

func TestVimInputUsesRuneCursorAndSupportsUndoRepeatFind(t *testing.T) {
	input := NewInput(">", "", 40)
	input.SetValue("a你 bc你")
	input.SetVimEnabled(true)
	input.Update(tea.KeyMsg{Type: tea.KeyEsc})

	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if input.Value != "a bc你" || input.CursorPos != 1 {
		t.Fatalf("after unicode delete: value = %q, cursor = %d", input.Value, input.CursorPos)
	}
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if input.Value != "a你 bc你" {
		t.Fatalf("after undo: %q", input.Value)
	}
	input.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if input.Value != "a bc你" {
		t.Fatalf("after redo: %q", input.Value)
	}
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	if input.Value != "a bc你" {
		t.Fatalf("after repeat: %q", input.Value)
	}
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f你")})
	if input.CursorPos != 4 {
		t.Fatalf("find cursor = %d", input.CursorPos)
	}
}

func TestVimInputTogglePreservesText(t *testing.T) {
	input := NewInput(">", "", 40)
	input.SetValue("保留 text")
	input.SetVimEnabled(true)
	input.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if input.CursorPos != 6 {
		t.Fatalf("normal cursor = %d", input.CursorPos)
	}
	input.SetVimEnabled(false)
	if input.Value != "保留 text" || input.CursorPos != 6 {
		t.Fatalf("value = %q, cursor = %d", input.Value, input.CursorPos)
	}
}

func TestVimInputAppliesDeleteChangeYankAndClearsHistory(t *testing.T) {
	input := NewInput(">", "", 40)
	input.SetValue("one two three")
	input.SetVimEnabled(true)
	input.Update(tea.KeyMsg{Type: tea.KeyEsc})
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0dw")})
	if input.Value != "two three" {
		t.Fatalf("delete word = %q", input.Value)
	}
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u0cw")})
	if input.Value != "two three" || input.VimState.Mode != "INSERT" {
		t.Fatalf("change word = %q, mode = %q", input.Value, input.VimState.Mode)
	}
	input.Update(tea.KeyMsg{Type: tea.KeyEsc})
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0vly")})
	if input.vimPersistent.Register != "tw" {
		t.Fatalf("register = %q", input.vimPersistent.Register)
	}

	input.Clear()
	input.Update(tea.KeyMsg{Type: tea.KeyEsc})
	input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if input.Value != "" {
		t.Fatalf("undo restored previous prompt: %q", input.Value)
	}
}

func TestInputHistoryUsesRuneCursor(t *testing.T) {
	input := NewInput(">", "", 40)
	input.SetValue("一")
	input.Clear()
	input.SetValue("二你")
	input.Clear()
	input.Update(tea.KeyMsg{Type: tea.KeyUp})
	input.Update(tea.KeyMsg{Type: tea.KeyUp})
	input.Update(tea.KeyMsg{Type: tea.KeyDown})
	if input.Value != "二你" || input.CursorPos != 2 {
		t.Fatalf("history value = %q, cursor = %d", input.Value, input.CursorPos)
	}
}
