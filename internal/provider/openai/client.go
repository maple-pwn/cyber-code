// Package openai implements the OpenAI-compatible Chat Completions protocol.
package openai

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
)

const (
	defaultBaseURL       = "https://api.openai.com"
	defaultResponseLimit = int64(16 << 20)
	defaultMaxRetries    = 2
	defaultRetryBase     = 250 * time.Millisecond
	defaultRetryMaximum  = 30 * time.Second
)

type Option func(*Client) error

// Client is deliberately SDK-free so it works with OpenAI-compatible hosts.
type Client struct {
	profile                 config.Profile
	apiKey                  string
	baseURL                 *url.URL
	httpClient              *http.Client
	toolCalls               bool
	maxRetries              int
	retryBase, retryMaximum time.Duration
	responseLimit           int64
}

var _ provider.Provider = (*Client)(nil)

func New(profile config.Profile, options ...Option) (*Client, error) {
	key, err := config.ResolveCredential(profile)
	if err != nil {
		return nil, &core.Error{Kind: core.ErrorKindAuthentication, Op: "openai.new", Message: "OpenAI-compatible credential is unavailable", Cause: err}
	}
	if key == "" {
		return nil, &core.Error{Kind: core.ErrorKindAuthentication, Op: "openai.new", Message: "OpenAI-compatible credential is required"}
	}
	base := strings.TrimSpace(profile.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "openai.new", Message: "OpenAI-compatible base URL must use http or https and include a host", Cause: err}
	}
	if strings.TrimSpace(profile.Model) == "" {
		return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "openai.new", Message: "OpenAI-compatible model is required"}
	}
	client := &Client{profile: profile, apiKey: key, baseURL: parsed, httpClient: http.DefaultClient, toolCalls: true, maxRetries: defaultMaxRetries, retryBase: defaultRetryBase, retryMaximum: defaultRetryMaximum, responseLimit: defaultResponseLimit}
	for _, option := range options {
		if option == nil {
			return nil, &core.Error{Kind: core.ErrorKindConfiguration, Op: "openai.new", Message: "OpenAI-compatible client option is nil"}
		}
		if err := option(client); err != nil {
			return nil, err
		}
	}
	return client, nil
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) error {
		if httpClient == nil {
			return &core.Error{Kind: core.ErrorKindConfiguration, Op: "openai.new", Message: "HTTP client is nil"}
		}
		c.httpClient = httpClient
		return nil
	}
}
func WithToolCalls(enabled bool) Option {
	return func(c *Client) error { c.toolCalls = enabled; return nil }
}
func WithRetryPolicy(retries int, base time.Duration) Option {
	return func(c *Client) error {
		if retries < 0 || base < 0 {
			return &core.Error{Kind: core.ErrorKindConfiguration, Op: "openai.new", Message: "retry policy values must not be negative"}
		}
		c.maxRetries, c.retryBase = retries, base
		return nil
	}
}
func WithResponseLimit(limit int64) Option {
	return func(c *Client) error {
		if limit <= 0 {
			return &core.Error{Kind: core.ErrorKindConfiguration, Op: "openai.new", Message: "response limit must be positive"}
		}
		c.responseLimit = limit
		return nil
	}
}

func (c *Client) Name() string { return "openai" }
func (c *Client) Capabilities(ctx context.Context) (provider.Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return provider.Capabilities{}, canceledError("openai.capabilities", err)
	}
	return provider.Capabilities{Streaming: true, ToolCalls: c.toolCalls}, nil
}
func (c *Client) CountTokens(context.Context, core.Request) (int, error) {
	return 0, &core.Error{Kind: core.ErrorKindProvider, Op: "openai.count_tokens", Message: "OpenAI-compatible Chat Completions does not provide portable token counting"}
}

func (c *Client) Stream(ctx context.Context, request core.Request) (<-chan core.Event, error) {
	if len(request.Tools) != 0 && !c.toolCalls {
		return nil, &core.Error{Kind: core.ErrorKindProvider, Op: "openai.stream", Message: "configured OpenAI-compatible backend does not support tool calls"}
	}
	payload, err := encodeRequest(c.profile, request)
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, payload)
	if err != nil {
		return nil, err
	}
	events := make(chan core.Event, 16)
	go c.consumeStream(ctx, response.Body, events)
	return events, nil
}

func (c *Client) do(ctx context.Context, payload []byte) (*http.Response, error) {
	endpoint := *c.baseURL
	endpoint.Path, _ = url.JoinPath(endpoint.Path, "v1", "chat", "completions")
	endpoint.RawPath = ""
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, &core.Error{Kind: core.ErrorKindInternal, Op: "openai.request", Message: "failed to construct OpenAI-compatible request", Cause: err}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		response, requestErr := c.httpClient.Do(req)
		if requestErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		if requestErr != nil {
			if ctx.Err() != nil {
				return nil, canceledError("openai.request", ctx.Err())
			}
			classified := &core.Error{Kind: core.ErrorKindProvider, Op: "openai.request", Message: "OpenAI-compatible request failed", Retryable: retryableTransport(requestErr), Cause: requestErr}
			if !classified.Retryable || attempt == c.maxRetries {
				return nil, classified
			}
			if err := wait(ctx, c.delay(attempt, "")); err != nil {
				return nil, canceledError("openai.retry", err)
			}
			continue
		}
		classified := c.responseError(response)
		retryAfter := response.Header.Get("Retry-After")
		response.Body.Close()
		if !classified.Retryable || attempt == c.maxRetries {
			return nil, classified
		}
		if err := wait(ctx, c.delay(attempt, retryAfter)); err != nil {
			return nil, canceledError("openai.retry", err)
		}
	}
	return nil, &core.Error{Kind: core.ErrorKindInternal, Op: "openai.request", Message: "retry loop terminated unexpectedly"}
}

func (c *Client) responseError(response *http.Response) *core.Error {
	body, err := bounded(response.Body, c.responseLimit)
	message := http.StatusText(response.StatusCode)
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err == nil && json.Unmarshal(body, &envelope) == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		message = envelope.Error.Message
	}
	kind, retryable := core.ErrorKindProvider, false
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = core.ErrorKindAuthentication
	case http.StatusTooManyRequests:
		kind, retryable = core.ErrorKindRateLimit, true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		retryable = true
	}
	return &core.Error{Kind: kind, Op: "openai.request", Message: fmt.Sprintf("OpenAI-compatible backend returned HTTP %d: %s", response.StatusCode, message), Retryable: retryable, Cause: err}
}

func (c *Client) delay(attempt int, retryAfter string) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(retryAfter), 10, 64); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, c.retryMaximum)
	}
	d := c.retryBase
	for i := 0; i < attempt && d < c.retryMaximum; i++ {
		if d > c.retryMaximum/2 {
			return c.retryMaximum
		}
		d *= 2
	}
	return min(d, c.retryMaximum)
}
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
func wait(ctx context.Context, delay time.Duration) error {
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
func bounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
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
	return &core.Error{Kind: core.ErrorKindCanceled, Op: op, Message: "OpenAI-compatible operation was canceled", Cause: cause}
}
func retryableTransport(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}
