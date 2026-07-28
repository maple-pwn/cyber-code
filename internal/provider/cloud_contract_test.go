package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"claude-code-go/internal/config"
	"claude-code-go/internal/core"
	"claude-code-go/internal/provider/azure"
	"claude-code-go/internal/provider/testkit"
	"claude-code-go/internal/provider/vertex"
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

func TestVertexProviderMapsRawPredictRequestAndCanonicalEvents(t *testing.T) {
	t.Setenv("TEST_VERTEX_ACCESS_TOKEN", "vertex-token")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		wantPath := "/v1/projects/project/locations/us-central1/publishers/anthropic/models/claude-vertex:streamRawPredict"
		if request.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", request.URL.Path, wantPath)
		}
		if request.Header.Get("Authorization") != "Bearer vertex-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("X-Api-Key") != "" || request.Header.Get("Anthropic-Version") != "" {
			t.Errorf("Anthropic authentication headers leaked to Vertex")
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if _, exists := payload["model"]; exists {
			t.Errorf("Vertex payload contains model: %s", payload["model"])
		}
		if string(payload["anthropic_version"]) != `"vertex-2023-10-16"` {
			t.Errorf("anthropic_version = %s", payload["anthropic_version"])
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_ = testkit.WriteSSE(response, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"vertex"}}`)
		_ = testkit.WriteSSE(response, "message_stop", `{"type":"message_stop"}`)
	})
	defer server.Close()

	client, err := vertex.New(config.Profile{
		Provider:  "vertex",
		BaseURL:   server.URL() + "/v1/projects/project/locations/us-central1/publishers/anthropic",
		Model:     "claude-vertex",
		APIKeyEnv: "TEST_VERTEX_ACCESS_TOKEN",
	}, vertex.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("new Vertex provider: %v", err)
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
				if len(events) != 2 || events[0].Type != core.EventTextDelta || events[0].Text != "vertex" || events[1].Type != core.EventCompleted {
					t.Fatalf("events = %#v", events)
				}
				return
			}
			events = append(events, event)
		case <-deadline:
			t.Fatal("Vertex stream did not close")
		}
	}
}

func TestVertexProviderUsesManagedTokenSource(t *testing.T) {
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer managed-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_ = testkit.WriteSSE(response, "message_stop", `{"type":"message_stop"}`)
	})
	defer server.Close()
	client, err := vertex.New(config.Profile{
		Provider: "vertex",
		BaseURL:  server.URL() + "/publishers/anthropic",
		Model:    "claude-vertex",
	}, vertex.WithHTTPClient(server.Client()), vertex.WithTokenSource(func(context.Context) (string, error) {
		return "managed-token", nil
	}))
	if err != nil {
		t.Fatalf("new Vertex provider: %v", err)
	}
	stream, err := client.Stream(context.Background(), core.Request{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range stream {
	}
}

func TestVertexProviderRejectsModelPathTraversal(t *testing.T) {
	t.Setenv("TEST_VERTEX_ACCESS_TOKEN", "vertex-token")
	server := testkit.NewServer(func(response http.ResponseWriter, request *http.Request) {
		t.Error("Vertex request was sent with an unsafe model")
	})
	defer server.Close()
	client, err := vertex.New(config.Profile{
		Provider:  "vertex",
		BaseURL:   server.URL() + "/publishers/anthropic",
		Model:     "claude-vertex",
		APIKeyEnv: "TEST_VERTEX_ACCESS_TOKEN",
	}, vertex.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Stream(context.Background(), core.Request{Model: "../escape"}); err == nil {
		t.Fatal("expected unsafe Vertex model to be rejected")
	}
	if len(server.Requests()) != 0 {
		t.Fatal("unsafe Vertex model reached the network")
	}
}
