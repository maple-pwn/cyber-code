// Package mission implements the terminal-native Tactical Ops product view.
package mission

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cyber-code/internal/productstate"
	"cyber-code/internal/ui/components"
)

type panelMode string

const (
	panelStream    panelMode = "stream"
	panelTasks     panelMode = "tasks"
	panelAgent     panelMode = "agent"
	panelInspector panelMode = "inspector"
)

type ActionKind string

const (
	ActionCreateTask      ActionKind = "task.create"
	ActionConfirmScope    ActionKind = "scope.confirm"
	ActionResolveApproval ActionKind = "approval.resolve"
	ActionSendInstruction ActionKind = "instruction.send"
	ActionPauseTask       ActionKind = "task.pause"
	ActionResumeTask      ActionKind = "task.resume"
	ActionCancelTask      ActionKind = "task.cancel.request"
	ActionTakeControl     ActionKind = "control.take"
)

type Action struct {
	Kind       ActionKind
	ApprovalID string
	Decision   string
	Text       string
}

type ActionMsg struct{ Action Action }

type StateMsg struct{ State productstate.State }

type ConnectionMsg struct {
	Status string
	Detail string
}

type Options struct {
	Width      int
	Height     int
	Demo       bool
	SourceMode string
	Runtime    string
	Connection string
	NoColor    bool
}

type Model struct {
	state            productstate.State
	width            int
	height           int
	demo             bool
	sourceMode       string
	runtime          string
	connection       string
	connectionDetail string
	noColor          bool

	panel              panelMode
	selectedAgent      int
	activeAgentID      string
	approvalFocused    bool
	reviewedApprovalID string
	input              *components.InputModel
}

func NewModel(state productstate.State, options Options) *Model {
	if options.Width <= 0 {
		options.Width = 100
	}
	if options.Height <= 0 {
		options.Height = 30
	}
	if strings.TrimSpace(options.Runtime) == "" {
		options.Runtime = "local"
	}
	if strings.TrimSpace(options.Connection) == "" {
		options.Connection = "unknown"
	}
	input := components.NewInput("Instruction >", "Send guidance to the active task...", options.Width)
	return &Model{
		state: state, width: options.Width, height: options.Height, demo: options.Demo, sourceMode: strings.ToLower(strings.TrimSpace(options.SourceMode)),
		runtime: options.Runtime, connection: options.Connection, noColor: options.NoColor, panel: panelStream, input: input,
	}
}

func (model *Model) SetSource(mode, runtimeID string) {
	model.sourceMode = strings.ToLower(strings.TrimSpace(mode))
	model.runtime = strings.TrimSpace(runtimeID)
}

func (model *Model) Init() tea.Cmd { return nil }

func (model *Model) SetState(state productstate.State) {
	model.state = state
	pending := model.pendingApprovalID()
	if pending == "" {
		model.approvalFocused = false
		model.reviewedApprovalID = ""
		model.input.Focus()
	} else if pending != model.reviewedApprovalID {
		model.reviewedApprovalID = ""
	}
	model.clampAgentSelection()
}
