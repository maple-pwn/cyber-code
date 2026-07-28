package azure

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"cyber-code/internal/config"
)

func TestAzureMetadataAndOptions(t *testing.T) {
	t.Setenv("AZURE_PROVIDER_TEST_KEY", "azure-key")
	profile := config.Profile{BaseURL: "https://example.test/anthropic", Model: "model", APIKeyEnv: "AZURE_PROVIDER_TEST_KEY"}
	client, err := New(profile)
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "azure" {
		t.Fatalf("Name() = %q", client.Name())
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil || !capabilities.Streaming || !capabilities.TokenCounting {
		t.Fatalf("capabilities = %#v, error = %v", capabilities, err)
	}
	if _, err := New(profile, nil); err == nil {
		t.Fatal("nil option was accepted")
	}
	if _, err := New(profile, WithHTTPClient(nil)); err == nil {
		t.Fatal("nil HTTP client was accepted")
	}
	if _, err := New(profile, WithTokenSource(nil)); err == nil {
		t.Fatal("nil token source was accepted")
	}
}

func TestFoundryTransportLeavesRequestsWithoutAnthropicKeyUnchanged(t *testing.T) {
	captured := false
	transport := foundryTransport{next: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = true
		if request.Header.Get("api-key") != "" || request.Header.Get("X-Api-Key") != "" {
			t.Fatalf("unexpected key headers: %v", request.Header)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	request, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); err != nil || !captured {
		t.Fatalf("RoundTrip error = %v, captured = %v", err, captured)
	}
}

func TestFoundryTransportUsesManagedTokenAndPropagatesSourceFailures(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Api-Key", "placeholder")
	captured := false
	transport := foundryTransport{
		tokenSource: func(context.Context) (string, error) { return "managed-token", nil },
		next: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			captured = true
			if request.Header.Get("Authorization") != "Bearer managed-token" || request.Header.Get("X-Api-Key") != "" {
				t.Fatalf("managed headers = %v", request.Header)
			}
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
		}),
	}
	if _, err := transport.RoundTrip(request); err != nil || !captured {
		t.Fatalf("RoundTrip error=%v captured=%t", err, captured)
	}

	sentinel := errors.New("token failed")
	transport.tokenSource = func(context.Context) (string, error) { return "", sentinel }
	if _, err := transport.RoundTrip(request); !errors.Is(err, sentinel) {
		t.Fatalf("token source error = %v", err)
	}
	transport.tokenSource = func(context.Context) (string, error) { return " ", nil }
	if _, err := transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty token error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
