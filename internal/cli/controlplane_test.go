package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configpkg "cyber-code/internal/config"
	"cyber-code/internal/controlplane"
	"cyber-code/internal/core"
	"cyber-code/internal/mcp"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tasks"
)

func TestControlPlaneTasksReportsLiveSnapshots(t *testing.T) {
	registry := testControlPlane(t, ControlActions{TaskSnapshots: func() []tasks.Snapshot {
		return []tasks.Snapshot{
			{ID: "task-a", Agent: "reviewer", Description: "review changes", Status: tasks.TaskStatusRunning, Usage: core.Usage{InputTokens: 10, OutputTokens: 3}, RecentTool: "read_file"},
			{ID: "task-b", Description: "run tests", Status: tasks.TaskStatusFailed, Usage: core.Usage{InputTokens: 4, OutputTokens: 2, CacheReadInputTokens: 1}, Truncated: true},
		}
	}})
	events, err := registry.Dispatch(context.Background(), "/tasks")
	text := controlEventText(events)
	for _, want := range []string{"task-a", "reviewer", "running", "review changes", "tokens: 13", "read_file", "task-b", "failed", "run tests", "tokens: 7", "truncated"} {
		if err != nil || !strings.Contains(text, want) {
			t.Fatalf("tasks missing %q: text=%q error=%v", want, text, err)
		}
	}
	if _, err := registry.Dispatch(context.Background(), "/tasks extra"); err == nil {
		t.Fatal("/tasks accepted arguments")
	}
}

func TestControlPlaneTasksReportsNone(t *testing.T) {
	registry := testControlPlane(t, ControlActions{TaskSnapshots: func() []tasks.Snapshot { return nil }})
	events, err := registry.Dispatch(context.Background(), "/tasks")
	if err != nil || controlEventText(events) != "tasks: none" {
		t.Fatalf("events=%#v error=%v", events, err)
	}
}

func TestRegisterGitCommandsDispatchesBoundedWorkflows(t *testing.T) {
	registry := controlplane.NewRegistry()
	service := &fakeGitWorkflow{diff: "diff output", review: "review output", commit: "commit output"}
	if err := registerGitCommands(registry, service); err != nil {
		t.Fatal(err)
	}
	for command, want := range map[string]string{
		"/diff":                 "diff output",
		"/review":               "review output",
		"/commit commit safely": "commit output",
	} {
		events, err := registry.Dispatch(context.Background(), command)
		if err != nil || len(events) == 0 || !strings.Contains(events[0].Text, want) {
			t.Fatalf("%s events = %#v, error = %v", command, events, err)
		}
	}
	if service.message != "commit safely" {
		t.Fatalf("commit message = %q", service.message)
	}
}

func TestControlPlaneInitCreatesCYBERInstructionsWithoutOverwrite(t *testing.T) {
	workspace := t.TempDir()
	actions := ControlActions{InitializeInstructions: newInstructionInitializer(workspace)}
	registry := testControlPlane(t, actions)

	events, err := registry.Dispatch(context.Background(), "/init")
	if err != nil || !strings.Contains(controlEventText(events), "CYBER.md") {
		t.Fatalf("init events = %#v, error = %v", events, err)
	}
	path := filepath.Join(workspace, "CYBER.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "# cyber-code project instructions") {
		t.Fatalf("CYBER.md content = %q", content)
	}
	if _, err := registry.Dispatch(context.Background(), "/init"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second /init error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(content) {
		t.Fatalf("existing CYBER.md was changed: content=%q error=%v", after, err)
	}
}

func TestInstructionInitializerRefusesSymlinkWithoutChangingTarget(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "CYBER.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := newInstructionInitializer(workspace)(context.Background()); err == nil {
		t.Fatal("initializer followed an existing CYBER.md symlink")
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "outside" {
		t.Fatalf("symlink target changed: content=%q error=%v", content, err)
	}
}

func TestControlPlaneUsageCostStatsClearAndVimActions(t *testing.T) {
	cleared := 0
	vimStates := []bool{}
	actions := ControlActions{
		UsageSnapshot: func() core.Usage {
			return core.Usage{InputTokens: 100, OutputTokens: 25, CacheReadInputTokens: 10, CacheCreationInputTokens: 5}
		},
		EstimateCost: func(usage core.Usage) (float64, error) {
			if usage.InputTokens != 100 {
				return 0, fmt.Errorf("unexpected usage")
			}
			return 0.0123, nil
		},
		HistoryCount: func() int { return 7 },
		ClearHistory: func(context.Context) error { cleared++; return nil },
		SetVimMode: func(enabled bool) error {
			vimStates = append(vimStates, enabled)
			return nil
		},
	}
	registry := testControlPlane(t, actions)

	cost, err := registry.Dispatch(context.Background(), "/cost")
	if err != nil || !strings.Contains(controlEventText(cost), "estimated cost: $0.0123") {
		t.Fatalf("cost events = %#v, error = %v", cost, err)
	}
	stats, err := registry.Dispatch(context.Background(), "/stats")
	statsText := controlEventText(stats)
	for _, want := range []string{"history messages: 7", "input tokens: 100", "output tokens: 25", "cache read tokens: 10", "cache creation tokens: 5"} {
		if err != nil || !strings.Contains(statsText, want) {
			t.Fatalf("stats missing %q: text=%q error=%v", want, statsText, err)
		}
	}
	if _, err := registry.Dispatch(context.Background(), "/clear now"); err == nil {
		t.Fatal("/clear accepted unexpected arguments")
	}
	if _, err := registry.Dispatch(context.Background(), "/clear"); err != nil || cleared != 1 {
		t.Fatalf("clear count = %d, error = %v", cleared, err)
	}
	for _, command := range []string{"/vim on", "/vim off", "/vim"} {
		if _, err := registry.Dispatch(context.Background(), command); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
	if got := fmt.Sprint(vimStates); got != "[true false true]" {
		t.Fatalf("vim states = %s", got)
	}
}

func TestControlPlaneConfigReportsEffectiveSettingsWithoutSecrets(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "never-print-this-secret")
	effective := configpkg.Config{
		ActiveProfile: "deepseek", PermissionMode: "default", SandboxMode: "best-effort",
		ContextWarningThreshold: 0.80, ContextCompactThreshold: 0.90,
		Profiles: map[string]configpkg.Profile{"deepseek": {
			Provider: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", APIKeyEnv: "DEEPSEEK_API_KEY",
		}},
	}
	registry := testControlPlane(t, ControlActions{EffectiveConfig: func() configpkg.Config { return effective }})
	events, err := registry.Dispatch(context.Background(), "/config")
	text := controlEventText(events)
	for _, want := range []string{"active profile: deepseek", "provider: openai", "model: deepseek-v4-pro", "credential env: DEEPSEEK_API_KEY", "context warning threshold: 0.80", "context compact threshold: 0.90", "cyber-code config"} {
		if err != nil || !strings.Contains(text, want) {
			t.Fatalf("config missing %q: text=%q error=%v", want, text, err)
		}
	}
	if strings.Contains(text, os.Getenv("DEEPSEEK_API_KEY")) {
		t.Fatalf("config output leaked credential: %q", text)
	}
}

func TestControlPlaneBugCreatesLocalReport(t *testing.T) {
	var description string
	registry := testControlPlane(t, ControlActions{CreateBugReport: func(_ context.Context, value string) (string, error) {
		description = value
		return "/tmp/cyber-code-bug.md", nil
	}})
	events, err := registry.Dispatch(context.Background(), "/bug streaming output stalls")
	if err != nil || description != "streaming output stalls" || !strings.Contains(controlEventText(events), "/tmp/cyber-code-bug.md") {
		t.Fatalf("events=%#v description=%q error=%v", events, description, err)
	}
}

func TestControlPlaneMCPAddPersistsAndConnectsHTTPServer(t *testing.T) {
	stateDir := t.TempDir()
	manager := &fakeMCPControl{}
	registry, err := buildControlPlane(nil, stateDir, "test", "test-model", permissions.PermissionModeDefault, nil, nil, nil, manager, nil, ControlActions{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := registry.Dispatch(context.Background(), `/mcp add docs --url "https://mcp.example.test/rpc"`)
	if err != nil {
		t.Fatal(err)
	}
	if text := controlEventText(events); !strings.Contains(text, "added and connected") {
		t.Fatalf("MCP add output = %q", text)
	}
	if len(manager.connected) != 1 || manager.connected[0].Name != "docs" || manager.connected[0].Transport != mcp.TransportHTTP || manager.connected[0].URL != "https://mcp.example.test/rpc" {
		t.Fatalf("connected configs = %#v", manager.connected)
	}
	entries, err := loadMCPEntries(filepath.Join(stateDir, "mcp.json"))
	if err != nil || entries["docs"].URL != "https://mcp.example.test/rpc" {
		t.Fatalf("persisted entries = %#v, error = %v", entries, err)
	}
}

func TestControlPlaneMCPAddSupportsQuotedStdioArgumentsAndReportsConnectionFailure(t *testing.T) {
	stateDir := t.TempDir()
	workspace := t.TempDir()
	manager := &fakeMCPControl{connectErr: fmt.Errorf("handshake unavailable")}
	registry, err := buildControlPlane(nil, stateDir, "test", "test-model", permissions.PermissionModeDefault, nil, nil, nil, manager, nil, ControlActions{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	events, err := registry.Dispatch(context.Background(), `/mcp add local --command npx --arg "-y" --arg "server package"`)
	if err != nil {
		t.Fatal(err)
	}
	text := controlEventText(events)
	if !strings.Contains(text, "configuration saved") || !strings.Contains(text, "handshake unavailable") {
		t.Fatalf("MCP failed connection output = %q", text)
	}
	if len(manager.connected) != 1 || manager.connected[0].Workspace != workspace || fmt.Sprint(manager.connected[0].Args) != "[-y server package]" {
		t.Fatalf("connected configs = %#v", manager.connected)
	}
	entries, loadErr := loadMCPEntries(filepath.Join(stateDir, "mcp.json"))
	if loadErr != nil || entries["local"].Command != "npx" || fmt.Sprint(entries["local"].Args) != "[-y server package]" {
		t.Fatalf("persisted entries = %#v, error = %v", entries, loadErr)
	}
}

func testControlPlane(t *testing.T, actions ControlActions) *controlplane.Registry {
	t.Helper()
	registry, err := buildControlPlane(nil, t.TempDir(), "test", "test-model", permissions.PermissionModeDefault, nil, nil, nil, nil, nil, actions)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func controlEventText(events []core.Event) string {
	var output strings.Builder
	for _, event := range events {
		output.WriteString(event.Text)
	}
	return output.String()
}

type fakeGitWorkflow struct {
	diff, review, commit string
	message              string
}

type fakeMCPControl struct {
	connected  []mcp.ServerConfig
	connectErr error
}

func (manager *fakeMCPControl) Connect(_ context.Context, config mcp.ServerConfig) error {
	manager.connected = append(manager.connected, config)
	return manager.connectErr
}

func (*fakeMCPControl) Reconnect(context.Context, string) error { return nil }
func (*fakeMCPControl) Disable(context.Context, string) error   { return nil }
func (*fakeMCPControl) Statuses() []mcp.ConnectionStatus        { return nil }

func (workflow *fakeGitWorkflow) Diff(context.Context) (string, error)   { return workflow.diff, nil }
func (workflow *fakeGitWorkflow) Review(context.Context) (string, error) { return workflow.review, nil }
func (workflow *fakeGitWorkflow) Commit(_ context.Context, message string) (string, error) {
	workflow.message = message
	return workflow.commit, nil
}
