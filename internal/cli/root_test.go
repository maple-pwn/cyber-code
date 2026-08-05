package cli

import (
	"bytes"
	"context"
	"errors"
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
	if uiFlag == nil || uiFlag.DefValue != "tactical" {
		t.Fatalf("--ui flag = %#v", uiFlag)
	}
	if sourceFlag == nil || sourceFlag.DefValue != "" {
		t.Fatalf("--source flag = %#v", sourceFlag)
	}
}

func TestValidateUISelectionUsesHonestTacticalSourceCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, ui, source string
		printMode        bool
		uiExplicit       bool
		wantError        bool
	}{
		{name: "classic", ui: "classic"},
		{name: "tactical default demo", ui: "tactical"},
		{name: "tactical explicit demo", ui: "tactical", source: "demo"},
		{name: "tactical local", ui: "tactical", source: "local"},
		{name: "unknown ui", ui: "movie", wantError: true},
		{name: "unknown tactical source", ui: "tactical", source: "remote", wantError: true},
		{name: "demo classic", ui: "classic", source: "demo", wantError: true},
		{name: "default print", ui: "tactical", printMode: true},
		{name: "explicit tactical print", ui: "tactical", printMode: true, uiExplicit: true, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateUISelection(test.ui, test.source, test.printMode, test.uiExplicit)
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
