package security_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-code-go/internal/config"
	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
	"claude-code-go/internal/provider/anthropic"
	"claude-code-go/internal/provider/openai"
)

func TestCoreErrorsAndAuditRecordsDoNotLeakCredentialText(t *testing.T) {
	secret := "core-secret-value"
	classified := &core.Error{
		Kind:    core.ErrorKindProvider,
		Message: "Authorization: Bearer " + secret + " Cookie: session=" + secret,
		Cause:   errors.New("api_key=" + secret),
	}
	for _, rendered := range []string{classified.Error(), classified.UserMessage()} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("core error leaked secret: %s", rendered)
		}
	}

	audit := permissions.NewAuditLog(1)
	audit.Record(permissions.AuditRecord{
		Tool:   "Authorization: Bearer " + secret,
		Action: "Cookie: session=" + secret,
		Reason: "api_key=" + secret,
	})
	encoded, err := json.Marshal(audit.Records())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("audit leaked secret: %s", encoded)
	}
}

func TestOpenAIProviderRedactsCredentialEchoedByBackend(t *testing.T) {
	secret := "provider-credential-value"
	t.Setenv("SECURITY_TEST_API_KEY", secret)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"error":{"message":"rejected credential ` + secret + `; Cookie: session=cookie-secret"}}`))
	}))
	defer server.Close()

	client, err := openai.New(config.Profile{
		Provider: "openai-compatible", BaseURL: server.URL, Model: "security-test", APIKeyEnv: "SECURITY_TEST_API_KEY",
	}, openai.WithRetryPolicy(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Stream(context.Background(), core.Request{Model: "security-test"})
	if err == nil {
		t.Fatal("expected authentication error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "cookie-secret") {
		t.Fatalf("provider error leaked credential: %s", err)
	}
}

func TestAnthropicProviderRedactsCredentialEchoedByBackend(t *testing.T) {
	secret := "anthropic-credential-value"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"error":{"message":"rejected credential ` + secret + `; Cookie: session=cookie-secret"}}`))
	}))
	defer server.Close()

	client, err := anthropic.New(config.Profile{
		Provider: "anthropic", BaseURL: server.URL, Model: "security-test",
	}, anthropic.WithCredential(secret), anthropic.WithRetryPolicy(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Stream(context.Background(), core.Request{Model: "security-test"})
	if err == nil {
		t.Fatal("expected authentication error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "cookie-secret") {
		t.Fatalf("provider error leaked credential: %s", err)
	}
}

func TestProviderStreamsRedactCredentialEchoedInErrorEvents(t *testing.T) {
	tests := []struct {
		name        string
		serve       func(http.ResponseWriter)
		newProvider func(string, string) streamProvider
	}{
		{
			name: "openai-compatible",
			serve: func(response http.ResponseWriter) {
				response.Header().Set("Content-Type", "text/event-stream")
				_, _ = response.Write([]byte("data: {\"error\":{\"message\":\"credential stream-secret\"}}\n\ndata: [DONE]\n\n"))
			},
			newProvider: func(baseURL, secret string) streamProvider {
				t.Setenv("STREAM_SECURITY_API_KEY", secret)
				client, err := openai.New(config.Profile{Provider: "openai-compatible", BaseURL: baseURL, Model: "security-test", APIKeyEnv: "STREAM_SECURITY_API_KEY"})
				if err != nil {
					t.Fatal(err)
				}
				return client
			},
		},
		{
			name: "anthropic",
			serve: func(response http.ResponseWriter) {
				response.Header().Set("Content-Type", "text/event-stream")
				_, _ = response.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"credential stream-secret\"}}\n\n"))
			},
			newProvider: func(baseURL, secret string) streamProvider {
				client, err := anthropic.New(config.Profile{Provider: "anthropic", BaseURL: baseURL, Model: "security-test"}, anthropic.WithCredential(secret))
				if err != nil {
					t.Fatal(err)
				}
				return client
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { test.serve(response) }))
			defer server.Close()
			stream, err := test.newProvider(server.URL, "stream-secret").Stream(context.Background(), core.Request{Model: "security-test"})
			if err != nil {
				t.Fatal(err)
			}
			for event := range stream {
				if event.Err != nil && strings.Contains(event.Err.Error(), "stream-secret") {
					t.Fatalf("stream error leaked credential: %s", event.Err)
				}
			}
		})
	}
}

type streamProvider interface {
	Stream(context.Context, core.Request) (<-chan core.Event, error)
}
