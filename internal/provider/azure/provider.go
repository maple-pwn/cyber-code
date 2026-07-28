// Package azure adapts Azure AI Foundry's Anthropic-compatible endpoint.
package azure

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
	"cyber-code/internal/provider"
	"cyber-code/internal/provider/anthropic"
)

type TokenSource func(context.Context) (string, error)

type settings struct {
	httpClient  *http.Client
	tokenSource TokenSource
}
type Option func(*settings) error

// WithHTTPClient sets the transport used by the Foundry provider.
func WithHTTPClient(client *http.Client) Option {
	return func(settings *settings) error {
		if client == nil {
			return fmt.Errorf("Azure HTTP client is nil")
		}
		settings.httpClient = client
		return nil
	}
}

// WithTokenSource enables a refreshable Azure AD credential.
func WithTokenSource(source TokenSource) Option {
	return func(settings *settings) error {
		if source == nil {
			return fmt.Errorf("Azure token source is nil")
		}
		settings.tokenSource = source
		return nil
	}
}

// Client exposes Azure under its own registry name while reusing the canonical
// Anthropic Messages codec and event parser.
type Client struct{ delegate provider.Provider }

var _ provider.Provider = (*Client)(nil)

func New(profile config.Profile, options ...Option) (*Client, error) {
	if strings.TrimSpace(profile.BaseURL) == "" {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "azure.new", Message: "Azure Foundry base URL is required"}
	}
	configured := settings{httpClient: http.DefaultClient}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Azure provider option is nil")
		}
		if err := option(&configured); err != nil {
			return nil, err
		}
	}
	httpClient := *configured.httpClient
	transport := configured.httpClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	httpClient.Transport = foundryTransport{next: transport, tokenSource: configured.tokenSource}
	anthropicOptions := []anthropic.Option{anthropic.WithHTTPClient(&httpClient)}
	if configured.tokenSource != nil {
		anthropicOptions = append(anthropicOptions, anthropic.WithCredential("managed-by-azure-transport"))
	}
	delegate, err := anthropic.New(profile, anthropicOptions...)
	if err != nil {
		return nil, err
	}
	return &Client{delegate: delegate}, nil
}

func (client *Client) Name() string { return "azure" }
func (client *Client) Capabilities(ctx context.Context) (provider.Capabilities, error) {
	return client.delegate.Capabilities(ctx)
}
func (client *Client) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	return client.delegate.Stream(ctx, request)
}
func (client *Client) CountTokens(ctx context.Context, request core.Request) (int, error) {
	return client.delegate.CountTokens(ctx, request)
}

type foundryTransport struct {
	next        http.RoundTripper
	tokenSource TokenSource
}

func (transport foundryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	if transport.tokenSource != nil {
		token, err := transport.tokenSource(request.Context())
		if err != nil {
			return nil, fmt.Errorf("resolve Azure access token: %w", err)
		}
		if strings.TrimSpace(token) == "" {
			return nil, fmt.Errorf("Azure access token is empty")
		}
		clone.Header.Del("X-Api-Key")
		clone.Header.Set("Authorization", "Bearer "+token)
		return transport.next.RoundTrip(clone)
	}
	if key := clone.Header.Get("X-Api-Key"); key != "" {
		clone.Header.Del("X-Api-Key")
		clone.Header.Set("api-key", key)
	}
	return transport.next.RoundTrip(clone)
}
