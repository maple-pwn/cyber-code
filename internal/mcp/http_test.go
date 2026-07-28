package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

func TestHTTPTransportRunsManagerProtocolAndSendsConfiguredHeaders(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		if request.Header.Get("Authorization") != "Bearer oauth-secret" || request.Header.Get("X-Tenant") != "acme" {
			t.Errorf("headers = %#v", request.Header)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var rpc jsonRPCRequest
		if err := json.Unmarshal(body, &rpc); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		methods = append(methods, rpc.Method)
		mu.Unlock()
		var result any
		switch rpc.Method {
		case "initialize":
			result = InitializeResult{ProtocolVersion: ProtocolVersion, ServerInfo: Implementation{Name: "http-test", Version: "1"}}
		case "tools/list":
			result = ListToolsResult{Tools: []Tool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
		case "resources/list":
			result = ListResourcesResult{Resources: []Resource{{URI: "test://resource", Name: "Test"}}}
		case "tools/call":
			result = CallToolResult{Content: []Content{{Type: "text", Text: "http result"}}}
		case "resources/read":
			result = ReadResourceResult{Contents: []ResourceContent{{URI: "test://resource", Text: "resource body"}}}
		default:
			t.Errorf("unexpected method %q", rpc.Method)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: rpc.ID, Result: mustRawJSON(t, result)})
	}))
	defer server.Close()

	registry := toolpkg.NewRegistry()
	factory := NewDefaultTransportFactory(DefaultTransportOptions{HTTPClient: server.Client()})
	manager, err := NewManager(ManagerOptions{
		Registry: registry, Authorizer: allowAllAuthorizer(), TransportFactory: factory, CallTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Connect(context.Background(), ServerConfig{
		Name: "http", Transport: TransportHTTP, URL: server.URL,
		Headers: map[string]string{"Authorization": "Bearer oauth-secret", "X-Tenant": "acme"},
	}); err != nil {
		t.Fatal(err)
	}
	bridge, _ := registry.Get("mcp__http__echo")
	if _, err := bridge.Run(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReadResource(context.Background(), "http", "test://resource"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"initialize", "tools/list", "resources/list", "tools/call", "resources/read"}
	if len(methods) != len(want) {
		t.Fatalf("methods = %#v", methods)
	}
	for index := range want {
		if methods[index] != want[index] {
			t.Fatalf("methods = %#v", methods)
		}
	}
}

func TestHTTPTransportRejectsMalformedAndMismatchedJSONRPCWithoutLeakingSecrets(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `not-json oauth-secret query-secret`},
		{name: "wrong version", body: `{"jsonrpc":"1.0","id":1,"result":{}}`},
		{name: "wrong id", body: `{"jsonrpc":"2.0","id":999,"result":{}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			transport, err := NewHTTPTransport(HTTPOptions{
				URL: server.URL + "?access_token=query-secret", Headers: map[string]string{"Authorization": "Bearer oauth-secret"}, Client: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer transport.Close()
			err = transport.Call(context.Background(), "initialize", map[string]any{}, &InitializeResult{})
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(err.Error(), "oauth-secret") || strings.Contains(err.Error(), "query-secret") {
				t.Fatalf("secret leaked in error: %v", err)
			}
		})
	}
}

func TestHTTPTransportHonorsCallContextAndResponseLimit(t *testing.T) {
	t.Run("context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			time.Sleep(100 * time.Millisecond)
		}))
		defer server.Close()
		transport, err := NewHTTPTransport(HTTPOptions{URL: server.URL, Client: server.Client()})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		err = transport.Call(ctx, "initialize", nil, &InitializeResult{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("response limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte(strings.Repeat("x", 128)))
		}))
		defer server.Close()
		transport, err := NewHTTPTransport(HTTPOptions{URL: server.URL, Client: server.Client(), MaxResponseBytes: 32})
		if err != nil {
			t.Fatal(err)
		}
		err = transport.Call(context.Background(), "initialize", nil, &InitializeResult{})
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestHTTPTransportPersistsSessionIDAndReadsEventStreamResponse(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls++
		var rpc jsonRPCRequest
		_ = json.NewDecoder(request.Body).Decode(&rpc)
		if calls == 1 {
			response.Header().Set("Mcp-Session-Id", "session-secret")
		} else if request.Header.Get("Mcp-Session-Id") != "session-secret" {
			t.Errorf("session header = %q", request.Header.Get("Mcp-Session-Id"))
		}
		response.Header().Set("Content-Type", "text/event-stream")
		result := mustRawJSON(t, map[string]any{"call": calls})
		payload, _ := json.Marshal(jsonRPCResponse{JSONRPC: "2.0", ID: rpc.ID, Result: result})
		_, _ = response.Write([]byte("event: message\ndata: " + string(payload) + "\n\n"))
	}))
	defer server.Close()
	transport, err := NewHTTPTransport(HTTPOptions{URL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	for index := 1; index <= 2; index++ {
		var result struct {
			Call int `json:"call"`
		}
		if err := transport.Call(context.Background(), "test", nil, &result); err != nil {
			t.Fatal(err)
		}
		if result.Call != index {
			t.Fatalf("result = %#v", result)
		}
	}
}

func TestHTTPConnectionPermissionDoesNotIncludeCredentials(t *testing.T) {
	request, err := connectionPermission(ServerConfig{
		Name: "secure", Transport: TransportHTTP,
		URL:     "https://user:password@example.test/rpc?access_token=query-secret",
		Headers: map[string]string{"Authorization": "Bearer oauth-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != permissions.ActionNetwork || len(request.Network) != 1 || request.Network[0] != "example.test" {
		t.Fatalf("permission = %#v", request)
	}
	encoded, _ := json.Marshal(request)
	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "secret") {
		t.Fatalf("credentials leaked into permission request: %s", encoded)
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
