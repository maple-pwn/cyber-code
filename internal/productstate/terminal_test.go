package productstate_test

import (
	"encoding/json"
	"errors"
	"testing"

	"cyber-code/internal/productstate"
)

func TestProjectTracksBoundedTerminalLifecycle(t *testing.T) {
	t.Parallel()
	state := productstate.Initial()
	events := []struct {
		eventType string
		payload   any
	}{
		{"terminal.opened", map[string]any{"session": map[string]any{
			"id": "terminal-1", "profileId": "default-shell", "processId": "pid-42", "workingDirectory": "/lab",
			"scopeId": "scope-1", "ownerClientId": "client-1", "leaseRevision": 2, "columns": 120, "rows": 40, "outputLimitBytes": 1048576,
		}}},
		{"terminal.output", map[string]any{"sessionId": "terminal-1", "sequence": 1, "data": "b2sK", "byteLength": 3}},
		{"terminal.input.accepted", map[string]any{"sessionId": "terminal-1", "sequence": 1, "byteLength": 3, "sha256": "dc51b8c96c2d745df9b5fd1680149e1a2a4e08ef294c5b49f7154f36f330a112"}},
		{"terminal.resized", map[string]any{"sessionId": "terminal-1", "columns": 100, "rows": 30}},
		{"terminal.exited", map[string]any{"sessionId": "terminal-1", "exitCode": 0, "reason": "exited"}},
	}
	for index, item := range events {
		result, err := productstate.Project(state, mustEvent(t, index+1, item.eventType, item.payload, nil))
		if err != nil {
			t.Fatalf("project %s: %v", item.eventType, err)
		}
		state = result.State
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Terminals map[string]struct {
			Status             string `json:"status"`
			Columns            int    `json:"columns"`
			Rows               int    `json:"rows"`
			OutputBytes        int    `json:"outputBytes"`
			NextInputSequence  int    `json:"nextInputSequence"`
			NextOutputSequence int    `json:"nextOutputSequence"`
		} `json:"terminals"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	terminal := snapshot.Terminals["terminal-1"]
	if terminal.Status != "exited" || terminal.Columns != 100 || terminal.Rows != 30 || terminal.OutputBytes != 3 || terminal.NextInputSequence != 2 || terminal.NextOutputSequence != 2 {
		t.Fatalf("terminal state = %+v", terminal)
	}
}

func TestProjectRejectsOutOfOrderTerminalOutput(t *testing.T) {
	t.Parallel()
	opened, err := productstate.Project(productstate.Initial(), mustEvent(t, 1, "terminal.opened", map[string]any{"session": map[string]any{
		"id": "terminal-1", "profileId": "default-shell", "processId": "pid-42", "workingDirectory": "/lab",
		"scopeId": "scope-1", "ownerClientId": "client-1", "leaseRevision": 1, "columns": 120, "rows": 40, "outputLimitBytes": 1048576,
	}}, nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = productstate.Project(opened.State, mustEvent(t, 2, "terminal.output", map[string]any{"sessionId": "terminal-1", "sequence": 2, "data": "b2sK", "byteLength": 3}, nil))
	if !errors.Is(err, productstate.ErrTerminalOutputSequence) {
		t.Fatalf("output sequence error = %v", err)
	}
}
