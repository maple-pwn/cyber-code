package cli

import (
	"context"
	"fmt"

	"cyber-code/internal/permissions"
	"cyber-code/internal/platform"
	"cyber-code/internal/tool/builtin"
)

type authorizedExecutor struct {
	tool     string
	broker   *permissions.Broker
	delegate platform.Executor
}

func (executor *authorizedExecutor) Run(ctx context.Context, request platform.ExecRequest) (platform.ExecResult, error) {
	if executor == nil || executor.broker == nil || executor.delegate == nil {
		return platform.ExecResult{}, fmt.Errorf("authorized process executor is not configured")
	}
	permissionRequest, err := builtin.ShellPermissionRequest(executor.tool, request.Workspace, request.Command)
	if err != nil {
		return platform.ExecResult{}, err
	}
	decision, err := executor.broker.Decide(ctx, permissionRequest)
	if err != nil {
		return platform.ExecResult{}, err
	}
	if decision.Behavior != permissions.PermissionBehaviorAllow {
		return platform.ExecResult{}, fmt.Errorf("%s execution denied: %s", executor.tool, decision.Reason)
	}
	return executor.delegate.Run(ctx, request)
}
