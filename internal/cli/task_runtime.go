package cli

import (
	"context"
	"fmt"
	"strings"

	"cyber-code/internal/agent"
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
	SystemPrompt   string
	SessionID      string
	ParentMode     permissions.PermissionMode
	ParentMaxTurns int
}

func configureTaskService(options taskCompositionOptions) (*tasks.ToolService, error) {
	if options.Provider == nil || options.Registry == nil || options.Broker == nil {
		return nil, fmt.Errorf("task provider, registry, and permission broker are required")
	}
	return tasks.NewToolService(tasks.ToolServiceOptions{
		ParentMode: options.ParentMode, ParentMaxTurns: options.ParentMaxTurns,
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
			childRunner := toolpkg.NewRunner(options.Registry, options.Broker, toolpkg.RunnerOptions{
				Hooks: options.Hooks, SessionID: request.TaskID,
			})
			child, err := tasks.NewSubAgent(tasks.SubAgentOptions{
				Provider: options.Provider,
				AgentOptions: agent.Options{
					Model: options.Model, SystemPrompt: options.SystemPrompt, MaxTurns: request.MaxTurns,
					Tools: options.Registry, ToolRunner: childRunner, SessionID: request.TaskID,
				},
				ParentMode: options.ParentMode, Mode: request.Mode,
				ParentMaxTurns: options.ParentMaxTurns, MaxTurns: request.MaxTurns, ParentBroker: options.Broker,
			})
			if err != nil {
				return nil, err
			}
			var output strings.Builder
			for event := range child.Engine.Run(ctx, request.Prompt) {
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
