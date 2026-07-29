// Package platform provides operating-system process and media boundaries.
package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Isolation string

const (
	IsolationProcessGroup Isolation = "process-group"
	IsolationJobObject    Isolation = "job-object"
	IsolationBubblewrap   Isolation = "bubblewrap"
	IsolationPolicyOnly   Isolation = "policy-only"
)

type ExecRequest struct {
	Command     string
	Workspace   string
	Environment map[string]string
	Stdin       string
	Timeout     time.Duration
	Sandbox     bool
}

type ExecResult struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Isolation Isolation
}

type Executor interface {
	Run(context.Context, ExecRequest) (ExecResult, error)
}

type Options struct {
	LookPath    func(string) (string, error)
	SandboxMode SandboxMode
}

type Runner struct {
	lookPath    func(string) (string, error)
	sandboxMode SandboxMode
}

func NewRunner(options Options) *Runner {
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	return &Runner{lookPath: options.LookPath, sandboxMode: normalizeSandboxMode(options.SandboxMode)}
}

func (runner *Runner) Run(ctx context.Context, request ExecRequest) (ExecResult, error) {
	if strings.TrimSpace(request.Command) == "" {
		return ExecResult{}, fmt.Errorf("command is required")
	}
	workspace, err := filepath.Abs(request.Workspace)
	if err != nil {
		return ExecResult{}, fmt.Errorf("resolve workspace: %w", err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return ExecResult{}, fmt.Errorf("resolve workspace links: %w", err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return ExecResult{}, fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return ExecResult{}, fmt.Errorf("workspace is not a directory")
	}
	if request.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	environment := FilterEnvironment(os.Environ(), request.Environment)
	switch runner.sandboxMode {
	case SandboxOff:
		request.Sandbox = false
	case SandboxBestEffort:
		request.Sandbox = true
	case SandboxRequired:
		request.Sandbox = true
		if capability := detectSandboxCapability(runner.sandboxMode, runner.lookPath); !capability.Strong {
			return ExecResult{}, fmt.Errorf("%w: %s", ErrSandboxUnavailable, capability.DegradedReason)
		}
	default:
		return ExecResult{}, fmt.Errorf("invalid sandbox mode %q", runner.sandboxMode)
	}
	command, isolation, err := buildCommand(request, workspace, environment, runner.lookPath)
	if err != nil {
		return ExecResult{}, err
	}
	stdout := &limitedBuffer{limit: 1 << 20}
	stderr := &limitedBuffer{limit: 1 << 20}
	command.Stdout = stdout
	command.Stderr = stderr
	if request.Stdin != "" {
		command.Stdin = strings.NewReader(request.Stdin)
	}
	if err := command.Start(); err != nil {
		return ExecResult{}, fmt.Errorf("start process: %w", err)
	}
	guard, err := attachProcess(command)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return ExecResult{}, fmt.Errorf("attach process tree: %w", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case waitErr := <-waited:
		guard.close()
		result := processResult(command, stdout.String(), stderr.String(), isolation)
		if waitErr != nil {
			return result, fmt.Errorf("process exited: %w", waitErr)
		}
		return result, nil
	case <-ctx.Done():
		guard.kill(command)
		<-waited
		return processResult(command, stdout.String(), stderr.String(), isolation), ctx.Err()
	}
}

func processResult(command *exec.Cmd, stdout, stderr string, isolation Isolation) ExecResult {
	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	return ExecResult{Stdout: stdout, Stderr: stderr, ExitCode: exitCode, Isolation: isolation}
}

var allowedEnvironment = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USERPROFILE": {}, "SYSTEMROOT": {}, "WINDIR": {}, "COMSPEC": {}, "PATHEXT": {},
	"TMP": {}, "TEMP": {}, "TMPDIR": {}, "LANG": {}, "LC_ALL": {}, "TERM": {},
}

func FilterEnvironment(base []string, overrides map[string]string) []string {
	filtered := make(map[string]string)
	for _, entry := range base {
		name, value, found := strings.Cut(entry, "=")
		if found && environmentAllowed(name) {
			filtered[strings.ToUpper(name)] = value
		}
	}
	for name, value := range overrides {
		if environmentAllowed(name) {
			filtered[strings.ToUpper(name)] = value
		}
	}
	names := make([]string, 0, len(filtered))
	for name := range filtered {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+filtered[name])
	}
	return result
}

func environmentAllowed(name string) bool {
	_, ok := allowedEnvironment[strings.ToUpper(strings.TrimSpace(name))]
	return ok
}

type limitedBuffer struct {
	data  []byte
	limit int
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := buffer.limit - len(buffer.data)
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		buffer.data = append(buffer.data, data...)
	}
	return written, nil
}
func (buffer *limitedBuffer) String() string { return string(buffer.data) }
