package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
)

type permissionRuntime struct{ broker *PermissionBroker }

func (runtime permissionRuntime) Run(ctx context.Context, _ string) <-chan core.Event {
	output := make(chan core.Event, 1)
	go func() {
		defer close(output)
		decision, err := runtime.broker.Confirm(ctx, permissions.Request{Tool: "shell", Action: permissions.ActionExecute})
		if err == nil {
			output <- core.Event{Type: core.EventTextDelta, Text: string(decision.Behavior)}
		}
	}()
	return output
}
func (permissionRuntime) SessionID() string       { return "permission" }
func (permissionRuntime) History() []core.Message { return nil }

func TestServerRoutesStructuredPermissionResponse(t *testing.T) {
	broker := NewPermissionBroker(2)
	defer broker.Close()
	server, err := NewServerWithOptions(permissionRuntime{broker: broker}, ServerOptions{Permissions: broker})
	if err != nil {
		t.Fatal(err)
	}
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), serverInput, serverOutput) }()
	encoder := json.NewEncoder(clientInput)
	reader := bufio.NewReader(clientOutput)
	if err := encoder.Encode(Request{Version: Version, ID: "turn", Type: "start", Prompt: "run"}); err != nil {
		t.Fatal(err)
	}
	permissionID := ""
	deadline := time.After(time.Second)
	for permissionID == "" {
		select {
		case <-deadline:
			t.Fatal("permission prompt timed out")
		default:
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response Response
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatal(err)
		}
		if response.Permission != nil {
			permissionID = response.Permission.ID
		}
	}
	if err := encoder.Encode(Request{Version: Version, ID: "permission-response", Type: "permission", PermissionID: permissionID, Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for !found {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response Response
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatal(err)
		}
		found = response.Event != nil && response.Event.Text == string(permissions.PermissionBehaviorAllow)
	}
	_ = clientInput.Close()
	_ = clientOutput.Close()
	_ = serverOutput.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not disconnect")
	}
}
