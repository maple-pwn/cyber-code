package productstate_test

import (
	"errors"
	"testing"

	"cyber-code/internal/productstate"
)

func TestProjectAssetGraphKeepsUnresolvedEdgesAndStatuses(t *testing.T) {
	state := productstate.Initial()
	evidence := mustEvent(t, 1, "evidence.committed", map[string]any{"evidence": map[string]any{"id": "evidence-1", "taskId": "task-1", "kind": "network", "summary": "observed", "data": map[string]any{}}}, nil)
	result, err := productstate.Project(state, evidence)
	if err != nil {
		t.Fatal(err)
	}
	state = result.State
	provenance := map[string]any{"kind": "evidence", "evidenceIds": []string{"evidence-1"}}
	node := map[string]any{"id": "asset:target:lab", "kind": "target", "label": "lab", "status": "active", "attributes": map[string]any{}, "provenance": provenance}
	result, err = productstate.Project(state, mustEvent(t, 2, "asset.node.committed", map[string]any{"node": node}, nil))
	if err != nil {
		t.Fatal(err)
	}
	state = result.State
	edge := map[string]any{"id": "edge:unknown", "kind": "routes_to", "sourceId": "asset:target:lab", "targetId": "asset:route:unknown", "directed": true, "provenance": provenance}
	result, err = productstate.Project(state, mustEvent(t, 3, "asset.edge.committed", map[string]any{"edge": edge}, nil))
	if err != nil {
		t.Fatal(err)
	}
	state = result.State
	if state.AssetEdges["edge:unknown"].TargetID != "asset:route:unknown" {
		t.Fatal("unresolved edge was not retained")
	}
	if _, ok := state.AssetNodes["asset:route:unknown"]; ok {
		t.Fatal("projector fabricated an unresolved node")
	}
	result, err = productstate.Project(state, mustEvent(t, 4, "asset.node.status.changed", map[string]any{"nodeId": "asset:target:lab", "status": "revoked", "reason": "rotated"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.State.AssetNodes["asset:target:lab"].Status != "revoked" {
		t.Fatal("node status was not projected")
	}
}

func TestProjectAssetGraphRequiresEvidenceAndRejectsConflicts(t *testing.T) {
	provenance := map[string]any{"kind": "evidence", "evidenceIds": []string{"missing"}}
	node := map[string]any{"id": "asset:target:lab", "kind": "target", "label": "lab", "status": "active", "attributes": map[string]any{}, "provenance": provenance}
	if _, err := productstate.Project(productstate.Initial(), mustEvent(t, 1, "asset.node.committed", map[string]any{"node": node}, nil)); !errors.Is(err, productstate.ErrAssetEvidenceMissing) {
		t.Fatalf("missing evidence error = %v", err)
	}
}
