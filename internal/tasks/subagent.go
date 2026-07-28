package tasks

import (
	"errors"
	"fmt"

	"claude-code-go/internal/agent"
	"claude-code-go/internal/core"
	"claude-code-go/internal/permissions"
	"claude-code-go/internal/provider"
	toolpkg "claude-code-go/internal/tool"
)

var (
	ErrBudgetExceeded       = errors.New("sub-agent budget exceeds parent budget")
	ErrPermissionEscalation = errors.New("sub-agent permission mode exceeds parent mode")
)

type SubAgentOptions struct {
	Provider       provider.Provider
	AgentOptions   agent.Options
	ParentHistory  []core.Message
	ParentMode     permissions.PermissionMode
	Mode           permissions.PermissionMode
	ParentMaxTurns int
	MaxTurns       int
	ParentBroker   *permissions.Broker
}

type SubAgent struct {
	Engine     *agent.Engine
	Broker     *permissions.Broker
	Provider   provider.Provider
	ToolRunner *toolpkg.Runner
}

func NewSubAgent(options SubAgentOptions) (*SubAgent, error) {
	if options.Provider == nil {
		return nil, fmt.Errorf("sub-agent provider is required")
	}
	if options.ParentBroker == nil {
		return nil, fmt.Errorf("sub-agent parent broker is required")
	}
	parentTurns := normalizedTurns(options.ParentMaxTurns)
	childTurns := normalizedTurns(options.MaxTurns)
	if childTurns > parentTurns {
		return nil, fmt.Errorf("%w: child %d, parent %d", ErrBudgetExceeded, childTurns, parentTurns)
	}
	parentMode := normalizedMode(options.ParentMode)
	childMode := normalizedMode(options.Mode)
	if modePrivilege(childMode) > modePrivilege(parentMode) {
		return nil, fmt.Errorf("%w: child %s, parent %s", ErrPermissionEscalation, childMode, parentMode)
	}
	childBroker, err := options.ParentBroker.Child(childMode, permissions.SourceCliArg, nil)
	if err != nil {
		return nil, fmt.Errorf("create sub-agent permission broker: %w", err)
	}

	agentOptions := options.AgentOptions
	agentOptions.MaxTurns = childTurns
	agentOptions.InitialHistory = options.ParentHistory
	if agentOptions.ToolRunner != nil {
		agentOptions.ToolRunner = agentOptions.ToolRunner.WithAuthorizer(childBroker)
	}
	return &SubAgent{
		Engine: agent.NewEngine(options.Provider, agentOptions), Broker: childBroker,
		Provider: options.Provider, ToolRunner: agentOptions.ToolRunner,
	}, nil
}

func normalizedTurns(turns int) int {
	if turns <= 0 {
		return 1
	}
	return turns
}

func normalizedMode(mode permissions.PermissionMode) permissions.PermissionMode {
	if mode == "" {
		return permissions.PermissionModeDefault
	}
	return mode
}

func modePrivilege(mode permissions.PermissionMode) int {
	switch mode {
	case permissions.PermissionModePlan:
		return 0
	case permissions.PermissionModeDefault:
		return 1
	case permissions.PermissionModeAcceptEdits:
		return 2
	case permissions.PermissionModeBypass:
		return 3
	default:
		return 4
	}
}
