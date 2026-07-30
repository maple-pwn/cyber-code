package components

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"cyber-code/internal/core"
)

type TaskListEntry struct {
	ID          string
	Agent       string
	Description string
	Status      string
	Usage       core.Usage
	RecentTool  string
	Truncated   bool
}

type TaskListModel struct {
	entries  []TaskListEntry
	selected int
}

func NewTaskList() *TaskListModel { return &TaskListModel{} }

func (model *TaskListModel) SetEntries(entries []TaskListEntry) {
	selectedID := ""
	if selected, ok := model.Selected(); ok {
		selectedID = selected.ID
	}
	model.entries = append([]TaskListEntry(nil), entries...)
	sort.Slice(model.entries, func(i, j int) bool { return model.entries[i].ID < model.entries[j].ID })
	model.selected = 0
	for index := range model.entries {
		if model.entries[index].ID == selectedID {
			model.selected = index
			break
		}
	}
}

func (model *TaskListModel) Update(key tea.KeyMsg) {
	if len(model.entries) == 0 {
		return
	}
	switch key.Type {
	case tea.KeyUp:
		model.selected = (model.selected - 1 + len(model.entries)) % len(model.entries)
	case tea.KeyDown:
		model.selected = (model.selected + 1) % len(model.entries)
	case tea.KeyHome:
		model.selected = 0
	case tea.KeyEnd:
		model.selected = len(model.entries) - 1
	}
}

func (model *TaskListModel) Selected() (TaskListEntry, bool) {
	if model == nil || len(model.entries) == 0 {
		return TaskListEntry{}, false
	}
	model.selected = min(max(0, model.selected), len(model.entries)-1)
	return model.entries[model.selected], true
}

func (model *TaskListModel) View(width, height int) string {
	if model == nil || len(model.entries) == 0 {
		return "No sub-agent tasks"
	}
	width = max(20, width)
	height = max(1, height)
	start := max(0, model.selected-height+1)
	end := min(len(model.entries), start+height)
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		entry := model.entries[index]
		marker := "  "
		if index == model.selected {
			marker = "> "
		}
		identity := entry.ID
		if entry.Agent != "" {
			identity += " " + entry.Agent
		}
		tokens := entry.Usage.InputTokens + entry.Usage.OutputTokens + entry.Usage.CacheReadInputTokens + entry.Usage.CacheCreationInputTokens
		details := []string{entry.Status}
		if tokens > 0 {
			details = append(details, fmt.Sprintf("tokens %d", tokens))
		}
		if entry.RecentTool != "" {
			details = append(details, "tool "+entry.RecentTool)
		}
		if entry.Truncated {
			details = append(details, "truncated")
		}
		line := fmt.Sprintf("%s%s  %s  %s", marker, identity, entry.Description, strings.Join(details, " | "))
		lines = append(lines, ansi.Truncate(line, width, ""))
	}
	return strings.Join(lines, "\n")
}
