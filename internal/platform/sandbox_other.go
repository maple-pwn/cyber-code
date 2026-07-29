//go:build !linux && !windows

package platform

func detectSandboxCapability(mode SandboxMode, _ func(string) (string, error)) SandboxCapability {
	return SandboxCapability{Mode: mode, ProcessTree: true, Backend: "process-group", DegradedReason: "strong sandbox is unsupported on this platform"}
}
