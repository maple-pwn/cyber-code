package adapter

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cyber-code/internal/runtimeapi"
	"cyber-code/internal/ui/mission"
)

type ModelOptions struct {
	Context          context.Context
	ClientID         string
	RuntimeID        string
	InitialObjective string
	Width            int
	Height           int
	Demo             bool
	SourceMode       string
	NoColor          bool
}

type Model struct {
	ctx              context.Context
	client           *Client
	runtimeID        string
	initialObjective string
	mission          *mission.Model
}

type operationResultMsg struct{ err error }

func NewModel(source Source, options ModelOptions) *Model {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	client := NewClient(source, ClientOptions{
		ClientID: options.ClientID, ExpectedMode: runtimeapi.SourceMode(strings.ToLower(strings.TrimSpace(options.SourceMode))),
	})
	return &Model{
		ctx: ctx, client: client, runtimeID: options.RuntimeID, initialObjective: strings.TrimSpace(options.InitialObjective),
		mission: mission.NewModel(client.View().State, mission.Options{
			Width: options.Width, Height: options.Height, Demo: options.Demo, SourceMode: options.SourceMode,
			Runtime: options.RuntimeID, Connection: string(ConnectionOffline), NoColor: options.NoColor,
		}),
	}
}

func (model *Model) Init() tea.Cmd {
	return func() tea.Msg {
		if err := model.client.Connect(model.ctx); err != nil {
			return operationResultMsg{err: err}
		}
		if model.initialObjective != "" {
			if source := model.client.View().Source; source != nil {
				model.runtimeID = source.RuntimeID
			}
			err := model.client.Dispatch(model.ctx, Command{
				Type: CommandTaskCreate, Objective: model.initialObjective, RuntimeID: model.runtimeID,
			})
			return operationResultMsg{err: err}
		}
		return operationResultMsg{}
	}
}

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case operationResultMsg:
		model.syncMission(message.err)
		return model, nil
	case mission.ActionMsg:
		return model, model.dispatch(message.Action)
	default:
		updated, command := model.mission.Update(message)
		model.mission = updated.(*mission.Model)
		return model, command
	}
}

func (model *Model) View() string { return model.mission.View() }

func (model *Model) dispatch(action mission.Action) tea.Cmd {
	return func() tea.Msg {
		command, err := CommandForAction(action, model.client.View().State, model.runtimeID)
		if err == nil {
			err = model.client.Dispatch(model.ctx, command)
		}
		return operationResultMsg{err: err}
	}
}

func (model *Model) syncMission(operationErr error) {
	view := model.client.View()
	if view.Source != nil {
		model.runtimeID = view.Source.RuntimeID
		model.mission.SetSource(string(view.Source.Mode), view.Source.RuntimeID)
	}
	model.mission.SetState(view.State)
	detail := view.Connection.ErrorCode
	if operationErr != nil {
		detail = operationErr.Error()
	} else if view.ReadOnly {
		detail = "read-only"
	}
	updated, _ := model.mission.Update(mission.ConnectionMsg{Status: string(view.Connection.Status), Detail: detail})
	model.mission = updated.(*mission.Model)
}
