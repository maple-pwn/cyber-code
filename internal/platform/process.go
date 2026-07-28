package platform

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type ProcessRequest struct {
	Command     string
	Args        []string
	Workspace   string
	Environment map[string]string
}

type processTreeGuard interface {
	kill(*exec.Cmd)
	close()
}

type Process struct {
	command *exec.Cmd
	guard   processTreeGuard
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	done    chan struct{}

	stopOnce sync.Once
	mu       sync.Mutex
	waitErr  error
	stopErr  error
}

func (runner *Runner) Start(ctx context.Context, request ProcessRequest) (*Process, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Command == "" {
		return nil, fmt.Errorf("command is required")
	}
	workspace, err := filepath.Abs(request.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace links: %w", err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		return nil, fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace is not a directory")
	}
	command, _ := configureDirectCommand(request.Command, request.Args)
	command.Dir = workspace
	command.Env = managedEnvironment(os.Environ(), request.Environment)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open process stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open process stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("open process stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("start process: %w", err)
	}
	guard, err := attachProcess(command)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("attach process tree: %w", err)
	}
	process := &Process{command: command, guard: guard, stdin: stdin, stdout: stdout, done: make(chan struct{})}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	go process.wait()
	go func() {
		select {
		case <-ctx.Done():
			process.stop(ctx.Err())
		case <-process.done:
		}
	}()
	return process, nil
}

func managedEnvironment(base []string, explicit map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range FilterEnvironment(base, explicit) {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[strings.ToUpper(name)] = value
		}
	}
	for name, value := range explicit {
		normalized := strings.ToUpper(strings.TrimSpace(name))
		if validEnvironmentName(normalized) && !dangerousEnvironmentName(normalized) {
			values[normalized] = value
		}
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result
}

func validEnvironmentName(name string) bool {
	if name == "" || !((name[0] >= 'A' && name[0] <= 'Z') || name[0] == '_') {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if !((character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_') {
			return false
		}
	}
	return true
}

func dangerousEnvironmentName(name string) bool {
	if strings.HasPrefix(name, "DYLD_") || strings.HasPrefix(name, "LD_") {
		return true
	}
	switch name {
	case "BASH_ENV", "ENV", "GCONV_PATH", "IFS", "NODE_OPTIONS", "PERL5OPT", "PS4", "PYTHONHOME", "PYTHONPATH", "RUBYOPT", "SHELLOPTS":
		return true
	default:
		return false
	}
}

func (process *Process) Stdin() io.WriteCloser { return process.stdin }
func (process *Process) Stdout() io.ReadCloser { return process.stdout }

func (process *Process) Wait() error {
	<-process.done
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.stopErr != nil {
		return process.stopErr
	}
	return process.waitErr
}

func (process *Process) Close() error {
	process.stop(nil)
	<-process.done
	return nil
}

func (process *Process) stop(cause error) {
	process.stopOnce.Do(func() {
		process.mu.Lock()
		process.stopErr = cause
		process.mu.Unlock()
		_ = process.stdin.Close()
		process.guard.kill(process.command)
	})
}

func (process *Process) wait() {
	err := process.command.Wait()
	process.guard.close()
	process.mu.Lock()
	if err != nil {
		process.waitErr = fmt.Errorf("process exited: %w", err)
	}
	process.mu.Unlock()
	close(process.done)
}
