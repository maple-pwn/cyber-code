package skill

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

func TestRegisterToolLoadsDiscoveredSkill(t *testing.T) {
	registry := toolpkg.NewRegistry()
	discovered := []Skill{{Name: "review", Source: "project", Instructions: "review instructions"}}
	if err := RegisterTool(registry, discovered); err != nil {
		t.Fatal(err)
	}
	loader, ok := registry.Get("load_skill")
	if !ok || !loader.Spec().ReadOnly {
		t.Fatalf("skill tool = %#v, present = %t", loader, ok)
	}
	arguments := json.RawMessage(`{"name":"review"}`)
	request, err := loader.Authorize(context.Background(), arguments)
	if err != nil || request.Action != permissions.ActionRead {
		t.Fatalf("permission = %#v, error = %v", request, err)
	}
	result, err := loader.Run(context.Background(), arguments)
	if err != nil || len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "review instructions") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if _, err := loader.Run(context.Background(), json.RawMessage(`{"name":"missing"}`)); err == nil {
		t.Fatal("unknown skill was accepted")
	}
}
