package provider_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
	"cyber-code/internal/provider/azure"
	"cyber-code/internal/provider/bedrock"
	"cyber-code/internal/provider/testkit"
	"cyber-code/internal/provider/vertex"
	api "cyber-code/internal/services/api"
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

func TestBedrockProviderDecodesAWSEventStream(t *testing.T) {
	t.Setenv("TEST_BEDROCK_BEARER_TOKEN", "bedrock-token")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/model/claude-bedrock/invoke-with-response-stream" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer bedrock-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if _, exists := payload["model"]; exists {
			t.Errorf("Bedrock payload contains model")
		}
		if _, exists := payload["stream"]; exists {
			t.Errorf("Bedrock payload contains stream")
		}
		if string(payload["anthropic_version"]) != `"bedrock-2023-05-31"` {
			t.Errorf("anthropic_version = %s", payload["anthropic_version"])
		}
		response.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		writeAWSChunk(t, response, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"bedrock"}}`)
		writeAWSChunk(t, response, `{"type":"message_stop"}`)
	}))
	defer server.Close()
	client, err := bedrock.New(config.Profile{
		Provider:  "bedrock",
		BaseURL:   server.URL,
		Model:     "claude-bedrock",
		APIKeyEnv: "TEST_BEDROCK_BEARER_TOKEN",
	}, bedrock.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("new Bedrock provider: %v", err)
	}
	stream, err := client.Stream(context.Background(), core.Request{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var events []core.Event
	for event := range stream {
		events = append(events, event)
	}
	if len(events) != 2 || events[0].Type != core.EventTextDelta || events[0].Text != "bedrock" || events[1].Type != core.EventCompleted {
		t.Fatalf("events = %#v", events)
	}
}

func TestBedrockProviderRejectsCorruptEventCRC(t *testing.T) {
	t.Setenv("TEST_BEDROCK_BEARER_TOKEN", "bedrock-token")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		frame := buildAWSChunk(t, `{"type":"message_stop"}`)
		frame[len(frame)-1] ^= 0xff
		response.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = response.Write(frame)
	}))
	defer server.Close()
	client, err := bedrock.New(config.Profile{Provider: "bedrock", BaseURL: server.URL, Model: "claude-bedrock", APIKeyEnv: "TEST_BEDROCK_BEARER_TOKEN"}, bedrock.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Stream(context.Background(), core.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var events []core.Event
	for event := range stream {
		events = append(events, event)
	}
	if len(events) != 1 || events[0].Type != core.EventError || events[0].Err == nil {
		t.Fatalf("corrupt event events = %#v", events)
	}
}

func TestBedrockProviderSignsWithManagedAWSCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKID/") {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("X-Amz-Content-Sha256") == "" || request.Header.Get("X-Amz-Date") == "" {
			t.Errorf("missing SigV4 headers: %v", request.Header)
		}
		response.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		writeAWSChunk(t, response, `{"type":"message_stop"}`)
	}))
	defer server.Close()
	client, err := bedrock.New(config.Profile{Provider: "bedrock", BaseURL: server.URL, Model: "claude-bedrock"},
		bedrock.WithHTTPClient(server.Client()),
		bedrock.WithCredentials("us-east-1", func(context.Context) (*api.AWSCredentials, error) {
			return &api.AWSCredentials{AccessKeyID: "AKID", SecretAccessKey: "secret", SessionToken: "session"}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Stream(context.Background(), core.Request{})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
}

func TestBedrockProviderEscapesInferenceProfileARN(t *testing.T) {
	t.Setenv("TEST_BEDROCK_BEARER_TOKEN", "bedrock-token")
	wantModel := "arn:aws:bedrock:us-east-1:123456789012:inference-profile/team/profile"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.RequestURI, "inference-profile%2Fteam%2Fprofile") {
			t.Errorf("model ARN is not one encoded path segment: %q", request.RequestURI)
		}
		response.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		writeAWSChunk(t, response, `{"type":"message_stop"}`)
	}))
	defer server.Close()
	client, err := bedrock.New(config.Profile{Provider: "bedrock", BaseURL: server.URL, Model: "fallback", APIKeyEnv: "TEST_BEDROCK_BEARER_TOKEN"}, bedrock.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Stream(context.Background(), core.Request{Model: wantModel})
	if err != nil {
		t.Fatalf("stream inference profile: %v", err)
	}
	for range stream {
	}
}

func writeAWSChunk(t *testing.T, response http.ResponseWriter, innerJSON string) {
	t.Helper()
	if _, err := response.Write(buildAWSChunk(t, innerJSON)); err != nil {
		t.Fatal(err)
	}
}

func buildAWSChunk(t *testing.T, innerJSON string) []byte {
	t.Helper()
	outer, err := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString([]byte(innerJSON))})
	if err != nil {
		t.Fatal(err)
	}
	headers := awsStringHeader(":message-type", "event")
	headers = append(headers, awsStringHeader(":event-type", "chunk")...)
	totalLength := 16 + len(headers) + len(outer)
	message := &bytes.Buffer{}
	_ = binary.Write(message, binary.BigEndian, uint32(totalLength))
	_ = binary.Write(message, binary.BigEndian, uint32(len(headers)))
	_ = binary.Write(message, binary.BigEndian, crc32.ChecksumIEEE(message.Bytes()))
	_, _ = message.Write(headers)
	_, _ = message.Write(outer)
	_ = binary.Write(message, binary.BigEndian, crc32.ChecksumIEEE(message.Bytes()))
	return message.Bytes()
}

func awsStringHeader(name, value string) []byte {
	header := &bytes.Buffer{}
	header.WriteByte(byte(len(name)))
	header.WriteString(name)
	header.WriteByte(7)
	_ = binary.Write(header, binary.BigEndian, uint16(len(value)))
	header.WriteString(value)
	return header.Bytes()
}
