package ui

import (
	"encoding/json"
	"fmt"

	"cyber-code/internal/core"
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
