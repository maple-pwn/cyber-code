package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
)

func TestEncodeRequestMapsCanonicalImageBlock(t *testing.T) {
	encoded, err := encodeRequest(config.Profile{Model: "claude-test"}, core.Request{Messages: []core.Message{{
		Role: core.RoleUser,
		Content: []core.ContentBlock{
			{Type: core.ContentText, Text: "inspect"},
			{Type: core.ContentImage, MediaType: "image/png", Data: "AA=="},
		},
	}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{`"type":"image"`, `"type":"base64"`, `"media_type":"image/png"`, `"data":"AA=="`} {
		if !strings.Contains(text, want) {
			t.Fatalf("Anthropic image request missing %s: %s", want, text)
		}
	}
}
