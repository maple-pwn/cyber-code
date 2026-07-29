// Package doctor provides deterministic, side-effect-limited diagnostics.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"cyber-code/internal/config"
	"cyber-code/internal/platform"
)

type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Check struct {
	Name        string `json:"name"`
	Status      Status `json:"status"`
	Required    bool   `json:"required"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type Report struct {
	SchemaVersion int     `json:"schema_version"`
	Healthy       bool    `json:"healthy"`
	Checks        []Check `json:"checks"`
}

type Options struct {
	ConfigFile string
	StateDir   string
	LookPath   func(string) (string, error)
	LookupEnv  func(string) (string, bool)
	GOOS       string
}

func Run(ctx context.Context, options Options) Report {
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.LookupEnv
	}
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	report := Report{SchemaVersion: 1, Healthy: true}
	add := func(check Check) {
		report.Checks = append(report.Checks, check)
		if check.Required && check.Status == StatusFail {
			report.Healthy = false
		}
	}
	if err := ctx.Err(); err != nil {
		add(Check{Name: "context", Status: StatusFail, Required: true, Message: err.Error()})
		return report
	}

	loaded, err := config.Load(config.LoadOptions{UserFile: options.ConfigFile})
	sandboxMode := platform.SandboxBestEffort
	if err != nil {
		add(Check{Name: "configuration", Status: StatusFail, Required: true, Message: err.Error(), Remediation: "Run `cyber-code config validate`, then correct the reported config file fields."})
	} else {
		sandboxMode = platform.SandboxMode(loaded.SandboxMode)
		add(Check{Name: "configuration", Status: StatusPass, Required: true, Message: "configuration is valid"})
		profile := loaded.Profiles[loaded.ActiveProfile]
		if profile.APIKeyEnv == "" {
			add(Check{Name: "credential", Status: StatusPass, Message: "profile does not require an environment credential"})
		} else if value, ok := options.LookupEnv(profile.APIKeyEnv); ok && strings.TrimSpace(value) != "" {
			add(Check{Name: "credential", Status: StatusPass, Message: profile.APIKeyEnv + " is set"})
		} else {
			add(Check{Name: "credential", Status: StatusWarn, Message: profile.APIKeyEnv + " is not set", Remediation: "Set " + profile.APIKeyEnv + " in the environment used to start cyber-code."})
		}
	}

	if err := checkStateDirectory(options.StateDir); err != nil {
		add(Check{Name: "state_directory", Status: StatusFail, Required: true, Message: err.Error(), Remediation: "Set CYBER_CODE_STATE_DIR to a private writable directory and retry."})
	} else {
		add(Check{Name: "state_directory", Status: StatusPass, Required: true, Message: "state directory is writable"})
	}
	capability := platform.NewRunner(platform.Options{SandboxMode: sandboxMode, LookPath: options.LookPath}).ProbeSandboxCapability(ctx, 2*time.Second)
	sandboxMsg := fmt.Sprintf("backend=%s filesystem=%t network=%t process_tree=%t", capability.Backend, capability.Filesystem, capability.Network, capability.ProcessTree)
	if capability.DegradedReason != "" {
		sandboxMsg += "; " + capability.DegradedReason
	}
	sandboxCheck := Check{Name: "sandbox", Status: StatusWarn, Message: sandboxMsg}
	if capability.Strong || sandboxMode == platform.SandboxOff {
		sandboxCheck.Status = StatusPass
	}
	if sandboxMode == platform.SandboxRequired {
		sandboxCheck.Required = true
		if !capability.Strong {
			sandboxCheck.Status = StatusFail
		}
	}
	if sandboxCheck.Status != StatusPass {
		sandboxCheck.Remediation = sandboxRemediation(options.GOOS, sandboxMode)
	}
	add(sandboxCheck)
	addOptionalCommand(&report, options.LookPath, "notifications", notificationCandidates(options.GOOS), notificationRemediation(options.GOOS))
	addOptionalCommand(&report, options.LookPath, "voice", voiceCandidates(options.GOOS), voiceRemediation(options.GOOS))
	add(Check{Name: "mcp", Status: StatusWarn, Message: "no MCP health probe was configured", Remediation: "Run `cyber-code mcp list` and `cyber-code mcp test NAME`, or add a server with `cyber-code mcp add`."})
	add(Check{Name: "lsp", Status: StatusWarn, Message: "no LSP health probe was configured", Remediation: "Configure CYBER_CODE_STATE_DIR/lsp.json and ensure each language server command is on PATH."})
	return report
}

func checkStateDirectory(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("state directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return err
	}
	probe, err := os.CreateTemp(absolute, ".doctor-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func addOptionalCommand(report *Report, lookPath func(string) (string, error), name string, candidates []string, remediation string) {
	for _, candidate := range candidates {
		if _, err := lookPath(candidate); err == nil {
			report.Checks = append(report.Checks, Check{Name: name, Status: StatusPass, Message: candidate + " is available"})
			return
		}
	}
	report.Checks = append(report.Checks, Check{Name: name, Status: StatusWarn, Message: "optional backend is unavailable", Remediation: remediation})
}

func sandboxRemediation(goos string, mode platform.SandboxMode) string {
	if goos == "linux" {
		return "Install bubblewrap (`bwrap`) for strong isolation, or choose sandbox_mode: best-effort/off if that degradation is acceptable."
	}
	if goos == "windows" {
		return "Windows Job Objects only provide process-tree cleanup; use sandbox_mode: best-effort/off or run cyber-code inside a stronger isolated environment."
	}
	if mode == platform.SandboxRequired {
		return "Strong sandbox mode is unavailable on this platform; use best-effort/off or run cyber-code inside a stronger isolated environment."
	}
	return "Review the reported sandbox backend and use an external sandbox if filesystem or network isolation is required."
}

func notificationRemediation(goos string) string {
	switch goos {
	case "linux":
		return "Install `notify-send` (for example, the libnotify-bin package), or leave notifications disabled."
	case "windows":
		return "Install or enable PowerShell (`powershell.exe` or `pwsh.exe`), or leave notifications disabled."
	default:
		return "Install a supported notification backend, or leave notifications disabled."
	}
}

func voiceRemediation(goos string) string {
	switch goos {
	case "linux":
		return "Install `arecord`, `rec`, or `ffmpeg`, or leave voice input disabled."
	case "windows":
		return "Install `ffmpeg.exe`, or leave voice input disabled."
	default:
		return "Install `ffmpeg` or another supported recorder, or leave voice input disabled."
	}
}

func notificationCandidates(goos string) []string {
	if goos == "windows" {
		return []string{"powershell.exe", "pwsh.exe"}
	}
	if goos == "linux" {
		return []string{"notify-send"}
	}
	return nil
}

func voiceCandidates(goos string) []string {
	if goos == "windows" {
		return []string{"ffmpeg.exe", "ffmpeg"}
	}
	if goos == "linux" {
		return []string{"rec", "arecord", "ffmpeg"}
	}
	return nil
}
