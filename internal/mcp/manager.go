package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/product"
	toolpkg "cyber-code/internal/tool"
)

type TransportKind string

const (
	TransportStdio TransportKind = "stdio"
	TransportHTTP  TransportKind = "http"
)

var ErrPermissionDenied = errors.New("MCP permission denied")

type ServerConfig struct {
	Name          string
	Transport     TransportKind
	Command       string
	Args          []string
	Workspace     string
	Environment   map[string]string
	URL           string
	Headers       map[string]string
	ReadOnlyTools map[string]bool
}

type Authorizer interface {
	Decide(context.Context, permissions.Request) (permissions.Decision, error)
}

type ManagerOptions struct {
	Registry         *toolpkg.Registry
	Authorizer       Authorizer
	TransportFactory TransportFactory
	CallTimeout      time.Duration
}

type HealthState string

const (
	HealthConnected HealthState = "connected"
	HealthUnhealthy HealthState = "unhealthy"
	HealthClosed    HealthState = "closed"
)

type Health struct {
	State       HealthState
	LastError   string
	ConnectedAt time.Time
}

type Manager struct {
	registry   *toolpkg.Registry
	authorizer Authorizer
	factory    TransportFactory
	timeout    time.Duration

	rootCtx context.Context
	cancel  context.CancelFunc

	operationMu sync.Mutex
	mu          sync.RWMutex
	connections map[string]*connection
	closed      bool
}

type connection struct {
	config     ServerConfig
	transport  Transport
	ctx        context.Context
	cancel     context.CancelFunc
	discovered []Tool
	resources  []Resource
	tools      map[string]*remoteTool
	health     Health
}

func NewManager(options ManagerOptions) (*Manager, error) {
	if options.Registry == nil {
		return nil, fmt.Errorf("tool registry is required")
	}
	if options.Authorizer == nil {
		return nil, fmt.Errorf("permission authorizer is required")
	}
	if options.TransportFactory == nil {
		return nil, fmt.Errorf("transport factory is required")
	}
	if options.CallTimeout <= 0 {
		options.CallTimeout = 30 * time.Second
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	return &Manager{
		registry: options.Registry, authorizer: options.Authorizer, factory: options.TransportFactory,
		timeout: options.CallTimeout, rootCtx: rootCtx, cancel: cancel, connections: make(map[string]*connection),
	}, nil
}

func (manager *Manager) Connect(ctx context.Context, config ServerConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	manager.operationMu.Lock()
	defer manager.operationMu.Unlock()
	if err := validateServerConfig(config); err != nil {
		return err
	}
	manager.mu.RLock()
	closed := manager.closed
	_, exists := manager.connections[config.Name]
	manager.mu.RUnlock()
	if closed {
		return fmt.Errorf("MCP manager is closed")
	}
	if exists {
		return fmt.Errorf("MCP server %q is already connected", config.Name)
	}
	return manager.connectLocked(ctx, config, nil)
}

func (manager *Manager) Reconnect(ctx context.Context, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	manager.operationMu.Lock()
	defer manager.operationMu.Unlock()
	manager.mu.RLock()
	old, ok := manager.connections[name]
	manager.mu.RUnlock()
	if !ok {
		return fmt.Errorf("MCP server %q is not connected", name)
	}
	return manager.connectLocked(ctx, old.config, old)
}

func (manager *Manager) connectLocked(ctx context.Context, config ServerConfig, old *connection) error {
	request, err := connectionPermission(config)
	if err != nil {
		return err
	}
	decision, err := manager.authorizer.Decide(ctx, request)
	if err != nil {
		return fmt.Errorf("authorize MCP server %q: %w", config.Name, err)
	}
	if decision.Behavior != permissions.PermissionBehaviorAllow {
		return fmt.Errorf("%w: %s", ErrPermissionDenied, decision.Reason)
	}

	connectionCtx, cancel := context.WithCancel(manager.rootCtx)
	transport, err := manager.open(connectionCtx, config)
	if err != nil {
		cancel()
		manager.recordFailed(config, err)
		return fmt.Errorf("open MCP server %q: %w", config.Name, err)
	}
	candidate := &connection{config: cloneConfig(config), transport: transport, ctx: connectionCtx, cancel: cancel}
	failed := true
	defer func() {
		if failed {
			cancel()
			_ = transport.Close()
		}
	}()
	if err := manager.discover(candidate); err != nil {
		manager.recordFailed(config, err)
		return fmt.Errorf("connect MCP server %q: %w", config.Name, err)
	}

	stableServer := normalizeName(config.Name)
	candidate.tools = make(map[string]*remoteTool)
	var additions []*remoteTool
	for _, remote := range candidateDiscoveredTools(candidate) {
		stableName := "mcp__" + stableServer + "__" + normalizeName(remote.Name)
		if _, duplicate := candidate.tools[stableName]; duplicate {
			return fmt.Errorf("MCP tools produce duplicate stable name %q", stableName)
		}
		bridge := &remoteTool{
			manager: manager, server: config.Name, remoteName: remote.Name,
			spec: toolpkg.Spec{
				Name: stableName, Description: remote.Description, Schema: cloneJSON(remote.InputSchema),
				ReadOnly: config.ReadOnlyTools[remote.Name], ConcurrencySafe: config.ReadOnlyTools[remote.Name],
			},
		}
		candidate.tools[stableName] = bridge
		if oldBridge := existingBridge(old, stableName); oldBridge != nil {
			if !sameToolSpec(oldBridge.spec, bridge.spec) {
				return fmt.Errorf("MCP tool %q changed during reconnect", stableName)
			}
			candidate.tools[stableName] = oldBridge
			continue
		}
		if _, exists := manager.registry.Get(stableName); exists {
			return fmt.Errorf("tool %q is already registered", stableName)
		}
		additions = append(additions, bridge)
	}
	if old != nil && old.transport != nil && len(candidate.tools) != len(old.tools) {
		return fmt.Errorf("MCP tool set changed during reconnect")
	}
	for _, bridge := range additions {
		if err := manager.registry.Register(bridge); err != nil {
			return err
		}
	}
	candidate.health = Health{State: HealthConnected, ConnectedAt: time.Now().UTC()}
	manager.mu.Lock()
	manager.connections[config.Name] = candidate
	manager.mu.Unlock()
	failed = false
	if old != nil && old.transport != nil {
		old.cancel()
		_ = old.transport.Close()
	}
	return nil
}

func (manager *Manager) open(ctx context.Context, config ServerConfig) (Transport, error) {
	return manager.factory.Open(ctx, cloneConfig(config))
}

func (manager *Manager) discover(conn *connection) error {
	var initialized InitializeResult
	if err := manager.callTransport(conn.ctx, conn.transport, "initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion, Capabilities: map[string]any{},
		ClientInfo: Implementation{Name: product.Name, Version: "1"},
	}, &initialized); err != nil {
		return err
	}
	if initialized.ProtocolVersion == "" {
		return fmt.Errorf("%w: initialize response omitted protocolVersion", ErrProtocol)
	}
	_, toolsDeclared := initialized.Capabilities["tools"]
	_, resourcesDeclared := initialized.Capabilities["resources"]
	legacyCapabilities := len(initialized.Capabilities) == 0
	var tools ListToolsResult
	if legacyCapabilities || toolsDeclared {
		if err := manager.callTransport(conn.ctx, conn.transport, "tools/list", map[string]any{}, &tools); err != nil {
			return err
		}
	}
	conn.resources = nil
	var resources ListResourcesResult
	if legacyCapabilities || resourcesDeclared {
		if err := manager.callTransport(conn.ctx, conn.transport, "resources/list", map[string]any{}, &resources); err != nil {
			return err
		}
	}
	conn.resources = append([]Resource(nil), resources.Resources...)
	conn.discovered = append([]Tool(nil), tools.Tools...)
	return nil
}

func candidateDiscoveredTools(conn *connection) []Tool {
	tools := conn.discovered
	conn.discovered = nil
	return tools
}

func (manager *Manager) callTransport(ctx context.Context, transport Transport, method string, params, result any) error {
	callCtx, cancel := context.WithTimeout(ctx, manager.timeout)
	defer cancel()
	return transport.Call(callCtx, method, params, result)
}

func (manager *Manager) ReadResource(ctx context.Context, server, uri string) (ReadResourceResult, error) {
	var result ReadResourceResult
	if err := manager.call(ctx, server, "resources/read", ReadResourceParams{URI: uri}, &result); err != nil {
		return ReadResourceResult{}, err
	}
	return result, nil
}

func (manager *Manager) Resources(server string) []Resource {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	conn, ok := manager.connections[server]
	if !ok {
		return nil
	}
	return append([]Resource(nil), conn.resources...)
}

func (manager *Manager) Health(server string) (Health, bool) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	conn, ok := manager.connections[server]
	if !ok {
		return Health{}, false
	}
	return conn.health, true
}

func (manager *Manager) call(ctx context.Context, server, method string, params, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	manager.mu.RLock()
	conn, ok := manager.connections[server]
	manager.mu.RUnlock()
	if !ok {
		return fmt.Errorf("MCP server %q is not connected", server)
	}
	callCtx, cancel := context.WithCancel(conn.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	defer cancel()
	err := manager.callTransport(callCtx, conn.transport, method, params, result)
	if err != nil {
		manager.mu.Lock()
		if current := manager.connections[server]; current == conn {
			current.health.State = HealthUnhealthy
			current.health.LastError = err.Error()
		}
		manager.mu.Unlock()
	}
	return err
}

func (manager *Manager) recordFailed(config ServerConfig, cause error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if existing := manager.connections[config.Name]; existing != nil {
		existing.health.State = HealthUnhealthy
		existing.health.LastError = cause.Error()
		return
	}
	failedCtx, cancel := context.WithCancel(manager.rootCtx)
	cancel()
	manager.connections[config.Name] = &connection{
		config: cloneConfig(config), ctx: failedCtx, cancel: cancel,
		health: Health{State: HealthUnhealthy, LastError: cause.Error()},
	}
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
	manager.cancel()
	connections := make([]*connection, 0, len(manager.connections))
	for _, conn := range manager.connections {
		conn.health.State = HealthClosed
		connections = append(connections, conn)
	}
	manager.mu.Unlock()
	var closeErrors []error
	for _, conn := range connections {
		if conn.cancel != nil {
			conn.cancel()
		}
		if conn.transport != nil {
			closeErrors = append(closeErrors, conn.transport.Close())
		}
	}
	return errors.Join(closeErrors...)
}

type remoteTool struct {
	manager    *Manager
	server     string
	remoteName string
	spec       toolpkg.Spec
}

func (bridge *remoteTool) Spec() toolpkg.Spec { return bridge.spec }

func (bridge *remoteTool) Authorize(context.Context, json.RawMessage) (permissions.Request, error) {
	action := permissions.ActionExecute
	if bridge.spec.ReadOnly {
		action = permissions.ActionRead
	}
	return permissions.Request{Tool: bridge.spec.Name, Action: action}, nil
}

func (bridge *remoteTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	var response CallToolResult
	if err := bridge.manager.call(ctx, bridge.server, "tools/call", CallToolParams{Name: bridge.remoteName, Arguments: arguments}, &response); err != nil {
		return core.ToolResult{}, err
	}
	result := core.ToolResult{IsError: response.IsError, Content: make([]core.ContentBlock, 0, len(response.Content))}
	for _, content := range response.Content {
		text := content.Text
		if content.Type != "text" {
			encoded, err := json.Marshal(content)
			if err != nil {
				return core.ToolResult{}, fmt.Errorf("encode MCP content: %w", err)
			}
			text = string(encoded)
		}
		result.Content = append(result.Content, core.ContentBlock{Type: core.ContentText, Text: text})
	}
	return result, nil
}

func validateServerConfig(config ServerConfig) error {
	if strings.TrimSpace(config.Name) == "" || normalizeName(config.Name) == "" {
		return fmt.Errorf("MCP server name is required")
	}
	switch config.Transport {
	case TransportHTTP:
		parsed, err := url.Parse(config.URL)
		if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
			return fmt.Errorf("MCP HTTP URL is invalid")
		}
	case TransportStdio:
		if strings.TrimSpace(config.Command) == "" {
			return fmt.Errorf("MCP stdio command is required")
		}
	default:
		return fmt.Errorf("unsupported MCP transport %q", config.Transport)
	}
	return nil
}

func connectionPermission(config ServerConfig) (permissions.Request, error) {
	switch config.Transport {
	case TransportHTTP:
		parsed, err := url.Parse(config.URL)
		if err != nil {
			return permissions.Request{}, err
		}
		return permissions.Request{Tool: "mcp.connect." + normalizeName(config.Name), Action: permissions.ActionNetwork, Network: []string{parsed.Hostname()}}, nil
	case TransportStdio:
		command := strings.Join(append([]string{config.Command}, config.Args...), " ")
		return permissions.Request{Tool: "mcp.connect." + normalizeName(config.Name), Action: permissions.ActionExecute, Workspace: config.Workspace, Command: command}, nil
	default:
		return permissions.Request{}, fmt.Errorf("unsupported MCP transport %q", config.Transport)
	}
}

func normalizeName(name string) string {
	var result strings.Builder
	for _, character := range name {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '_', character == '-':
			result.WriteRune(character)
		default:
			result.WriteByte('_')
		}
	}
	return strings.Trim(result.String(), "_")
}

func cloneConfig(config ServerConfig) ServerConfig {
	config.Args = append([]string(nil), config.Args...)
	config.Environment = cloneMap(config.Environment)
	config.Headers = cloneMap(config.Headers)
	config.ReadOnlyTools = make(map[string]bool, len(config.ReadOnlyTools))
	for name, readOnly := range config.ReadOnlyTools {
		config.ReadOnlyTools[name] = readOnly
	}
	return config
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func cloneJSON(source json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), source...)
}

func existingBridge(conn *connection, name string) *remoteTool {
	if conn == nil {
		return nil
	}
	return conn.tools[name]
}

func sameToolSpec(left, right toolpkg.Spec) bool {
	return left.Name == right.Name && left.Description == right.Description && string(left.Schema) == string(right.Schema) && left.ReadOnly == right.ReadOnly
}
