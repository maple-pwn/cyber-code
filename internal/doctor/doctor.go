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

	"claude-code-go/internal/config"
)

type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Check struct {
	Name     string `json:"name"`
	Status   Status `json:"status"`
	Required bool   `json:"required"`
	Message  string `json:"message"`
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
	if err != nil {
		add(Check{Name: "configuration", Status: StatusFail, Required: true, Message: err.Error()})
	} else {
		add(Check{Name: "configuration", Status: StatusPass, Required: true, Message: "configuration is valid"})
		profile := loaded.Profiles[loaded.ActiveProfile]
		if profile.APIKeyEnv == "" {
			add(Check{Name: "credential", Status: StatusPass, Message: "profile does not require an environment credential"})
		} else if value, ok := options.LookupEnv(profile.APIKeyEnv); ok && strings.TrimSpace(value) != "" {
			add(Check{Name: "credential", Status: StatusPass, Message: profile.APIKeyEnv + " is set"})
		} else {
			add(Check{Name: "credential", Status: StatusWarn, Message: profile.APIKeyEnv + " is not set"})
		}
	}

	if err := checkStateDirectory(options.StateDir); err != nil {
		add(Check{Name: "state_directory", Status: StatusFail, Required: true, Message: err.Error()})
	} else {
		add(Check{Name: "state_directory", Status: StatusPass, Required: true, Message: "state directory is writable"})
	}
	if options.GOOS == "windows" {
		add(Check{Name: "sandbox", Status: StatusPass, Message: "Windows Job Object isolation is available"})
	} else if _, err := options.LookPath("bwrap"); err == nil {
		add(Check{Name: "sandbox", Status: StatusPass, Message: "bubblewrap is available"})
	} else {
		add(Check{Name: "sandbox", Status: StatusWarn, Message: "bubblewrap is unavailable; policy-only isolation will be used"})
	}
	addOptionalCommand(&report, options.LookPath, "notifications", notificationCandidates(options.GOOS))
	addOptionalCommand(&report, options.LookPath, "voice", voiceCandidates(options.GOOS))
	add(Check{Name: "mcp", Status: StatusWarn, Message: "no MCP health probe was configured"})
	add(Check{Name: "lsp", Status: StatusWarn, Message: "no LSP health probe was configured"})
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

func addOptionalCommand(report *Report, lookPath func(string) (string, error), name string, candidates []string) {
	for _, candidate := range candidates {
		if _, err := lookPath(candidate); err == nil {
			report.Checks = append(report.Checks, Check{Name: name, Status: StatusPass, Message: candidate + " is available"})
			return
		}
	}
	report.Checks = append(report.Checks, Check{Name: name, Status: StatusWarn, Message: "optional backend is unavailable"})
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
