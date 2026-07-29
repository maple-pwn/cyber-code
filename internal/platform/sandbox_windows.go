//go:build windows

package platform

func detectSandboxCapability(mode SandboxMode, _ func(string) (string, error)) SandboxCapability {
	return SandboxCapability{Mode: mode, ProcessTree: true, Backend: "job-object", DegradedReason: "restricted token and filesystem boundary are unavailable"}
}
