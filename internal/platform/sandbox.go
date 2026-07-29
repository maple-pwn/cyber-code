package platform

import (
	"context"
	"errors"
	"time"
)

type SandboxMode string

const (
	SandboxOff        SandboxMode = "off"
	SandboxBestEffort SandboxMode = "best-effort"
	SandboxRequired   SandboxMode = "required"
)

var ErrSandboxUnavailable = errors.New("required process sandbox is unavailable")

type SandboxCapability struct {
	Mode           SandboxMode `json:"mode"`
	Strong         bool        `json:"strong"`
	Filesystem     bool        `json:"filesystem"`
	Network        bool        `json:"network"`
	ProcessTree    bool        `json:"process_tree"`
	Backend        string      `json:"backend"`
	DegradedReason string      `json:"degraded_reason,omitempty"`
}

func normalizeSandboxMode(mode SandboxMode) SandboxMode {
	if mode == "" {
		return SandboxBestEffort
	}
	return mode
}

func (runner *Runner) SandboxCapability() SandboxCapability {
	if runner == nil {
		return SandboxCapability{Mode: SandboxBestEffort, DegradedReason: "process runner is unavailable"}
	}
	return detectSandboxCapability(runner.sandboxMode, runner.lookPath)
}

// ProbeSandboxCapability performs the platform-specific functional check used
// by explicit diagnostics. Normal runtime capability detection remains
// side-effect free.
func (runner *Runner) ProbeSandboxCapability(ctx context.Context, timeout time.Duration) SandboxCapability {
	if runner == nil {
		return SandboxCapability{Mode: SandboxBestEffort, DegradedReason: "process runner is unavailable"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 2 * time.Second
	}
	probeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return probeSandboxCapability(probeContext, runner.sandboxMode, runner.lookPath)
}
