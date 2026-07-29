package openai

import (
	"strings"
	"testing"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
)

func TestEncodeRequestMapsCanonicalImageBlock(t *testing.T) {
	encoded, err := encodeRequest(config.Profile{Model: "vision-test"}, core.Request{Messages: []core.Message{{
		Role: core.RoleUser,
		Content: []core.ContentBlock{
			{Type: core.ContentText, Text: "inspect"},
			{Type: core.ContentImage, MediaType: "image/png", Data: "AA=="},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{`"type":"text"`, `"type":"image_url"`, `"url":"data:image/png;base64,AA=="`} {
		if !strings.Contains(text, want) {
			t.Fatalf("OpenAI image request missing %s: %s", want, text)
		}
	}
}
