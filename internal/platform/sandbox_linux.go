//go:build linux

package platform

func detectSandboxCapability(mode SandboxMode, lookPath func(string) (string, error)) SandboxCapability {
	capability := SandboxCapability{Mode: mode, ProcessTree: true, Backend: "process-group"}
	if _, err := lookPath("bwrap"); err == nil {
		capability.Strong, capability.Filesystem, capability.Network = true, true, true
		capability.Backend = "bubblewrap"
		return capability
	}
	capability.DegradedReason = "bubblewrap is unavailable"
	return capability
}
