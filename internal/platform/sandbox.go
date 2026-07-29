package platform

import "errors"

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
