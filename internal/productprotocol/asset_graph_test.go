package productprotocol_test

import (
	"testing"

	"cyber-code/internal/productprotocol"
)

func TestValidateAcceptsAssetGraphEvents(t *testing.T) {
	provenance := map[string]any{"kind": "human", "annotationId": "annotation-1", "author": "operator"}
	node := map[string]any{"id": "asset:target:lab", "kind": "target", "label": "lab", "status": "active", "attributes": map[string]any{}, "provenance": provenance}
	edge := map[string]any{"id": "edge:1", "kind": "related_to", "sourceId": "asset:target:lab", "targetId": "asset:service:443", "directed": true, "provenance": provenance}
	for eventType, payload := range map[string]any{
		"asset.node.committed":      map[string]any{"node": node},
		"asset.edge.committed":      map[string]any{"edge": edge},
		"asset.node.status.changed": map[string]any{"nodeId": node["id"], "status": "revoked", "reason": "rotated"},
	} {
		event, err := productprotocol.Validate(rawEvent(t, 1, eventType, payload, nil))
		if err != nil || event.Kind != productprotocol.EventKindKnown {
			t.Fatalf("Validate(%s) = %v, kind %q", eventType, err, event.Kind)
		}
	}
}

func TestValidateRejectsUnattributedAssetGraphEvents(t *testing.T) {
	raw := rawEvent(t, 1, "asset.edge.committed", map[string]any{"edge": map[string]any{
		"id": "edge:1", "kind": "related_to", "sourceId": "a", "targetId": "a", "directed": true,
		"provenance": map[string]any{"kind": "human", "annotationId": "", "author": "operator"},
	}}, nil)
	if _, err := productprotocol.Validate(raw); err != productprotocol.ErrInvalidEvent {
		t.Fatalf("Validate() error = %v, want %v", err, productprotocol.ErrInvalidEvent)
	}
}
