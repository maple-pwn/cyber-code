package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"cyber-code/internal/contextbuilder"
	"cyber-code/internal/controlplane"
	"cyber-code/internal/core"
	"cyber-code/internal/hooks"
	"cyber-code/internal/permissions"
	runtimepkg "cyber-code/internal/runtime"
	"cyber-code/internal/skill"
)

func buildControlPlane(runtime *runtimepkg.Runtime, model string, mode permissions.PermissionMode, hooksRunner *hooks.Runner, skills []skill.Skill, contextBuilder *contextbuilder.Builder) (*controlplane.Registry, error) {
	registry := controlplane.NewRegistry()
	register := func(spec controlplane.Spec) error { return registry.Register(spec) }
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
	return registry, nil
}

func runtimeSessionID(runtime *runtimepkg.Runtime) string {
	if runtime == nil {
		return ""
	}
	return runtime.SessionID()
}
