package mission

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"cyber-code/internal/productprotocol"
)

func TestResponsiveLayoutsStayWithinTerminalAndPreserveCriticalControls(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{{80, 24}, {100, 30}, {120, 40}, {160, 50}} {
		size := size
		t.Run(strconv.Itoa(size.width)+"x"+strconv.Itoa(size.height), func(t *testing.T) {
			t.Parallel()
			model := NewModel(missionState(), Options{Width: size.width, Height: size.height, Demo: true, Runtime: "local", Connection: "live"})
			view := model.View()
			assertTerminalBounds(t, view, size.width, size.height)
			plain := ansi.Strip(view)
			for _, want := range []string{"NARRATIVE STREAM", "APPROVAL REQUIRED", "Pause", "Cancel", "LIVE"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("%dx%d missing %q:\n%s", size.width, size.height, want, plain)
				}
			}
			if size.width < 110 && strings.Contains(plain, "AGENTS · SCOPE · EVIDENCE") {
				t.Fatalf("%dx%d retained the inline Inspector:\n%s", size.width, size.height, plain)
			}
			if size.width == 80 && !strings.Contains(plain, "[^T]Agents") {
				t.Fatalf("80-column controls were truncated:\n%s", plain)
			}
			if size.width >= 110 && !strings.Contains(plain, "AGENTS · SCOPE · EVIDENCE") {
				t.Fatalf("%dx%d omitted the inline Inspector:\n%s", size.width, size.height, plain)
			}
			if size.width >= 110 && !strings.Contains(plain, "●●○ MEDIUM") {
				t.Fatalf("%dx%d truncated the color-independent confidence label:\n%s", size.width, size.height, plain)
			}
		})
	}
}

func TestNarrowLayoutOpensInspectorAsFullScreenSubview(t *testing.T) {
	t.Parallel()

	model := NewModel(missionState(), Options{Width: 80, Height: 24})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyF2})
	rawView := model.View()
	assertTerminalBounds(t, rawView, 80, 24)
	view := ansi.Strip(rawView)
	for _, want := range []string{"INSPECTOR", "Scope", "Evidence", "Finding", "Report"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Inspector missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "NARRATIVE STREAM") {
		t.Fatalf("narrow Inspector was not full-screen:\n%s", view)
	}
	model = update(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.panel != panelStream || !strings.Contains(modelView(model), "NARRATIVE STREAM") {
		t.Fatal("Esc did not return from Inspector")
	}
}

func TestTaskControlKeysDispatchSemanticActions(t *testing.T) {
	t.Parallel()

	model := NewModel(missionState(), Options{})
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyF4})
	assertAction(t, command, Action{Kind: ActionConfirmScope})
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyF5})
	assertAction(t, command, Action{Kind: ActionPauseTask})
	model.state.Task.Status = "paused"
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyF5})
	assertAction(t, command, Action{Kind: ActionResumeTask})
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyF6})
	assertAction(t, command, Action{Kind: ActionTakeControl})
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyF8})
	assertAction(t, command, Action{Kind: ActionCancelTask})
}

func TestControlCQuitsFromEveryTacticalPanel(t *testing.T) {
	t.Parallel()

	for _, panel := range []panelMode{panelStream, panelTasks, panelAgent, panelInspector} {
		model := NewModel(missionState(), Options{})
		model.panel = panel
		_, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if command == nil {
			t.Fatalf("panel %q did not return a quit command", panel)
		}
		if _, ok := command().(tea.QuitMsg); !ok {
			t.Fatalf("panel %q command = %#v", panel, command())
		}
	}
}

func TestNoColorLayoutContainsNoANSI(t *testing.T) {
	t.Parallel()

	view := NewModel(missionState(), Options{Width: 100, Height: 30, NoColor: true}).View()
	if strings.Contains(view, "\x1b") {
		t.Fatalf("no-color view contains ANSI: %q", view)
	}
	assertTerminalBounds(t, view, 100, 30)
}

func TestLayoutBoundsHostileLongAndWideText(t *testing.T) {
	t.Parallel()

	state := missionState()
	state.Task.Title = "東方の長い調査 " + strings.Repeat("very-long-english-label-", 12)
	state.Scope.Workspace = "/workspace/" + strings.Repeat("nested-directory/", 20)
	state.Agents["agent-1"] = productprotocol.AgentState{
		ID: "agent-1", Name: "分析担当者 👩🏽‍💻", Status: "running",
		CurrentAction: "e\u0301vidence \x1b[31mspoof\x1b[0m\u202e " + strings.Repeat("scan ", 30),
	}
	state.Evidence["evidence-1"] = productprotocol.ImmutableEvidence{
		ID: "evidence-1", TaskID: "task-1", Kind: "http",
		Summary: strings.Repeat("東方 evidence 👩🏽‍💻 e\u0301 ", 20), Data: map[string]any{},
	}
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}} {
		view := NewModel(state, Options{Width: size.width, Height: size.height, Connection: "live"}).View()
		assertTerminalBounds(t, view, size.width, size.height)
		if strings.Contains(view, "\x1b[31m") || strings.Contains(view, "\u202e") {
			t.Fatalf("unsafe text survived in %dx%d view", size.width, size.height)
		}
	}
}

func assertTerminalBounds(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("view has %d lines, terminal height is %d:\n%s", len(lines), height, ansi.Strip(view))
	}
	for index, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("line %d has width %d, terminal width is %d: %q", index+1, got, width, ansi.Strip(line))
		}
	}
}
