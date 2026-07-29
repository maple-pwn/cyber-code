//go:build windows

package platform

import "context"

func detectSandboxCapability(mode SandboxMode, _ func(string) (string, error)) SandboxCapability {
	return SandboxCapability{
		Mode:           mode,
		ProcessTree:    true,
		Backend:        "job-object",
		DegradedReason: "Job Object provides process-tree management only; filesystem and network isolation are unavailable; use Windows Sandbox or a container when stronger isolation is required",
	}
}

func probeSandboxCapability(_ context.Context, mode SandboxMode, lookPath func(string) (string, error)) SandboxCapability {
	return detectSandboxCapability(mode, lookPath)
}
