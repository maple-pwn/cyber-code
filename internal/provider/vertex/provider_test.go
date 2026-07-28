package vertex

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
)

func TestVertexMetadataAndOptions(t *testing.T) {
	profile := config.Profile{BaseURL: "https://example.test/publishers/anthropic", Model: "model"}
	client, err := New(profile, WithTokenSource(func(context.Context) (string, error) { return "token", nil }))
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "vertex" {
		t.Fatalf("Name() = %q", client.Name())
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil || !capabilities.Streaming || capabilities.TokenCounting {
		t.Fatalf("capabilities = %#v, error = %v", capabilities, err)
	}
	if _, err := client.CountTokens(context.Background(), core.Request{}); err == nil {
		t.Fatal("portable token counting unexpectedly succeeded")
	}
	for _, option := range []Option{nil, WithHTTPClient(nil), WithTokenSource(nil)} {
		if _, err := New(profile, option); err == nil {
			t.Fatalf("invalid option was accepted: %#v", option)
		}
	}
	for _, baseURL := range []string{"", "://bad", "ftp://example.test"} {
		if _, err := New(config.Profile{BaseURL: baseURL, Model: "model"}, WithTokenSource(func(context.Context) (string, error) { return "token", nil })); err == nil {
			t.Fatalf("invalid base URL %q was accepted", baseURL)
		}
	}
}

func TestVertexTransportRejectsMalformedRequestsAndCredentialFailures(t *testing.T) {
	publisher, _ := url.Parse("https://example.test/publishers/anthropic")
	baseRequest, err := http.NewRequest(http.MethodPost, "https://source.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"not-json", `{}`, `{"model":"../escape"}`} {
		request := baseRequest.Clone(context.Background())
		request.Body = io.NopCloser(strings.NewReader(body))
		transport := vertexTransport{publisherURL: publisher, next: vertexRoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid request reached next transport")
			return nil, nil
		})}
		if _, err := transport.RoundTrip(request); err == nil {
			t.Fatalf("invalid body was accepted: %s", body)
		}
	}

	request := baseRequest.Clone(context.Background())
	request.Body = io.NopCloser(strings.NewReader(`{"model":"safe"}`))
	request.Header.Set("X-Api-Key", "")
	transport := vertexTransport{publisherURL: publisher, next: vertexRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })}
	if _, err := transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty token error = %v", err)
	}

	sentinel := errors.New("token failed")
	request = baseRequest.Clone(context.Background())
	request.Body = io.NopCloser(strings.NewReader(`{"model":"safe"}`))
	transport.tokenSource = func(context.Context) (string, error) { return "", sentinel }
	if _, err := transport.RoundTrip(request); !errors.Is(err, sentinel) {
		t.Fatalf("token source error = %v", err)
	}
}

type vertexRoundTripFunc func(*http.Request) (*http.Response, error)

func (function vertexRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
