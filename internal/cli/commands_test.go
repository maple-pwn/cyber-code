package cli

import (
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
	"strings"
	"testing"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/mcp"
	"cyber-code/internal/session"
)

func TestExecuteConfigSetGetListAndValidate(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CYBER_CODE_CONFIG", configFile)
	t.Setenv("CYBER_CODE_STATE_DIR", t.TempDir())

	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"config", "set", "permission_mode", "plan"}, "plan"},
		{[]string{"config", "get", "permission_mode"}, "plan"},
		{[]string{"config", "list"}, "anthropic"},
		{[]string{"config", "validate"}, "valid"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Execute(context.Background(), strings.NewReader(""), &stdout, &stderr, test.args); code != 0 {
			t.Fatalf("args %v: code = %d, stderr = %q", test.args, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), test.want) {
			t.Fatalf("args %v: stdout = %q", test.args, stdout.String())
		}
	}
	if info, err := os.Stat(configFile); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		t.Fatalf("config permissions = %v, error = %v", info, err)
	}
}

func TestExecuteConfigProfileSetCreatesCompleteProfile(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CYBER_CODE_CONFIG", configFile)
	t.Setenv("CYBER_CODE_STATE_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	args := []string{
		"config", "profile", "set", "deepseek",
		"--provider", "openai-compatible", "--base-url", "https://api.deepseek.com",
		"--model", "deepseek-v4-pro", "--api-key-env", "DEEPSEEK_API_KEY", "--activate",
	}
	if code := Execute(context.Background(), strings.NewReader(""), &stdout, &stderr, args); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	loaded, err := loadCommandConfig(configFile)
	if err != nil {
		t.Fatal(err)
	}
	profile := loaded.Profiles["deepseek"]
	if loaded.ActiveProfile != "deepseek" || profile.Provider != "openai-compatible" || profile.Model != "deepseek-v4-pro" || profile.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("config = %#v", loaded)
	}
}

func TestExecuteMCPAndPluginManagement(t *testing.T) {
	t.Setenv("CYBER_CODE_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("CYBER_CODE_STATE_DIR", t.TempDir())
	commands := []struct {
		args []string
		want string
	}{
		{[]string{"mcp", "add", "local", "--url", "https://example.test/mcp"}, "local"},
		{[]string{"mcp", "list"}, "local"},
		{[]string{"mcp", "test", "local"}, "valid"},
		{[]string{"plugins", "enable", "example"}, ""},
		{[]string{"plugins", "list"}, "example"},
		{[]string{"plugins", "disable", "example"}, ""},
		{[]string{"mcp", "remove", "local"}, ""},
	}
	for _, test := range commands {
		var stdout, stderr bytes.Buffer
		code := Execute(context.Background(), strings.NewReader(""), &stdout, &stderr, test.args)
		if code != 0 || (test.want != "" && !strings.Contains(stdout.String(), test.want)) {
			t.Fatalf("args %v: code = %d, stdout = %q, stderr = %q", test.args, code, stdout.String(), stderr.String())
		}
		if strings.Contains(strings.ToLower(stdout.String()+stderr.String()), "todo") || strings.Contains(strings.ToLower(stdout.String()+stderr.String()), "placeholder") {
			t.Fatalf("args %v emitted placeholder output", test.args)
		}
	}
}

func TestMCPDisableEnableAndStatusPersistLifecycle(t *testing.T) {
	stateDir := t.TempDir()
	var output bytes.Buffer
	for _, args := range [][]string{{"mcp", "add", "remote", "--url", "https://example.test/mcp"}, {"mcp", "disable", "remote"}, {"mcp", "status", "remote"}} {
		output.Reset()
		if code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &output, io.Discard, args, ExecuteOptions{StateDir: stateDir}); code != 0 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
	if !strings.Contains(output.String(), "disabled") {
		t.Fatalf("status=%q", output.String())
	}
	output.Reset()
	if code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &output, io.Discard, []string{"mcp", "enable", "remote"}, ExecuteOptions{StateDir: stateDir}); code != 0 {
		t.Fatalf("enable code=%d", code)
	}
	entries, err := loadMCPEntries(filepath.Join(stateDir, "mcp.json"))
	if err != nil || entries["remote"].Disabled {
		t.Fatalf("entry=%#v err=%v", entries["remote"], err)
	}
}

func TestMCPAuthSetReadsTokensFromEnvironment(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("MCP_ACCESS", "access-secret")
	t.Setenv("MCP_REFRESH", "refresh-secret")
	for _, args := range [][]string{{"mcp", "add", "remote", "--url", "https://example.test/mcp"}, {"mcp", "auth", "set", "remote", "--access-token-env", "MCP_ACCESS", "--refresh-token-env", "MCP_REFRESH", "--expires-at", "123"}} {
		if code := ExecuteWithOptions(context.Background(), strings.NewReader(""), io.Discard, io.Discard, args, ExecuteOptions{StateDir: stateDir}); code != 0 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
	store, err := mcp.NewCredentialStore(filepath.Join(stateDir, "mcp-credentials"))
	if err != nil {
		t.Fatal(err)
	}
	value, ok, err := store.Get(context.Background(), "remote")
	if err != nil || !ok || value.AccessToken != "access-secret" || value.RefreshToken != "refresh-secret" {
		t.Fatalf("value=%#v ok=%v err=%v", value, ok, err)
	}
	configBytes, _ := os.ReadFile(filepath.Join(stateDir, "mcp.json"))
	if bytes.Contains(configBytes, []byte("access-secret")) {
		t.Fatal("secret leaked into MCP config")
	}
}

func TestResolveMCPHeadersUsesStoredCredential(t *testing.T) {
	stateDir := t.TempDir()
	store, err := mcp.NewCredentialStore(filepath.Join(stateDir, "mcp-credentials"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "remote", mcp.Credential{AccessToken: "secret", TokenType: "Bearer"}); err != nil {
		t.Fatal(err)
	}
	headers, err := resolveMCPHeaders(context.Background(), stateDir, "remote", mcpEntry{})
	if err != nil || headers["Authorization"] != "Bearer secret" {
		t.Fatalf("headers=%#v err=%v", headers, err)
	}
}

func TestExecuteMCPAddStoresHeaderEnvironmentReferenceWithoutSecret(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CYBER_CODE_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("CYBER_CODE_STATE_DIR", stateDir)
	t.Setenv("MCP_AUTH_VALUE", "Bearer secret-token")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), strings.NewReader(""), &stdout, &stderr, []string{
		"mcp", "add", "private", "--url", "https://example.test/mcp", "--header-env", "Authorization=MCP_AUTH_VALUE",
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	encoded, err := os.ReadFile(filepath.Join(stateDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("secret-token")) {
		t.Fatalf("MCP state contains credential: %s", encoded)
	}
	entries, err := loadMCPEntries(filepath.Join(stateDir, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if entries["private"].HeaderEnv["Authorization"] != "MCP_AUTH_VALUE" {
		t.Fatalf("MCP entry = %#v", entries["private"])
	}
}

func TestExecuteSessionManagement(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CYBER_CODE_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("CYBER_CODE_STATE_DIR", stateDir)
	store, err := session.NewStore(filepath.Join(stateDir, "sessions"), session.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.Append(ctx, "session-one", core.Event{Type: core.EventCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, session.Snapshot{SessionID: "session-one", LastSequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err := recordSession(stateDir, sessionMetadata{ID: "session-one", Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"sessions", "list"}, "session-one"},
		{[]string{"sessions", "resume", "session-one"}, "session-one"},
		{[]string{"sessions", "export", "session-one"}, "session_id"},
		{[]string{"sessions", "delete", "session-one"}, ""},
	} {
		var stdout, stderr bytes.Buffer
		if code := Execute(ctx, strings.NewReader(""), &stdout, &stderr, test.args); code != 0 || (test.want != "" && !strings.Contains(stdout.String(), test.want)) {
			t.Fatalf("args %v: code = %d, stdout = %q, stderr = %q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestExecuteSessionExportRedactsConfiguredProviderCredential(t *testing.T) {
	stateDir := t.TempDir()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	secret := "unlabeled-provider-secret-value"
	t.Setenv("SESSION_EXPORT_API_KEY", secret)
	configText := "active_profile: test\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: https://example.test\n    model: test\n    api_key_env: SESSION_EXPORT_API_KEY\n"
	if err := os.WriteFile(configFile, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := session.NewStore(filepath.Join(stateDir, "sessions"), session.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), "secret-session", core.Event{Type: core.EventTextDelta, Text: "prefix " + secret + " suffix"}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"sessions", "export", "secret-session"}, ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), secret) || !strings.Contains(stdout.String(), "[REDACTED]") {
		t.Fatalf("export was not credential-redacted: %s", stdout.String())
	}
}

func TestExecuteSessionExportRedactsImplicitCloudCredentials(t *testing.T) {
	for _, test := range []struct {
		name, provider, environment string
	}{
		{name: "bedrock", provider: "bedrock", environment: "AWS_BEARER_TOKEN_BEDROCK"},
		{name: "azure", provider: "azure", environment: "ANTHROPIC_FOUNDRY_API_KEY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			configFile := filepath.Join(t.TempDir(), "config.yaml")
			secret := "implicit-" + test.name + "-credential"
			t.Setenv(test.environment, secret)
			configText := fmt.Sprintf("active_profile: cloud\nprofiles:\n  cloud:\n    provider: %s\n    base_url: https://example.test\n    model: test\n", test.provider)
			if err := os.WriteFile(configFile, []byte(configText), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := session.NewStore(filepath.Join(stateDir, "sessions"), session.StoreOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Append(context.Background(), "cloud-session", core.Event{Type: core.EventTextDelta, Text: secret}); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
				[]string{"sessions", "export", "cloud-session"}, ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
			if code != 0 || strings.Contains(stdout.String(), secret) || !strings.Contains(stdout.String(), "[REDACTED]") {
				t.Fatalf("code=%d stdout=%s stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecuteSessionDeletePreservesDataWhenIndexIsCorrupt(t *testing.T) {
	stateDir := t.TempDir()
	store, err := session.NewStore(filepath.Join(stateDir, "sessions"), session.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), "preserved-session", core.Event{Type: core.EventCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "sessions.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"sessions", "delete", "preserved-session"}, ExecuteOptions{ConfigFile: filepath.Join(t.TempDir(), "missing.yaml"), StateDir: stateDir})
	if code == 0 {
		t.Fatal("delete unexpectedly succeeded with a corrupt index")
	}
	records, err := store.Events(context.Background(), "preserved-session")
	if err != nil || len(records) != 1 {
		t.Fatalf("session data was lost: records=%#v error=%v", records, err)
	}
}

func TestExecuteSessionDeleteRejectsActiveSessionWithoutChangingState(t *testing.T) {
	stateDir := t.TempDir()
	store, err := session.NewStore(filepath.Join(stateDir, "sessions"), session.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), "active-session", core.Event{Type: core.EventCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := recordSession(stateDir, sessionMetadata{ID: "active-session"}); err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireLease("active-session")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"sessions", "delete", "active-session"}, ExecuteOptions{ConfigFile: filepath.Join(t.TempDir(), "missing.yaml"), StateDir: stateDir})
	if code == 0 || !strings.Contains(stderr.String(), "active") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	entries, err := loadSessionIndex(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := entries["active-session"]; !exists {
		t.Fatal("active session index was removed")
	}
}

func TestExecuteDoctorJSONHasStableSchema(t *testing.T) {
	t.Setenv("CYBER_CODE_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("CYBER_CODE_STATE_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), strings.NewReader(""), &stdout, &stderr, []string{"doctor", "--json"})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("output = %q: %v", stdout.String(), err)
	}
	checks, _ := report["checks"].([]any)
	if report["schema_version"] != float64(1) || len(checks) == 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestExecutePrintUsesInjectedCanonicalRunner(t *testing.T) {
	runner := &commandTestRunner{events: []core.Event{
		{Type: core.EventTextDelta, Text: "hello"},
		{Type: core.EventCompleted, FinishReason: "stop"},
	}}
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr, []string{"--print", "prompt"}, ExecuteOptions{Runner: runner})
	if code != 0 || stdout.String() != "hello\n" || stderr.Len() != 0 || runner.prompt != "prompt" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q, prompt = %q", code, stdout.String(), stderr.String(), runner.prompt)
	}
}

func TestExecutePrintComposesOpenAICompatibleRuntime(t *testing.T) {
	t.Setenv("TEST_DEEPSEEK_API_KEY", "test-secret")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("path = %q, authorization = %q", request.URL.Path, request.Header.Get("Authorization"))
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Model != "deepseek-v4-pro" {
			t.Errorf("payload = %#v, error = %v", payload, err)
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"deepseek ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	configText := fmt.Sprintf("active_profile: deepseek\npermission_mode: default\nprofiles:\n  deepseek:\n    provider: openai-compatible\n    base_url: %s\n    model: deepseek-v4-pro\n    api_key_env: TEST_DEEPSEEK_API_KEY\n", server.URL)
	if err := os.WriteFile(configFile, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr, []string{"--print", "hello"}, ExecuteOptions{
		ConfigFile: configFile, StateDir: t.TempDir(),
	})
	if code != 0 || stdout.String() != "deepseek ok\n" || stderr.Len() != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestExecutePrintDiscoversCyberCodeProjectConfig(t *testing.T) {
	t.Setenv("PROJECT_CONFIG_API_KEY", "test-secret")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Model != "project-model" {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(response, `{"error":{"message":"model was %s"}}`, payload.Model)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"project ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	userConfig := fmt.Sprintf("active_profile: local\nprofiles:\n  local:\n    provider: openai-compatible\n    base_url: %s\n    model: user-model\n    api_key_env: PROJECT_CONFIG_API_KEY\n", server.URL)
	if err := os.WriteFile(configFile, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".cyber-code.yaml"), []byte("profiles:\n  local:\n    model: project-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--cwd", workspace, "hello"}, ExecuteOptions{ConfigFile: configFile, StateDir: t.TempDir()})
	if code != 0 || stdout.String() != "project ok\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteMapsCompositionAuthenticationError(t *testing.T) {
	t.Setenv("MISSING_TEST_API_KEY", "")
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	configText := "active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: https://example.test\n    model: test\n    api_key_env: MISSING_TEST_API_KEY\n"
	if err := os.WriteFile(configFile, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr, []string{"--print", "hello"}, ExecuteOptions{
		ConfigFile: configFile, StateDir: t.TempDir(),
	})
	if code != 3 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

type commandTestRunner struct {
	prompt string
	events []core.Event
}

func (runner *commandTestRunner) Run(_ context.Context, prompt string) <-chan core.Event {
	runner.prompt = prompt
	events := make(chan core.Event, len(runner.events))
	for _, event := range runner.events {
		events <- event
	}
	close(events)
	return events
}
