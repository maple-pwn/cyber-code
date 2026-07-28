package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"claude-code-go/internal/permissions"
)

func TestManagerAuthorizesStartupAndReusesWorkspaceLanguageClient(t *testing.T) {
	workspace := t.TempDir()
	authorizer := &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}}
	starter := &lspTestStarter{t: t, serve: serveLSPQueries}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "fake-gopls"}}, Starter: starter, Authorizer: authorizer,
		CallTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	query := Query{Workspace: workspace, Language: "go", File: filepath.Join(workspace, "main.go"), Position: Position{Line: 1, Character: 2}}
	locations, err := manager.Definition(context.Background(), query)
	if err != nil || len(locations) != 1 || locations[0].URI != "file:///definition.go" {
		t.Fatalf("definition = %#v, error = %v", locations, err)
	}
	hover, err := manager.Hover(context.Background(), query)
	if err != nil || hover == nil {
		t.Fatalf("hover = %#v, error = %v", hover, err)
	}
	if starter.count() != 1 || authorizer.count() != 1 {
		t.Fatalf("starts = %d, authorizations = %d", starter.count(), authorizer.count())
	}
	request := authorizer.lastRequest()
	if request.Action != permissions.ActionExecute || request.Workspace != workspace || request.Command != "fake-gopls" {
		t.Fatalf("startup permission = %#v", request)
	}
}

func TestManagerIsolatesClientsByWorkspaceAndLanguage(t *testing.T) {
	starter := &lspTestStarter{t: t, serve: serveLSPQueries}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls"}, {Language: "rust", Command: "rust-analyzer"}},
		Starter: starter, Authorizer: &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	for _, item := range []struct{ workspace, language, file string }{
		{t.TempDir(), "go", "main.go"}, {t.TempDir(), "go", "other.go"}, {t.TempDir(), "rust", "lib.rs"},
	} {
		query := Query{Workspace: item.workspace, Language: item.language, File: filepath.Join(item.workspace, item.file)}
		if _, err := manager.Hover(context.Background(), query); err != nil {
			t.Fatal(err)
		}
	}
	if starter.count() != 3 {
		t.Fatalf("process starts = %d", starter.count())
	}
}

func TestManagerSupportsReferencesCompletionAndDiagnostics(t *testing.T) {
	workspace := t.TempDir()
	starter := &lspTestStarter{t: t, serve: serveLSPQueries}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls"}}, Starter: starter,
		Authorizer: &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	query := Query{Workspace: workspace, Language: "go", File: filepath.Join(workspace, "main.go")}
	references, err := manager.References(context.Background(), query, true)
	if err != nil || len(references) != 1 {
		t.Fatalf("references = %#v, error = %v", references, err)
	}
	completions, err := manager.Completion(context.Background(), query)
	if err != nil || len(completions.Items) != 1 || completions.Items[0].Label != "Println" {
		t.Fatalf("completions = %#v, error = %v", completions, err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		diagnostics, _ := manager.Diagnostics(context.Background(), query)
		if len(diagnostics) > 0 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if diagnostics, err := manager.Diagnostics(context.Background(), query); err != nil || len(diagnostics) != 1 || diagnostics[0].Message != "diagnostic" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestManagerDiagnosticsStartsServerAndPullsReport(t *testing.T) {
	workspace := t.TempDir()
	starter := &lspTestStarter{t: t, serve: func(t *testing.T, server *pipeServer) {
		serveInitialize(t, server)
		request := server.read(t)
		if request.Method != "textDocument/diagnostic" {
			t.Errorf("diagnostic method = %q", request.Method)
			return
		}
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": map[string]interface{}{
			"kind": "full", "items": []interface{}{map[string]interface{}{
				"message": "pulled", "range": map[string]interface{}{"start": map[string]int{}, "end": map[string]int{}},
			}},
		}})
	}}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls"}}, Starter: starter,
		Authorizer: &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	query := Query{Workspace: workspace, Language: "go", File: filepath.Join(workspace, "main.go")}
	diagnostics, err := manager.Diagnostics(nil, query)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Message != "pulled" || starter.count() != 1 {
		t.Fatalf("diagnostics = %#v, starts = %d, error = %v", diagnostics, starter.count(), err)
	}
}

func TestManagerAppliesDefaultCallTimeout(t *testing.T) {
	workspace := t.TempDir()
	starter := &lspTestStarter{t: t, serve: func(t *testing.T, server *pipeServer) {
		serveInitialize(t, server)
		_ = server.read(t)
	}}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls"}}, Starter: starter,
		Authorizer:  &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}},
		CallTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	query := Query{Workspace: workspace, Language: "go", File: filepath.Join(workspace, "main.go")}
	if _, err := manager.Hover(context.Background(), query); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hover error = %v", err)
	}
}

func TestManagerRestartsCrashedServerWithinLimit(t *testing.T) {
	workspace := t.TempDir()
	var generation int
	var mu sync.Mutex
	starter := &lspTestStarter{t: t, serve: func(t *testing.T, server *pipeServer) {
		mu.Lock()
		generation++
		current := generation
		mu.Unlock()
		serveInitialize(t, server)
		if current == 1 {
			_ = server.read(t)
			server.closeOutput()
			return
		}
		serveOneDefinition(t, server)
	}}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls"}}, Starter: starter,
		Authorizer:  &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}},
		MaxRestarts: 1, BaseBackoff: time.Millisecond, CallTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	query := Query{Workspace: workspace, Language: "go", File: filepath.Join(workspace, "main.go")}
	locations, err := manager.Definition(context.Background(), query)
	if err != nil || len(locations) != 1 || starter.count() != 2 {
		t.Fatalf("definition = %#v, starts = %d, error = %v", locations, starter.count(), err)
	}
}

func TestManagerStopsRestartingAfterConfiguredLimit(t *testing.T) {
	workspace := t.TempDir()
	starter := &lspTestStarter{t: t, serve: func(t *testing.T, server *pipeServer) {
		serveInitialize(t, server)
		_ = server.read(t)
		server.closeOutput()
	}}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls"}}, Starter: starter,
		Authorizer:  &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}},
		MaxRestarts: 1, BaseBackoff: time.Millisecond, CallTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	query := Query{Workspace: workspace, Language: "go", File: filepath.Join(workspace, "main.go")}
	_, _ = manager.Definition(context.Background(), query)
	if _, err := manager.Definition(context.Background(), query); !errors.Is(err, ErrRestartLimit) {
		t.Fatalf("restart limit error = %v", err)
	}
	if starter.count() != 2 {
		t.Fatalf("process starts = %d", starter.count())
	}
}

func TestManagerRejectsDeniedStartupAndFilesOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	starter := &lspTestStarter{t: t, serve: serveLSPQueries}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls"}}, Starter: starter,
		Authorizer: &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorDeny}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	query := Query{Workspace: workspace, Language: "go", File: filepath.Join(workspace, "main.go")}
	if _, err := manager.Hover(context.Background(), query); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("denied startup error = %v", err)
	}
	if starter.count() != 0 {
		t.Fatal("denied server process was started")
	}

	allowed, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls"}}, Starter: starter,
		Authorizer: &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer allowed.Close()
	query.File = filepath.Join(filepath.Dir(workspace), "outside.go")
	if _, err := allowed.Hover(context.Background(), query); !errors.Is(err, ErrWorkspaceBoundary) {
		t.Fatalf("outside workspace error = %v", err)
	}
}

func TestManagerSnapshotsServerConfiguration(t *testing.T) {
	workspace := t.TempDir()
	args := []string{"serve"}
	environment := map[string]string{"GOPLS_MODE": "before"}
	initialization := map[string]interface{}{"settings": []string{"before"}}
	observed := make(chan string, 1)
	starter := &lspTestStarter{t: t, serve: func(t *testing.T, server *pipeServer) {
		initialize := server.read(t)
		var params struct {
			InitializationOptions map[string]interface{} `json:"initializationOptions"`
		}
		if err := json.Unmarshal(initialize.Params, &params); err != nil {
			t.Errorf("decode initialize params: %v", err)
			return
		}
		observed <- params.InitializationOptions["settings"].([]interface{})[0].(string)
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": initialize.ID, "result": map[string]interface{}{"capabilities": map[string]interface{}{}}})
		_ = server.read(t)
		hover := server.read(t)
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": hover.ID, "result": map[string]interface{}{"contents": "hover"}})
		shutdown := server.read(t)
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": shutdown.ID, "result": nil})
		_ = server.read(t)
	}}
	manager, err := NewManager(ManagerOptions{
		Configs: []ServerConfig{{Language: "go", Command: "gopls", Args: args, Environment: environment, InitializationOptions: initialization}},
		Starter: starter, Authorizer: &lspTestAuthorizer{decision: permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	args[0] = "changed"
	environment["GOPLS_MODE"] = "changed"
	initialization["settings"].([]string)[0] = "changed"
	query := Query{Workspace: workspace, Language: "go", File: filepath.Join(workspace, "main.go")}
	if _, err := manager.Hover(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	config := starter.lastConfig()
	if config.Args[0] != "serve" || config.Environment["GOPLS_MODE"] != "before" || <-observed != "before" {
		t.Fatalf("starter config = %#v", config)
	}
}

func serveInitialize(t *testing.T, server *pipeServer) {
	request := server.read(t)
	if request.Method != "initialize" {
		t.Errorf("initialize method = %q", request.Method)
		return
	}
	server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": map[string]interface{}{"capabilities": map[string]interface{}{}}})
	initialized := server.read(t)
	if initialized.Method != "initialized" || initialized.ID != 0 {
		t.Errorf("initialized notification = %#v", initialized)
	}
}

func serveLSPQueries(t *testing.T, server *pipeServer) {
	serveInitialize(t, server)
	for {
		request := server.read(t)
		switch request.Method {
		case "textDocument/definition":
			server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": []interface{}{testLocation("file:///definition.go")}})
		case "textDocument/references":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(request.Params, &params)
			server.write(t, map[string]interface{}{
				"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
				"params": map[string]interface{}{"uri": params.TextDocument.URI, "diagnostics": []interface{}{map[string]interface{}{
					"message": "diagnostic", "range": map[string]interface{}{"start": map[string]int{}, "end": map[string]int{}},
				}}},
			})
			server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": []interface{}{testLocation("file:///reference.go")}})
		case "textDocument/completion":
			server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": map[string]interface{}{"isIncomplete": false, "items": []interface{}{map[string]string{"label": "Println"}}}})
		case "textDocument/hover":
			server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": map[string]interface{}{"contents": "hover"}})
		case "shutdown":
			server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": nil})
			return
		default:
			return
		}
	}
}

func serveOneDefinition(t *testing.T, server *pipeServer) {
	request := server.read(t)
	server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": []interface{}{testLocation("file:///definition.go")}})
}

func testLocation(uri string) map[string]interface{} {
	return map[string]interface{}{"uri": uri, "range": map[string]interface{}{"start": map[string]int{}, "end": map[string]int{}}}
}

type lspTestStarter struct {
	mu      sync.Mutex
	configs []ProcessConfig
	serve   func(*testing.T, *pipeServer)
	t       *testing.T
}

func (starter *lspTestStarter) Start(_ context.Context, config ProcessConfig) (Process, error) {
	starter.mu.Lock()
	starter.configs = append(starter.configs, config)
	serve := starter.serve
	t := starter.t
	starter.mu.Unlock()
	process, server := newPipeProcess()
	go serve(t, server)
	return process, nil
}

func (starter *lspTestStarter) count() int {
	starter.mu.Lock()
	defer starter.mu.Unlock()
	return len(starter.configs)
}

func (starter *lspTestStarter) lastConfig() ProcessConfig {
	starter.mu.Lock()
	defer starter.mu.Unlock()
	return starter.configs[len(starter.configs)-1]
}

type lspTestAuthorizer struct {
	mu       sync.Mutex
	decision permissions.Decision
	requests []permissions.Request
}

func (authorizer *lspTestAuthorizer) Decide(_ context.Context, request permissions.Request) (permissions.Decision, error) {
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	authorizer.requests = append(authorizer.requests, request)
	return authorizer.decision, nil
}
func (authorizer *lspTestAuthorizer) count() int {
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	return len(authorizer.requests)
}
func (authorizer *lspTestAuthorizer) lastRequest() permissions.Request {
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	return authorizer.requests[len(authorizer.requests)-1]
}
