package mission

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width, model.height = max(20, message.Width), max(8, message.Height)
		model.input.Width = model.width
		return model, nil
	case StateMsg:
		model.SetState(message.State)
		return model, nil
	case ConnectionMsg:
		model.connection = message.Status
		model.connectionDetail = message.Detail
		return model, nil
	case tea.KeyMsg:
		return model.updateKey(message)
	default:
		return model, nil
	}
}

func (model *Model) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyCtrlC {
		return model, tea.Quit
	}
	if key.Type == tea.KeyF2 {
		if model.panel == panelInspector {
			model.returnToStream()
		} else {
			model.panel = panelInspector
			model.activeAgentID = ""
			model.approvalFocused = false
			model.input.Blur()
		}
		return model, nil
	}
	if key.Type == tea.KeyF4 {
		return model, actionCommand(Action{Kind: ActionConfirmScope})
	}
	if key.Type == tea.KeyF5 {
		if model.state.Task != nil && model.state.Task.Status == "paused" {
			return model, actionCommand(Action{Kind: ActionResumeTask})
		}
		return model, actionCommand(Action{Kind: ActionPauseTask})
	}
	if key.Type == tea.KeyF6 {
		return model, actionCommand(Action{Kind: ActionTakeControl})
	}
	if key.Type == tea.KeyF8 {
		return model, actionCommand(Action{Kind: ActionCancelTask})
	}
	if key.Type == tea.KeyCtrlT {
		if model.panel == panelStream {
			model.panel = panelTasks
			model.approvalFocused = false
			model.clampAgentSelection()
		} else {
			model.returnToStream()
		}
		return model, nil
	}

	if model.panel != panelStream {
		return model.updateAgentPanel(key)
	}
	if model.approvalFocused {
		return model.updateApproval(key)
	}
	if key.Type == tea.KeyTab && model.pendingApprovalID() != "" {
		model.approvalFocused = true
		model.input.Blur()
		return model, nil
	}
	if key.Type == tea.KeyEnter && !key.Alt {
		text := strings.TrimSpace(model.input.Value)
		if text == "" {
			return model, nil
		}
		model.input.Clear()
		if model.state.Task == nil {
			return model, actionCommand(Action{Kind: ActionCreateTask, Text: text})
		}
		return model, actionCommand(Action{Kind: ActionSendInstruction, Text: text})
	}
	return model, model.input.Update(key)
}

func (model *Model) updateApproval(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	approvalID := model.pendingApprovalID()
	if approvalID == "" {
		model.approvalFocused = false
		model.input.Focus()
		return model, nil
	}
	if key.Type == tea.KeyEsc || key.Type == tea.KeyTab {
		model.approvalFocused = false
		model.input.Focus()
		return model, nil
	}
	if key.Type != tea.KeyRunes || len(key.Runes) != 1 {
		return model, nil
	}
	switch key.Runes[0] {
	case 'r', 'R':
		model.reviewedApprovalID = approvalID
		return model, nil
	case 'c', 'C':
		if model.reviewedApprovalID != approvalID {
			return model, nil
		}
		return model, actionCommand(Action{Kind: ActionResolveApproval, ApprovalID: approvalID, Decision: "allow_once"})
	case 'd', 'D':
		return model, actionCommand(Action{Kind: ActionResolveApproval, ApprovalID: approvalID, Decision: "deny"})
	default:
		return model, nil
	}
}

func (model *Model) updateAgentPanel(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	ids := model.agentIDs()
	if key.Type == tea.KeyEsc {
		model.returnToStream()
		return model, nil
	}
	if model.panel == panelTasks {
		switch key.Type {
		case tea.KeyUp:
			if len(ids) > 0 {
				model.selectedAgent = (model.selectedAgent - 1 + len(ids)) % len(ids)
			}
		case tea.KeyDown:
			if len(ids) > 0 {
				model.selectedAgent = (model.selectedAgent + 1) % len(ids)
			}
		case tea.KeyEnter:
			if len(ids) > 0 {
				model.activeAgentID = ids[model.selectedAgent]
				model.panel = panelAgent
			}
		}
		return model, nil
	}
	if model.panel == panelAgent && key.Type == tea.KeyRunes && len(key.Runes) == 1 {
		delta := 0
		switch key.Runes[0] {
		case '[':
			delta = -1
		case ']':
			delta = 1
		}
		if delta != 0 && len(ids) > 0 {
			index := sort.SearchStrings(ids, model.activeAgentID)
			if index >= len(ids) || ids[index] != model.activeAgentID {
				index = 0
			}
			index = (index + delta + len(ids)) % len(ids)
			model.selectedAgent, model.activeAgentID = index, ids[index]
		}
	}
	return model, nil
}

func (model *Model) returnToStream() {
	model.panel = panelStream
	model.activeAgentID = ""
	model.approvalFocused = false
	model.input.Focus()
}

func (model *Model) agentIDs() []string {
	ids := make([]string, 0, len(model.state.Agents))
	for id := range model.state.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (model *Model) clampAgentSelection() {
	count := len(model.state.Agents)
	if count == 0 {
		model.selectedAgent = 0
		model.activeAgentID = ""
		if model.panel == panelAgent {
			model.panel = panelTasks
		}
		return
	}
	model.selectedAgent = min(model.selectedAgent, count-1)
	if model.activeAgentID != "" {
		if _, exists := model.state.Agents[model.activeAgentID]; !exists {
			model.activeAgentID = ""
			model.panel = panelTasks
		}
	}
}

func actionCommand(action Action) tea.Cmd {
	return func() tea.Msg { return ActionMsg{Action: action} }
}
