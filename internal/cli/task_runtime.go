package cli

import (
	"context"
	"fmt"
	"strings"

	"cyber-code/internal/agent"
	"cyber-code/internal/collaboration"
	"cyber-code/internal/contextbuilder"
	"cyber-code/internal/core"
	"cyber-code/internal/hooks"
	"cyber-code/internal/permissions"
	"cyber-code/internal/provider"
	"cyber-code/internal/tasks"
	toolpkg "cyber-code/internal/tool"
)

type taskCompositionOptions struct {
	Provider       provider.Provider
	Registry       *toolpkg.Registry
	Broker         *permissions.Broker
	Hooks          *hooks.Runner
	Model          string
	ContextBuilder agent.ContextBuilder
	SessionID      string
	ParentMode     permissions.PermissionMode
	ParentMaxTurns int
	Definitions    []collaboration.Definition
	Board          *collaboration.Board
	Coordinator    *collaboration.Coordinator
}

func configureTaskService(options taskCompositionOptions) (*tasks.ToolService, error) {
	if options.Provider == nil || options.Registry == nil || options.Broker == nil {
		return nil, fmt.Errorf("task provider, registry, and permission broker are required")
	}
	return tasks.NewToolService(tasks.ToolServiceOptions{
		ParentMode: options.ParentMode, ParentMaxTurns: options.ParentMaxTurns,
		Definitions: options.Definitions,
		Board:       options.Board,
		Execute: func(ctx context.Context, request tasks.AgentRequest) (any, error) {
			if options.Hooks != nil {
				outcome, err := options.Hooks.Run(ctx, hooks.HookInput{
					EventName: hooks.HookEventSubagentStart, SessionID: options.SessionID, AgentID: request.TaskID, Prompt: request.Prompt,
				})
				if err != nil {
					return nil, err
				}
				if outcome.Denied {
					return nil, fmt.Errorf("sub-agent start denied: %s", outcome.Reason)
				}
			}
			childRegistry := options.Registry
			childContext := options.ContextBuilder
			if request.Definition != nil {
				if request.Definition.Model != "" && request.Definition.Model != options.Model {
					return nil, fmt.Errorf("sub-agent model %q exceeds the parent model boundary", request.Definition.Model)
				}
				boundedRegistry, subsetErr := options.Registry.Subset(request.Definition.Tools)
				if subsetErr != nil {
					return nil, fmt.Errorf("apply sub-agent tool boundary: %w", subsetErr)
				}
				childRegistry = boundedRegistry
				childContext = definitionContextBuilder{base: options.ContextBuilder, definition: *request.Definition}
			}
			childRunner := toolpkg.NewRunner(childRegistry, options.Broker, toolpkg.RunnerOptions{
				Hooks: options.Hooks, SessionID: request.TaskID, Gate: collaborationExecutionGate(options.Coordinator),
			})
			child, err := tasks.NewSubAgent(tasks.SubAgentOptions{
				Provider: options.Provider,
				AgentOptions: agent.Options{
					Model: options.Model, ContextBuilder: childContext, MaxTurns: request.MaxTurns,
					Tools: childRegistry, ToolRunner: childRunner, SessionID: request.TaskID,
				},
				ParentMode: options.ParentMode, Mode: request.Mode,
				ParentMaxTurns: options.ParentMaxTurns, MaxTurns: request.MaxTurns, ParentBroker: options.Broker,
			})
			if err != nil {
				return nil, err
			}
			var output strings.Builder
			for event := range child.Engine.Run(ctx, request.Prompt) {
				if request.Emit != nil {
					if err := request.Emit(event); err != nil {
						return nil, err
					}
				}
				switch event.Type {
				case core.EventTextDelta:
					output.WriteString(event.Text)
				case core.EventAssistantMessage:
					if output.Len() == 0 && event.Message != nil {
						for _, block := range event.Message.Content {
							if block.Type == core.ContentText {
								output.WriteString(block.Text)
							}
						}
					}
				case core.EventError:
					if event.Err != nil {
						return nil, event.Err
					}
					return nil, fmt.Errorf("sub-agent failed")
				}
			}
			return strings.TrimSpace(output.String()), nil
		},
	})
}

func collaborationExecutionGate(coordinator *collaboration.Coordinator) toolpkg.ExecutionGate {
	if coordinator == nil {
		return nil
	}
	return func(ctx context.Context, request permissions.Request, action func() (core.ToolResult, error)) (core.ToolResult, error) {
		if request.Action == permissions.ActionRead || request.Action == permissions.ActionNetwork || strings.HasPrefix(request.Tool, "task_") {
			return action()
		}
		var result core.ToolResult
		var actionErr error
		gateErr := coordinator.WithWorkspaceWrite(ctx, func() error {
			result, actionErr = action()
			return actionErr
		})
		if gateErr != nil {
			return core.ToolResult{}, gateErr
		}
		return result, actionErr
	}
}

type definitionContextBuilder struct {
	base       agent.ContextBuilder
	definition collaboration.Definition
}

func (builder definitionContextBuilder) Build(ctx context.Context, input contextbuilder.BuildInput) (contextbuilder.Plan, error) {
	if strings.TrimSpace(builder.definition.Instructions) != "" {
		input.Sources = append(input.Sources, contextbuilder.Source{
			ID: "agent:" + builder.definition.Name, Kind: contextbuilder.SourceAgent,
			Priority: 300, Content: builder.definition.Instructions,
		})
	}
	return builder.base.Build(ctx, input)
}
