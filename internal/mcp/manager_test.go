package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	toolpkg "cyber-code/internal/tool"
)

func TestManagerConnectsDiscoversAndBridgesToolsAndResources(t *testing.T) {
	transport := newScriptedTransport()
	registry := toolpkg.NewRegistry()
	authorizer := &recordingAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}
	manager := newTestManager(t, registry, authorizer, func(context.Context, ServerConfig) (Transport, error) {
		return transport, nil
	})
	t.Cleanup(func() { _ = manager.Close() })

	config := ServerConfig{
		Name: "docs.server", Transport: TransportHTTP, URL: "https://mcp.example.test/rpc",
		ReadOnlyTools: map[string]bool{"lookup": true},
	}
	if err := manager.Connect(context.Background(), config); err != nil {
		t.Fatal(err)
	}

	if got := transport.methods(); !reflect.DeepEqual(got, []string{"initialize", "tools/list", "resources/list"}) {
		t.Fatalf("handshake methods = %#v", got)
	}
	if transport.clientInfo.Name != "cyber-code" {
		t.Fatalf("MCP client info = %#v", transport.clientInfo)
	}
	if got := registry.Specs(); len(got) != 1 || got[0].Name != "mcp__docs_server__lookup" || !got[0].ReadOnly {
		t.Fatalf("registered specs = %#v", got)
	}
	startup := authorizer.lastRequest()
	if startup.Action != permissions.ActionNetwork || !reflect.DeepEqual(startup.Network, []string{"mcp.example.test"}) {
		t.Fatalf("startup permission = %#v", startup)
	}

	bridge, ok := registry.Get("mcp__docs_server__lookup")
	if !ok {
		t.Fatal("discovered tool was not registered")
	}
	request, err := bridge.Authorize(context.Background(), json.RawMessage(`{"query":"go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != permissions.ActionRead || request.Tool != "mcp__docs_server__lookup" {
		t.Fatalf("tool permission = %#v", request)
	}
	result, err := bridge.Run(context.Background(), json.RawMessage(`{"query":"go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "found: go" {
		t.Fatalf("tool result = %#v", result)
	}

	resources := manager.Resources("docs.server")
	if len(resources) != 1 || resources[0].URI != "docs://guide" {
		t.Fatalf("resources = %#v", resources)
	}
	read, err := manager.ReadResource(context.Background(), "docs.server", "docs://guide")
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Contents) != 1 || read.Contents[0].Text != "guide body" {
		t.Fatalf("resource result = %#v", read)
	}
	health, ok := manager.Health("docs.server")
	if !ok || health.State != HealthConnected || health.LastError != "" {
		t.Fatalf("health = %#v, present = %v", health, ok)
	}
}

func TestRegisterResourceToolReadsOnlyDiscoveredResource(t *testing.T) {
	transport := newScriptedTransport()
	registry := toolpkg.NewRegistry()
	manager := newTestManager(t, registry, allowAllAuthorizer(), func(context.Context, ServerConfig) (Transport, error) {
		return transport, nil
	})
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Connect(context.Background(), ServerConfig{Name: "docs", Transport: TransportHTTP, URL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterResourceTool(registry, manager); err != nil {
		t.Fatal(err)
	}
	resourceTool, ok := registry.Get("mcp_read_resource")
	if !ok || !resourceTool.Spec().ReadOnly {
		t.Fatalf("resource tool = %#v, present = %t", resourceTool, ok)
	}
	arguments := json.RawMessage(`{"server":"docs","uri":"docs://guide"}`)
	request, err := resourceTool.Authorize(context.Background(), arguments)
	if err != nil || request.Action != permissions.ActionRead {
		t.Fatalf("permission request = %#v, error = %v", request, err)
	}
	result, err := resourceTool.Run(context.Background(), arguments)
	if err != nil || len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "guide body") {
		t.Fatalf("resource result = %#v, error = %v", result, err)
	}
	if _, err := resourceTool.Run(context.Background(), json.RawMessage(`{"server":"docs","uri":"docs://missing"}`)); err == nil {
		t.Fatal("undiscovered resource URI was accepted")
	}
}

func TestManagerDeniesConnectionBeforeOpeningTransport(t *testing.T) {
	opened := false
	manager := newTestManager(t, toolpkg.NewRegistry(), &recordingAuthorizer{
		decision: permissions.Decision{Behavior: permissions.PermissionBehaviorDeny, Reason: "blocked"},
	}, func(context.Context, ServerConfig) (Transport, error) {
		opened = true
		return newScriptedTransport(), nil
	})
	t.Cleanup(func() { _ = manager.Close() })

	err := manager.Connect(context.Background(), ServerConfig{Name: "local", Transport: TransportStdio, Command: "server", Args: []string{"--stdio"}})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("error = %v", err)
	}
	if opened {
		t.Fatal("transport opened before permission approval")
	}
}

func TestManagerConnectionTimeoutAndProtocolFailureAreUnhealthy(t *testing.T) {
	tests := []struct {
		name string
		call func(context.Context, string, any, any) error
		want error
	}{
		{name: "timeout", call: func(ctx context.Context, _ string, _, _ any) error { <-ctx.Done(); return ctx.Err() }, want: context.DeadlineExceeded},
		{name: "protocol", call: func(context.Context, string, any, any) error { return ErrProtocol }, want: ErrProtocol},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newScriptedTransport()
			transport.callOverride = test.call
			manager := newTestManagerWithTimeout(t, toolpkg.NewRegistry(), allowAllAuthorizer(), func(context.Context, ServerConfig) (Transport, error) {
				return transport, nil
			}, 20*time.Millisecond)
			t.Cleanup(func() { _ = manager.Close() })

			err := manager.Connect(context.Background(), ServerConfig{Name: test.name, Transport: TransportHTTP, URL: "https://example.test/rpc"})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			health, ok := manager.Health(test.name)
			if !ok || health.State != HealthUnhealthy || health.LastError == "" {
				t.Fatalf("health = %#v, present = %v", health, ok)
			}
		})
	}
}

func TestManagerReconnectsWithoutDuplicatingStableTool(t *testing.T) {
	first := newScriptedTransport()
	second := newScriptedTransport()
	second.toolText = "reconnected"
	var opens int
	manager := newTestManager(t, toolpkg.NewRegistry(), allowAllAuthorizer(), func(context.Context, ServerConfig) (Transport, error) {
		opens++
		if opens == 1 {
			return first, nil
		}
		return second, nil
	})
	t.Cleanup(func() { _ = manager.Close() })
	config := ServerConfig{Name: "remote", Transport: TransportHTTP, URL: "https://example.test/rpc"}
	if err := manager.Connect(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconnect(context.Background(), "remote"); err != nil {
		t.Fatal(err)
	}
	if !first.closed {
		t.Fatal("old transport was not closed")
	}
	bridge, _ := manager.registry.Get("mcp__remote__lookup")
	result, err := bridge.Run(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content[0].Text != "reconnected" {
		t.Fatalf("result = %#v", result)
	}
	if len(manager.registry.Specs()) != 1 {
		t.Fatalf("reconnect duplicated tools: %#v", manager.registry.Specs())
	}
}

func TestManagerReconnectsAfterInitialHandshakeFailure(t *testing.T) {
	failed := newScriptedTransport()
	failed.callOverride = func(context.Context, string, any, any) error { return ErrProtocol }
	recovered := newScriptedTransport()
	var opens int
	manager := newTestManager(t, toolpkg.NewRegistry(), allowAllAuthorizer(), func(context.Context, ServerConfig) (Transport, error) {
		opens++
		if opens == 1 {
			return failed, nil
		}
		return recovered, nil
	})
	t.Cleanup(func() { _ = manager.Close() })
	config := ServerConfig{Name: "recover", Transport: TransportHTTP, URL: "https://example.test/rpc"}
	if err := manager.Connect(context.Background(), config); !errors.Is(err, ErrProtocol) {
		t.Fatalf("initial error = %v", err)
	}
	if err := manager.Reconnect(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	health, ok := manager.Health("recover")
	if !ok || health.State != HealthConnected {
		t.Fatalf("health = %#v, present = %v", health, ok)
	}
}

func TestManagerRetriesIdempotentCallAfterTransportFailure(t *testing.T) {
	first := newScriptedTransport()
	first.failMethod = "resources/read"
	first.failOnce = true
	second := newScriptedTransport()
	second.toolText = "recovered"
	opens := 0
	manager := newTestManager(t, toolpkg.NewRegistry(), allowAllAuthorizer(), func(context.Context, ServerConfig) (Transport, error) {
		opens++
		if opens == 1 {
			return first, nil
		}
		return second, nil
	})
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Connect(context.Background(), ServerConfig{Name: "retry", Transport: TransportHTTP, URL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	result, err := manager.ReadResource(context.Background(), "retry", "docs://guide")
	if err != nil || len(result.Contents) != 1 || result.Contents[0].Text != "guide body" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if opens != 2 {
		t.Fatalf("transport opens=%d", opens)
	}
}

func TestManagerDisableClosesConnectionAndReportsStatus(t *testing.T) {
	transport := newScriptedTransport()
	manager := newTestManager(t, toolpkg.NewRegistry(), allowAllAuthorizer(), func(context.Context, ServerConfig) (Transport, error) { return transport, nil })
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Connect(context.Background(), ServerConfig{Name: "server", Transport: TransportHTTP, URL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Disable(context.Background(), "server"); err != nil {
		t.Fatal(err)
	}
	status, ok := manager.Status("server")
	if !ok || !status.Disabled || status.Health.State != HealthClosed {
		t.Fatalf("status=%#v ok=%v", status, ok)
	}
	if !transport.closed {
		t.Fatal("disabled transport was not closed")
	}
	statuses := manager.Statuses()
	if len(statuses) != 1 || statuses[0].Name != "server" {
		t.Fatalf("statuses=%#v", statuses)
	}
	if err := manager.Reconnect(context.Background(), "server"); err == nil {
		t.Fatal("disabled server reconnected")
	}
}

func TestManagerToolConflictRollsBackConnection(t *testing.T) {
	registry := toolpkg.NewRegistry()
	conflict := &testTool{name: "mcp__conflict__lookup"}
	if err := registry.Register(conflict); err != nil {
		t.Fatal(err)
	}
	transport := newScriptedTransport()
	manager := newTestManager(t, registry, allowAllAuthorizer(), func(context.Context, ServerConfig) (Transport, error) {
		return transport, nil
	})
	t.Cleanup(func() { _ = manager.Close() })

	err := manager.Connect(context.Background(), ServerConfig{Name: "conflict", Transport: TransportHTTP, URL: "https://example.test/rpc"})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("error = %v", err)
	}
	if !transport.closed {
		t.Fatal("failed connection transport was not closed")
	}
	if got, _ := registry.Get("mcp__conflict__lookup"); got != conflict {
		t.Fatal("conflicting tool was replaced")
	}
}

func TestManagerOnlyDiscoversCapabilitiesDeclaredByServer(t *testing.T) {
	transport := newScriptedTransport()
	transport.callOverride = func(_ context.Context, method string, _, result any) error {
		switch method {
		case "initialize":
			return assignJSON(result, InitializeResult{
				ProtocolVersion: ProtocolVersion, Capabilities: map[string]any{"tools": map[string]any{}},
				ServerInfo: Implementation{Name: "tools-only", Version: "1"},
			})
		case "tools/list":
			return assignJSON(result, ListToolsResult{Tools: []Tool{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}}})
		case "resources/list":
			return errors.New("resources/list must not be called")
		default:
			return errors.New("unexpected method: " + method)
		}
	}
	manager := newTestManager(t, toolpkg.NewRegistry(), allowAllAuthorizer(), func(context.Context, ServerConfig) (Transport, error) {
		return transport, nil
	})
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Connect(context.Background(), ServerConfig{Name: "tools-only", Transport: TransportHTTP, URL: "https://example.test/rpc"}); err != nil {
		t.Fatal(err)
	}
	if got := transport.methods(); !reflect.DeepEqual(got, []string{"initialize", "tools/list"}) {
		t.Fatalf("methods = %#v", got)
	}
}

func newTestManager(t *testing.T, registry *toolpkg.Registry, authorizer Authorizer, open TransportFactoryFunc) *Manager {
	t.Helper()
	return newTestManagerWithTimeout(t, registry, authorizer, open, time.Second)
}

func newTestManagerWithTimeout(t *testing.T, registry *toolpkg.Registry, authorizer Authorizer, open TransportFactoryFunc, timeout time.Duration) *Manager {
	t.Helper()
	manager, err := NewManager(ManagerOptions{Registry: registry, Authorizer: authorizer, TransportFactory: open, CallTimeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func allowAllAuthorizer() *recordingAuthorizer {
	return &recordingAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}
}

type recordingAuthorizer struct {
	mu       sync.Mutex
	decision permissions.Decision
	requests []permissions.Request
}

func (a *recordingAuthorizer) Decide(_ context.Context, request permissions.Request) (permissions.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, request)
	return a.decision, nil
}

func (a *recordingAuthorizer) lastRequest() permissions.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.requests[len(a.requests)-1]
}

type scriptedTransport struct {
	mu           sync.Mutex
	calls        []string
	closed       bool
	toolText     string
	callOverride func(context.Context, string, any, any) error
	failMethod   string
	failOnce     bool
	failed       bool
	clientInfo   Implementation
}

func newScriptedTransport() *scriptedTransport { return &scriptedTransport{toolText: "found: go"} }

func (transport *scriptedTransport) Call(ctx context.Context, method string, params, result any) error {
	transport.mu.Lock()
	transport.calls = append(transport.calls, method)
	override := transport.callOverride
	fail := transport.failMethod == method && (!transport.failOnce || !transport.failed)
	if fail {
		transport.failed = true
	}
	transport.mu.Unlock()
	if fail {
		return errors.New("transport EOF")
	}
	if override != nil {
		return override(ctx, method, params, result)
	}
	switch method {
	case "initialize":
		var request InitializeParams
		if err := assignJSON(&request, params); err != nil {
			return err
		}
		transport.mu.Lock()
		transport.clientInfo = request.ClientInfo
		transport.mu.Unlock()
		return assignJSON(result, InitializeResult{ProtocolVersion: ProtocolVersion, ServerInfo: Implementation{Name: "fake", Version: "1.0"}})
	case "tools/list":
		return assignJSON(result, ListToolsResult{Tools: []Tool{{Name: "lookup", Description: "looks up docs", InputSchema: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`)}}})
	case "tools/call":
		text := transport.toolText
		if text == "found: go" {
			var request CallToolParams
			if err := assignJSON(&request, params); err != nil {
				return err
			}
			var arguments map[string]string
			if err := json.Unmarshal(request.Arguments, &arguments); err != nil {
				return err
			}
			text = "found: " + arguments["query"]
		}
		return assignJSON(result, CallToolResult{Content: []Content{{Type: "text", Text: text}}})
	case "resources/list":
		return assignJSON(result, ListResourcesResult{Resources: []Resource{{URI: "docs://guide", Name: "Guide", MIMEType: "text/plain"}}})
	case "resources/read":
		return assignJSON(result, ReadResourceResult{Contents: []ResourceContent{{URI: "docs://guide", MIMEType: "text/plain", Text: "guide body"}}})
	default:
		return errors.New("unexpected method: " + method)
	}
}

func (transport *scriptedTransport) Close() error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.closed = true
	return nil
}

func (transport *scriptedTransport) methods() []string {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]string(nil), transport.calls...)
}

func assignJSON(target, source any) error {
	data, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

type testTool struct{ name string }

func (tool *testTool) Spec() toolpkg.Spec {
	return toolpkg.Spec{Name: tool.name, Schema: json.RawMessage(`{"type":"object"}`)}
}
func (*testTool) Authorize(context.Context, json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Tool: "test", Action: permissions.ActionRead}, nil
}
func (*testTool) Run(context.Context, json.RawMessage) (core.ToolResult, error) {
	return core.ToolResult{}, nil
}
