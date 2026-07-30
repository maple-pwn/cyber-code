package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"cyber-code/internal/core"
	"cyber-code/internal/product"
	"cyber-code/internal/ui/components"
)

const (
	maxSubagentMessages = 500
	maxSubagentTools    = 200
)

// SubagentView is the isolated presentation state for one child task.
type SubagentView struct {
	TaskID       string
	Agent        string
	Description  string
	Status       string
	Messages     []Message
	Tools        []ToolPresentation
	Usage        core.Usage
	RecentTool   string
	Err          error
	Truncated    bool
	ScrollOffset int

	messageIndex int
	messageRole  string
	toolIndexes  map[string]int
}

func newSubagentView(taskID string) *SubagentView {
	return &SubagentView{TaskID: taskID, messageIndex: -1, toolIndexes: make(map[string]int)}
}

func (model *Model) applyObservation(event core.Event) {
	payload := event.Subagent
	if payload == nil || payload.TaskID == "" {
		return
	}
	view := model.subagents[payload.TaskID]
	if view == nil {
		view = newSubagentView(payload.TaskID)
		model.subagents[payload.TaskID] = view
	}
	if payload.Agent != "" {
		view.Agent = payload.Agent
	}
	if payload.Description != "" {
		view.Description = payload.Description
	}
	if payload.Status != "" {
		view.Status = payload.Status
	}
	if payload.Usage != nil {
		view.Usage = *payload.Usage
	}
	if payload.RecentTool != "" {
		view.RecentTool = payload.RecentTool
	}
	view.Truncated = view.Truncated || payload.Truncated
	if event.Type == core.EventSubagentEvent && payload.Event != nil {
		view.applyEvent(*payload.Event, payload.Usage == nil)
	}
	if model.showTaskList {
		model.syncTaskList()
	}
}

func (model *Model) handleSubagentNavigation(key tea.KeyMsg) bool {
	if key.Type == tea.KeyCtrlC {
		return false
	}
	if key.Type == tea.KeyCtrlT {
		if model.activeSubagent != "" || model.showTaskList {
			model.activeSubagent = ""
			model.showTaskList = false
		} else {
			model.syncTaskList()
			model.showTaskList = true
		}
		return true
	}
	if key.Type == tea.KeyEsc && (model.activeSubagent != "" || model.showTaskList) {
		model.activeSubagent = ""
		model.showTaskList = false
		return true
	}
	if model.showTaskList {
		if key.Type == tea.KeyEnter {
			if selected, ok := model.taskList.Selected(); ok {
				model.activeSubagent = selected.ID
				model.showTaskList = false
			}
			return true
		}
		model.taskList.Update(key)
		return true
	}
	if model.activeSubagent == "" {
		return false
	}
	if key.Type == tea.KeyPgUp {
		model.scrollBy(model.pageSize())
		return true
	}
	if key.Type == tea.KeyPgDown {
		model.scrollBy(-model.pageSize())
		return true
	}
	if key.Type == tea.KeyRunes && len(key.Runes) == 1 {
		switch key.Runes[0] {
		case '[':
			model.switchSubagent(-1)
		case ']':
			model.switchSubagent(1)
		}
	}
	return true
}

func (model *Model) syncTaskList() {
	entries := make([]components.TaskListEntry, 0, len(model.subagents))
	for _, view := range model.subagents {
		entries = append(entries, components.TaskListEntry{
			ID: view.TaskID, Agent: view.Agent, Description: view.Description, Status: view.Status,
			Usage: view.Usage, RecentTool: view.RecentTool, Truncated: view.Truncated,
		})
	}
	model.taskList.SetEntries(entries)
}

func (model *Model) orderedSubagentIDs() []string {
	ids := make([]string, 0, len(model.subagents))
	for id := range model.subagents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (model *Model) switchSubagent(delta int) {
	ids := model.orderedSubagentIDs()
	if len(ids) == 0 {
		model.activeSubagent = ""
		return
	}
	index := sort.SearchStrings(ids, model.activeSubagent)
	if index >= len(ids) || ids[index] != model.activeSubagent {
		index = 0
	}
	index = (index + delta + len(ids)) % len(ids)
	model.activeSubagent = ids[index]
}

func (model *Model) renderTaskListView() string {
	width, height := max(20, model.Width), max(6, model.Height)
	header := []string{product.Name + " / tasks", "Ctrl+T or Esc: parent  Enter: open  Up/Down: select"}
	footer := append([]string{strings.Repeat("-", width)}, displayLines(model.Input.View())...)
	available := max(1, height-len(header)-len(footer))
	middle := displayLines(model.taskList.View(width, available))
	return strings.Join(append(append(header, middle...), footer...), "\n")
}

func (model *Model) renderSubagentView(view *SubagentView) string {
	width, height := max(20, model.Width), max(6, model.Height)
	tokens := view.Usage.InputTokens + view.Usage.OutputTokens + view.Usage.CacheReadInputTokens + view.Usage.CacheCreationInputTokens
	title := view.Description
	if title == "" {
		title = view.Agent
	}
	if title == "" {
		title = "sub-agent"
	}
	header := []string{
		ansi.Truncate(fmt.Sprintf("%s / %s", product.Name, title), width, ""),
		ansi.Truncate(fmt.Sprintf("%s  status=%s  tokens=%d  Ctrl+T/Esc parent  [/] switch", view.TaskID, view.Status, tokens), width, ""),
	}
	middle := view.renderScrollableContent(width)
	overlays := model.modalOverlayLines()
	footer := append([]string{strings.Repeat("-", width)}, displayLines(model.Input.View())...)
	available := max(0, height-len(header)-len(footer)-len(overlays))
	if view.ScrollOffset > 0 && len(middle) > available {
		available = max(0, available-1)
		view.ScrollOffset = min(view.ScrollOffset, max(0, len(middle)-available))
		overlays = append([]string{fmt.Sprintf("up: scrolled %d lines", view.ScrollOffset)}, overlays...)
	} else {
		view.ScrollOffset = 0
	}
	view.ScrollOffset = min(view.ScrollOffset, max(0, len(middle)-available))
	middle = viewportLines(middle, available, view.ScrollOffset)
	middle = append(middle, overlays...)
	return strings.Join(append(append(header, middle...), footer...), "\n")
}

func (model *Model) modalOverlayLines() []string {
	var overlays []string
	if model.Permission != nil {
		overlays = append(overlays, displayLines(model.Permission.View())...)
	}
	if model.QuestionSelect != nil {
		overlays = append(overlays, displayLines(model.QuestionSelect.View())...)
	}
	if model.QuestionInput != nil {
		overlays = append(overlays, displayLines(model.QuestionInput.View())...)
	}
	return overlays
}

func (view *SubagentView) applyEvent(event core.Event, accumulateUsage bool) {
	switch event.Type {
	case core.EventUserMessage:
		if text := coreMessageText(event.Message); text != "" {
			view.appendMessage("user", text)
		}
	case core.EventTextDelta:
		view.appendDelta("assistant", event.Text)
	case core.EventThinkingDelta:
		view.appendDelta("thinking", event.Text)
	case core.EventAssistantMessage:
		if text := coreMessageText(event.Message); text != "" && view.messageRole != "assistant" {
			view.appendMessage("assistant", text)
		}
	case core.EventToolCall:
		if event.ToolCall != nil {
			view.recordToolCall(event.ToolCall)
		}
	case core.EventToolResult:
		if event.ToolResult != nil {
			view.recordToolResult(event.ToolResult)
		}
	case core.EventUsage:
		if accumulateUsage && event.Usage != nil {
			view.Usage.InputTokens += event.Usage.InputTokens
			view.Usage.OutputTokens += event.Usage.OutputTokens
			view.Usage.CacheReadInputTokens += event.Usage.CacheReadInputTokens
			view.Usage.CacheCreationInputTokens += event.Usage.CacheCreationInputTokens
		}
	case core.EventWarning:
		view.appendMessage("system", event.Text)
	case core.EventCompleted:
		view.messageIndex, view.messageRole = -1, ""
	case core.EventError:
		if event.Err != nil {
			view.Err = event.Err
			view.appendMessage("error", event.Err.Error())
		} else {
			view.Err = fmt.Errorf("sub-agent failed")
			view.appendMessage("error", view.Err.Error())
		}
	}
}

func (view *SubagentView) appendDelta(role, text string) {
	if text == "" {
		return
	}
	if view.messageIndex < 0 || view.messageRole != role || view.messageIndex >= len(view.Messages) {
		view.appendMessage(role, "")
	}
	view.Messages[view.messageIndex].Content += text
}

func (view *SubagentView) appendMessage(role, content string) {
	if len(view.Messages) == maxSubagentMessages {
		copy(view.Messages, view.Messages[1:])
		view.Messages = view.Messages[:len(view.Messages)-1]
		view.Truncated = true
	}
	view.Messages = append(view.Messages, Message{Role: role, Content: content})
	view.messageIndex = len(view.Messages) - 1
	view.messageRole = role
}

func (view *SubagentView) recordToolCall(call *core.ToolCall) {
	input := make(map[string]interface{})
	if len(call.Arguments) > 0 {
		_ = json.Unmarshal(call.Arguments, &input)
	}
	if index, ok := view.toolIndexes[call.ID]; ok {
		view.Tools[index].Name = call.Name
		view.Tools[index].Status = "running"
		view.Tools[index].Input = input
		return
	}
	if len(view.Tools) == maxSubagentTools {
		view.Tools = append([]ToolPresentation(nil), view.Tools[1:]...)
		view.rebuildToolIndexes()
		view.Truncated = true
	}
	view.toolIndexes[call.ID] = len(view.Tools)
	view.Tools = append(view.Tools, ToolPresentation{ID: call.ID, Name: call.Name, Status: "running", Input: input, FilePath: toolFilePath(input)})
	view.RecentTool = call.Name
}

func (view *SubagentView) recordToolResult(result *core.ToolResult) {
	index, ok := view.toolIndexes[result.ToolCallID]
	if !ok {
		return
	}
	status := "succeeded"
	if result.IsError {
		status = "failed"
	}
	view.Tools[index].Status = status
	view.Tools[index].Output = toolResultText(result)
	view.Tools[index].IsError = result.IsError
	if result.Diff != nil {
		view.Tools[index].FilePath = result.Diff.Path
	}
}

func (view *SubagentView) rebuildToolIndexes() {
	view.toolIndexes = make(map[string]int, len(view.Tools))
	for index := range view.Tools {
		view.toolIndexes[view.Tools[index].ID] = index
	}
}

func (view *SubagentView) renderScrollableContent(width int) []string {
	lines := renderMessages(view.Messages, width)
	lines = append(lines, renderToolPresentations(view.Tools, width, view.Status == "running")...)
	if view.Usage != (core.Usage{}) {
		lines = append(lines, fmt.Sprintf("tokens: input=%d output=%d cache_read=%d cache_creation=%d",
			view.Usage.InputTokens, view.Usage.OutputTokens,
			view.Usage.CacheReadInputTokens, view.Usage.CacheCreationInputTokens))
	}
	if view.Truncated {
		lines = append(lines, "warning: earlier sub-agent output was truncated")
	}
	return lines
}
