//go:build linux

package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

func detectSandboxCapability(mode SandboxMode, lookPath func(string) (string, error)) SandboxCapability {
	capability := SandboxCapability{Mode: mode, ProcessTree: true, Backend: "process-group"}

	_, bwrapErr := lookPath("bwrap")
	if bwrapErr != nil {
		capability.DegradedReason = buildBubblewrapDiagnosis()
		return capability
	}
	capability.Strong, capability.Filesystem, capability.Network = true, true, true
	capability.Backend = "bubblewrap"
	return capability
}

func probeSandboxCapability(ctx context.Context, mode SandboxMode, lookPath func(string) (string, error)) SandboxCapability {
	capability := detectSandboxCapability(mode, lookPath)
	if !capability.Strong {
		return capability
	}
	bwrapPath, err := lookPath("bwrap")
	if err != nil {
		return detectSandboxCapability(mode, lookPath)
	}
	command := exec.CommandContext(ctx, bwrapPath, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--unshare-all", "--echo", "ok")
	if err := command.Run(); err != nil {
		capability.Strong, capability.Filesystem, capability.Network = false, false, false
		capability.Backend = "process-group"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			capability.DegradedReason = "bubblewrap functional probe timed out"
		} else if errors.Is(ctx.Err(), context.Canceled) {
			capability.DegradedReason = "bubblewrap functional probe canceled"
		} else {
			capability.DegradedReason = "bubblewrap is installed but cannot create the required namespaces"
		}
	}
	return capability
}

// buildBubblewrapDiagnosis returns a human-readable explanation of why
// bubblewrap is not available, including hints for common Linux distributions.
func buildBubblewrapDiagnosis() string {
	var hints []string
	hints = append(hints, "bubblewrap (bwrap) is not installed or not in PATH")

	// Detect the package manager and provide install instructions.
	switch {
	case fileExists("/usr/bin/apt-get") || fileExists("/usr/bin/apt"):
		hints = append(hints, "install with: sudo apt-get install bubblewrap")
	case fileExists("/usr/bin/dnf"):
		hints = append(hints, "install with: sudo dnf install bubblewrap")
	case fileExists("/usr/bin/yum"):
		hints = append(hints, "install with: sudo yum install bubblewrap")
	case fileExists("/usr/bin/pacman"):
		hints = append(hints, "install with: sudo pacman -S bubblewrap")
	case fileExists("/usr/bin/zypper"):
		hints = append(hints, "install with: sudo zypper install bubblewrap")
	case fileExists("/usr/bin/apk"):
		hints = append(hints, "install with: sudo apk add bubblewrap")
	}

	// Check if unprivileged user namespaces are disabled.
	if nsClone := readFileString("/proc/sys/kernel/unprivileged_userns_clone"); nsClone == "0" {
		hints = append(hints, "kernel.unprivileged_userns_clone=0; enable with: sudo sysctl kernel.unprivileged_userns_clone=1")
	}

	return strings.Join(hints, "; ")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
