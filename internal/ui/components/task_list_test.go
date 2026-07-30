package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cyber-code/internal/core"
)

func TestTaskListSortsSelectsAndRendersTaskSummaries(t *testing.T) {
	list := NewTaskList()
	list.SetEntries([]TaskListEntry{
		{ID: "task-b", Description: "second", Status: "failed", Truncated: true},
		{ID: "task-a", Agent: "reviewer", Description: "first", Status: "running", Usage: core.Usage{InputTokens: 8, OutputTokens: 3}},
	})
	if selected, ok := list.Selected(); !ok || selected.ID != "task-a" {
		t.Fatalf("initial selection = %#v, %t", selected, ok)
	}
	list.Update(tea.KeyMsg{Type: tea.KeyDown})
	if selected, ok := list.Selected(); !ok || selected.ID != "task-b" {
		t.Fatalf("down selection = %#v, %t", selected, ok)
	}
	view := list.View(80, 10)
	for _, expected := range []string{"task-a", "reviewer", "running", "tokens 11", "task-b", "failed", "truncated"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("task list missing %q: %q", expected, view)
		}
	}
}
