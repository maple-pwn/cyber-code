package provider_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"claude-code-go/internal/config"
	"claude-code-go/internal/core"
	"claude-code-go/internal/provider/azure"
	"claude-code-go/internal/provider/testkit"
)

func TestAzureProviderUsesFoundryAPIKeyAndCanonicalEvents(t *testing.T) {
	t.Setenv("TEST_AZURE_API_KEY", "azure-secret")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/anthropic/v1/messages" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("api-key") != "azure-secret" {
			t.Errorf("api-key = %q", request.Header.Get("api-key"))
		}
		if request.Header.Get("x-api-key") != "" {
			t.Errorf("Anthropic key header leaked to Foundry")
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_ = testkit.WriteSSE(response, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"azure"}}`)
		_ = testkit.WriteSSE(response, "message_stop", `{"type":"message_stop"}`)
	})
	defer server.Close()
	client, err := azure.New(config.Profile{Provider: "azure", BaseURL: server.URL() + "/anthropic", Model: "claude-test", APIKeyEnv: "TEST_AZURE_API_KEY"}, azure.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("new Azure provider: %v", err)
	}
	stream, err := client.Stream(context.Background(), core.Request{Messages: []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "hello"}}}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var events []core.Event
	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				if len(events) != 2 || events[0].Type != core.EventTextDelta || events[0].Text != "azure" || events[1].Type != core.EventCompleted {
					t.Fatalf("events = %#v", events)
				}
				return
			}
			events = append(events, event)
		case <-deadline:
			t.Fatal("Azure stream did not close")
		}
	}
}

func TestAzureProviderRequiresExplicitBaseURL(t *testing.T) {
	t.Setenv("TEST_AZURE_API_KEY", "azure-secret")
	if _, err := azure.New(config.Profile{Provider: "azure", Model: "claude-test", APIKeyEnv: "TEST_AZURE_API_KEY"}); err == nil {
		t.Fatal("expected missing Azure base URL to be rejected")
	}
}
