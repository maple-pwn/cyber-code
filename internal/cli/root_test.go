package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	configpkg "cyber-code/internal/config"
	"cyber-code/internal/core"
	"cyber-code/internal/frontend"
	"cyber-code/internal/permissions"
	"cyber-code/internal/product"
	"cyber-code/internal/ui"
)

func TestRootCommandUsesCyberCodeBrand(t *testing.T) {
	command := newRootCommand(&commandEnvironment{
		ctx: context.Background(), stdin: strings.NewReader(""), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{},
		configFile: filepath.Join(t.TempDir(), "config.yaml"), stateDir: t.TempDir(),
	})
	if command.Use != "cyber-code [prompt]" {
		t.Fatalf("root command use = %q", command.Use)
	}
}

func TestRootCommandExposesAuthenticatedRuntimeServe(t *testing.T) {
	command := newRootCommand(&commandEnvironment{
		ctx: context.Background(), stdin: strings.NewReader(""), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{},
		configFile: filepath.Join(t.TempDir(), "config.yaml"), stateDir: t.TempDir(),
	})
	found, _, err := command.Find([]string{"runtime", "serve"})
	if err != nil || found == nil || found.CommandPath() != "cyber-code runtime serve" {
		t.Fatalf("runtime serve command = %#v, %v", found, err)
	}
}

func TestRuntimeServeRequiresEnvironmentBearer(t *testing.T) {
	t.Setenv(product.EnvRuntimeBearer, "")
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"runtime", "serve"}, ExecuteOptions{StateDir: t.TempDir()})
	if code != frontend.ExitConfiguration || !strings.Contains(stderr.String(), product.EnvRuntimeBearer) {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeServeUsesAuthenticatedInheritedStdioWithoutLeakingBearer(t *testing.T) {
	secret := "desktop-launch-secret"
	t.Setenv(product.EnvRuntimeBearer, secret)
	input := strings.NewReader(`{"id":"1","type":"health","bearer":"` + secret + `"}` + "\n")
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), input, &stdout, &stderr,
		[]string{"runtime", "serve"}, ExecuteOptions{StateDir: t.TempDir()})
	if code != frontend.ExitOK || !strings.Contains(stdout.String(), `"type":"health"`) || !strings.Contains(stdout.String(), `"ready":true`) {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("runtime output leaked bearer: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRootCommandExposesExplicitTacticalScenarioFlags(t *testing.T) {
	command := newRootCommand(&commandEnvironment{
		ctx: context.Background(), stdin: strings.NewReader(""), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{},
		configFile: filepath.Join(t.TempDir(), "config.yaml"), stateDir: t.TempDir(),
	})
	uiFlag := command.Flags().Lookup("ui")
	sourceFlag := command.Flags().Lookup("source")
	realSourcesFlag := command.Flags().Lookup("enable-real-sources")
	if uiFlag == nil || uiFlag.DefValue != "tactical" {
		t.Fatalf("--ui flag = %#v", uiFlag)
	}
	if sourceFlag == nil || sourceFlag.DefValue != "" {
		t.Fatalf("--source flag = %#v", sourceFlag)
	}
	if realSourcesFlag == nil || realSourcesFlag.DefValue != "false" {
		t.Fatalf("--enable-real-sources flag = %#v", realSourcesFlag)
	}
}

func TestRootCommandExposesExplicitSecurityRuntimeFlags(t *testing.T) {
	command := newRootCommand(&commandEnvironment{
		ctx: context.Background(), stdin: strings.NewReader(""), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{},
		configFile: filepath.Join(t.TempDir(), "config.yaml"), stateDir: t.TempDir(),
	})
	runtimeFlag := command.Flags().Lookup("runtime")
	locationFlag := command.Flags().Lookup("runtime-location")
	pathFlag := command.Flags().Lookup("cyber-agent-path")
	endpointFlag := command.Flags().Lookup("cyber-agent-url")
	if runtimeFlag == nil || runtimeFlag.DefValue != "coding" || locationFlag == nil || locationFlag.DefValue != "local" || pathFlag == nil || endpointFlag == nil {
		t.Fatalf("runtime flags: runtime=%#v location=%#v path=%#v endpoint=%#v", runtimeFlag, locationFlag, pathFlag, endpointFlag)
	}
}

func TestValidateSecurityRuntimeSelectionIsExplicit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, runtimeName, location, endpoint string
		wantError                             bool
	}{
		{name: "coding local", runtimeName: "coding", location: "local"},
		{name: "local cyber agent", runtimeName: "cyber-agent", location: "local"},
		{name: "remote cyber agent", runtimeName: "cyber-agent", location: "remote", endpoint: "https://agent.example.test"},
		{name: "unknown runtime", runtimeName: "automatic", location: "local", wantError: true},
		{name: "unknown location", runtimeName: "cyber-agent", location: "cluster", wantError: true},
		{name: "remote missing endpoint", runtimeName: "cyber-agent", location: "remote", wantError: true},
		{name: "coding remote", runtimeName: "coding", location: "remote", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateRuntimeSelection(test.runtimeName, test.location, test.endpoint)
			if (err != nil) != test.wantError {
				t.Fatalf("validateRuntimeSelection() error=%v wantError=%t", err, test.wantError)
			}
		})
	}
}

func TestValidateUISelectionUsesHonestTacticalSourceCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, ui, source string
		realSources      bool
		printMode        bool
		uiExplicit       bool
		wantError        bool
	}{
		{name: "classic", ui: "classic"},
		{name: "tactical default demo", ui: "tactical"},
		{name: "tactical explicit demo", ui: "tactical", source: "demo"},
		{name: "tactical local gated", ui: "tactical", source: "local", wantError: true},
		{name: "tactical local enabled", ui: "tactical", source: "local", realSources: true},
		{name: "unknown ui", ui: "movie", wantError: true},
		{name: "unknown tactical source", ui: "tactical", source: "remote", wantError: true},
		{name: "demo classic", ui: "classic", source: "demo", wantError: true},
		{name: "default print", ui: "tactical", printMode: true},
		{name: "explicit tactical print", ui: "tactical", printMode: true, uiExplicit: true, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateUISelection(test.ui, test.source, test.printMode, test.uiExplicit, test.realSources)
			if (err != nil) != test.wantError {
				t.Fatalf("validateUISelection() error = %v, wantError = %v", err, test.wantError)
			}
		})
	}
}

func TestRootPrintVerboseReportsProgressWithoutPollutingStdout(t *testing.T) {
	runner := &commandTestRunner{events: []core.Event{
		{Type: core.EventToolCall, ToolCall: &core.ToolCall{ID: "call-1", Name: "read_file"}},
		{Type: core.EventToolResult, ToolResult: &core.ToolResult{ToolCallID: "call-1"}},
		{Type: core.EventTextDelta, Text: "ok"},
		{Type: core.EventCompleted},
	}}
	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--verbose", "inspect"}, ExecuteOptions{Runner: runner})
	if code != 0 || stdout.String() != "ok\n" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "read_file started") || !strings.Contains(stderr.String(), "read_file succeeded") {
		t.Fatalf("verbose progress = %q", stderr.String())
	}
}

func TestRootPrintUsesCyberAgentRuntimeWithoutCodingFallback(t *testing.T) {
	t.Setenv("CYBER_AGENT_TOKEN", "print-token")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer print-token" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/v1/capabilities":
			_, _ = fmt.Fprint(response, `{"product":"cyber-agent","runtime_version":"0.1.0","protocol_version":1,"capabilities":["session.events.v1"]}`)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sessions":
			_, _ = fmt.Fprint(response, printSecuritySnapshot(0, ""))
		case request.URL.Path == "/v1/sessions/session-print":
			_, _ = fmt.Fprint(response, printSecuritySnapshot(2, "source-event-2"))
		case request.URL.Path == "/v1/sessions/session-print/events":
			response.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(response, "data: "+printSecuritySourceEvent(1, "session.created", `{"schema_version":1,"session_id":"session-print","task_id":"task-print","revision":1,"status":"active"}`)+"\n\n")
			_, _ = fmt.Fprint(response, "data: "+printSecuritySourceEvent(2, "session.terminal", `{"schema_version":1,"revision":2,"status":"completed"}`)+"\n\n")
		default:
			http.Error(response, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	code := ExecuteWithOptions(context.Background(), strings.NewReader(""), &stdout, &stderr,
		[]string{"--print", "--runtime", "cyber-agent", "--runtime-location", "remote", "--cyber-agent-url", server.URL, "Assess lab"},
		ExecuteOptions{StateDir: t.TempDir()})
	if code != frontend.ExitOK || !strings.Contains(stdout.String(), "[task.created]") || !strings.Contains(stdout.String(), "[task.completed]") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func printSecuritySnapshot(sequence int, eventID string) string {
	return fmt.Sprintf(`{"schema_version":1,"session_id":"session-print","task_id":"task-print","revision":1,"status":"active","harness_ref":"harness://network-assessment","harness_version":"1.0.0","skill_refs":[],"skill_versions":[],"skill_digests":[],"capability_lease":null,"working_plan":[],"child_run_refs":[],"graph_ref":null,"artifact_refs":[],"event_cursor":{"session_id":"session-print","sequence":%d,"event_id":%q},"unified_state_ref":null,"unified_state_version":null,"pending_interaction":null,"pending_action":null,"pending_action_state":null,"in_flight_action_ref":null,"pending_subagent":null,"boundary_approvals":[],"resume_turn":null,"finish_confirmation_pending":false,"interaction_outcomes":[],"memory_summary":null,"conversation_history":[],"raw_tool_outputs":[],"last_response":null,"actions_used":0,"llm_calls_used":0,"tool_calls_used":0,"updated_at":"2026-08-09T12:00:00Z"}`, sequence, eventID)
}

func printSecuritySourceEvent(sequence int, topic, payload string) string {
	return fmt.Sprintf(`{"event_id":"source-event-%d","task_id":"task-print","session_id":"session-print","sequence":%d,"topic":%q,"payload":%s,"emitted_by":"system","emitted_at":"2026-08-09T12:00:00Z","causation_id":null}`, sequence, sequence, topic, payload)
}

func TestCyberCodeConfigurationNamespaceHardCut(t *testing.T) {
	t.Setenv("CYBER_CODE_CONFIG", "/new/config.yaml")
	t.Setenv("CYBER_CODE_STATE_DIR", "/new/state")
	t.Setenv("CLAUDE_GO_CONFIG", "/legacy/config.yaml")
	t.Setenv("CLAUDE_GO_STATE_DIR", "/legacy/state")
	if got := resolveConfigFile(""); got != "/new/config.yaml" {
		t.Fatalf("config path = %q", got)
	}
	if got := resolveStateDir(""); got != "/new/state" {
		t.Fatalf("state path = %q", got)
	}

	t.Setenv("CYBER_CODE_CONFIG", "")
	t.Setenv("CYBER_CODE_STATE_DIR", "")
	if got := resolveConfigFile(""); got == "/legacy/config.yaml" || !strings.Contains(got, "cyber-code") {
		t.Fatalf("default config path used legacy namespace: %q", got)
	}
	if got := resolveStateDir(""); got == "/legacy/state" || !strings.Contains(got, "cyber-code") {
		t.Fatalf("default state path used legacy namespace: %q", got)
	}
}

func TestBuildProviderSupportsCloudProfiles(t *testing.T) {
	t.Setenv("CLI_CLOUD_PROVIDER_KEY", "test-token")
	for _, name := range []string{"bedrock", "vertex", "azure"} {
		t.Run(name, func(t *testing.T) {
			built, err := buildProvider(configpkg.Profile{
				Provider: name, BaseURL: "https://example.test", Model: "model", APIKeyEnv: "CLI_CLOUD_PROVIDER_KEY",
			})
			if err != nil {
				t.Fatalf("build %s provider: %v", name, err)
			}
			if built.Name() != name {
				t.Fatalf("provider name = %q", built.Name())
			}
		})
	}
}

func TestBuildProviderSupportsManagedCloudProfiles(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	for _, name := range []string{"bedrock", "vertex", "azure"} {
		t.Run(name, func(t *testing.T) {
			built, err := buildProvider(configpkg.Profile{
				Provider: name, BaseURL: "https://example.test", Model: "model",
			})
			if err != nil {
				t.Fatalf("build managed %s provider: %v", name, err)
			}
			if built.Name() != name {
				t.Fatalf("provider name = %q", built.Name())
			}
		})
	}
}

func TestResolvePluginRootRejectsTraversalAndEscapingSymlink(t *testing.T) {
	stateDir := t.TempDir()
	pluginsDir := filepath.Join(stateDir, "plugins")
	validDir := filepath.Join(pluginsDir, "valid")
	if err := os.MkdirAll(validDir, 0o700); err != nil {
		t.Fatal(err)
	}
	resolvedValid, err := filepath.EvalSymlinks(validDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolvePluginRoot(stateDir, "valid"); err != nil || got != resolvedValid {
		t.Fatalf("valid plugin root = %q, error = %v", got, err)
	}
	for _, name := range []string{"../outside", "a/b", ".", "..", ""} {
		if _, err := resolvePluginRoot(stateDir, name); err == nil {
			t.Fatalf("unsafe plugin name %q was accepted", name)
		}
	}

	outside := t.TempDir()
	link := filepath.Join(pluginsDir, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := resolvePluginRoot(stateDir, "linked"); err == nil {
		t.Fatal("plugin symlink escaping root was accepted")
	}
}

func TestBootstrapConfirmerAllowsExplicitYesAndRedactsRequestDetails(t *testing.T) {
	input := strings.NewReader("yes\n")
	var output bytes.Buffer
	confirm := newBootstrapConfirmer(input, &output)
	decision, err := confirm(context.Background(), permissions.Request{
		Tool: "mcp", Action: permissions.ActionNetwork,
		Command: "curl -H 'Authorization: Bearer secret-token'", Paths: []string{"/private/file"}, Network: []string{"secret.example"},
	})
	if err != nil || decision.Behavior != permissions.PermissionBehaviorAllow {
		t.Fatalf("decision = %#v, error = %v", decision, err)
	}
	text := output.String()
	for _, secret := range []string{"secret-token", "/private/file", "Authorization"} {
		if strings.Contains(text, secret) {
			t.Fatalf("bootstrap prompt leaked %q: %q", secret, text)
		}
	}
	if !strings.Contains(text, "mcp") || !strings.Contains(text, "network") || !strings.Contains(text, "secret.example") {
		t.Fatalf("bootstrap prompt lacks actionable context: %q", text)
	}
}

func TestBootstrapConfirmerShowsExecutableWithoutArguments(t *testing.T) {
	var output bytes.Buffer
	confirm := newBootstrapConfirmer(strings.NewReader("no\n"), &output)
	_, _ = confirm(context.Background(), permissions.Request{
		Tool: "plugin.example", Action: permissions.ActionExecute,
		Command: "/usr/local/bin/plugin-helper --token secret-value",
	})
	text := output.String()
	if !strings.Contains(text, "plugin-helper") || strings.Contains(text, "secret-value") || strings.Contains(text, "--token") {
		t.Fatalf("unsafe or uninformative process prompt: %q", text)
	}
}

func TestBootstrapConfirmerDefaultsToDenyAndHonorsCancellation(t *testing.T) {
	confirm := newBootstrapConfirmer(strings.NewReader("no\n"), &bytes.Buffer{})
	decision, err := confirm(context.Background(), permissions.Request{Tool: "plugin", Action: permissions.ActionExecute})
	if err != nil || decision.Behavior != permissions.PermissionBehaviorDeny {
		t.Fatalf("deny decision = %#v, error = %v", decision, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := confirm(canceled, permissions.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled confirmation error = %v", err)
	}
}

func TestInteractiveConfirmerUsesBootstrapBeforeAttachAndBridgeAfter(t *testing.T) {
	bridge := ui.NewPermissionBridge()
	bootstrapCalls := 0
	confirm := newInteractiveConfirmer(bridge, func(context.Context, permissions.Request) (permissions.Decision, error) {
		bootstrapCalls++
		return permissions.Decision{Behavior: permissions.PermissionBehaviorAllow}, nil
	})
	request := permissions.Request{Tool: "mcp.connect.local", Action: permissions.ActionNetwork}
	decision, err := confirm(context.Background(), request)
	if err != nil || decision.Behavior != permissions.PermissionBehaviorAllow || bootstrapCalls != 1 {
		t.Fatalf("bootstrap decision = %#v, calls = %d, error = %v", decision, bootstrapCalls, err)
	}
	bridge.Attach(func(message tea.Msg) {
		prompt := message.(ui.PermissionRequestMsg)
		prompt.Respond <- permissions.Decision{Behavior: permissions.PermissionBehaviorDeny}
	})
	decision, err = confirm(context.Background(), request)
	if err != nil || decision.Behavior != permissions.PermissionBehaviorDeny || bootstrapCalls != 1 {
		t.Fatalf("bridge decision = %#v, calls = %d, error = %v", decision, bootstrapCalls, err)
	}
}
