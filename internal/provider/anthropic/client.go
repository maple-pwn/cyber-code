// Package anthropic implements the native Anthropic Messages API.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"claude-code-go/internal/config"
	"claude-code-go/internal/core"
	"claude-code-go/internal/provider"
	"claude-code-go/internal/security"
)

const (
	defaultBaseURL       = "https://api.anthropic.com"
	defaultMaxTokens     = 4096
	defaultResponseLimit = int64(16 << 20)
	defaultMaxRetries    = 2
	defaultRetryBase     = 250 * time.Millisecond
	defaultRetryMaximum  = 30 * time.Second
	anthropicVersion     = "2023-06-01"
)

// Option customizes a Client.
type Option func(*Client) error

// Client implements provider.Provider using Anthropic's native protocol.
type Client struct {
	profile       config.Profile
	apiKey        string
	baseURL       string
	httpClient    *http.Client
	maxRetries    int
	retryBase     time.Duration
	retryMaximum  time.Duration
	responseLimit int64
}

var _ provider.Provider = (*Client)(nil)

// New creates an Anthropic client and resolves its API key on demand.
func New(profile config.Profile, options ...Option) (*Client, error) {
	baseURL := strings.TrimSpace(profile.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "anthropic.new", Message: "Anthropic base URL must use http or https and include a host", Cause: err}
	}
	if strings.TrimSpace(profile.Model) == "" {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "anthropic.new", Message: "Anthropic model is required"}
	}

	client := &Client{
		profile:       profile,
		baseURL:       strings.TrimRight(baseURL, "/"),
		httpClient:    http.DefaultClient,
		maxRetries:    defaultMaxRetries,
		retryBase:     defaultRetryBase,
		retryMaximum:  defaultRetryMaximum,
		responseLimit: defaultResponseLimit,
	}
	for _, option := range options {
		if option == nil {
			return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "anthropic.new", Message: "Anthropic client option is nil"}
		}
		if err := option(client); err != nil {
			return nil, err
		}
	}
	if client.apiKey == "" {
		credential, err := config.ResolveCredential(profile)
		if err != nil {
			return nil, &core.Error{Kind: core.ErrorKindAuthentication, Op: "anthropic.new", Message: "Anthropic credential is unavailable", Cause: err}
		}
		client.apiKey = credential
	}
	if client.apiKey == "" {
		return nil, &core.Error{Kind: core.ErrorKindAuthentication, Op: "anthropic.new", Message: "Anthropic credential is required"}
	}
	return client, nil
}

// WithCredential supplies a credential from a managed source instead of an
// environment variable. The value is retained only by the client instance.
func WithCredential(credential string) Option {
	return func(client *Client) error {
		if strings.TrimSpace(credential) == "" {
			return &core.Error{Kind: core.ErrorKindAuthentication, Op: "anthropic.new", Message: "Anthropic credential is required"}
		}
		client.apiKey = credential
		return nil
	}
}

// WithHTTPClient sets the HTTP transport used by the provider.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) error {
		if httpClient == nil {
			return &core.Error{Kind: core.ErrorKindConfiguration, Op: "anthropic.new", Message: "HTTP client is nil"}
		}
		client.httpClient = httpClient
		return nil
	}
}

// WithRetryPolicy sets the number of retries after the first request and the
// exponential-backoff base delay.
func WithRetryPolicy(maxRetries int, baseDelay time.Duration) Option {
	return func(client *Client) error {
		if maxRetries < 0 || baseDelay < 0 {
			return &core.Error{Kind: core.ErrorKindConfiguration, Op: "anthropic.new", Message: "retry policy values must not be negative"}
		}
		client.maxRetries = maxRetries
		client.retryBase = baseDelay
		return nil
	}
}

// WithResponseLimit sets the maximum decoded response body size.
func WithResponseLimit(limit int64) Option {
	return func(client *Client) error {
		if limit <= 0 {
			return &core.Error{Kind: core.ErrorKindConfiguration, Op: "anthropic.new", Message: "response limit must be positive"}
		}
		client.responseLimit = limit
		return nil
	}
}

// Name returns the stable registry name.
func (client *Client) Name() string {
	return "anthropic"
}

// Capabilities reports native Anthropic API features.
func (client *Client) Capabilities(ctx context.Context) (provider.Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return provider.Capabilities{}, canceledError("anthropic.capabilities", err)
	}
	return provider.Capabilities{
		Streaming:     true,
		ToolCalls:     true,
		Thinking:      true,
		TokenCounting: true,
	}, nil
}

// Stream starts a Messages API stream.
func (client *Client) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	payload, err := encodeRequest(client.profile, request, true)
	if err != nil {
		return nil, err
	}
	response, err := client.do(ctx, "v1/messages", payload, "text/event-stream")
	if err != nil {
		return nil, err
	}

	events := make(chan core.Event, 16)
	go client.consumeStream(ctx, response.Body, events)
	return events, nil
}

// CountTokens calls Anthropic's token-counting endpoint.
func (client *Client) CountTokens(ctx context.Context, request core.Request) (int, error) {
	payload, err := encodeRequest(client.profile, request, false)
	if err != nil {
		return 0, err
	}
	response, err := client.do(ctx, "v1/messages/count_tokens", payload, "application/json")
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	body, err := readBounded(response.Body, client.responseLimit)
	if err != nil {
		return 0, &core.Error{Kind: core.ErrorKindProvider, Op: "anthropic.count_tokens", Message: "Anthropic response exceeded the configured limit", Cause: err}
	}
	var decoded struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return 0, &core.Error{Kind: core.ErrorKindProvider, Op: "anthropic.count_tokens", Message: "Anthropic returned invalid token-count JSON", Cause: err}
	}
	if decoded.InputTokens < 0 {
		return 0, &core.Error{Kind: core.ErrorKindProvider, Op: "anthropic.count_tokens", Message: "Anthropic returned a negative token count"}
	}
	return decoded.InputTokens, nil
}

func (client *Client) do(ctx context.Context, endpoint string, payload []byte, accept string) (*http.Response, error) {
	requestURL, err := url.JoinPath(client.baseURL, endpoint)
	if err != nil {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "anthropic.request", Message: "failed to construct Anthropic endpoint", Cause: err}
	}

	for attempt := 0; attempt <= client.maxRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
		if err != nil {
			return nil, &core.Error{Kind: core.ErrorKindInternal, Op: "anthropic.request", Message: "failed to construct Anthropic request", Cause: err}
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", accept)
		request.Header.Set("X-Api-Key", client.apiKey)
		request.Header.Set("Anthropic-Version", anthropicVersion)

		response, requestErr := client.httpClient.Do(request)
		if requestErr != nil {
			if ctx.Err() != nil {
				return nil, canceledError("anthropic.request", ctx.Err())
			}
			retryable := isRetryableTransportError(requestErr)
			classified := &core.Error{Kind: core.ErrorKindProvider, Op: "anthropic.request", Message: "Anthropic request failed", Retryable: retryable, Cause: security.NewRedactor(client.apiKey).Error(requestErr)}
			if !retryable || attempt == client.maxRetries {
				return nil, classified
			}
			if err := waitForRetry(ctx, client.retryDelay(attempt, "")); err != nil {
				return nil, canceledError("anthropic.retry", err)
			}
			continue
		}

		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}

		classified := client.responseError(response)
		response.Body.Close()
		if !classified.Retryable || attempt == client.maxRetries {
			return nil, classified
		}
		if err := waitForRetry(ctx, client.retryDelay(attempt, response.Header.Get("Retry-After"))); err != nil {
			return nil, canceledError("anthropic.retry", err)
		}
	}
	return nil, &core.Error{Kind: core.ErrorKindInternal, Op: "anthropic.request", Message: "retry loop terminated unexpectedly"}
}

func (client *Client) responseError(response *http.Response) *core.Error {
	body, readErr := readBounded(response.Body, client.responseLimit)
	message := http.StatusText(response.StatusCode)
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if readErr == nil && json.Unmarshal(body, &envelope) == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		message = envelope.Error.Message
	}
	if readErr != nil {
		message = "Anthropic error response exceeded the configured limit"
	}
	redactor := security.NewRedactor(client.apiKey)
	message = redactor.Text(message)
	kind := core.ErrorKindProvider
	retryable := false
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = core.ErrorKindAuthentication
	case http.StatusTooManyRequests:
		kind = core.ErrorKindRateLimit
		retryable = true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		retryable = true
	}
	return &core.Error{
		Kind:      kind,
		Op:        "anthropic.request",
		Message:   fmt.Sprintf("Anthropic returned HTTP %d: %s", response.StatusCode, message),
		Retryable: retryable,
		Cause:     redactor.Error(readErr),
	}
}

func (client *Client) retryDelay(attempt int, retryAfter string) time.Duration {
	if delay, ok := parseRetryAfter(retryAfter, time.Now()); ok {
		return minDuration(delay, client.retryMaximum)
	}
	delay := client.retryBase
	for index := 0; index < attempt && delay < client.retryMaximum; index++ {
		if delay > client.retryMaximum/2 {
			return client.retryMaximum
		}
		delay *= 2
	}
	return minDuration(delay, client.retryMaximum)
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if !when.After(now) {
		return 0, true
	}
	return when.Sub(now), true
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return body, nil
}

func canceledError(op string, cause error) *core.Error {
	if cause == nil {
		cause = context.Canceled
	}
	return &core.Error{Kind: core.ErrorKindCanceled, Op: op, Message: "Anthropic operation was canceled", Cause: cause}
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func isRetryableTransportError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}
