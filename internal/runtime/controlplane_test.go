package runtime

import (
	"context"
	"strings"
	"testing"

	"cyber-code/internal/agent"
	"cyber-code/internal/controlplane"
	"cyber-code/internal/core"
	"cyber-code/internal/session"
)

func TestRuntimeDispatchesCommandsWithoutCallingProvider(t *testing.T) {
	registry := controlplane.NewRegistry()
	if err := registry.Register(controlplane.Spec{
		Name: "status", Description: "status", Handler: func(_ context.Context, _ controlplane.Invocation) ([]core.Event, error) {
			return controlplane.TextEvents("ready"), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	runtime := New(nil, agent.Options{})
	runtime.AttachControlPlane(registry)
	defer runtime.Shutdown(context.Background())

	var events []core.Event
	for event := range runtime.Run(context.Background(), "/status") {
		events = append(events, event)
	}
	if len(events) != 2 || events[0].Text != "ready" || events[1].Type != core.EventCompleted {
		t.Fatalf("events = %#v", events)
	}
}

func TestRuntimeReportsUnknownSlashCommand(t *testing.T) {
	runtime := New(nil, agent.Options{})
	registry := controlplane.NewRegistry()
	if err := registry.Register(controlplane.Spec{Name: "status", Handler: func(context.Context, controlplane.Invocation) ([]core.Event, error) {
		return controlplane.TextEvents("ready"), nil
	}}); err != nil {
		t.Fatal(err)
	}
	runtime.AttachControlPlane(registry)
	defer runtime.Shutdown(context.Background())

	var event core.Event
	for event = range runtime.Run(context.Background(), "/missing") {
		if event.Type == core.EventError {
			break
		}
	}
	if event.Type != core.EventError || event.Err == nil || !strings.Contains(event.Err.Error(), "unknown command") {
		t.Fatalf("event = %#v", event)
	}
}

func TestPersistentRuntimeCheckpointAndRewindRestoreEngineHistory(t *testing.T) {
	store, err := session.NewStore(t.TempDir(), session.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewPersistent(&completedProvider{}, agent.Options{}, store, "runtime-graph")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown(context.Background())
	for range runtime.Run(context.Background(), "first") {
	}
	checkpoint, err := runtime.CreateCheckpoint(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	for range runtime.Run(context.Background(), "second") {
	}
	if got := len(runtime.History()); got != 2 {
		t.Fatalf("history before rewind = %d", got)
	}
	if _, err := runtime.Rewind(context.Background(), checkpoint.ID); err != nil {
		t.Fatal(err)
	}
	if got := len(runtime.History()); got != 1 || runtime.History()[0].Content[0].Text != "first" {
		t.Fatalf("history after rewind = %#v", runtime.History())
	}
}
