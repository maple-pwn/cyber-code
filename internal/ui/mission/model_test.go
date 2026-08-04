package mission

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
)

func TestViewRendersStableMissionRegions(t *testing.T) {
	t.Parallel()

	model := NewModel(missionState(), Options{
		Width: 120, Height: 40, Demo: true, Runtime: "local", Connection: "live",
	})
	view := ansi.Strip(model.View())
	for _, want := range []string{
		"CYBER", "DEMO", "LOCAL", "Assessment", "RUNNING", "cursor 9",
		"NARRATIVE STREAM", "Recon", "72%", "Scope", "/lab", "Evidence",
		"Observed admin route", "Finding", "HIGH", "VERIFYING", "Report", "DRAFT",
		"APPROVAL REQUIRED", "GET /admin", "Review", "Deny", "Instruction",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q:\n%s", want, view)
		}
	}
}

func TestViewSanitizesControlANSIAndBidiText(t *testing.T) {
	t.Parallel()

	state := missionState()
	state.Task.Title = "\x1b[31mEVIL\x1b[0m\u202e\x00"
	state.Evidence["evidence-1"] = productprotocol.ImmutableEvidence{
		ID: "evidence-1", TaskID: "task-1", Kind: "http", Summary: "safe\u2066text", Data: map[string]any{},
	}
	model := NewModel(state, Options{Width: 100, Height: 30})
	model.input.SetValue("prompt\x1b[2J\u202e\rtext")
	view := modelView(model)
	for _, forbidden := range []string{"\x1b", "\u202e", "\u2066", "\x00", "\r"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("View() retained unsafe text %q: %q", forbidden, view)
		}
	}
	if !strings.Contains(view, "EVIL") || !strings.Contains(view, "safetext") || !strings.Contains(view, "prompttext") {
		t.Fatalf("View() discarded safe content: %q", view)
	}
}

func TestAgentPanelNavigationAndParentReturn(t *testing.T) {
	t.Parallel()

	state := missionState()
	state.Agents["agent-2"] = productprotocol.AgentState{ID: "agent-2", Name: "Verify", Status: "waiting"}
	model := NewModel(state, Options{Width: 100, Height: 30})

	model = update(t, model, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(modelView(model), "AGENT TASKS") {
		t.Fatalf("Ctrl+T did not open the task panel:\n%s", model.View())
	}
	model = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(modelView(model), "AGENT DETAIL") || model.activeAgentID != "agent-2" {
		t.Fatalf("Enter did not open selected agent: active=%q\n%s", model.activeAgentID, model.View())
	}
	model = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	if model.activeAgentID != "agent-1" {
		t.Fatalf("[ selected %q, want agent-1", model.activeAgentID)
	}
	model = update(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.panel != panelStream || model.activeAgentID != "" {
		t.Fatalf("Esc did not return to parent: panel=%q active=%q", model.panel, model.activeAgentID)
	}
}

func TestNarrativeStreamRendersMeaningfulEventsChronologically(t *testing.T) {
	t.Parallel()

	state := missionState()
	state.Timeline = []productprotocol.Event{
		missionEvent(1, "tool.started", `{"callId":"call-1","name":"web_fetch"}`),
		missionEvent(2, "evidence.committed", `{"evidence":{"id":"evidence-1","taskId":"task-1","kind":"http","summary":"Observed admin route","data":{}}}`),
		missionEvent(3, "finding.created", `{"finding":{"id":"finding-1","title":"Admin access","severity":"high","status":"candidate","confidence":"medium","evidenceIds":["evidence-1"]}}`),
	}
	view := modelView(NewModel(state, Options{Width: 120, Height: 40}))
	toolIndex := strings.Index(view, "TOOL  web_fetch")
	evidenceIndex := strings.Index(view, "EVIDENCE  Observed admin route")
	findingIndex := strings.Index(view, "FINDING  Admin access")
	if toolIndex < 0 || evidenceIndex <= toolIndex || findingIndex <= evidenceIndex {
		t.Fatalf("narrative order is not tool -> evidence -> finding:\n%s", view)
	}
}

func TestApprovalRequiresReviewBeforeAllowAndDeniesImmediately(t *testing.T) {
	t.Parallel()

	model := NewModel(missionState(), Options{})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if _, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}); command != nil {
		t.Fatal("confirm dispatched before review")
	}
	model = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !strings.Contains(modelView(model), "Confirm") {
		t.Fatalf("review did not expose confirmation:\n%s", model.View())
	}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assertAction(t, command, Action{Kind: ActionResolveApproval, ApprovalID: "approval-1", Decision: "allow_once"})

	model = NewModel(missionState(), Options{})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyTab})
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assertAction(t, command, Action{Kind: ActionResolveApproval, ApprovalID: "approval-1", Decision: "deny"})
}

func TestResolvedApprovalRestoresInstructionComposer(t *testing.T) {
	t.Parallel()

	state := missionState()
	model := NewModel(state, Options{})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if !model.approvalFocused || model.input.Focused {
		t.Fatal("approval did not take focus")
	}
	approval := state.Approvals["approval-1"]
	approval.Decision = "deny"
	state.Approvals["approval-1"] = approval
	model = update(t, model, StateMsg{State: state})
	if model.approvalFocused || !model.input.Focused {
		t.Fatalf("resolved approval retained focus: approval=%v input=%v", model.approvalFocused, model.input.Focused)
	}
}

func TestInstructionComposerDispatchesSemanticAction(t *testing.T) {
	t.Parallel()

	state := missionState()
	state.Approvals = map[string]productprotocol.ApprovalState{}
	model := NewModel(state, Options{})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("verify access")})
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assertAction(t, command, Action{Kind: ActionSendInstruction, Text: "verify access"})
	if model.input.Value != "" {
		t.Fatalf("composer retained submitted input %q", model.input.Value)
	}
}

func missionState() productstate.State {
	progress := 72.0
	state := productstate.Initial()
	state.Task = &productstate.TaskState{ID: "task-1", Title: "Assessment", Status: "running"}
	state.CommittedCursor = 9
	state.Scope = &productprotocol.ScopeSnapshot{
		ID: "scope-1", Principal: "operator", Workspace: "/lab", Validity: "task",
		Targets: []string{"/lab"}, AllowedActions: []string{"read"}, DeniedActions: []string{"destroy"}, RiskCeiling: "medium",
	}
	state.Agents["agent-1"] = productprotocol.AgentState{ID: "agent-1", Name: "Recon", Status: "running", Progress: &progress, CurrentAction: "Scanning routes"}
	state.Evidence["evidence-1"] = productprotocol.ImmutableEvidence{ID: "evidence-1", TaskID: "task-1", Kind: "http", Summary: "Observed admin route", Data: map[string]any{"path": "/admin"}}
	state.Findings["finding-1"] = productprotocol.FindingState{ID: "finding-1", Title: "Admin access", Severity: "high", Status: "verifying", Confidence: "medium", EvidenceIDs: []string{"evidence-1"}}
	state.Report = &productprotocol.ReportState{ID: "report-1", TaskID: "task-1", Status: "draft", Findings: []productprotocol.ReportFinding{}}
	state.Approvals["approval-1"] = productprotocol.ApprovalState{ApprovalChallenge: productprotocol.ApprovalChallenge{
		ID: "approval-1", AgentID: "agent-1", Action: "GET", Target: "/admin", ParameterDigest: "sha256:abc", Risk: "medium", ExpiresAt: "2026-08-03T12:10:00Z",
	}}
	state.Timeline = []productprotocol.Event{{
		SchemaVersion: 1, EventID: "event-1", TaskID: "task-1", Cursor: 1,
		OccurredAt: "2026-08-03T12:00:01Z", Type: "task.started", Kind: productprotocol.EventKindKnown,
		Source: productprotocol.EventSourceRef{RuntimeID: "scenario-local"}, Payload: json.RawMessage(`{"title":"Assessment"}`),
	}}
	return state
}

func missionEvent(cursor int, eventType, payload string) productprotocol.Event {
	return productprotocol.Event{
		SchemaVersion: 1, EventID: "event-" + eventType, TaskID: "task-1", Cursor: cursor,
		OccurredAt: "2026-08-03T12:00:01Z", Type: eventType, Kind: productprotocol.EventKindKnown,
		Source: productprotocol.EventSourceRef{RuntimeID: "scenario-local"}, Payload: json.RawMessage(payload),
	}
}

func update(t *testing.T, model *Model, message tea.Msg) *Model {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(*Model)
}

func modelView(model *Model) string { return ansi.Strip(model.View()) }

func assertAction(t *testing.T, command tea.Cmd, want Action) {
	t.Helper()
	if command == nil {
		t.Fatalf("missing command for %+v", want)
	}
	message, ok := command().(ActionMsg)
	if !ok || message.Action != want {
		t.Fatalf("command message = %#v, want %#v", message, ActionMsg{Action: want})
	}
}
