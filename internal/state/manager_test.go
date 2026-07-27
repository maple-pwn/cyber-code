package state_test

import (
	"testing"

	"claude-code-go/internal/state"
)

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
