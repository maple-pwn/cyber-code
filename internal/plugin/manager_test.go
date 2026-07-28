package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"claude-code-go/internal/permissions"
	toolpkg "claude-code-go/internal/tool"
)

func TestManagerAuthorizesCapabilitiesAndBridgesPluginTool(t *testing.T) {
	root := validPluginRoot(t)
	registry := toolpkg.NewRegistry()
	authorizer := &pluginAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}
	process := &fakePluginProcess{text: "plugin result"}
	manager := newPluginManager(t, registry, authorizer, ProcessFactoryFunc(func(context.Context, Manifest) (Process, error) { return process, nil }))
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Load(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	requests := authorizer.snapshot()
	if len(requests) != 3 || requests[0].Action != permissions.ActionRead || requests[1].Action != permissions.ActionNetwork || requests[2].Action != permissions.ActionExecute {
		t.Fatalf("permission requests = %#v", requests)
	}
	bridge, ok := registry.Get("plugin__example__echo")
	if !ok {
		t.Fatal("plugin tool was not registered")
	}
	request, err := bridge.Authorize(context.Background(), json.RawMessage(`{}`))
	if err != nil || request.Action != permissions.ActionExecute {
		t.Fatalf("tool permission = %#v, error = %v", request, err)
	}
	result, err := bridge.Run(context.Background(), json.RawMessage(`{"value":"hi"}`))
	if err != nil || result.Content[0].Text != "plugin result" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestManagerDoesNotRegisterToolsWhenStartFails(t *testing.T) {
	registry := toolpkg.NewRegistry()
	manager := newPluginManager(t, registry, allowPluginAuthorizer(), ProcessFactoryFunc(func(context.Context, Manifest) (Process, error) {
		return nil, errors.New("start failed")
	}))
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Load(context.Background(), validPluginRoot(t)); err == nil {
		t.Fatal("start failure was accepted")
	}
	if len(registry.Specs()) != 0 {
		t.Fatalf("tools registered after start failure: %#v", registry.Specs())
	}
}

func TestManagerHotReloadFailureKeepsOldProcess(t *testing.T) {
	old := &fakePluginProcess{text: "old"}
	var starts int
	registry := toolpkg.NewRegistry()
	manager := newPluginManager(t, registry, allowPluginAuthorizer(), ProcessFactoryFunc(func(context.Context, Manifest) (Process, error) {
		starts++
		if starts == 1 {
			return old, nil
		}
		return nil, errors.New("replacement failed")
	}))
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Load(context.Background(), validPluginRoot(t)); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reload(context.Background(), "example"); err == nil {
		t.Fatal("reload failure was accepted")
	}
	bridge, _ := registry.Get("plugin__example__echo")
	result, err := bridge.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil || result.Content[0].Text != "old" || old.closed {
		t.Fatalf("old process not preserved: result=%#v error=%v closed=%v", result, err, old.closed)
	}
}

func TestManagerStopTimeoutIsBounded(t *testing.T) {
	process := &fakePluginProcess{blockClose: true}
	manager, err := NewManager(ManagerOptions{
		Registry: toolpkg.NewRegistry(), Authorizer: allowPluginAuthorizer(),
		ProcessFactory: ProcessFactoryFunc(func(context.Context, Manifest) (Process, error) { return process, nil }),
		CallTimeout:    time.Second, StopTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Load(context.Background(), validPluginRoot(t)); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := manager.Close(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("plugin stop timeout was not bounded")
	}
}

func TestNewManagerDefaultsToManagedProcessFactory(t *testing.T) {
	manager, err := NewManager(ManagerOptions{Registry: toolpkg.NewRegistry(), Authorizer: allowPluginAuthorizer()})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func validPluginRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "entry"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"example","version":"1","entrypoint":"entry","capabilities":{"process":true,"files":[{"path":"data","access":"read"}],"network":["api.example.test:443"],"tools":[{"name":"echo","action":"execute"}]},"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}`
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func newPluginManager(t *testing.T, registry *toolpkg.Registry, authorizer Authorizer, factory ProcessFactory) *Manager {
	t.Helper()
	manager, err := NewManager(ManagerOptions{Registry: registry, Authorizer: authorizer, ProcessFactory: factory, CallTimeout: time.Second, StopTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

type pluginAuthorizer struct {
	mu       sync.Mutex
	decision permissions.Decision
	requests []permissions.Request
}

func (a *pluginAuthorizer) Decide(_ context.Context, request permissions.Request) (permissions.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, request)
	return a.decision, nil
}
func (a *pluginAuthorizer) snapshot() []permissions.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]permissions.Request(nil), a.requests...)
}
func allowPluginAuthorizer() *pluginAuthorizer {
	return &pluginAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}
}

type fakePluginProcess struct {
	mu         sync.Mutex
	text       string
	closed     bool
	blockClose bool
}

func (p *fakePluginProcess) Call(_ context.Context, method string, _ any, result any) error {
	if method != "tools/call" {
		return errors.New("unexpected method")
	}
	return assignPluginJSON(result, ToolResult{Content: []PluginContent{{Type: "text", Text: p.text}}})
}
func (p *fakePluginProcess) Close(ctx context.Context) error {
	p.mu.Lock()
	block := p.blockClose
	if !block {
		p.closed = true
	}
	p.mu.Unlock()
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func assignPluginJSON(target, source any) error {
	data, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
