package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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

func buildControlPlane(runtime *runtimepkg.Runtime, stateDir, profileName, model string, mode permissions.PermissionMode, hooksRunner *hooks.Runner, skills []skill.Skill, contextBuilder *contextbuilder.Builder, mcpManager *mcp.Manager, git *gitworkflow.Service) (*controlplane.Registry, error) {
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
	if err := register(controlplane.Spec{Name: "mcp", Usage: "/mcp status|reconnect|disable", Description: "manage runtime MCP connections", Handler: func(ctx context.Context, invocation controlplane.Invocation) ([]core.Event, error) {
		if mcpManager == nil {
			return controlplane.TextEvents("mcp: unavailable"), nil
		}
		operation := "status"
		if len(invocation.Args) > 0 {
			operation = invocation.Args[0]
		}
		switch operation {
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
