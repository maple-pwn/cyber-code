package tasks

import (
	"context"
	"errors"
	"testing"

	"cyber-code/internal/agent"
	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/provider"
	toolpkg "cyber-code/internal/tool"
)

func TestNewSubAgentRejectsBudgetAboveParent(t *testing.T) {
	parent, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeDefault, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewSubAgent(SubAgentOptions{
		Provider: modelStub{}, ParentBroker: parent,
		ParentMode: permissions.PermissionModeDefault, Mode: permissions.PermissionModePlan,
		ParentMaxTurns: 4, MaxTurns: 5,
	})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestNewSubAgentRejectsPermissionModeEscalation(t *testing.T) {
	parent, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModePlan, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewSubAgent(SubAgentOptions{
		Provider: modelStub{}, ParentBroker: parent,
		ParentMode: permissions.PermissionModePlan, Mode: permissions.PermissionModeAcceptEdits,
		ParentMaxTurns: 4, MaxTurns: 2,
	})
	if !errors.Is(err, ErrPermissionEscalation) {
		t.Fatalf("error = %v", err)
	}
}

func TestNewSubAgentOwnsEngineAndHistoryWhileSharingProvider(t *testing.T) {
	model := modelStub{}
	parent, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeAcceptEdits, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	history := []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{
		Type:     core.ContentToolCall,
		ToolCall: &core.ToolCall{ID: "call-1", Name: "read", Arguments: []byte(`{"path":"before"}`)},
	}}}}
	child, err := NewSubAgent(SubAgentOptions{
		Provider: model, AgentOptions: agent.Options{Model: "child"}, ParentHistory: history,
		ParentBroker: parent, ParentMode: permissions.PermissionModeAcceptEdits, Mode: permissions.PermissionModePlan,
		ParentMaxTurns: 4, MaxTurns: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.Engine == nil || child.Provider != model || child.Broker == nil {
		t.Fatalf("child = %#v", child)
	}
	history[0].Content[0].ToolCall.Arguments[9] = 'X'
	got := child.Engine.History()
	if string(got[0].Content[0].ToolCall.Arguments) != `{"path":"before"}` {
		t.Fatalf("child history aliases parent: %s", got[0].Content[0].ToolCall.Arguments)
	}
	decision, err := child.Broker.Decide(context.Background(), permissions.Request{
		Tool: "write", Action: permissions.ActionWrite, Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != permissions.PermissionBehaviorDeny {
		t.Fatalf("child plan write decision = %#v", decision)
	}
}

func TestNewSubAgentRebindsParentToolRunnerToChildBroker(t *testing.T) {
	parent, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeAcceptEdits, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	parentRunner := toolpkg.NewRunner(nil, parent, toolpkg.RunnerOptions{})
	child, err := NewSubAgent(SubAgentOptions{
		Provider: modelStub{}, AgentOptions: agent.Options{ToolRunner: parentRunner},
		ParentBroker: parent, ParentMode: permissions.PermissionModeAcceptEdits, Mode: permissions.PermissionModePlan,
		ParentMaxTurns: 2, MaxTurns: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.ToolRunner == nil || child.ToolRunner == parentRunner {
		t.Fatal("child reused parent permission-bound tool runner")
	}
}

type modelStub struct{}

func (modelStub) Name() string { return "stub" }
func (modelStub) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{}, nil
}
func (modelStub) Stream(context.Context, core.Request) (<-chan core.Event, error) {
	events := make(chan core.Event)
	close(events)
	return events, nil
}
func (modelStub) CountTokens(context.Context, core.Request) (int, error) { return 0, nil }
