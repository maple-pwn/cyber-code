package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"cyber-code/internal/cli"
	"cyber-code/internal/lsp"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
)

func TestCLICompositionLoadsConfiguredMCPTool(t *testing.T) {
	var mcpCalled atomic.Bool
	mcpServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var rpc struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			t.Errorf("decode MCP request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		var result any
		switch rpc.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}}, "serverInfo": map[string]string{"name": "local", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "echo text", "inputSchema": map[string]any{"type": "object"}}}}
		case "resources/list":
			result = map[string]any{"resources": []any{map[string]any{"uri": "memory://status", "name": "status"}}}
		case "tools/call":
			mcpCalled.Store(true)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "echoed"}}}
		default:
			t.Errorf("unexpected MCP method %q", rpc.Method)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result})
	}))
	defer mcpServer.Close()

	var modelCalls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := modelCalls.Add(1)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"mcp__local__echo\",\"arguments\":\"{\\\"text\\\":\\\"hi\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		if !mcpCalled.Load() {
			response.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(response, `{"error":{"message":"MCP tool was not called"}}`)
			return
		}
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"mcp ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()

	stateDir := t.TempDir()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: deepseek\npermission_mode: default\nprofiles:\n  deepseek:\n    provider: openai-compatible\n    base_url: %s\n    model: deepseek-v4-pro\n    api_key_env: INTEGRATION_API_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	mcpConfig := fmt.Sprintf("{\"local\":{\"name\":\"local\",\"url\":%q}}\n", mcpServer.URL)
	if err := os.WriteFile(filepath.Join(stateDir, "mcp.json"), []byte(mcpConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_API_KEY", "integration-secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--permission-mode", "bypass", "use MCP"},
		cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir},
	)
	if code != 0 || stdout.String() != "mcp ok\n" || stderr.Len() != 0 || !mcpCalled.Load() {
		t.Fatalf("code=%d stdout=%q stderr=%q mcp_called=%t model_calls=%d", code, stdout.String(), stderr.String(), mcpCalled.Load(), modelCalls.Load())
	}
}

func TestCLICompositionReadsAdvertisedMCPResource(t *testing.T) {
	var resourceRead atomic.Bool
	mcpServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer integration-mcp-token" {
			t.Fatalf("MCP authorization header = %q", request.Header.Get("Authorization"))
		}
		var rpc struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			t.Fatal(err)
		}
		var result any
		switch rpc.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"resources": map[string]any{}}, "serverInfo": map[string]string{"name": "docs", "version": "1"}}
		case "resources/list":
			result = map[string]any{"resources": []any{map[string]any{"uri": "memory://guide", "name": "guide"}}}
		case "resources/read":
			resourceRead.Store(true)
			result = map[string]any{"contents": []any{map[string]any{"uri": "memory://guide", "text": "resource body"}}}
		default:
			t.Fatalf("unexpected MCP method %q", rpc.Method)
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result})
	}))
	defer mcpServer.Close()

	var modelCalls atomic.Int32
	var secondRequest atomic.Value
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := modelCalls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			if !bytes.Contains(body, []byte("mcp_read_resource")) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(response, `{"error":{"message":"resource tool missing"}}`)
				return
			}
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"resource-1\",\"function\":{\"name\":\"mcp_read_resource\",\"arguments\":\"{\\\"server\\\":\\\"docs\\\",\\\"uri\\\":\\\"memory://guide\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		secondRequest.Store(string(body))
		if !bytes.Contains(body, []byte("resource body")) {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(response, `{"error":{"message":"resource body missing"}}`)
			return
		}
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"resource ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()

	stateDir := t.TempDir()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: INTEGRATION_RESOURCE_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "mcp.json"), []byte(fmt.Sprintf(`{"docs":{"name":"docs","url":%q,"header_env":{"Authorization":"INTEGRATION_MCP_AUTH"}}}`, mcpServer.URL)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_RESOURCE_KEY", "secret")
	t.Setenv("INTEGRATION_MCP_AUTH", "Bearer integration-mcp-token")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--permission-mode", "bypass", "read resource"},
		cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
	if code != 0 || stdout.String() != "resource ok\n" || stderr.Len() != 0 || !resourceRead.Load() {
		request, _ := secondRequest.Load().(string)
		t.Fatalf("code=%d stdout=%q stderr=%q resource_read=%t calls=%d second_request=%s", code, stdout.String(), stderr.String(), resourceRead.Load(), modelCalls.Load(), request)
	}
}

func TestCLICompositionDiscoversAndLoadsSkills(t *testing.T) {
	workspace := t.TempDir()
	stateDir := t.TempDir()
	projectSkill := filepath.Join(workspace, ".cyber-code", "skills", "project-review")
	userSkill := filepath.Join(stateDir, "skills", "user-debug")
	for _, directory := range []string{projectSkill, userSkill} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectSkill, "SKILL.md"), []byte("PROJECT SECRET INSTRUCTIONS"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("USER DEBUG INSTRUCTIONS"), 0o600); err != nil {
		t.Fatal(err)
	}

	var modelCalls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := modelCalls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			for _, expected := range []string{"load_skill", "project-review", "user-debug"} {
				if !bytes.Contains(body, []byte(expected)) {
					response.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprintf(response, `{"error":{"message":"missing %s"}}`, expected)
					return
				}
			}
			if bytes.Contains(body, []byte("PROJECT SECRET INSTRUCTIONS")) || bytes.Contains(body, []byte("USER DEBUG INSTRUCTIONS")) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(response, `{"error":{"message":"skill instructions were eagerly injected"}}`)
				return
			}
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"skill-1\",\"function\":{\"name\":\"load_skill\",\"arguments\":\"{\\\"name\\\":\\\"user-debug\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		if !bytes.Contains(body, []byte("USER DEBUG INSTRUCTIONS")) {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(response, `{"error":{"message":"loaded skill instructions missing"}}`)
			return
		}
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"skill ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: INTEGRATION_SKILL_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_SKILL_KEY", "secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--permission-mode", "bypass", "--cwd", workspace, "load a skill"},
		cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
	if code != 0 || stdout.String() != "skill ok\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q model_calls=%d", code, stdout.String(), stderr.String(), modelCalls.Load())
	}
}

func TestLSPToolUsesCanonicalRegistryAndPermissionBoundary(t *testing.T) {
	workspace := t.TempDir()
	file := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(file, []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	if err := lsp.RegisterTools(registry, integrationLSP{}); err != nil {
		t.Fatal(err)
	}
	broker, err := permissions.NewBroker(permissions.Options{Mode: permissions.PermissionModeDefault, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	runner := tool.NewRunner(registry, broker, tool.RunnerOptions{})
	arguments, _ := json.Marshal(map[string]any{"workspace": workspace, "language": "go", "file": file, "line": 0, "character": 1})
	result, err := runner.Run(context.Background(), "lsp_hover", arguments)
	if err != nil || len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "hover text") {
		t.Fatalf("LSP result=%#v error=%v", result, err)
	}
}

func TestCLICompositionAdvertisesConfiguredLSPToolsWithoutStartingServer(t *testing.T) {
	stateDir := t.TempDir()
	workspace := t.TempDir()
	file := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lspConfig := `{"go":{"command":"definitely-missing-cyber-code-lsp","args":["serve"]}}`
	if err := os.WriteFile(filepath.Join(stateDir, "lsp.json"), []byte(lspConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		for _, name := range []string{"lsp_completion", "lsp_definition", "lsp_diagnostics", "lsp_hover", "lsp_references"} {
			if !bytes.Contains(body, []byte(name)) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(response, `{"error":{"message":"missing %s"}}`, name)
				return
			}
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"lsp tools ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: INTEGRATION_LSP_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_LSP_KEY", "secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--permission-mode", "bypass", "--cwd", workspace, "inspect tools"},
		cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
	if code != 0 || stdout.String() != "lsp tools ok\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCLICompositionExecutesConfiguredLSPQuery(t *testing.T) {
	stateDir := t.TempDir()
	workspace := t.TempDir()
	file := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	lspConfig := fmt.Sprintf(`{"go":{"command":%q,"args":["-test.run=^TestLSPHelperProcess$","--","lsp-helper"]}}`, executable)
	if err := os.WriteFile(filepath.Join(stateDir, "lsp.json"), []byte(lspConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			arguments, _ := json.Marshal(map[string]any{
				"workspace": workspace, "language": "go", "file": file, "line": 0, "character": 1,
			})
			_, _ = fmt.Fprintf(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"lsp-call\",\"function\":{\"name\":\"lsp_hover\",\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", string(arguments))
			return
		}
		if !bytes.Contains(body, []byte("hover from cli lsp")) {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(response, `{"error":{"message":"LSP hover result missing"}}`)
			return
		}
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"lsp query ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: INTEGRATION_LSP_QUERY_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_LSP_QUERY_KEY", "secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--permission-mode", "bypass", "--cwd", workspace, "query LSP"},
		cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
	if code != 0 || stdout.String() != "lsp query ok\n" || stderr.Len() != 0 || calls.Load() != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q calls=%d", code, stdout.String(), stderr.String(), calls.Load())
	}
}

func TestLSPHelperProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "lsp-helper" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		length := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				os.Exit(2)
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if value, found := strings.CutPrefix(strings.ToLower(line), "content-length:"); found {
				length, err = strconv.Atoi(strings.TrimSpace(value))
				if err != nil {
					os.Exit(2)
				}
			}
		}
		if length <= 0 {
			os.Exit(2)
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			os.Exit(2)
		}
		var request struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(payload, &request) != nil {
			os.Exit(2)
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"capabilities": map[string]any{}}
		case "initialized":
			continue
		case "textDocument/hover":
			result = map[string]any{"contents": map[string]string{"kind": "plaintext", "value": "hover from cli lsp"}}
		case "shutdown":
			result = nil
		case "exit":
			os.Exit(0)
		default:
			os.Exit(2)
		}
		encoded, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(encoded), encoded)
	}
}

type integrationLSP struct{}

func (integrationLSP) Definition(context.Context, lsp.Query) ([]lsp.Location, error) {
	return nil, nil
}
func (integrationLSP) References(context.Context, lsp.Query, bool) ([]lsp.Location, error) {
	return nil, nil
}
func (integrationLSP) Completion(context.Context, lsp.Query) (lsp.CompletionList, error) {
	return lsp.CompletionList{}, nil
}
func (integrationLSP) Hover(context.Context, lsp.Query) (*lsp.Hover, error) {
	return &lsp.Hover{Contents: json.RawMessage(`{"kind":"plaintext","value":"hover text"}`)}, nil
}
func (integrationLSP) Diagnostics(context.Context, lsp.Query) ([]lsp.Diagnostic, error) {
	return nil, nil
}

func TestCLICompositionLoadsEnabledPluginTool(t *testing.T) {
	stateDir := t.TempDir()
	pluginRoot := filepath.Join(stateDir, "plugins", "example")
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	entrypoint := "plugin-helper"
	if runtime.GOOS == "windows" {
		entrypoint += ".exe"
	}
	copyFile(t, executable, filepath.Join(pluginRoot, entrypoint), 0o700)
	manifest := fmt.Sprintf(`{"name":"example","version":"1","entrypoint":%q,"args":["-test.run=^TestPluginHelperProcess$","plugin-helper"],"capabilities":{"process":true,"tools":[{"name":"echo","action":"execute"}]},"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}`+"\n", entrypoint)
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plugins.json"), []byte(`{"example":{"name":"example","enabled":true}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var modelCalls atomic.Int32
	var lastModelRequest atomic.Value
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := modelCalls.Add(1)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"plugin-call\",\"function\":{\"name\":\"plugin__example__echo\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		body, _ := io.ReadAll(request.Body)
		lastModelRequest.Store(string(body))
		if !bytes.Contains(body, []byte("plugin echoed")) {
			response.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(response, `{"error":{"message":"plugin result missing"}}`)
			return
		}
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"plugin ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: deepseek\npermission_mode: default\nprofiles:\n  deepseek:\n    provider: openai-compatible\n    base_url: %s\n    model: deepseek-v4-pro\n    api_key_env: INTEGRATION_PLUGIN_API_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_PLUGIN_API_KEY", "integration-secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--permission-mode", "bypass", "use plugin"},
		cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir},
	)
	if code != 0 || stdout.String() != "plugin ok\n" || stderr.Len() != 0 {
		lastRequest, _ := lastModelRequest.Load().(string)
		t.Fatalf("code=%d stdout=%q stderr=%q model_calls=%d last_request=%s", code, stdout.String(), stderr.String(), modelCalls.Load(), lastRequest)
	}
}

func TestPluginHelperProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "plugin-helper" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int64  `json:"id"`
			Method  string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return
		}
		result := map[string]any{}
		if request.Method == "tools/call" {
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "plugin echoed"}}}
		}
		encoded, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		_, _ = fmt.Fprintln(os.Stdout, string(encoded))
	}
}

func copyFile(t *testing.T, source, destination string, mode os.FileMode) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
