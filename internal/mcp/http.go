package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"cyber-code/internal/platform"
)

const defaultMaxResponseBytes int64 = 4 << 20

type HTTPOptions struct {
	URL              string
	Headers          map[string]string
	Client           *http.Client
	MaxResponseBytes int64
}

type HTTPTransport struct {
	url       string
	headers   http.Header
	client    *http.Client
	maxBytes  int64
	secrets   []string
	mu        sync.RWMutex
	sessionID string
	nextID    atomic.Int64
	closeOnce sync.Once
	closed    chan struct{}
}

func NewHTTPTransport(options HTTPOptions) (*HTTPTransport, error) {
	parsed, err := url.Parse(options.URL)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("MCP HTTP URL is invalid")
	}
	if options.Client == nil {
		options.Client = http.DefaultClient
	}
	if options.MaxResponseBytes <= 0 {
		options.MaxResponseBytes = defaultMaxResponseBytes
	}
	headers := make(http.Header, len(options.Headers))
	secrets := make([]string, 0, len(options.Headers)+2)
	for name, value := range options.Headers {
		headers.Set(name, value)
		if sensitiveHeader(name) && value != "" {
			secrets = append(secrets, value)
		}
	}
	if parsed.User != nil {
		if password, present := parsed.User.Password(); present && password != "" {
			secrets = append(secrets, password)
		}
	}
	for name, values := range parsed.Query() {
		if sensitiveValueName(name) {
			secrets = append(secrets, values...)
		}
	}
	return &HTTPTransport{
		url: options.URL, headers: headers, client: options.Client, maxBytes: options.MaxResponseBytes,
		secrets: secrets, closed: make(chan struct{}),
	}, nil
}

func (transport *HTTPTransport) Call(ctx context.Context, method string, params, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-transport.closed:
		return fmt.Errorf("MCP HTTP transport is closed")
	default:
	}
	if strings.TrimSpace(method) == "" {
		return fmt.Errorf("MCP JSON-RPC method is required")
	}
	id := transport.nextID.Add(1)
	payload, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode MCP JSON-RPC request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, transport.url, bytes.NewReader(payload))
	if err != nil {
		return transport.sanitize("create MCP HTTP request", err)
	}
	request.Header = transport.headers.Clone()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	transport.mu.RLock()
	sessionID := transport.sessionID
	transport.mu.RUnlock()
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	response, err := transport.client.Do(request)
	if err != nil {
		return transport.sanitize("send MCP HTTP request", err)
	}
	defer response.Body.Close()
	if newSessionID := strings.TrimSpace(response.Header.Get("Mcp-Session-Id")); newSessionID != "" {
		transport.mu.Lock()
		transport.sessionID = newSessionID
		transport.secrets = append(transport.secrets, newSessionID)
		transport.mu.Unlock()
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, transport.maxBytes))
		return fmt.Errorf("MCP HTTP response status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, transport.maxBytes+1))
	if err != nil {
		return transport.sanitize("read MCP HTTP response", err)
	}
	if int64(len(body)) > transport.maxBytes {
		return ErrResponseTooLarge
	}
	rpcResponse, err := decodeHTTPResponse(body, response.Header.Get("Content-Type"))
	if err != nil {
		return err
	}
	if rpcResponse.JSONRPC != "2.0" {
		return fmt.Errorf("%w: unsupported jsonrpc version", ErrProtocol)
	}
	if rpcResponse.ID != id {
		return fmt.Errorf("%w: response ID does not match request", ErrProtocol)
	}
	if rpcResponse.Error != nil {
		transport.mu.RLock()
		secrets := append([]string(nil), transport.secrets...)
		transport.mu.RUnlock()
		return &RPCError{Code: rpcResponse.Error.Code, Message: redact(rpcResponse.Error.Message, secrets)}
	}
	if len(rpcResponse.Result) == 0 {
		return fmt.Errorf("%w: response omitted result", ErrProtocol)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(rpcResponse.Result, result); err != nil {
		return fmt.Errorf("%w: malformed result", ErrProtocol)
	}
	return nil
}

func (transport *HTTPTransport) Close() error {
	transport.closeOnce.Do(func() { close(transport.closed) })
	return nil
}

func (transport *HTTPTransport) sanitize(operation string, cause error) error {
	transport.mu.RLock()
	secrets := append([]string(nil), transport.secrets...)
	transport.mu.RUnlock()
	return &redactedError{message: operation + ": " + redact(cause.Error(), secrets), cause: cause}
}

func decodeHTTPResponse(body []byte, contentType string) (jsonRPCResponse, error) {
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		var response jsonRPCResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return jsonRPCResponse{}, fmt.Errorf("%w: malformed response", ErrProtocol)
		}
		return response, nil
	}
	var dataLines []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			if len(dataLines) == 0 {
				continue
			}
			var response jsonRPCResponse
			if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &response); err != nil {
				return jsonRPCResponse{}, fmt.Errorf("%w: malformed event-stream data", ErrProtocol)
			}
			return response, nil
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataLines) > 0 {
		var response jsonRPCResponse
		if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &response); err == nil {
			return response, nil
		}
	}
	return jsonRPCResponse{}, fmt.Errorf("%w: event stream omitted JSON-RPC data", ErrProtocol)
}

type redactedError struct {
	message string
	cause   error
}

func (redacted *redactedError) Error() string { return redacted.message }
func (redacted *redactedError) Unwrap() error { return redacted.cause }

func redact(message string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	return message
}

func sensitiveHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key":
		return true
	default:
		return sensitiveValueName(name)
	}
}

func sensitiveValueName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "key")
}

type DefaultTransportOptions struct {
	HTTPClient       *http.Client
	MaxResponseBytes int64
	StdioStarter     StdioStarter
	MaxMessageBytes  int
	Runner           *platform.Runner
}

type DefaultTransportFactory struct{ options DefaultTransportOptions }

func NewDefaultTransportFactory(options DefaultTransportOptions) *DefaultTransportFactory {
	if options.StdioStarter == nil {
		options.StdioStarter = newPlatformStdioStarter(options.Runner)
	}
	return &DefaultTransportFactory{options: options}
}

func (factory *DefaultTransportFactory) Open(ctx context.Context, config ServerConfig) (Transport, error) {
	switch config.Transport {
	case TransportHTTP:
		return NewHTTPTransport(HTTPOptions{
			URL: config.URL, Headers: config.Headers, Client: factory.options.HTTPClient,
			MaxResponseBytes: factory.options.MaxResponseBytes,
		})
	case TransportStdio:
		return NewStdioTransport(ctx, StdioOptions{
			Starter: factory.options.StdioStarter,
			Config: StdioProcessConfig{
				Command: config.Command, Args: config.Args, Workspace: config.Workspace, Environment: config.Environment,
			},
			MaxMessageBytes: factory.options.MaxMessageBytes,
		})
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q", config.Transport)
	}
}
