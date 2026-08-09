package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/productprotocol"
	"cyber-code/internal/runtimeapi"
	"cyber-code/internal/ui/adapter"
)

func TestSecurityRunnerStreamsAuthoritativeEventsWithoutCodingFallback(t *testing.T) {
	t.Parallel()
	source := &securityRunnerSource{}
	runner := newSecurityRunner(source, "cyber-agent-remote")
	events := runner.Run(context.Background(), "Assess the authorized target")

	var got []core.Event
	for event := range events {
		got = append(got, event)
	}
	if source.command.Type != adapter.CommandTaskCreate || source.command.Objective != "Assess the authorized target" {
		t.Fatalf("security command = %#v", source.command)
	}
	if len(got) < 3 || got[0].Type != core.EventTextDelta || got[len(got)-1].Type != core.EventCompleted {
		t.Fatalf("security events = %#v", got)
	}
}

type securityRunnerSource struct {
	receive func(json.RawMessage)
	command adapter.Command
}

func (source *securityRunnerSource) Handshake(context.Context, runtimeapi.HandshakeRequest) (runtimeapi.HandshakeResponse, error) {
	return runtimeapi.HandshakeResponse{ProtocolVersion: 1, RuntimeID: "cyber-agent-remote", Principal: "cyber-agent", Role: "owner", Capabilities: []string{"events", "snapshot", "commands"}, Source: runtimeapi.SourceMetadata{Mode: runtimeapi.SourceModeRemote, RuntimeID: "cyber-agent-remote", Principal: "cyber-agent", Capabilities: []string{"events", "snapshot", "commands"}}}, nil
}

func (source *securityRunnerSource) Subscribe(_ context.Context, _ int, receive func(json.RawMessage)) (func(), error) {
	source.receive = receive
	return func() {}, nil
}

func (source *securityRunnerSource) Snapshot(context.Context) (adapter.Snapshot, error) {
	return adapter.Snapshot{}, nil
}

func (source *securityRunnerSource) Send(_ context.Context, command adapter.Command) error {
	source.command = command
	source.receive(securityProductEvent("task.created", 1))
	source.receive(securityProductEvent("task.completed", 2))
	return nil
}

func (source *securityRunnerSource) Close(context.Context) error { return nil }

func securityProductEvent(eventType string, cursor int) json.RawMessage {
	payload := map[string]any{}
	if eventType == "task.created" {
		payload["title"] = "Security task"
	}
	payloadJSON, _ := json.Marshal(payload)
	event := productprotocol.Event{SchemaVersion: 1, EventID: fmt.Sprintf("event-%d", cursor), TaskID: "security-task-fixed", Cursor: cursor, OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Type: eventType, Source: productprotocol.EventSourceRef{RuntimeID: "cyber-agent-remote"}, Payload: payloadJSON, Kind: productprotocol.EventKindKnown}
	encoded, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}
	return encoded
}
