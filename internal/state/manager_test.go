package state_test

import (
	"testing"

	"cyber-code/internal/state"
)

type customPayload struct {
	Values []string `json:"values"`
}

func TestGetStateReturnsIndependentSnapshot(t *testing.T) {
	manager := state.NewStateManager()
	if err := manager.Restore([]byte(`{
		"session_id": "original",
		"features": {"safe": true},
		"custom": {
			"nested": {"value": "original"},
			"list": ["original"]
		}
	}`)); err != nil {
		t.Fatalf("restore state: %v", err)
	}

	snapshot := manager.GetState()
	snapshot.SessionID = "changed"
	snapshot.Features["safe"] = false
	snapshot.Custom["nested"].(map[string]interface{})["value"] = "changed"
	snapshot.Custom["list"].([]interface{})[0] = "changed"

	got := manager.GetState()
	if got.SessionID != "original" {
		t.Fatalf("session ID changed through snapshot: %q", got.SessionID)
	}
	if !got.Features["safe"] {
		t.Fatal("feature changed through snapshot")
	}
	if value := got.Custom["nested"].(map[string]interface{})["value"]; value != "original" {
		t.Fatalf("custom data changed through snapshot: %q", value)
	}
	if value := got.Custom["list"].([]interface{})[0]; value != "original" {
		t.Fatalf("custom list changed through snapshot: %q", value)
	}
}

func TestGetStateClonesTypedCustomContainers(t *testing.T) {
	t.Setenv("CLAUDE_CACHE_HOME", t.TempDir())

	manager := state.NewStateManager()
	if err := manager.Initialize(); err != nil {
		t.Fatalf("initialize manager: %v", err)
	}
	if err := manager.SetCustom("map", map[string]string{"value": "original"}); err != nil {
		t.Fatalf("set custom map: %v", err)
	}
	if err := manager.SetCustom("slice", []string{"original"}); err != nil {
		t.Fatalf("set custom slice: %v", err)
	}

	snapshot := manager.GetState()
	snapshot.Custom["map"].(map[string]string)["value"] = "changed"
	snapshot.Custom["slice"].([]string)[0] = "changed"

	got := manager.GetState()
	if value := got.Custom["map"].(map[string]string)["value"]; value != "original" {
		t.Fatalf("typed custom map changed through snapshot: %q", value)
	}
	if value := got.Custom["slice"].([]string)[0]; value != "original" {
		t.Fatalf("typed custom slice changed through snapshot: %q", value)
	}
}

func TestCustomValuesDoNotShareMutableAliases(t *testing.T) {
	t.Setenv("CLAUDE_CACHE_HOME", t.TempDir())

	manager := state.NewStateManager()
	if err := manager.Initialize(); err != nil {
		t.Fatalf("initialize manager: %v", err)
	}

	original := &customPayload{Values: []string{"original"}}
	if err := manager.SetCustom("payload", original); err != nil {
		t.Fatalf("set custom payload: %v", err)
	}
	original.Values[0] = "changed through input"

	snapshot := manager.GetState()
	snapshot.Custom["payload"].(*customPayload).Values[0] = "changed through snapshot"

	custom, ok := manager.GetCustom("payload")
	if !ok {
		t.Fatal("custom payload missing")
	}
	custom.(*customPayload).Values[0] = "changed through getter"

	got, ok := manager.GetCustom("payload")
	if !ok {
		t.Fatal("custom payload missing after mutation")
	}
	if value := got.(*customPayload).Values[0]; value != "original" {
		t.Fatalf("custom payload retained a mutable alias: %q", value)
	}
}

func TestSetCustomRejectsCyclesWithoutRetainingValue(t *testing.T) {
	t.Setenv("CLAUDE_CACHE_HOME", t.TempDir())

	manager := state.NewStateManager()
	if err := manager.Initialize(); err != nil {
		t.Fatalf("initialize manager: %v", err)
	}

	cyclic := map[string]interface{}{}
	cyclic["self"] = cyclic
	if err := manager.SetCustom("cyclic", cyclic); err == nil {
		t.Fatal("expected cyclic custom value to be rejected")
	}
	if _, ok := manager.GetCustom("cyclic"); ok {
		t.Fatal("rejected cyclic custom value was retained")
	}
}

func TestUpdateStateDoesNotRetainCallbackAliases(t *testing.T) {
	t.Setenv("CLAUDE_CACHE_HOME", t.TempDir())

	manager := state.NewStateManager()
	if err := manager.Initialize(); err != nil {
		t.Fatalf("initialize manager: %v", err)
	}

	var captured *state.AppState
	if err := manager.UpdateState(func(candidate *state.AppState) {
		candidate.SessionID = "committed"
		candidate.Custom["payload"] = &customPayload{Values: []string{"original"}}
		captured = candidate
	}); err != nil {
		t.Fatalf("update state: %v", err)
	}

	captured.SessionID = "changed through callback alias"
	captured.Custom["payload"].(*customPayload).Values[0] = "changed through callback alias"

	got := manager.GetState()
	if got.SessionID != "committed" {
		t.Fatalf("session ID changed through callback alias: %q", got.SessionID)
	}
	if value := got.Custom["payload"].(*customPayload).Values[0]; value != "original" {
		t.Fatalf("custom payload changed through callback alias: %q", value)
	}
}

func TestCustomClonePreservesOverlappingSliceShapes(t *testing.T) {
	t.Setenv("CLAUDE_CACHE_HOME", t.TempDir())

	manager := state.NewStateManager()
	if err := manager.Initialize(); err != nil {
		t.Fatalf("initialize manager: %v", err)
	}

	backing := []string{"first", "second"}
	payload := struct {
		Short []string `json:"short"`
		Long  []string `json:"long"`
	}{
		Short: backing[:1],
		Long:  backing[:2],
	}
	if err := manager.SetCustom("overlap", payload); err != nil {
		t.Fatalf("set custom payload: %v", err)
	}

	got, ok := manager.GetCustom("overlap")
	if !ok {
		t.Fatal("custom payload missing")
	}
	if length := len(got.(struct {
		Short []string `json:"short"`
		Long  []string `json:"long"`
	}).Long); length != 2 {
		t.Fatalf("long slice length changed during clone: %d", length)
	}
}
