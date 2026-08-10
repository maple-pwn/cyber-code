package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRuntimeSwitchModelTogglesWithoutReplacingRuntimeModels(t *testing.T) {
	t.Parallel()
	coding := &runtimeSwitchStub{name: "coding"}
	security := &runtimeSwitchStub{name: "cyber-agent"}
	model := NewRuntimeSwitchModel(coding, security, RuntimeCoding)

	if got := model.View(); got != "coding" {
		t.Fatalf("initial view = %q", got)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	model = updated.(*RuntimeSwitchModel)
	if got := model.View(); got != "cyber-agent" || model.ActiveRuntime() != RuntimeSecurity {
		t.Fatalf("security view = %q active=%q", got, model.ActiveRuntime())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	model = updated.(*RuntimeSwitchModel)
	if got := model.View(); got != "coding" || model.ActiveRuntime() != RuntimeCoding {
		t.Fatalf("restored view = %q active=%q", got, model.ActiveRuntime())
	}
	if coding != model.coding || security != model.security {
		t.Fatal("runtime switch replaced a child model and lost its session state")
	}
}

type runtimeSwitchStub struct{ name string }

func (stub *runtimeSwitchStub) Init() tea.Cmd                       { return nil }
func (stub *runtimeSwitchStub) Update(tea.Msg) (tea.Model, tea.Cmd) { return stub, nil }
func (stub *runtimeSwitchStub) View() string                        { return stub.name }
