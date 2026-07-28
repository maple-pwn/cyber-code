// Package vertex adapts Vertex AI's Anthropic raw-predict endpoint.
package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"claude-code-go/internal/config"
	"claude-code-go/internal/core"
	"claude-code-go/internal/provider"
	"claude-code-go/internal/provider/anthropic"
)

const vertexAnthropicVersion = "vertex-2023-10-16"

// TokenSource returns a current Google access token.
type TokenSource func(context.Context) (string, error)

type settings struct {
	httpClient  *http.Client
	tokenSource TokenSource
}
type Option func(*settings) error

// WithHTTPClient sets the HTTP client used for Vertex requests.
func WithHTTPClient(client *http.Client) Option {
	return func(settings *settings) error {
		if client == nil {
			return fmt.Errorf("Vertex HTTP client is nil")
		}
		settings.httpClient = client
		return nil
	}
}

// WithTokenSource enables managed credentials that can refresh between requests.
func WithTokenSource(source TokenSource) Option {
	return func(settings *settings) error {
		if source == nil {
			return fmt.Errorf("Vertex token source is nil")
		}
		settings.tokenSource = source
		return nil
	}
}

// Client implements provider.Provider using Vertex raw-predict.
type Client struct{ delegate provider.Provider }

var _ provider.Provider = (*Client)(nil)

func New(profile config.Profile, options ...Option) (*Client, error) {
	if strings.TrimSpace(profile.BaseURL) == "" {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "vertex.new", Message: "Vertex publisher base URL is required"}
	}
	baseURL, err := url.Parse(profile.BaseURL)
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "vertex.new", Message: "Vertex publisher base URL must use http or https and include a host", Cause: err}
	}
	configured := settings{httpClient: http.DefaultClient}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Vertex provider option is nil")
		}
		if err := option(&configured); err != nil {
			return nil, err
		}
	}
	httpClient := *configured.httpClient
	next := configured.httpClient.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	httpClient.Transport = vertexTransport{next: next, publisherURL: baseURL, tokenSource: configured.tokenSource}
	anthropicOptions := []anthropic.Option{anthropic.WithHTTPClient(&httpClient)}
	if configured.tokenSource != nil {
		anthropicOptions = append(anthropicOptions, anthropic.WithCredential("managed-by-vertex-transport"))
	}
	delegate, err := anthropic.New(profile, anthropicOptions...)
	if err != nil {
		return nil, err
	}
	return &Client{delegate: delegate}, nil
}

func (client *Client) Name() string { return "vertex" }

func (client *Client) Capabilities(ctx context.Context) (provider.Capabilities, error) {
	capabilities, err := client.delegate.Capabilities(ctx)
	capabilities.TokenCounting = false
	return capabilities, err
}

func (client *Client) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	return client.delegate.Stream(ctx, request)
}

func (client *Client) CountTokens(context.Context, core.Request) (int, error) {
	return 0, &core.Error{Kind: core.ErrorKindProvider, Op: "vertex.count_tokens", Message: "Vertex Anthropic raw-predict does not provide portable token counting"}
}

type vertexTransport struct {
	next         http.RoundTripper
	publisherURL *url.URL
	tokenSource  TokenSource
}

func (transport vertexTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("read Vertex request body: %w", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Vertex request body: %w", err)
	}
	var model string
	if err := json.Unmarshal(payload["model"], &model); err != nil || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("Vertex request model is required")
	}
	if model == "." || model == ".." || strings.ContainsAny(model, "/\\?# \t\r\n") {
		return nil, fmt.Errorf("Vertex model must be a single safe path segment")
	}
	delete(payload, "model")
	payload["anthropic_version"] = json.RawMessage(`"` + vertexAnthropicVersion + `"`)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Vertex request body: %w", err)
	}

	endpoint := *transport.publisherURL
	endpoint.Path, err = url.JoinPath(endpoint.Path, "models", model+":streamRawPredict")
	if err != nil {
		return nil, fmt.Errorf("construct Vertex endpoint: %w", err)
	}
	endpoint.RawPath = ""
	clone := request.Clone(request.Context())
	clone.URL = &endpoint
	clone.Host = endpoint.Host
	clone.Header = request.Header.Clone()
	token := clone.Header.Get("X-Api-Key")
	if transport.tokenSource != nil {
		token, err = transport.tokenSource(request.Context())
		if err != nil {
			return nil, fmt.Errorf("resolve Vertex access token: %w", err)
		}
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("Vertex access token is empty")
	}
	clone.Header.Del("X-Api-Key")
	clone.Header.Del("Anthropic-Version")
	clone.Header.Set("Authorization", "Bearer "+token)
	clone.Body = io.NopCloser(bytes.NewReader(encoded))
	clone.ContentLength = int64(len(encoded))
	clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(encoded)), nil }
	return transport.next.RoundTrip(clone)
}
