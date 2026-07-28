package services

import (
	"encoding/json"
	"testing"

	"claude-code-go/internal/types"
)

func TestPluginServiceReturnsImmutableSnapshots(t *testing.T) {
	service := NewPluginService()
	service.plugins["example"] = &Plugin{
		Name: "example", State: PluginStateEnabled,
		Tools: []PluginTool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
	plugin, ok := service.GetPlugin("example")
	if !ok {
		t.Fatal("plugin missing")
	}
	plugin.State = PluginStateDisabled
	plugin.Tools[0].Name = "changed"
	plugin.Tools[0].InputSchema[0] = 'X'
	all := service.GetAllPlugins()
	all["example"].Description = "changed"
	stored := service.plugins["example"]
	if stored.State != PluginStateEnabled || stored.Tools[0].Name != "echo" || !json.Valid(stored.Tools[0].InputSchema) || stored.Description != "" {
		t.Fatalf("stored plugin was mutated: %#v", stored)
	}
}

func TestPluginLoaderReturnsImmutableManifestAndErrors(t *testing.T) {
	loader := &PluginLoader{
		loadedManifests: map[string]*types.PluginManifest{"example": {Name: "example", Skills: []string{"skill"}}},
		errors:          []types.PluginErrorDetail{{Type: "invalid", ValidationErrors: []string{"original"}}},
	}
	manifest := loader.GetPluginManifest("example")
	manifest.Name = "changed"
	manifest.Skills[0] = "changed"
	errorsFound := loader.GetErrors()
	errorsFound[0].Type = "changed"
	errorsFound[0].ValidationErrors[0] = "changed"
	if loader.loadedManifests["example"].Name != "example" || loader.loadedManifests["example"].Skills[0] != "skill" {
		t.Fatalf("stored manifest mutated: %#v", loader.loadedManifests["example"])
	}
	if loader.errors[0].Type != "invalid" || loader.errors[0].ValidationErrors[0] != "original" {
		t.Fatalf("stored errors mutated: %#v", loader.errors)
	}
}
