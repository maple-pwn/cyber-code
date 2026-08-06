package productprotocol_test

import (
	"errors"
	"testing"

	"cyber-code/internal/productprotocol"
)

func TestValidateTerminalLifecycleEvents(t *testing.T) {
	t.Parallel()
	payloads := map[string]any{
		"terminal.opened": map[string]any{"session": map[string]any{
			"id": "terminal-1", "profileId": "default-shell", "processId": "pid-42",
			"workingDirectory": "/lab", "scopeId": "scope-1", "ownerClientId": "client-1",
			"leaseRevision": 2, "columns": 120, "rows": 40, "outputLimitBytes": 1048576,
		}},
		"terminal.output":         map[string]any{"sessionId": "terminal-1", "sequence": 1, "data": "b2sK", "byteLength": 3},
		"terminal.input.accepted": map[string]any{"sessionId": "terminal-1", "sequence": 1, "byteLength": 3, "sha256": "dc51b8c96c2d745df9b5fd1680149e1a2a4e08ef294c5b49f7154f36f330a112"},
		"terminal.resized":        map[string]any{"sessionId": "terminal-1", "columns": 100, "rows": 30},
		"terminal.exited":         map[string]any{"sessionId": "terminal-1", "exitCode": 0, "reason": "exited"},
	}
	for eventType, payload := range payloads {
		event, err := productprotocol.Validate(rawEvent(t, 1, eventType, payload, nil))
		if err != nil || event.Kind != productprotocol.EventKindKnown {
			t.Fatalf("%s validation = kind %q, error %v", eventType, event.Kind, err)
		}
	}
}

func TestValidateRejectsUnsafeTerminalEvents(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		eventType string
		payload   any
	}{
		{"terminal.output", map[string]any{"sessionId": "terminal-1", "sequence": 0, "data": "not base64", "byteLength": 3}},
		{"terminal.opened", map[string]any{"session": map[string]any{
			"id": "terminal-1", "profileId": "sh -i", "processId": "pid-42", "workingDirectory": "/lab", "scopeId": "scope-1", "ownerClientId": "client-1", "leaseRevision": 1, "columns": 120, "rows": 40, "outputLimitBytes": 1048576,
		}}},
		{"terminal.input.accepted", map[string]any{"sessionId": "terminal-1", "sequence": 1, "byteLength": 3, "sha256": "short"}},
	} {
		if _, err := productprotocol.Validate(rawEvent(t, 1, test.eventType, test.payload, nil)); !errors.Is(err, productprotocol.ErrInvalidEvent) {
			t.Fatalf("%s error = %v", test.eventType, err)
		}
	}
}
