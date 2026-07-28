package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
	toolpkg "claude-code-go/internal/tool"
)

var ErrPermissionDenied = errors.New("plugin permission denied")

type Authorizer interface {
	Decide(context.Context, permissions.Request) (permissions.Decision, error)
}

type ManagerOptions struct {
	Registry       *toolpkg.Registry
	Authorizer     Authorizer
	ProcessFactory ProcessFactory
	CallTimeout    time.Duration
	StopTimeout    time.Duration
}

type Manager struct {
	registry    *toolpkg.Registry
	authorizer  Authorizer
	factory     ProcessFactory
	callTimeout time.Duration
	stopTimeout time.Duration

	operationMu sync.Mutex
	mu          sync.RWMutex
	plugins     map[string]*loadedPlugin
	closed      bool
}

type loadedPlugin struct {
	root     string
	manifest Manifest
	process  Process
	tools    map[string]*pluginTool
}

func NewManager(options ManagerOptions) (*Manager, error) {
	if options.Registry == nil || options.Authorizer == nil {
		return nil, fmt.Errorf("plugin registry and authorizer are required")
	}
	if options.ProcessFactory == nil {
		options.ProcessFactory = NewDefaultProcessFactory(DefaultProcessOptions{})
	}
	if options.CallTimeout <= 0 {
		options.CallTimeout = 30 * time.Second
	}
	if options.StopTimeout <= 0 {
		options.StopTimeout = 5 * time.Second
	}
	return &Manager{
		registry: options.Registry, authorizer: options.Authorizer, factory: options.ProcessFactory,
		callTimeout: options.CallTimeout, stopTimeout: options.StopTimeout, plugins: make(map[string]*loadedPlugin),
	}, nil
}

func (manager *Manager) Load(ctx context.Context, root string) error {
	manager.operationMu.Lock()
	defer manager.operationMu.Unlock()
	manifest, err := LoadManifest(root)
	if err != nil {
		return err
	}
	manager.mu.RLock()
	_, exists := manager.plugins[manifest.Name]
	closed := manager.closed
	manager.mu.RUnlock()
	if closed {
		return fmt.Errorf("plugin manager is closed")
	}
	if exists {
		return fmt.Errorf("plugin %q is already loaded", manifest.Name)
	}
	return manager.loadLocked(ctx, manifest, nil)
}

func (manager *Manager) Reload(ctx context.Context, name string) error {
	manager.operationMu.Lock()
	defer manager.operationMu.Unlock()
	manager.mu.RLock()
	old := manager.plugins[name]
	manager.mu.RUnlock()
	if old == nil {
		return fmt.Errorf("plugin %q is not loaded", name)
	}
	manifest, err := LoadManifest(old.root)
	if err != nil {
		return err
	}
	return manager.loadLocked(ctx, manifest, old)
}

func (manager *Manager) loadLocked(ctx context.Context, manifest Manifest, old *loadedPlugin) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := manager.authorize(ctx, manifest); err != nil {
		return err
	}
	process, err := manager.factory.Start(ctx, cloneManifest(manifest))
	if err != nil {
		return fmt.Errorf("start plugin %q: %w", manifest.Name, err)
	}
	candidate := &loadedPlugin{root: manifest.Root, manifest: cloneManifest(manifest), process: process, tools: make(map[string]*pluginTool)}
	failed := true
	defer func() {
		if failed {
			stopCtx, cancel := context.WithTimeout(context.Background(), manager.stopTimeout)
			defer cancel()
			_ = process.Close(stopCtx)
		}
	}()
	capabilities := make(map[string]string, len(manifest.Capabilities.Tools))
	for _, capability := range manifest.Capabilities.Tools {
		capabilities[capability.Name] = capability.Action
	}
	for _, declared := range manifest.Tools {
		stableName := "plugin__" + manifest.Name + "__" + declared.Name
		bridge := &pluginTool{manager: manager, plugin: manifest.Name, remoteName: declared.Name, action: capabilities[declared.Name], spec: toolpkg.Spec{Name: stableName, Description: declared.Description, Schema: append(json.RawMessage(nil), declared.InputSchema...)}}
		candidate.tools[stableName] = bridge
		if old != nil {
			oldBridge := old.tools[stableName]
			if oldBridge == nil || !samePluginTool(oldBridge, bridge) || len(old.tools) != len(candidate.tools) && len(candidate.tools) == len(manifest.Tools) {
				return fmt.Errorf("plugin tool set changed during reload")
			}
			candidate.tools[stableName] = oldBridge
			continue
		}
		if _, exists := manager.registry.Get(stableName); exists {
			return fmt.Errorf("tool %q is already registered", stableName)
		}
	}
	if old == nil {
		for _, bridge := range candidate.tools {
			if err := manager.registry.Register(bridge); err != nil {
				return err
			}
		}
	}
	manager.mu.Lock()
	manager.plugins[manifest.Name] = candidate
	manager.mu.Unlock()
	failed = false
	if old != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), manager.stopTimeout)
		defer cancel()
		if err := old.process.Close(stopCtx); err != nil {
			return fmt.Errorf("stop replaced plugin %q: %w", manifest.Name, err)
		}
	}
	return nil
}

func (manager *Manager) authorize(ctx context.Context, manifest Manifest) error {
	requests := make([]permissions.Request, 0, len(manifest.Capabilities.Files)+2)
	for _, file := range manifest.Capabilities.Files {
		requests = append(requests, permissions.Request{Tool: "plugin." + manifest.Name, Action: file.Access, Workspace: manifest.Root, Paths: []string{file.Path}})
	}
	if len(manifest.Capabilities.Network) > 0 {
		requests = append(requests, permissions.Request{Tool: "plugin." + manifest.Name, Action: permissions.ActionNetwork, Network: append([]string(nil), manifest.Capabilities.Network...)})
	}
	requests = append(requests, permissions.Request{Tool: "plugin." + manifest.Name, Action: permissions.ActionExecute, Workspace: manifest.Root, Command: manifest.Entrypoint})
	for _, request := range requests {
		decision, err := manager.authorizer.Decide(ctx, request)
		if err != nil {
			return err
		}
		if decision.Behavior != permissions.PermissionBehaviorAllow {
			return fmt.Errorf("%w: %s", ErrPermissionDenied, decision.Reason)
		}
	}
	return nil
}

func (manager *Manager) call(ctx context.Context, plugin, method string, params, result any) error {
	manager.mu.RLock()
	loaded := manager.plugins[plugin]
	manager.mu.RUnlock()
	if loaded == nil {
		return fmt.Errorf("plugin %q is not loaded", plugin)
	}
	callCtx, cancel := context.WithTimeout(ctx, manager.callTimeout)
	defer cancel()
	return loaded.process.Call(callCtx, method, params, result)
}

func (manager *Manager) Close() error {
	manager.operationMu.Lock()
	defer manager.operationMu.Unlock()
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil
	}
	manager.closed = true
	plugins := make([]*loadedPlugin, 0, len(manager.plugins))
	for _, loaded := range manager.plugins {
		plugins = append(plugins, loaded)
	}
	manager.mu.Unlock()
	var closeErrors []error
	for _, loaded := range plugins {
		ctx, cancel := context.WithTimeout(context.Background(), manager.stopTimeout)
		closeErrors = append(closeErrors, loaded.process.Close(ctx))
		cancel()
	}
	return errors.Join(closeErrors...)
}

type pluginTool struct {
	manager                    *Manager
	plugin, remoteName, action string
	spec                       toolpkg.Spec
}

func (bridge *pluginTool) Spec() toolpkg.Spec { return bridge.spec }
func (bridge *pluginTool) Authorize(context.Context, json.RawMessage) (permissions.Request, error) {
	return permissions.Request{Tool: bridge.spec.Name, Action: bridge.action}, nil
}
func (bridge *pluginTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	var response ToolResult
	if err := bridge.manager.call(ctx, bridge.plugin, "tools/call", CallToolParams{Name: bridge.remoteName, Arguments: arguments}, &response); err != nil {
		return core.ToolResult{}, err
	}
	result := core.ToolResult{IsError: response.IsError}
	for _, content := range response.Content {
		result.Content = append(result.Content, core.ContentBlock{Type: core.ContentText, Text: content.Text})
	}
	return result, nil
}

func samePluginTool(left, right *pluginTool) bool {
	return left.remoteName == right.remoteName && left.action == right.action && left.spec.Description == right.spec.Description && string(left.spec.Schema) == string(right.spec.Schema)
}
