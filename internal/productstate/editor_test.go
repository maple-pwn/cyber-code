package productstate_test

import (
	"encoding/json"
	"strings"
	"testing"

	"cyber-code/internal/productstate"
)

func TestProjectTracksEvidenceAwareEditorLifecycle(t *testing.T) {
	t.Parallel()
	digest := func(character string) string { return strings.Repeat(character, 64) }
	state := productstate.Initial()
	events := []struct {
		eventType string
		payload   any
	}{
		{"evidence.committed", map[string]any{"evidence": map[string]any{"id": "evidence-1", "taskId": "task-1", "kind": "source", "summary": "Affected handler", "data": map[string]any{"path": "/lab/app.go"}}}},
		{"finding.created", map[string]any{"finding": map[string]any{"id": "finding-1", "title": "Unsafe handler", "severity": "high", "status": "candidate", "confidence": "high", "evidenceIds": []string{"evidence-1"}}}},
		{"editor.draft.opened", map[string]any{"draft": map[string]any{
			"id": "draft-1", "path": "/lab/app.go", "scopeId": "scope-1", "ownerClientId": "client-1", "leaseRevision": 2,
			"baseSha256": digest("a"), "baseByteLength": 128, "encoding": "utf-8",
			"evidenceReferences": []any{map[string]any{"findingId": "finding-1", "evidenceId": "evidence-1", "startLine": 4, "endLine": 8}},
		}}},
		{"editor.draft.saved", map[string]any{"draftId": "draft-1", "revision": 1, "baseSha256": digest("a"), "proposedSha256": digest("b"), "proposedByteLength": 144}},
		{"editor.patch.applied", map[string]any{"draftId": "draft-1", "revision": 1, "baseSha256": digest("a"), "proposedSha256": digest("b"), "resultSha256": digest("b"), "reviewer": "client-1"}},
		{"editor.patch.verified", map[string]any{"draftId": "draft-1", "revision": 1, "verificationId": "verification-1", "success": true, "evidenceIds": []string{"evidence-1"}}},
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
		EditorDrafts map[string]struct {
			Status       string `json:"status"`
			NextRevision int    `json:"nextRevision"`
			ResultSHA256 string `json:"resultSha256"`
			Reviewer     string `json:"reviewer"`
			Verification struct {
				ID      string `json:"id"`
				Success bool   `json:"success"`
			} `json:"verification"`
		} `json:"editorDrafts"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	draft := snapshot.EditorDrafts["draft-1"]
	if draft.Status != "verified" || draft.NextRevision != 2 || draft.ResultSHA256 != digest("b") || draft.Reviewer != "client-1" || draft.Verification.ID != "verification-1" || !draft.Verification.Success {
		t.Fatalf("editor draft state = %+v", draft)
	}
}

func TestProjectRejectsMissingEditorProvenance(t *testing.T) {
	t.Parallel()
	_, err := productstate.Project(productstate.Initial(), mustEvent(t, 1, "editor.draft.opened", map[string]any{"draft": map[string]any{
		"id": "draft-1", "path": "/lab/app.go", "scopeId": "scope-1", "ownerClientId": "client-1", "leaseRevision": 1,
		"baseSha256": strings.Repeat("a", 64), "baseByteLength": 128, "encoding": "utf-8",
		"evidenceReferences": []any{map[string]any{"findingId": "finding-1", "evidenceId": "evidence-1", "startLine": 4, "endLine": 8}},
	}}, nil))
	if err == nil || err.Error() != "editor_provenance_missing" {
		t.Fatalf("missing provenance error = %v", err)
	}
}
