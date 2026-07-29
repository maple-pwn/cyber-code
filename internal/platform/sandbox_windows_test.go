//go:build windows

package platform

import (
	"os/exec"
	"strings"
	"testing"
)

func TestWindowsCapabilityOnlyClaimsProcessTreeManagement(t *testing.T) {
	capability := detectSandboxCapability(SandboxBestEffort, exec.LookPath)
	if capability.Backend != "job-object" || !capability.ProcessTree {
		t.Fatalf("capability = %#v", capability)
	}
	if capability.Strong || capability.Filesystem || capability.Network {
		t.Fatalf("Windows capability overclaims isolation = %#v", capability)
	}
	if !strings.Contains(capability.DegradedReason, "process-tree management only") {
		t.Fatalf("degraded reason = %q", capability.DegradedReason)
	}
}
