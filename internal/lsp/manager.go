package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"claude-code-go/internal/permissions"
	"claude-code-go/internal/platform"
)

var (
	ErrPermissionDenied  = errors.New("LSP server startup denied")
	ErrWorkspaceBoundary = errors.New("LSP file is outside workspace")
	ErrRestartLimit      = errors.New("LSP restart limit exceeded")
)

type Authorizer interface {
	Decide(context.Context, permissions.Request) (permissions.Decision, error)
}

type ProcessConfig struct {
	Command     string
	Args        []string
	Workspace   string
	Environment map[string]string
}

type ProcessStarter interface {
	Start(context.Context, ProcessConfig) (Process, error)
}

type platformStarter struct{ runner *platform.Runner }

func (starter *platformStarter) Start(ctx context.Context, config ProcessConfig) (Process, error) {
	return starter.runner.Start(ctx, platform.ProcessRequest{
		Command: config.Command, Args: config.Args, Workspace: config.Workspace, Environment: config.Environment,
	})
}

type ServerConfig struct {
	Language              string
	Command               string
	Args                  []string
	Environment           map[string]string
	InitializationOptions any
}

type ManagerOptions struct {
	Configs         []ServerConfig
	Starter         ProcessStarter
	Authorizer      Authorizer
	CallTimeout     time.Duration
	StopTimeout     time.Duration
	MaxRestarts     int
	BaseBackoff     time.Duration
	MaxMessageBytes int
}

type Query struct {
	Workspace string
	Language  string
	File      string
	Position  Position
}

type managedClient struct {
	client *Client
	config ServerConfig
}

type Manager struct {
	configs         map[string]ServerConfig
	starter         ProcessStarter
	authorizer      Authorizer
	callTimeout     time.Duration
	stopTimeout     time.Duration
	maxRestarts     int
	baseBackoff     time.Duration
	maxMessageBytes int

	rootCtx   context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	clients   map[string]*managedClient
	restarts  map[string]int
	closed    bool
	restartMu sync.Mutex
}

func NewManager(options ManagerOptions) (*Manager, error) {
	configs := make(map[string]ServerConfig, len(options.Configs))
	for _, config := range options.Configs {
		language := strings.ToLower(strings.TrimSpace(config.Language))
		if language == "" || strings.TrimSpace(config.Command) == "" {
			return nil, fmt.Errorf("LSP language and command are required")
		}
		if _, exists := configs[language]; exists {
			return nil, fmt.Errorf("duplicate LSP language %q", language)
		}
		config.Language = language
		config.Args = append([]string(nil), config.Args...)
		config.Environment = cloneStrings(config.Environment)
		if config.InitializationOptions != nil {
			encoded, err := json.Marshal(config.InitializationOptions)
			if err != nil {
				return nil, fmt.Errorf("LSP language %q initialization options: %w", language, err)
			}
			config.InitializationOptions = json.RawMessage(append([]byte(nil), encoded...))
		}
		configs[language] = config
	}
	if options.Starter == nil {
		options.Starter = &platformStarter{runner: platform.NewRunner(platform.Options{})}
	}
	if options.CallTimeout <= 0 {
		options.CallTimeout = 10 * time.Second
	}
	if options.StopTimeout <= 0 {
		options.StopTimeout = 250 * time.Millisecond
	}
	if options.MaxRestarts <= 0 {
		options.MaxRestarts = 3
	}
	if options.BaseBackoff <= 0 {
		options.BaseBackoff = 50 * time.Millisecond
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	return &Manager{
		configs: configs, starter: options.Starter, authorizer: options.Authorizer,
		callTimeout: options.CallTimeout, stopTimeout: options.StopTimeout, maxRestarts: options.MaxRestarts,
		baseBackoff: options.BaseBackoff, maxMessageBytes: options.MaxMessageBytes,
		rootCtx: rootCtx, cancel: cancel, clients: make(map[string]*managedClient), restarts: make(map[string]int),
	}, nil
}

func (manager *Manager) Definition(ctx context.Context, query Query) ([]Location, error) {
	query, key, uri, err := manager.normalizeQuery(query)
	if err != nil {
		return nil, err
	}
	var response locationResponse
	err = manager.call(ctx, query, key, "textDocument/definition", positionParams(uri, query.Position), &response)
	return []Location(response), err
}

func (manager *Manager) References(ctx context.Context, query Query, includeDeclaration bool) ([]Location, error) {
	query, key, uri, err := manager.normalizeQuery(query)
	if err != nil {
		return nil, err
	}
	params := positionParams(uri, query.Position)
	params["context"] = map[string]bool{"includeDeclaration": includeDeclaration}
	var response []Location
	err = manager.call(ctx, query, key, "textDocument/references", params, &response)
	return response, err
}

func (manager *Manager) Completion(ctx context.Context, query Query) (CompletionList, error) {
	query, key, uri, err := manager.normalizeQuery(query)
	if err != nil {
		return CompletionList{}, err
	}
	var response completionResponse
	err = manager.call(ctx, query, key, "textDocument/completion", positionParams(uri, query.Position), &response)
	return CompletionList(response), err
}

func (manager *Manager) Hover(ctx context.Context, query Query) (*Hover, error) {
	query, key, uri, err := manager.normalizeQuery(query)
	if err != nil {
		return nil, err
	}
	var response *Hover
	err = manager.call(ctx, query, key, "textDocument/hover", positionParams(uri, query.Position), &response)
	return response, err
}

func (manager *Manager) Diagnostics(ctx context.Context, query Query) ([]Diagnostic, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	query, key, uri, err := manager.normalizeQuery(query)
	if err != nil {
		return nil, err
	}
	instance, err := manager.acquire(ctx, query, key)
	if err != nil {
		return nil, err
	}
	if cached := instance.client.Diagnostics(uri); len(cached) > 0 {
		return cached, nil
	}
	var report struct {
		Items []Diagnostic `json:"items"`
	}
	err = manager.call(ctx, query, key, "textDocument/diagnostic", map[string]any{
		"textDocument": map[string]string{"uri": uri},
	}, &report)
	if err != nil {
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) && rpcErr.Code == -32601 {
			return instance.client.Diagnostics(uri), nil
		}
		return nil, err
	}
	return append([]Diagnostic(nil), report.Items...), nil
}

func (manager *Manager) call(ctx context.Context, query Query, key, method string, params, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	instance, err := manager.acquire(ctx, query, key)
	if err != nil {
		return err
	}
	err = manager.clientCall(ctx, instance.client, method, params, result)
	if !isTransportFailure(err) {
		return err
	}
	instance, err = manager.restart(ctx, query, key, instance)
	if err != nil {
		return err
	}
	return manager.clientCall(ctx, instance.client, method, params, result)
}

func (manager *Manager) clientCall(ctx context.Context, client *Client, method string, params, result any) error {
	callCtx, cancel := context.WithTimeout(ctx, manager.callTimeout)
	defer cancel()
	return client.Call(callCtx, method, params, result)
}

func (manager *Manager) acquire(ctx context.Context, query Query, key string) (*managedClient, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return nil, ErrClientClosed
	}
	if existing := manager.clients[key]; existing != nil {
		return existing, nil
	}
	config, ok := manager.configs[query.Language]
	if !ok {
		return nil, fmt.Errorf("no LSP server configured for language %q", query.Language)
	}
	instance, err := manager.start(ctx, query.Workspace, config)
	if err != nil {
		return nil, err
	}
	manager.clients[key] = instance
	return instance, nil
}

func (manager *Manager) start(ctx context.Context, workspace string, config ServerConfig) (*managedClient, error) {
	if manager.authorizer == nil {
		return nil, fmt.Errorf("%w: permission broker unavailable", ErrPermissionDenied)
	}
	command := strings.TrimSpace(strings.Join(append([]string{config.Command}, config.Args...), " "))
	decision, err := manager.authorizer.Decide(ctx, permissions.Request{
		Tool: "lsp_server", Action: permissions.ActionExecute, Workspace: workspace, Command: command,
	})
	if err != nil {
		return nil, fmt.Errorf("authorize LSP server: %w", err)
	}
	if decision.Behavior != permissions.PermissionBehaviorAllow {
		return nil, fmt.Errorf("%w: %s", ErrPermissionDenied, decision.Reason)
	}
	process, err := manager.starter.Start(manager.rootCtx, ProcessConfig{
		Command: config.Command, Args: append([]string(nil), config.Args...), Workspace: workspace,
		Environment: cloneStrings(config.Environment),
	})
	if err != nil {
		return nil, fmt.Errorf("start LSP server: %w", err)
	}
	client, err := NewClient(ClientOptions{Process: process, MaxMessageBytes: manager.maxMessageBytes})
	if err != nil {
		_ = process.Close()
		return nil, err
	}
	initializeCtx, cancel := context.WithTimeout(ctx, manager.callTimeout)
	defer cancel()
	var initialized json.RawMessage
	err = client.Call(initializeCtx, "initialize", map[string]any{
		"processId": os.Getpid(), "rootUri": fileURI(workspace), "capabilities": map[string]any{},
		"workspaceFolders":      []map[string]string{{"uri": fileURI(workspace), "name": filepath.Base(workspace)}},
		"initializationOptions": config.InitializationOptions,
	}, &initialized)
	if err == nil {
		err = client.Notify(initializeCtx, "initialized", map[string]any{})
	}
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("initialize LSP server: %w", err)
	}
	return &managedClient{client: client, config: config}, nil
}

func (manager *Manager) restart(ctx context.Context, query Query, key string, failed *managedClient) (*managedClient, error) {
	manager.restartMu.Lock()
	defer manager.restartMu.Unlock()
	manager.mu.Lock()
	if current := manager.clients[key]; current != nil && current != failed {
		manager.mu.Unlock()
		return current, nil
	}
	if manager.restarts[key] >= manager.maxRestarts {
		manager.mu.Unlock()
		return nil, ErrRestartLimit
	}
	delete(manager.clients, key)
	manager.restarts[key]++
	attempt := manager.restarts[key]
	manager.mu.Unlock()
	_ = failed.client.Close()
	delay := manager.baseBackoff
	for step := 1; step < attempt && delay < time.Second; step++ {
		if delay > time.Second/2 {
			delay = time.Second
		} else {
			delay *= 2
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-manager.rootCtx.Done():
		return nil, ErrClientClosed
	}
	return manager.acquire(ctx, query, key)
}

func (manager *Manager) Close() error {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil
	}
	manager.closed = true
	clients := make([]*managedClient, 0, len(manager.clients))
	for _, instance := range manager.clients {
		clients = append(clients, instance)
	}
	manager.clients = make(map[string]*managedClient)
	manager.mu.Unlock()
	var closeErrors []error
	for _, instance := range clients {
		ctx, cancel := context.WithTimeout(context.Background(), manager.stopTimeout)
		var response any
		shutdownErr := instance.client.Call(ctx, "shutdown", nil, &response)
		if shutdownErr == nil {
			_ = instance.client.Notify(ctx, "exit", nil)
		}
		cancel()
		if err := instance.client.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	manager.cancel()
	return errors.Join(closeErrors...)
}

func (manager *Manager) normalizeQuery(query Query) (Query, string, string, error) {
	workspace, err := filepath.Abs(query.Workspace)
	if err != nil || strings.TrimSpace(query.Workspace) == "" {
		return Query{}, "", "", fmt.Errorf("LSP workspace is required")
	}
	workspace = filepath.Clean(workspace)
	file := query.File
	if !filepath.IsAbs(file) {
		file = filepath.Join(workspace, file)
	}
	file, err = filepath.Abs(file)
	if err != nil {
		return Query{}, "", "", err
	}
	file = filepath.Clean(file)
	relative, err := filepath.Rel(workspace, file)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return Query{}, "", "", ErrWorkspaceBoundary
	}
	language := strings.ToLower(strings.TrimSpace(query.Language))
	if language == "" {
		return Query{}, "", "", fmt.Errorf("LSP language is required")
	}
	if query.Position.Line < 0 || query.Position.Character < 0 {
		return Query{}, "", "", fmt.Errorf("LSP position must not be negative")
	}
	query.Workspace, query.File, query.Language = workspace, file, language
	return query, workspace + "\x00" + language, fileURI(file), nil
}

func positionParams(uri string, position Position) map[string]any {
	return map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position}
}

func fileURI(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func isTransportFailure(err error) bool {
	return errors.Is(err, ErrClientClosed) || errors.Is(err, ErrProtocol) || errors.Is(err, ErrMessageTooLarge)
}

func cloneStrings(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

type locationResponse []Location

func (response *locationResponse) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*response = nil
		return nil
	}
	var locations []Location
	if err := json.Unmarshal(data, &locations); err == nil {
		*response = locations
		return nil
	}
	var location Location
	if err := json.Unmarshal(data, &location); err != nil {
		return err
	}
	*response = []Location{location}
	return nil
}

type completionResponse CompletionList

func (response *completionResponse) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*response = completionResponse{}
		return nil
	}
	var list CompletionList
	if err := json.Unmarshal(data, &list); err == nil && list.Items != nil {
		*response = completionResponse(list)
		return nil
	}
	var items []CompletionItem
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	*response = completionResponse(CompletionList{Items: items})
	return nil
}
