// Package bedrock adapts Amazon Bedrock's Anthropic streaming endpoint.
package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"cyber-code/internal/config"
	"cyber-code/internal/core"
	"cyber-code/internal/provider"
	"cyber-code/internal/provider/anthropic"
	api "cyber-code/internal/services/api"
)

const bedrockAnthropicVersion = "bedrock-2023-05-31"

// CredentialSource returns current AWS credentials for request signing.
type CredentialSource func(context.Context) (*api.AWSCredentials, error)

type settings struct {
	httpClient       *http.Client
	region           string
	credentialSource CredentialSource
}
type Option func(*settings) error

func WithHTTPClient(client *http.Client) Option {
	return func(settings *settings) error {
		if client == nil {
			return fmt.Errorf("Bedrock HTTP client is nil")
		}
		settings.httpClient = client
		return nil
	}
}

// WithCredentials enables per-request SigV4 credentials.
func WithCredentials(region string, source CredentialSource) Option {
	return func(settings *settings) error {
		if strings.TrimSpace(region) == "" || source == nil {
			return fmt.Errorf("Bedrock region and credential source are required")
		}
		settings.region, settings.credentialSource = region, source
		return nil
	}
}

type Client struct{ delegate provider.Provider }

var _ provider.Provider = (*Client)(nil)

func New(profile config.Profile, options ...Option) (*Client, error) {
	if strings.TrimSpace(profile.BaseURL) == "" {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "bedrock.new", Message: "Bedrock runtime base URL is required"}
	}
	baseURL, err := url.Parse(profile.BaseURL)
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "bedrock.new", Message: "Bedrock runtime base URL must use http or https and include a host", Cause: err}
	}
	configured := settings{httpClient: http.DefaultClient}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Bedrock provider option is nil")
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
	httpClient.Transport = bedrockTransport{next: next, baseURL: baseURL, region: configured.region, credentialSource: configured.credentialSource}
	anthropicOptions := []anthropic.Option{anthropic.WithHTTPClient(&httpClient)}
	if configured.credentialSource != nil {
		anthropicOptions = append(anthropicOptions, anthropic.WithCredential("managed-by-bedrock-transport"))
	}
	delegate, err := anthropic.New(profile, anthropicOptions...)
	if err != nil {
		return nil, err
	}
	return &Client{delegate: delegate}, nil
}

func (client *Client) Name() string { return "bedrock" }
func (client *Client) Capabilities(ctx context.Context) (provider.Capabilities, error) {
	capabilities, err := client.delegate.Capabilities(ctx)
	capabilities.TokenCounting = false
	return capabilities, err
}
func (client *Client) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	return client.delegate.Stream(ctx, request)
}
func (client *Client) CountTokens(context.Context, core.Request) (int, error) {
	return 0, &core.Error{Kind: core.ErrorKindProvider, Op: "bedrock.count_tokens", Message: "Bedrock streaming invocation does not provide portable token counting"}
}

type bedrockTransport struct {
	next             http.RoundTripper
	baseURL          *url.URL
	region           string
	credentialSource CredentialSource
}

func (transport bedrockTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("read Bedrock request body: %w", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Bedrock request body: %w", err)
	}
	var model string
	if err := json.Unmarshal(payload["model"], &model); err != nil || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("Bedrock request model is required")
	}
	if model == "." || model == ".." || strings.ContainsAny(model, " \t\r\n") {
		return nil, fmt.Errorf("Bedrock model identifier is invalid")
	}
	delete(payload, "model")
	delete(payload, "stream")
	payload["anthropic_version"] = json.RawMessage(`"` + bedrockAnthropicVersion + `"`)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Bedrock request body: %w", err)
	}
	endpoint := *transport.baseURL
	rawPath := strings.TrimRight(endpoint.EscapedPath(), "/") + "/model/" + url.PathEscape(model) + "/invoke-with-response-stream"
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	endpoint.Path, err = url.PathUnescape(rawPath)
	if err != nil {
		return nil, fmt.Errorf("construct Bedrock endpoint: %w", err)
	}
	endpoint.RawPath = rawPath
	clone := request.Clone(request.Context())
	clone.URL, clone.Host = &endpoint, endpoint.Host
	clone.Header = request.Header.Clone()
	staticToken := clone.Header.Get("X-Api-Key")
	clone.Header.Del("X-Api-Key")
	clone.Header.Del("Anthropic-Version")
	clone.Header.Set("Accept", "application/vnd.amazon.eventstream")
	clone.Body = io.NopCloser(bytes.NewReader(encoded))
	clone.ContentLength = int64(len(encoded))
	clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(encoded)), nil }
	if transport.credentialSource != nil {
		credentials, err := transport.credentialSource(request.Context())
		if err != nil {
			return nil, fmt.Errorf("resolve Bedrock credentials: %w", err)
		}
		if credentials == nil {
			return nil, fmt.Errorf("Bedrock credential source returned nil")
		}
		if err := api.NewAWSSigner(credentials, transport.region).SignRequest(clone); err != nil {
			return nil, err
		}
	} else {
		if strings.TrimSpace(staticToken) == "" {
			return nil, fmt.Errorf("Bedrock bearer token is empty")
		}
		clone.Header.Set("Authorization", "Bearer "+staticToken)
	}
	response, err := transport.next.RoundTrip(clone)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		response.Body = transcodeEventStream(response.Body)
		response.ContentLength = -1
		response.Header.Set("Content-Type", "text/event-stream")
	}
	return response, nil
}
