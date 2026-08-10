package runtimeapi_test

import (
	"encoding/json"
	"testing"

	"cyber-code/internal/runtimeapi"
)

func TestValidateStructuredTerminalCommands(t *testing.T) {
	t.Parallel()
	commands := []string{
		`{"type":"terminal.open","sessionId":"terminal-1","profileId":"default-shell","workingDirectory":"/lab","scopeId":"scope-1","columns":120,"rows":40,"outputLimitBytes":1048576,"expectedLeaseRevision":2}`,
		`{"type":"terminal.input","sessionId":"terminal-1","sequence":1,"data":"bHMK","byteLength":3,"expectedLeaseRevision":2}`,
		`{"type":"terminal.resize","sessionId":"terminal-1","columns":100,"rows":30,"expectedLeaseRevision":2}`,
		`{"type":"terminal.cancel","sessionId":"terminal-1","expectedLeaseRevision":2}`,
	}
	for _, command := range commands {
		if err := runtimeapi.ValidateCommandEnvelope(runtimeapi.CommandEnvelope{IdempotencyKey: "cmd-terminal", Command: json.RawMessage(command)}); err != nil {
			t.Fatalf("terminal command %s rejected: %v", command, err)
		}
	}
}

func TestValidateTerminalCommandsRejectsShellStringsAndForgedApproval(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		`{"type":"terminal.open","command":"sh -c rm","workingDirectory":"/lab"}`,
		`{"type":"terminal.open","sessionId":"terminal-1","profileId":"sh -i","workingDirectory":"/lab","scopeId":"scope-1","columns":120,"rows":40,"outputLimitBytes":1048576,"expectedLeaseRevision":2}`,
		`{"type":"terminal.open","sessionId":"terminal-1","profileId":"default-shell","workingDirectory":"/lab","scopeId":"scope-1","columns":120,"rows":40,"outputLimitBytes":1048576,"expectedLeaseRevision":2,"approval":"allow"}`,
	} {
		err := runtimeapi.ValidateCommandEnvelope(runtimeapi.CommandEnvelope{IdempotencyKey: "cmd-terminal", Command: json.RawMessage(command)})
		if err != runtimeapi.ErrInvalidCommandEnvelope {
			t.Fatalf("terminal command %s error = %v", command, err)
		}
	}
}
