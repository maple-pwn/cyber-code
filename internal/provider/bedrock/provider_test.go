package bedrock

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
	api "cyber-code/internal/services/api"
)

func TestBedrockMetadataAndOptions(t *testing.T) {
	profile := config.Profile{BaseURL: "https://example.test", Model: "model"}
	client, err := New(profile, WithCredentials("us-east-1", func(context.Context) (*api.AWSCredentials, error) {
		return &api.AWSCredentials{AccessKeyID: "key", SecretAccessKey: "secret"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "bedrock" {
		t.Fatalf("Name() = %q", client.Name())
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil || !capabilities.Streaming || capabilities.TokenCounting {
		t.Fatalf("capabilities = %#v, error = %v", capabilities, err)
	}
	if _, err := client.CountTokens(context.Background(), core.Request{}); err == nil {
		t.Fatal("portable token counting unexpectedly succeeded")
	}
	for _, option := range []Option{nil, WithHTTPClient(nil), WithCredentials("", func(context.Context) (*api.AWSCredentials, error) { return nil, nil }), WithCredentials("region", nil)} {
		if _, err := New(profile, option); err == nil {
			t.Fatalf("invalid option was accepted: %#v", option)
		}
	}
	for _, baseURL := range []string{"", "://bad", "ftp://example.test"} {
		if _, err := New(config.Profile{BaseURL: baseURL, Model: "model"}, WithCredentials("region", func(context.Context) (*api.AWSCredentials, error) { return &api.AWSCredentials{}, nil })); err == nil {
			t.Fatalf("invalid base URL %q was accepted", baseURL)
		}
	}
}

func TestBedrockTransportRejectsMalformedRequestsAndCredentialFailures(t *testing.T) {
	baseURL, _ := url.Parse("https://example.test")
	baseRequest, err := http.NewRequest(http.MethodPost, "https://source.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"not-json", `{}`, `{"model":".."}`} {
		request := baseRequest.Clone(context.Background())
		request.Body = io.NopCloser(strings.NewReader(body))
		transport := bedrockTransport{baseURL: baseURL, next: bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid request reached next transport")
			return nil, nil
		})}
		if _, err := transport.RoundTrip(request); err == nil {
			t.Fatalf("invalid body was accepted: %s", body)
		}
	}

	request := baseRequest.Clone(context.Background())
	request.Body = io.NopCloser(strings.NewReader(`{"model":"safe"}`))
	transport := bedrockTransport{baseURL: baseURL, next: bedrockRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })}
	if _, err := transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty bearer error = %v", err)
	}

	sentinel := errors.New("credentials failed")
	request = baseRequest.Clone(context.Background())
	request.Body = io.NopCloser(strings.NewReader(`{"model":"safe"}`))
	transport.credentialSource = func(context.Context) (*api.AWSCredentials, error) { return nil, sentinel }
	if _, err := transport.RoundTrip(request); !errors.Is(err, sentinel) {
		t.Fatalf("credential source error = %v", err)
	}
	request = baseRequest.Clone(context.Background())
	request.Body = io.NopCloser(strings.NewReader(`{"model":"safe"}`))
	transport.credentialSource = func(context.Context) (*api.AWSCredentials, error) { return nil, nil }
	if _, err := transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "returned nil") {
		t.Fatalf("nil credentials error = %v", err)
	}
}

type bedrockRoundTripFunc func(*http.Request) (*http.Response, error)

func (function bedrockRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
