package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-code-go/internal/core"
	"claude-code-go/internal/session"
)

func TestExecuteConfigSetGetListAndValidate(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CLAUDE_GO_CONFIG", configFile)
	t.Setenv("CLAUDE_GO_STATE_DIR", t.TempDir())

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
	if info, err := os.Stat(configFile); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config permissions = %v, error = %v", info, err)
	}
}

func TestExecuteConfigProfileSetCreatesCompleteProfile(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CLAUDE_GO_CONFIG", configFile)
	t.Setenv("CLAUDE_GO_STATE_DIR", t.TempDir())
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
	t.Setenv("CLAUDE_GO_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("CLAUDE_GO_STATE_DIR", t.TempDir())
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

func TestExecuteSessionManagement(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("CLAUDE_GO_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("CLAUDE_GO_STATE_DIR", stateDir)
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

func TestExecuteDoctorJSONHasStableSchema(t *testing.T) {
	t.Setenv("CLAUDE_GO_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("CLAUDE_GO_STATE_DIR", t.TempDir())
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
