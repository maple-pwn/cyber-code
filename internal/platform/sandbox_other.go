//go:build !linux && !windows

package platform

import "context"

func detectSandboxCapability(mode SandboxMode, _ func(string) (string, error)) SandboxCapability {
	return SandboxCapability{Mode: mode, ProcessTree: true, Backend: "process-group", DegradedReason: "strong sandbox is unsupported on this platform"}
}

func probeSandboxCapability(_ context.Context, mode SandboxMode, lookPath func(string) (string, error)) SandboxCapability {
	return detectSandboxCapability(mode, lookPath)
}
