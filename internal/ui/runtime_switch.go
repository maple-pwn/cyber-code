package ui

import tea "github.com/charmbracelet/bubbletea"

type RuntimeKind string

const (
	RuntimeCoding   RuntimeKind = "coding"
	RuntimeSecurity RuntimeKind = "cyber-agent"
)

type RuntimeSwitchModel struct {
	coding   tea.Model
	security tea.Model
	active   RuntimeKind
}

type runtimeChildMsg struct {
	runtime RuntimeKind
	message tea.Msg
}

type runtimeBatchMsg struct {
	runtime  RuntimeKind
	commands tea.BatchMsg
}

func NewRuntimeSwitchModel(coding, security tea.Model, active RuntimeKind) *RuntimeSwitchModel {
	if active != RuntimeSecurity {
		active = RuntimeCoding
	}
	return &RuntimeSwitchModel{coding: coding, security: security, active: active}
}

func (model *RuntimeSwitchModel) ActiveRuntime() RuntimeKind { return model.active }

func (model *RuntimeSwitchModel) Init() tea.Cmd {
	return tea.Batch(wrapRuntimeCmd(RuntimeCoding, model.coding.Init()), wrapRuntimeCmd(RuntimeSecurity, model.security.Init()))
}

func (model *RuntimeSwitchModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlR {
		if model.active == RuntimeCoding {
			model.active = RuntimeSecurity
		} else {
			model.active = RuntimeCoding
		}
		return model, nil
	}
	if batch, ok := message.(runtimeBatchMsg); ok {
		commands := make([]tea.Cmd, 0, len(batch.commands))
		for _, command := range batch.commands {
			commands = append(commands, wrapRuntimeCmd(batch.runtime, command))
		}
		return model, tea.Batch(commands...)
	}
	if child, ok := message.(runtimeChildMsg); ok {
		if _, quit := child.message.(tea.QuitMsg); quit {
			return model, tea.Quit
		}
		return model.updateRuntime(child.runtime, child.message)
	}
	if _, ok := message.(tea.WindowSizeMsg); ok {
		_, codingCmd := model.updateRuntime(RuntimeCoding, message)
		_, securityCmd := model.updateRuntime(RuntimeSecurity, message)
		return model, tea.Batch(codingCmd, securityCmd)
	}
	return model.updateRuntime(model.active, message)
}

func (model *RuntimeSwitchModel) View() string {
	if model.active == RuntimeSecurity {
		return model.security.View()
	}
	return model.coding.View()
}

func (model *RuntimeSwitchModel) updateRuntime(runtime RuntimeKind, message tea.Msg) (tea.Model, tea.Cmd) {
	child := model.coding
	if runtime == RuntimeSecurity {
		child = model.security
	}
	updated, command := child.Update(message)
	if runtime == RuntimeSecurity {
		model.security = updated
	} else {
		model.coding = updated
	}
	return model, wrapRuntimeCmd(runtime, command)
}

func wrapRuntimeCmd(runtime RuntimeKind, command tea.Cmd) tea.Cmd {
	if command == nil {
		return nil
	}
	return func() tea.Msg {
		message := command()
		if batch, ok := message.(tea.BatchMsg); ok {
			return runtimeBatchMsg{runtime: runtime, commands: batch}
		}
		return runtimeChildMsg{runtime: runtime, message: message}
	}
}
