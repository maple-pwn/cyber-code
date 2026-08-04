package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
	"cyber-code/internal/ui/mission"
)

func TestCommandForActionMapsTacticalOperationsExactly(t *testing.T) {
	t.Parallel()

	state := productstate.Initial()
	state.Scope = &productprotocol.ScopeSnapshot{ID: "scope-7"}
	state.HighestCommittedLeaseRevision = 4
	tests := []struct {
		name   string
		action mission.Action
		want   Command
	}{
		{name: "create", action: mission.Action{Kind: mission.ActionCreateTask, Text: "Assess juice-shop.lab"}, want: Command{Type: CommandTaskCreate, Objective: "Assess juice-shop.lab", RuntimeID: "scenario-local"}},
		{name: "confirm scope", action: mission.Action{Kind: mission.ActionConfirmScope}, want: Command{Type: CommandScopeConfirm, ScopeID: "scope-7"}},
		{name: "pause", action: mission.Action{Kind: mission.ActionPauseTask}, want: Command{Type: CommandTaskPause}},
		{name: "resume", action: mission.Action{Kind: mission.ActionResumeTask}, want: Command{Type: CommandTaskResume}},
		{name: "cancel", action: mission.Action{Kind: mission.ActionCancelTask}, want: Command{Type: CommandTaskCancel}},
		{name: "approval", action: mission.Action{Kind: mission.ActionResolveApproval, ApprovalID: "approval-1", Decision: "allow_once"}, want: Command{Type: CommandApprovalRespond, ChallengeID: "approval-1", Decision: "allow_once"}},
		{name: "take control", action: mission.Action{Kind: mission.ActionTakeControl}, want: Command{Type: CommandControlTake, ExpectedRevision: 4}},
		{name: "instruction", action: mission.Action{Kind: mission.ActionSendInstruction, Text: "Check headers"}, want: Command{Type: CommandInstructionSend, Content: "Check headers"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CommandForAction(test.action, state, "scenario-local")
			if err != nil {
				t.Fatalf("CommandForAction() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("CommandForAction() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestClientRecoversCursorGapFromSnapshotAndResubscribes(t *testing.T) {
	t.Parallel()

	snapshotState := projectEvents(t, productstate.Initial(), testEvent(1, "task.created", `{"title":"Assessment"}`))
	source := &scriptedSource{snapshot: Snapshot{Cursor: 1, State: snapshotState}}
	source.subscribe = func(after int, receive func(json.RawMessage)) error {
		source.after = append(source.after, after)
		if len(source.after) == 1 {
			receive(rawTestEvent(t, testEvent(2, "task.started", `{"title":"Assessment"}`)))
		} else {
			receive(rawTestEvent(t, testEvent(2, "task.started", `{"title":"Assessment"}`)))
		}
		return nil
	}
	client := NewClient(source, ClientOptions{ClientID: "tui-client"})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	view := client.View()
	if view.Connection.Status != ConnectionHealthy || view.State.CommittedCursor != 2 {
		t.Fatalf("recovered view = %#v", view)
	}
	if !reflect.DeepEqual(source.after, []int{0, 1}) || source.snapshotCalls != 1 {
		t.Fatalf("subscribe cursors = %v, snapshots = %d", source.after, source.snapshotCalls)
	}
}

func TestClientDisablesWritesWhenOfflineIncompatibleOrDisplaced(t *testing.T) {
	t.Parallel()

	t.Run("offline", func(t *testing.T) {
		source := &scriptedSource{}
		client := NewClient(source, ClientOptions{ClientID: "tui-client"})
		if err := client.Disconnect(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := client.Dispatch(context.Background(), Command{Type: CommandTaskPause}); !errors.Is(err, ErrWritesDisabled) {
			t.Fatalf("Dispatch() error = %v", err)
		}
	})

	t.Run("incompatible", func(t *testing.T) {
		source := &scriptedSource{}
		source.subscribe = func(_ int, receive func(json.RawMessage)) error {
			raw := rawTestEvent(t, testEvent(1, "task.created", `{"title":"Assessment"}`))
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			value["schemaVersion"] = 2
			raw, _ = json.Marshal(value)
			receive(raw)
			receive(rawTestEvent(t, testEvent(1, "task.created", `{"title":"must not apply"}`)))
			return nil
		}
		client := NewClient(source, ClientOptions{ClientID: "tui-client"})
		_ = client.Connect(context.Background())
		if got := client.View().Connection.Status; got != ConnectionIncompatible {
			t.Fatalf("connection = %q", got)
		}
		if got := client.View().State.CommittedCursor; got != 0 || source.unsubscribed != 1 {
			t.Fatalf("incompatible stream advanced to %d or remained subscribed (%d)", got, source.unsubscribed)
		}
		if err := client.Dispatch(context.Background(), Command{Type: CommandTaskPause}); !errors.Is(err, ErrWritesDisabled) {
			t.Fatalf("Dispatch() error = %v", err)
		}
	})

	t.Run("displaced", func(t *testing.T) {
		source := &scriptedSource{}
		source.subscribe = func(_ int, receive func(json.RawMessage)) error {
			receive(rawTestEvent(t, testEvent(1, "control.acquired", `{"lease":{"clientId":"web-client","revision":1}}`)))
			return nil
		}
		client := NewClient(source, ClientOptions{ClientID: "tui-client"})
		if err := client.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !client.View().ReadOnly {
			t.Fatal("displaced controller remained writable")
		}
		if err := client.Dispatch(context.Background(), Command{Type: CommandTaskPause}); !errors.Is(err, ErrWritesDisabled) {
			t.Fatalf("Dispatch() error = %v", err)
		}
	})
}

func TestScenarioSourceRunsAllowWorkflow(t *testing.T) {
	t.Parallel()

	source, err := NewScenarioSource(ScenarioOptions{RuntimeID: "scenario-local"})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(source, ClientOptions{ClientID: "tui-client"})
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	commands := []Command{
		{Type: CommandTaskCreate, Objective: "Assess juice-shop.lab", RuntimeID: "scenario-local"},
		{Type: CommandScopeConfirm, ScopeID: "scope-1"},
		{Type: CommandApprovalRespond, ChallengeID: "approval-1", Decision: "allow_once"},
	}
	for _, command := range commands {
		if err := client.Dispatch(ctx, command); err != nil {
			t.Fatalf("Dispatch(%s) error = %v", command.Type, err)
		}
	}
	view := client.View()
	if view.State.Task == nil || view.State.Task.Status != "completed" {
		t.Fatalf("task = %#v", view.State.Task)
	}
	if finding := view.State.Findings["finding-login-injection"]; finding.Status != "confirmed" {
		t.Fatalf("finding = %#v", finding)
	}
	if _, ok := view.State.Evidence["evidence-bounded-verification"]; !ok {
		t.Fatal("bounded verification evidence was not committed")
	}
	if got := source.Events()[0].OccurredAt; got != "2026-08-03T12:00:01.000Z" {
		t.Fatalf("scenario timestamp = %q", got)
	}
}

func TestScenarioLocalFailureBlocksAgentWithoutRemoteFallback(t *testing.T) {
	t.Parallel()

	source, err := NewScenarioSource(ScenarioOptions{RuntimeID: "scenario-local", InjectFailureAt: "recon"})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(source, ClientOptions{ClientID: "tui-client"})
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	for _, command := range []Command{
		{Type: CommandTaskCreate, Objective: "Assess juice-shop.lab", RuntimeID: "scenario-local"},
		{Type: CommandScopeConfirm, ScopeID: "scope-1"},
	} {
		if err := client.Dispatch(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	view := client.View()
	if view.State.Task == nil || view.State.Task.Status != "blocked" {
		t.Fatalf("task = %#v", view.State.Task)
	}
	if agent := view.State.Agents["agent-recon"]; agent.Status != "failed" {
		t.Fatalf("agent = %#v", agent)
	}
	for _, event := range source.Events() {
		if event.Source.RuntimeID != "scenario-local" {
			t.Fatalf("event %q fell back to %q", event.EventID, event.Source.RuntimeID)
		}
	}
}

func TestScenarioSourceMapsLifecycleControlAndInstructionCommands(t *testing.T) {
	t.Parallel()

	source, err := NewScenarioSource(ScenarioOptions{RuntimeID: "scenario-local"})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(source, ClientOptions{ClientID: "tui-client"})
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	commands := []Command{
		{Type: CommandTaskCreate, Objective: "Assess juice-shop.lab", RuntimeID: "scenario-local"},
		{Type: CommandScopeConfirm, ScopeID: "scope-1"},
		{Type: CommandTaskPause},
		{Type: CommandTaskResume},
		{Type: CommandControlTake, ExpectedRevision: 0},
		{Type: CommandControlTake, ExpectedRevision: 1},
		{Type: CommandInstructionSend, Content: "Check headers"},
		{Type: CommandInstructionSend, Content: "request_scope_revision"},
		{Type: CommandTaskCancel},
	}
	for _, command := range commands {
		if err := client.Dispatch(ctx, command); err != nil {
			t.Fatalf("Dispatch(%#v) error = %v", command, err)
		}
	}
	types := make([]string, 0, len(source.Events()))
	for _, event := range source.Events() {
		types = append(types, event.Type)
	}
	for _, want := range []string{
		"task.paused", "task.resumed", "control.acquired", "control.transferred",
		"question.resolved", "scope.proposed", "task.cancel.requested", "task.cancelled",
	} {
		if !contains(types, want) {
			t.Fatalf("events missing %q: %v", want, types)
		}
	}
	if got := client.View().State.Scope.ID; got != "scope-2" {
		t.Fatalf("revised scope = %q", got)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type scriptedSource struct {
	subscribe     func(int, func(json.RawMessage)) error
	snapshot      Snapshot
	after         []int
	snapshotCalls int
	sent          []Command
	unsubscribed  int
}

func (source *scriptedSource) Subscribe(_ context.Context, after int, receive func(json.RawMessage)) (func(), error) {
	if source.subscribe != nil {
		if err := source.subscribe(after, receive); err != nil {
			return nil, err
		}
	}
	return func() { source.unsubscribed++ }, nil
}

func (source *scriptedSource) Snapshot(context.Context) (Snapshot, error) {
	source.snapshotCalls++
	return source.snapshot, nil
}

func (source *scriptedSource) Send(_ context.Context, command Command) error {
	source.sent = append(source.sent, command)
	return nil
}

func (*scriptedSource) Close(context.Context) error { return nil }

func projectEvents(t *testing.T, state productstate.State, events ...productprotocol.Event) productstate.State {
	t.Helper()
	for _, event := range events {
		result, err := productstate.Project(state, event)
		if err != nil {
			t.Fatal(err)
		}
		state = result.State
	}
	return state
}

func testEvent(cursor int, eventType, payload string) productprotocol.Event {
	return productprotocol.Event{
		SchemaVersion: 1, EventID: "event-" + eventType, TaskID: "task-1", Cursor: cursor,
		OccurredAt: "2026-08-03T12:00:01Z", Type: eventType,
		Source: productprotocol.EventSourceRef{RuntimeID: "scenario-local"}, Payload: json.RawMessage(payload),
		Kind: productprotocol.EventKindKnown,
	}
}

func rawTestEvent(t *testing.T, event productprotocol.Event) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
