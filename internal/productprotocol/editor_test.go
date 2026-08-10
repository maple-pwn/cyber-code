package productprotocol_test

import (
	"errors"
	"strings"
	"testing"

	"cyber-code/internal/productprotocol"
)

func TestValidateEditorLifecycleEvents(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	payloads := map[string]any{
		"editor.draft.opened": map[string]any{"draft": map[string]any{
			"id": "draft-1", "path": "/lab/app.go", "scopeId": "scope-1", "ownerClientId": "client-1",
			"leaseRevision": 2, "baseSha256": digest, "baseByteLength": 128, "encoding": "utf-8",
			"evidenceReferences": []any{map[string]any{"findingId": "finding-1", "evidenceId": "evidence-1", "startLine": 4, "endLine": 8}},
		}},
		"editor.draft.saved":   map[string]any{"draftId": "draft-1", "revision": 1, "baseSha256": digest, "proposedSha256": strings.Repeat("b", 64), "proposedByteLength": 144},
		"editor.patch.applied": map[string]any{"draftId": "draft-1", "revision": 1, "baseSha256": digest, "proposedSha256": strings.Repeat("b", 64), "resultSha256": strings.Repeat("b", 64), "reviewer": "client-1"},
		"editor.patch.verified": map[string]any{
			"draftId": "draft-1", "revision": 1, "verificationId": "verification-1", "success": true, "evidenceIds": []string{"evidence-1"},
		},
		"editor.draft.discarded": map[string]any{"draftId": "draft-1", "reason": "operator_cancelled"},
	}
	for eventType, payload := range payloads {
		event, err := productprotocol.Validate(rawEvent(t, 1, eventType, payload, nil))
		if err != nil || event.Kind != productprotocol.EventKindKnown {
			t.Fatalf("%s validation = kind %q, error %v", eventType, event.Kind, err)
		}
	}
}

func TestValidateRejectsMalformedEditorEvents(t *testing.T) {
	t.Parallel()
	draft := map[string]any{
		"id": "draft-1", "path": "/lab/app.go", "scopeId": "scope-1", "ownerClientId": "client-1",
		"leaseRevision": 2, "baseSha256": strings.Repeat("a", 64), "baseByteLength": 128, "encoding": "utf-8",
		"evidenceReferences": []any{map[string]any{"findingId": "finding-1", "evidenceId": "evidence-1", "startLine": 4, "endLine": 8}},
	}
	for _, payload := range []any{
		map[string]any{"draft": func() map[string]any { copy := cloneMap(draft); copy["baseSha256"] = "short"; return copy }()},
		map[string]any{"draft": func() map[string]any {
			copy := cloneMap(draft)
			copy["evidenceReferences"] = []any{map[string]any{"findingId": "finding-1", "evidenceId": "evidence-1", "startLine": 9, "endLine": 8}}
			return copy
		}()},
	} {
		if _, err := productprotocol.Validate(rawEvent(t, 1, "editor.draft.opened", payload, nil)); !errors.Is(err, productprotocol.ErrInvalidEvent) {
			t.Fatalf("editor event error = %v", err)
		}
	}
}

func cloneMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
