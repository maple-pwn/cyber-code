package runtimeapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"cyber-code/internal/runtimeapi"
)

func TestValidateStructuredEditorCommands(t *testing.T) {
	t.Parallel()
	digestA, digestB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	commands := []string{
		`{"type":"editor.open","draftId":"draft-1","path":"/lab/app.go","scopeId":"scope-1","evidenceReferences":[{"findingId":"finding-1","evidenceId":"evidence-1","startLine":4,"endLine":8}],"expectedLeaseRevision":2}`,
		`{"type":"editor.save","draftId":"draft-1","revision":1,"baseSha256":"` + digestA + `","data":"aGVsbG8K","byteLength":6,"expectedLeaseRevision":2}`,
		`{"type":"editor.apply","draftId":"draft-1","revision":1,"proposedSha256":"` + digestB + `","expectedLeaseRevision":2}`,
		`{"type":"editor.discard","draftId":"draft-1","expectedLeaseRevision":2}`,
	}
	for _, command := range commands {
		if err := runtimeapi.ValidateCommandEnvelope(runtimeapi.CommandEnvelope{IdempotencyKey: "cmd-editor", Command: json.RawMessage(command)}); err != nil {
			t.Fatalf("editor command %s rejected: %v", command, err)
		}
	}
}

func TestValidateEditorCommandsRejectsMalformedContentAndForgedAuthority(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	commands := []string{
		`{"type":"editor.save","draftId":"draft-1","revision":1,"baseSha256":"` + digest + `","data":"not base64","byteLength":6,"expectedLeaseRevision":2}`,
		`{"type":"editor.apply","draftId":"draft-1","revision":1,"proposedSha256":"` + digest + `","expectedLeaseRevision":2,"reviewer":"forged"}`,
		`{"type":"editor.open","draftId":"draft-1","path":"/lab/app.go","scopeId":"scope-1","evidenceReferences":[{"findingId":"finding-1","evidenceId":"evidence-1","startLine":9,"endLine":8}],"expectedLeaseRevision":2}`,
	}
	for _, command := range commands {
		if err := runtimeapi.ValidateCommandEnvelope(runtimeapi.CommandEnvelope{IdempotencyKey: "cmd-editor", Command: json.RawMessage(command)}); err != runtimeapi.ErrInvalidCommandEnvelope {
			t.Fatalf("editor command %s error = %v", command, err)
		}
	}
}
