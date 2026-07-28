package integration_test

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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cyber-code/internal/agent"
	"cyber-code/internal/cli"
	"cyber-code/internal/core"
	"cyber-code/internal/hooks"
	"cyber-code/internal/provider"
	runtimepkg "cyber-code/internal/runtime"
	"cyber-code/internal/session"
	"cyber-code/internal/tasks"
)

func TestCLIResumesPersistedConversation(t *testing.T) {
	var calls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"first answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		if !bytes.Contains(body, []byte("first question")) || !bytes.Contains(body, []byte("first answer")) || !bytes.Contains(body, []byte("second question")) {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(response, `{"error":{"message":"resumed history missing"}}`)
			return
		}
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"resumed answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()

	stateDir := t.TempDir()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: deepseek\npermission_mode: default\nprofiles:\n  deepseek:\n    provider: openai-compatible\n    base_url: %s\n    model: deepseek-v4-pro\n    api_key_env: INTEGRATION_RESUME_API_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_RESUME_API_KEY", "integration-secret")
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr, args, cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
		return code, stdout.String(), stderr.String()
	}
	if code, stdout, stderr := run("--print", "first question"); code != 0 || stdout != "first answer\n" || stderr != "" {
		t.Fatalf("first run: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	indexBytes, err := os.ReadFile(filepath.Join(stateDir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index map[string]json.RawMessage
	if err := json.Unmarshal(indexBytes, &index); err != nil || len(index) != 1 {
		t.Fatalf("session index=%s error=%v", indexBytes, err)
	}
	var sessionID string
	for id := range index {
		sessionID = id
	}
	if code, stdout, stderr := run("--print", "--resume", sessionID, "second question"); code != 0 || stdout != "resumed answer\n" || stderr != "" {
		t.Fatalf("resume: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestCLICompositionRunsConfiguredPromptHook(t *testing.T) {
	stateDir := t.TempDir()
	workspace := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hookCommand := fmt.Sprintf("\"%s\" -test.run=^TestHookHelperProcess$ -- hook-helper", strings.ReplaceAll(executable, "\"", "\\\""))
	hookConfig, err := json.Marshal(map[string][]string{"UserPromptSubmit": {hookCommand}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "hooks.json"), hookConfig, 0o600); err != nil {
		t.Fatal(err)
	}

	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if !bytes.Contains(body, []byte("context from configured hook")) {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(response, `{"error":{"message":"hook context missing"}}`)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"hook ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: INTEGRATION_HOOK_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_HOOK_KEY", "secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--permission-mode", "bypass", "--cwd", workspace, "original prompt"},
		cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
	if code != 0 || stdout.String() != "hook ok\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCLIConfiguredHookRequiresPermission(t *testing.T) {
	stateDir := t.TempDir()
	workspace := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hookCommand := fmt.Sprintf("\"%s\" -test.run=^TestHookHelperProcess$ -- hook-helper", strings.ReplaceAll(executable, "\"", "\\\""))
	hookConfig, _ := json.Marshal(map[string][]string{"UserPromptSubmit": {hookCommand}})
	if err := os.WriteFile(filepath.Join(stateDir, "hooks.json"), hookConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	var modelCalls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		modelCalls.Add(1)
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"unexpected\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer modelServer.Close()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: INTEGRATION_HOOK_PERMISSION_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_HOOK_PERMISSION_KEY", "secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--cwd", workspace, "prompt"}, cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
	if code == 0 || stdout.Len() != 0 || modelCalls.Load() != 0 || !strings.Contains(stderr.String(), "hook failed") {
		t.Fatalf("code=%d stdout=%q stderr=%q model_calls=%d", code, stdout.String(), stderr.String(), modelCalls.Load())
	}
}

func TestCLICompositionCompactsResumedConversation(t *testing.T) {
	var calls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		switch call {
		case 1:
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"first answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case 2:
			if !bytes.Contains(body, []byte("first question")) || !bytes.Contains(body, []byte("first answer")) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(response, `{"error":{"message":"messages to summarize missing"}}`)
				return
			}
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"compact summary\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case 3:
			if !bytes.Contains(body, []byte("compact summary")) || !bytes.Contains(body, []byte("second question")) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(response, `{"error":{"message":"compacted context missing"}}`)
				return
			}
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"compact ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Errorf("unexpected model call %d", call)
		}
	}))
	defer modelServer.Close()

	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "compact.json"), []byte(`{"threshold_tokens":1,"keep_recent_messages":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: INTEGRATION_COMPACT_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_COMPACT_KEY", "secret")
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr, args,
			cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
		return code, stdout.String(), stderr.String()
	}
	if code, stdout, stderr := run("--print", "first question"); code != 0 || stdout != "first answer\n" || stderr != "" {
		t.Fatalf("first run: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	indexBytes, err := os.ReadFile(filepath.Join(stateDir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index map[string]json.RawMessage
	if err := json.Unmarshal(indexBytes, &index); err != nil || len(index) != 1 {
		t.Fatalf("session index=%s error=%v", indexBytes, err)
	}
	var sessionID string
	for id := range index {
		sessionID = id
	}
	if code, stdout, stderr := run("--print", "--resume", sessionID, "second question"); code != 0 || stdout != "compact ok\n" || stderr != "" {
		t.Fatalf("second run: code=%d stdout=%q stderr=%q calls=%d", code, stdout, stderr, calls.Load())
	}
}

func TestCLICompositionRunsBoundedSubAgentTask(t *testing.T) {
	var calls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/event-stream")
		switch call {
		case 1:
			if !bytes.Contains(body, []byte("task_run")) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(response, `{"error":{"message":"task tool missing"}}`)
				return
			}
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"task-call\",\"function\":{\"name\":\"task_run\",\"arguments\":\"{\\\"prompt\\\":\\\"inspect child target\\\",\\\"max_turns\\\":1,\\\"permission_mode\\\":\\\"plan\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
		case 2:
			if !bytes.Contains(body, []byte("inspect child target")) || bytes.Contains(body, []byte(`"name":"task_run"`)) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(response, `{"error":{"message":"child request boundary invalid"}}`)
				return
			}
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"child result\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case 3:
			if !bytes.Contains(body, []byte("completed")) || !bytes.Contains(body, []byte("child result")) {
				response.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(response, `{"error":{"message":"child result missing"}}`)
				return
			}
			_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"task ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Errorf("unexpected model call %d", call)
		}
	}))
	defer modelServer.Close()

	stateDir := t.TempDir()
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	config := fmt.Sprintf("active_profile: test\npermission_mode: default\nprofiles:\n  test:\n    provider: openai-compatible\n    base_url: %s\n    model: test\n    api_key_env: INTEGRATION_TASK_KEY\n", modelServer.URL)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INTEGRATION_TASK_KEY", "secret")
	var stdout, stderr bytes.Buffer
	code := cli.ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--permission-mode", "bypass", "--max-turns", "2", "delegate"},
		cli.ExecuteOptions{ConfigFile: configFile, StateDir: stateDir})
	if code != 0 || stdout.String() != "task ok\n" || stderr.Len() != 0 || calls.Load() != 3 {
		t.Fatalf("code=%d stdout=%q stderr=%q calls=%d", code, stdout.String(), stderr.String(), calls.Load())
	}
	audit, err := os.ReadFile(filepath.Join(stateDir, "audit.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(audit, []byte("task_run")) || !bytes.Contains(audit, []byte("allow")) {
		t.Fatalf("audit record missing decision: %s", audit)
	}
	for _, forbidden := range []string{"delegate", "inspect child target", "child result"} {
		if bytes.Contains(audit, []byte(forbidden)) {
			t.Fatalf("audit leaked %q: %s", forbidden, audit)
		}
	}
}

func TestHookHelperProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "hook-helper" {
		return
	}
	var input hooks.HookInput
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil || input.EventName != hooks.HookEventUserPromptSubmit {
		os.Exit(2)
	}
	_, _ = fmt.Fprintln(os.Stdout, `{"continue":true,"additionalContext":"context from configured hook"}`)
	os.Exit(0)
}

func TestRuntimeHooksCompactSessionAndTaskLifecycle(t *testing.T) {
	hookRegistry := hooks.NewRegistry()
	hookRegistry.Register(hooks.HookEventUserPromptSubmit, func(_ context.Context, input hooks.HookInput) (hooks.HookOutput, error) {
		return hooks.HookOutput{Continue: true, UpdatedInput: map[string]any{"prompt": input.Prompt + " transformed"}}, nil
	})
	hookRunner, err := hooks.NewRunner(hooks.RunnerOptions{Registry: hookRegistry})
	if err != nil {
		t.Fatal(err)
	}
	compactor, err := session.NewCompactor(session.CompactOptions{ThresholdTokens: 1, KeepRecentMessages: 1, Summarize: func(context.Context, []core.Message) (string, error) {
		return "summary context", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	model := &integrationProvider{}
	store, err := session.NewStore(t.TempDir(), session.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := runtimepkg.NewPersistent(model, agent.Options{
		Model: "integration", MaxTurns: 2, Hooks: hookRunner, Compactor: compactor, SessionID: "integration-session",
		InitialHistory: []core.Message{
			{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "old question"}}},
			{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentText, Text: "old answer"}}},
		},
	}, store, "integration-session")
	if err != nil {
		t.Fatal(err)
	}
	for range runtime.Run(context.Background(), "new question") {
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(model.lastRequestText(), "summary context") || !strings.Contains(model.lastRequestText(), "new question transformed") {
		t.Fatalf("provider request=%s", model.lastRequestText())
	}
	if _, err := store.Resume(context.Background(), "integration-session"); err != nil {
		t.Fatalf("resume snapshot: %v", err)
	}

	registry := tasks.NewRegistry()
	task := tasks.CreateLocalShellTask("background", "wait", t.TempDir(), "integration")
	if err := registry.Register(task); err != nil {
		t.Fatal(err)
	}
	executor := tasks.NewExecutor(registry)
	exited := make(chan struct{})
	if err := executor.ExecuteLocalShell(context.Background(), task, func(ctx context.Context, _ *tasks.LocalShellTaskState) (*int, error) {
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if err := executor.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("background task was not reclaimed")
	}
}

type integrationProvider struct {
	request core.Request
}

func (*integrationProvider) Name() string { return "integration" }
func (*integrationProvider) Capabilities(context.Context) (provider.Capabilities, error) {
	return provider.Capabilities{Streaming: true, TokenCounting: true}, nil
}
func (model *integrationProvider) Stream(_ context.Context, request core.Request) (<-chan core.Event, error) {
	model.request = request
	events := make(chan core.Event, 2)
	events <- core.Event{Type: core.EventTextDelta, Text: "answer"}
	events <- core.Event{Type: core.EventCompleted}
	close(events)
	return events, nil
}
func (*integrationProvider) CountTokens(context.Context, core.Request) (int, error) { return 100, nil }
func (model *integrationProvider) lastRequestText() string {
	encoded, _ := json.Marshal(model.request.Messages)
	return string(encoded)
}
