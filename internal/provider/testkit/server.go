// Package testkit provides local provider contract-test helpers.
package testkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// CapturedRequest is an immutable snapshot of an HTTP request received by a
// scripted server.
type CapturedRequest struct {
	Method   string
	Path     string
	RawQuery string
	Header   http.Header
	Body     []byte
}

// Server runs handlers sequentially and records each request before invoking
// its handler.
type Server struct {
	server *httptest.Server

	mu       sync.Mutex
	handlers []http.HandlerFunc
	next     int
	requests []CapturedRequest
}

// NewServer starts a local HTTP server with one handler per expected request.
func NewServer(handlers ...http.HandlerFunc) *Server {
	result := &Server{handlers: append([]http.HandlerFunc(nil), handlers...)}
	result.server = httptest.NewServer(http.HandlerFunc(result.serveHTTP))
	return result
}

// URL returns the local server base URL.
func (server *Server) URL() string {
	return server.server.URL
}

// Client returns an HTTP client configured for the local server.
func (server *Server) Client() *http.Client {
	return server.server.Client()
}

// Close stops the local server.
func (server *Server) Close() {
	server.server.Close()
}

// Requests returns independent snapshots of all captured requests.
func (server *Server) Requests() []CapturedRequest {
	server.mu.Lock()
	defer server.mu.Unlock()

	requests := make([]CapturedRequest, len(server.requests))
	for index, request := range server.requests {
		requests[index] = CapturedRequest{
			Method:   request.Method,
			Path:     request.Path,
			RawQuery: request.RawQuery,
			Header:   request.Header.Clone(),
			Body:     append([]byte(nil), request.Body...),
		}
	}
	return requests
}

func (server *Server) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "failed to read scripted request", http.StatusBadRequest)
		return
	}
	request.Body = io.NopCloser(bytes.NewReader(body))

	server.mu.Lock()
	server.requests = append(server.requests, CapturedRequest{
		Method:   request.Method,
		Path:     request.URL.Path,
		RawQuery: request.URL.RawQuery,
		Header:   request.Header.Clone(),
		Body:     append([]byte(nil), body...),
	})
	index := server.next
	server.next++
	var handler http.HandlerFunc
	if index < len(server.handlers) {
		handler = server.handlers[index]
	}
	server.mu.Unlock()

	if handler == nil {
		http.Error(response, "unexpected scripted request", http.StatusInternalServerError)
		return
	}
	handler(response, request)
}

// WriteSSE writes and flushes one standards-compliant SSE frame.
func WriteSSE(response http.ResponseWriter, event, data string) error {
	if strings.ContainsAny(event, "\r\n") {
		return fmt.Errorf("SSE event name contains a newline")
	}
	if event != "" {
		if _, err := fmt.Fprintf(response, "event: %s\n", event); err != nil {
			return err
		}
	}

	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	for _, line := range strings.Split(data, "\n") {
		if _, err := fmt.Fprintf(response, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(response, "\n"); err != nil {
		return err
	}
	if flusher, ok := response.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

// WriteSSEJSON marshals value and writes it as one SSE frame.
func WriteSSEJSON(response http.ResponseWriter, event string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return WriteSSE(response, event, string(data))
}
