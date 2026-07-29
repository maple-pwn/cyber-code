package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	configpkg "cyber-code/internal/config"
	"cyber-code/internal/contextbuilder"
	"cyber-code/internal/controlplane"
	"cyber-code/internal/core"
	"cyber-code/internal/gitworkflow"
	"cyber-code/internal/hooks"
	"cyber-code/internal/mcp"
	"cyber-code/internal/memory"
	"cyber-code/internal/permissions"
	runtimepkg "cyber-code/internal/runtime"
	"cyber-code/internal/skill"
)

type gitWorkflow interface {
	Diff(context.Context) (string, error)
	Review(context.Context) (string, error)
	Commit(context.Context, string) (string, error)
}

type mcpControl interface {
	Connect(context.Context, mcp.ServerConfig) error
	Reconnect(context.Context, string) error
	Disable(context.Context, string) error
	Statuses() []mcp.ConnectionStatus
}

// ControlActions isolates slash commands from Runtime, UI, and configuration
// implementations while keeping those components authoritative for state.
type ControlActions struct {
	Workspace              string
	InitializeInstructions func(context.Context) (string, error)
	EffectiveConfig        func() configpkg.Config
	UsageSnapshot          func() core.Usage
	EstimateCost           func(core.Usage) (float64, error)
	HistoryCount           func() int
	ClearHistory           func(context.Context) error
	SetVimMode             func(bool) error
	CreateBugReport        func(context.Context, string) (string, error)
}

func buildControlPlane(runtime *runtimepkg.Runtime, stateDir, profileName, model string, mode permissions.PermissionMode, hooksRunner *hooks.Runner, skills []skill.Skill, contextBuilder *contextbuilder.Builder, mcpManager mcpControl, git *gitworkflow.Service, actions ControlActions) (*controlplane.Registry, error) {
	registry := controlplane.NewRegistry()
	memoryStore, err := memory.NewStore(filepath.Join(stateDir, "memory"), memory.Options{})
	if err != nil {
		return nil, err
	}
	register := func(spec controlplane.Spec) error { return registry.Register(spec) }
	if err := registerGitCommands(registry, git); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "help", Aliases: []string{"h"}, Usage: "/help", Description: "list available commands", Handler: registry.Help}); err != nil {
		return nil, err
	}
	if err := registerProductCommands(registry, actions); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "status", Usage: "/status", Description: "show runtime status", Handler: func(_ context.Context, _ controlplane.Invocation) ([]core.Event, error) {
		return controlplane.TextEvents(fmt.Sprintf("cyber-code\nmodel: %s\nsession: %s\nhistory messages: %d", model, runtimeSessionID(runtime), len(runtime.History()))), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "model", Usage: "/model [name]", Description: "show the active model", Handler: func(_ context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
		if len(invocation.Args) > 0 {
			return nil, fmt.Errorf("model changes require a new turn with --model; active model is %s", model)
		}
		return controlplane.TextEvents(model), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "permissions", Usage: "/permissions", Description: "show the permission mode", Handler: func(_ context.Context, _ controlplane.Invocation) ([]core.Event, error) {
		return controlplane.TextEvents(string(mode)), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "hooks", Usage: "/hooks", Description: "show hook status", Handler: func(_ context.Context, _ controlplane.Invocation) ([]core.Event, error) {
		if hooksRunner == nil {
			return controlplane.TextEvents("hooks: disabled"), nil
		}
		return controlplane.TextEvents("hooks: enabled"), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "skills", Usage: "/skills", Description: "list available skills", Handler: func(_ context.Context, _ controlplane.Invocation) ([]core.Event, error) {
		names := make([]string, len(skills))
		for index := range skills {
			names[index] = skills[index].Name
		}
		sort.Strings(names)
		if len(names) == 0 {
			return controlplane.TextEvents("skills: none"), nil
		}
		return controlplane.TextEvents(strings.Join(names, "\n")), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "tasks", Usage: "/tasks", Description: "show task service status", Handler: func(_ context.Context, _ controlplane.Invocation) ([]core.Event, error) {
		return controlplane.TextEvents("tasks: enabled"), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "mcp", Usage: "/mcp status|add NAME (--url URL|--command CMD [--arg ARG])|reconnect NAME|disable NAME", Description: "manage runtime MCP connections", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
		if mcpManager == nil {
			return controlplane.TextEvents("mcp: unavailable"), nil
		}
		operation := "status"
		if len(invocation.Args) > 0 {
			operation = invocation.Args[0]
		}
		switch operation {
		case "add":
			entry, config, err := parseMCPControlAdd(invocation.Args, actions.Workspace)
			if err != nil {
				return nil, err
			}
			path := filepath.Join(stateDir, "mcp.json")
			if err := withStateFileLock(path, func() error {
				entries, err := loadMCPEntries(path)
				if err != nil {
					return err
				}
				if _, exists := entries[entry.Name]; exists {
					return fmt.Errorf("MCP server %q already exists", entry.Name)
				}
				entries[entry.Name] = entry
				return writeStateFile(path, entries)
			}); err != nil {
				return nil, err
			}
			if err := mcpManager.Connect(ctx, config); err != nil {
				return controlplane.TextEvents(fmt.Sprintf("mcp %s configuration saved; connection failed: %v", entry.Name, err)), nil
			}
			return controlplane.TextEvents("mcp " + entry.Name + ": added and connected"), nil
		case "status":
			statuses := mcpManager.Statuses()
			if len(statuses) == 0 {
				return controlplane.TextEvents("mcp: none"), nil
			}
			lines := make([]string, len(statuses))
			for index, status := range statuses {
				state := string(status.Health.State)
				if status.Disabled {
					state = "disabled"
				}
				lines[index] = status.Name + ": " + state
			}
			return controlplane.TextEvents(strings.Join(lines, "\n")), nil
		case "reconnect", "disable":
			if len(invocation.Args) != 2 {
				return nil, fmt.Errorf("/mcp %s requires a server name", operation)
			}
			var err error
			if operation == "reconnect" {
				err = mcpManager.Reconnect(ctx, invocation.Args[1])
			} else {
				err = mcpManager.Disable(ctx, invocation.Args[1])
			}
			if err != nil {
				return nil, err
			}
			return controlplane.TextEvents("mcp " + invocation.Args[1] + ": " + operation + " complete"), nil
		default:
			return nil, fmt.Errorf("unknown MCP operation %q", operation)
		}
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "context", Usage: "/context", Description: "show context sources and budget", Handler: func(ctx context.Context, _ controlplane.Invocation) ([]core.Event, error) {
		if contextBuilder == nil {
			return controlplane.TextEvents("context: unavailable"), nil
		}
		plan, err := contextBuilder.Build(ctx, contextbuilder.BuildInput{})
		if err != nil {
			return nil, err
		}
		lines := []string{fmt.Sprintf("estimated tokens: %d", plan.EstimatedTokens)}
		for _, source := range plan.Sources {
			lines = append(lines, fmt.Sprintf("%s (%s)", source.ID, source.Kind))
		}
		return controlplane.TextEvents(strings.Join(lines, "\n")), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "memory", Usage: "/memory list|add|remove", Description: "manage scoped agent memory", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
		if len(invocation.Args) == 0 || invocation.Args[0] == "list" {
			scope := memory.Scope("")
			if len(invocation.Args) > 1 {
				scope = memory.Scope(invocation.Args[1])
			}
			entries, err := memoryStore.List(ctx, scope)
			if err != nil {
				return nil, err
			}
			if len(entries) == 0 {
				return controlplane.TextEvents("memory: empty"), nil
			}
			lines := make([]string, len(entries))
			for i, entry := range entries {
				lines[i] = fmt.Sprintf("%s [%s] %s", entry.ID, entry.Scope, entry.Content)
			}
			return controlplane.TextEvents(strings.Join(lines, "\n")), nil
		}
		switch invocation.Args[0] {
		case "add":
			if len(invocation.Args) < 3 {
				return nil, fmt.Errorf("/memory add requires scope and content")
			}
			if err := memoryStore.Add(ctx, memory.Entry{Scope: memory.Scope(invocation.Args[1]), Content: strings.Join(invocation.Args[2:], " "), Source: "user"}); err != nil {
				return nil, err
			}
			return controlplane.TextEvents("memory: added"), nil
		case "remove":
			if len(invocation.Args) != 2 {
				return nil, fmt.Errorf("/memory remove requires an ID")
			}
			if err := memoryStore.Remove(ctx, invocation.Args[1]); err != nil {
				return nil, err
			}
			return controlplane.TextEvents("memory: removed"), nil
		default:
			return nil, fmt.Errorf("unknown memory operation %q", invocation.Args[0])
		}
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "compact", Usage: "/compact", Description: "compact the current conversation", Handler: func(ctx context.Context, _ controlplane.Invocation) ([]core.Event, error) {
		result, err := runtime.Compact(ctx)
		if err != nil {
			return nil, err
		}
		if result.Warning != "" {
			return controlplane.TextEvents(result.Warning), nil
		}
		if !result.Applied {
			return controlplane.TextEvents("conversation is below the compact threshold"), nil
		}
		return controlplane.TextEvents(fmt.Sprintf("conversation compacted; covered %d messages", result.CoveredMessages)), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "checkpoint", Usage: "/checkpoint [name]", Description: "save a conversation checkpoint", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
		name := "manual"
		if len(invocation.Args) > 1 {
			return nil, fmt.Errorf("/checkpoint accepts at most one name")
		}
		if len(invocation.Args) == 1 {
			name = invocation.Args[0]
		}
		checkpoint, err := runtime.CreateCheckpoint(ctx, name)
		if err != nil {
			return nil, err
		}
		return controlplane.TextEvents(fmt.Sprintf("checkpoint %s saved at sequence %d", checkpoint.ID, checkpoint.Sequence)), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "rewind", Usage: "/rewind CHECKPOINT_ID", Description: "rewind conversation history", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
		if len(invocation.Args) != 1 {
			return nil, fmt.Errorf("/rewind requires a checkpoint ID")
		}
		snapshot, err := runtime.Rewind(ctx, invocation.Args[0])
		if err != nil {
			return nil, err
		}
		return controlplane.TextEvents(fmt.Sprintf("rewound to %s at sequence %d", invocation.Args[0], snapshot.LastSequence)), nil
	}}); err != nil {
		return nil, err
	}
	if err := register(controlplane.Spec{Name: "branch", Usage: "/branch CHECKPOINT_ID SESSION_ID", Description: "create a session branch", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
		if len(invocation.Args) != 2 {
			return nil, fmt.Errorf("/branch requires a checkpoint ID and session ID")
		}
		branch, err := runtime.Branch(ctx, invocation.Args[0], invocation.Args[1])
		if err != nil {
			return nil, err
		}
		if err := recordSession(stateDir, sessionMetadata{ID: branch.SessionID, Profile: profileName, Model: model}); err != nil {
			return nil, fmt.Errorf("record branch session: %w", err)
		}
		return controlplane.TextEvents(fmt.Sprintf("branch %s created from %s", branch.SessionID, branch.CheckpointID)), nil
	}}); err != nil {
		return nil, err
	}
	return registry, nil
}

func parseMCPControlAdd(args []string, workspace string) (mcpEntry, mcp.ServerConfig, error) {
	if len(args) < 2 || args[0] != "add" || strings.TrimSpace(args[1]) == "" {
		return mcpEntry{}, mcp.ServerConfig{}, fmt.Errorf("/mcp add requires a server name and exactly one of --url or --command")
	}
	name := strings.TrimSpace(args[1])
	flags := flag.NewFlagSet("/mcp add", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var command, serverURL string
	var serverArgs []string
	flags.StringVar(&command, "command", "", "stdio server executable")
	flags.StringVar(&serverURL, "url", "", "HTTP server URL")
	flags.Func("arg", "stdio server argument", func(value string) error {
		serverArgs = append(serverArgs, value)
		return nil
	})
	if err := flags.Parse(args[2:]); err != nil {
		return mcpEntry{}, mcp.ServerConfig{}, fmt.Errorf("parse /mcp add: %w", err)
	}
	if flags.NArg() != 0 {
		return mcpEntry{}, mcp.ServerConfig{}, fmt.Errorf("/mcp add has unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if (command == "") == (serverURL == "") {
		return mcpEntry{}, mcp.ServerConfig{}, fmt.Errorf("/mcp add requires exactly one of --url or --command")
	}
	entry := mcpEntry{Name: name, Command: command, Args: append([]string(nil), serverArgs...), URL: serverURL}
	config := mcp.ServerConfig{Name: name, Workspace: workspace, Args: append([]string(nil), serverArgs...)}
	if serverURL != "" {
		parsed, err := url.Parse(serverURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return mcpEntry{}, mcp.ServerConfig{}, fmt.Errorf("MCP URL must use http or https")
		}
		config.Transport, config.URL = mcp.TransportHTTP, serverURL
	} else {
		config.Transport, config.Command = mcp.TransportStdio, command
	}
	return entry, config, nil
}

func registerProductCommands(registry *controlplane.Registry, actions ControlActions) error {
	var vimMu sync.Mutex
	vimEnabled := false
	commands := []controlplane.Spec{
		{Name: "init", Usage: "/init", Description: "create CYBER.md project instructions", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) != 0 {
				return nil, fmt.Errorf("/init does not accept arguments")
			}
			if actions.InitializeInstructions == nil {
				return nil, fmt.Errorf("instruction initialization is unavailable")
			}
			path, err := actions.InitializeInstructions(ctx)
			if err != nil {
				return nil, err
			}
			return controlplane.TextEvents("created " + path), nil
		}},
		{Name: "cost", Usage: "/cost", Description: "show cumulative session cost", Handler: func(_ context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) != 0 {
				return nil, fmt.Errorf("/cost does not accept arguments")
			}
			usage := controlUsage(actions)
			cost := "estimated cost: unavailable (pricing is not configured)"
			if actions.EstimateCost != nil {
				value, err := actions.EstimateCost(usage)
				if err != nil {
					return nil, err
				}
				cost = fmt.Sprintf("estimated cost: $%.4f", value)
			}
			return controlplane.TextEvents(fmt.Sprintf("%s\ninput tokens: %d\noutput tokens: %d", cost, usage.InputTokens, usage.OutputTokens)), nil
		}},
		{Name: "stats", Usage: "/stats", Description: "show cumulative session statistics", Handler: func(_ context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) != 0 {
				return nil, fmt.Errorf("/stats does not accept arguments")
			}
			usage := controlUsage(actions)
			historyCount := 0
			if actions.HistoryCount != nil {
				historyCount = actions.HistoryCount()
			}
			return controlplane.TextEvents(fmt.Sprintf("history messages: %d\ninput tokens: %d\noutput tokens: %d\ncache read tokens: %d\ncache creation tokens: %d",
				historyCount, usage.InputTokens, usage.OutputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens)), nil
		}},
		{Name: "clear", Usage: "/clear", Description: "clear the current conversation", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) != 0 {
				return nil, fmt.Errorf("/clear does not accept arguments")
			}
			if actions.ClearHistory == nil {
				return nil, fmt.Errorf("history clearing is unavailable")
			}
			if err := actions.ClearHistory(ctx); err != nil {
				return nil, err
			}
			return controlplane.TextEvents("conversation history cleared"), nil
		}},
		{Name: "vim", Usage: "/vim [on|off|toggle]", Description: "change Vim input mode", Handler: func(_ context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) > 1 {
				return nil, fmt.Errorf("/vim accepts at most one of on, off, or toggle")
			}
			if actions.SetVimMode == nil {
				return nil, fmt.Errorf("Vim mode control is unavailable")
			}
			vimMu.Lock()
			defer vimMu.Unlock()
			operation := "toggle"
			if len(invocation.Args) == 1 {
				operation = strings.ToLower(invocation.Args[0])
			}
			switch operation {
			case "on":
				vimEnabled = true
			case "off":
				vimEnabled = false
			case "toggle":
				vimEnabled = !vimEnabled
			default:
				return nil, fmt.Errorf("/vim expects on, off, or toggle")
			}
			if err := actions.SetVimMode(vimEnabled); err != nil {
				return nil, err
			}
			return controlplane.TextEvents(fmt.Sprintf("Vim mode: %t", vimEnabled)), nil
		}},
		{Name: "config", Usage: "/config", Description: "show effective non-secret configuration", Handler: func(_ context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) != 0 {
				return nil, fmt.Errorf("/config does not accept arguments; use cyber-code config to make persistent changes")
			}
			if actions.EffectiveConfig == nil {
				return nil, fmt.Errorf("effective configuration is unavailable")
			}
			return controlplane.TextEvents(formatEffectiveConfig(actions.EffectiveConfig())), nil
		}},
		{Name: "bug", Usage: "/bug DESCRIPTION", Description: "create a redacted local bug report", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) == 0 {
				return nil, fmt.Errorf("/bug requires a description")
			}
			if actions.CreateBugReport == nil {
				return nil, fmt.Errorf("bug reporting is unavailable")
			}
			path, err := actions.CreateBugReport(ctx, strings.Join(invocation.Args, " "))
			if err != nil {
				return nil, err
			}
			return controlplane.TextEvents("redacted local bug report created: " + path), nil
		}},
	}
	for _, command := range commands {
		if err := registry.Register(command); err != nil {
			return err
		}
	}
	return nil
}

func controlUsage(actions ControlActions) core.Usage {
	if actions.UsageSnapshot == nil {
		return core.Usage{}
	}
	return actions.UsageSnapshot()
}

func formatEffectiveConfig(config configpkg.Config) string {
	profile := config.Profiles[config.ActiveProfile]
	return fmt.Sprintf("active profile: %s\nprovider: %s\nbase URL: %s\nmodel: %s\ncredential env: %s\npermission mode: %s\nsandbox mode: %s\ncontext warning threshold: %.2f\ncontext compact threshold: %.2f\n\nPersistent changes: cyber-code config",
		config.ActiveProfile, profile.Provider, profile.BaseURL, profile.Model, profile.APIKeyEnv, config.PermissionMode, config.SandboxMode,
		config.ContextWarningThreshold, config.ContextCompactThreshold)
}

const cyberInstructionsTemplate = `# cyber-code project instructions

Describe this project's build, test, architecture, and coding conventions here.
`

func newInstructionInitializer(workspace string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		path := filepath.Join(workspace, "CYBER.md")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if os.IsExist(err) {
				return "", fmt.Errorf("CYBER.md already exists")
			}
			return "", fmt.Errorf("create CYBER.md: %w", err)
		}
		keep := false
		defer func() {
			_ = file.Close()
			if !keep {
				_ = os.Remove(path)
			}
		}()
		if _, err := file.WriteString(cyberInstructionsTemplate); err != nil {
			return "", fmt.Errorf("write CYBER.md: %w", err)
		}
		if err := file.Sync(); err != nil {
			return "", fmt.Errorf("sync CYBER.md: %w", err)
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("close CYBER.md: %w", err)
		}
		keep = true
		return path, nil
	}
}

func registerGitCommands(registry *controlplane.Registry, git gitWorkflow) error {
	if git == nil {
		return nil
	}
	commands := []controlplane.Spec{
		{Name: "diff", Usage: "/diff", Description: "show unstaged repository changes", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) != 0 {
				return nil, fmt.Errorf("/diff does not accept arguments")
			}
			output, err := git.Diff(ctx)
			return controlplane.TextEvents(output), err
		}},
		{Name: "review", Usage: "/review", Description: "collect bounded repository review context", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) != 0 {
				return nil, fmt.Errorf("/review does not accept arguments")
			}
			output, err := git.Review(ctx)
			return controlplane.TextEvents(output), err
		}},
		{Name: "commit", Usage: "/commit MESSAGE", Description: "commit already staged changes", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
			if len(invocation.Args) == 0 {
				return nil, fmt.Errorf("/commit requires a message")
			}
			output, err := git.Commit(ctx, strings.Join(invocation.Args, " "))
			return controlplane.TextEvents(output), err
		}},
	}
	for _, command := range commands {
		if err := registry.Register(command); err != nil {
			return err
		}
	}
	return nil
}

func runtimeSessionID(runtime *runtimepkg.Runtime) string {
	if runtime == nil {
		return ""
	}
	return runtime.SessionID()
}
