package adapter

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cyber-code/internal/ui/mission"
)

func TestTacticalModelStartsDemoTaskFromInitialObjective(t *testing.T) {
	t.Parallel()

	source, err := NewScenarioSource(ScenarioOptions{RuntimeID: "scenario-local"})
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(source, ModelOptions{
		Context: context.Background(), ClientID: "tui-client", RuntimeID: "scenario-local",
		InitialObjective: "Assess juice-shop.lab", Width: 120, Height: 30, Demo: true,
	})
	command := model.Init()
	if command == nil {
		t.Fatal("Init() did not connect the scenario source")
	}
	updated, _ := model.Update(command())
	model = updated.(*Model)
	view := model.View()
	for _, want := range []string{"CYBER", "DEMO", "Assess juice-shop.lab", "CREATED", "Scope"} {
		if !strings.Contains(view, want) {
			t.Fatalf("tactical view missing %q:\n%s", want, view)
		}
	}
}

func TestTacticalModelDispatchesMissionActionsAndRefreshesState(t *testing.T) {
	t.Parallel()

	source, err := NewScenarioSource(ScenarioOptions{RuntimeID: "scenario-local"})
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(source, ModelOptions{
		Context: context.Background(), ClientID: "tui-client", RuntimeID: "scenario-local",
		InitialObjective: "Assess juice-shop.lab", Width: 100, Height: 30, Demo: true,
	})
	model = runModelCommand(t, model, model.Init())
	updated, command := model.Update(mission.ActionMsg{Action: mission.Action{Kind: mission.ActionConfirmScope}})
	model = updated.(*Model)
	model = runModelCommand(t, model, command)
	if view := model.View(); !strings.Contains(view, "APPROVAL REQUIRED") || !strings.Contains(view, "RUNNING") {
		t.Fatalf("scope confirmation did not refresh the mission:\n%s", view)
	}
}

func runModelCommand(t *testing.T, model *Model, command tea.Cmd) *Model {
	t.Helper()
	if command == nil {
		t.Fatal("missing Bubble Tea command")
	}
	updated, _ := model.Update(command())
	return updated.(*Model)
}
