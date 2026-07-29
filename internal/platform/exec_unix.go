//go:build !windows

package platform

import (
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
)

func configureDirectCommand(program string, arguments []string, workspace string, mode SandboxMode, lookPath func(string) (string, error)) (*exec.Cmd, Isolation, error) {
	isolation := IsolationProcessGroup
	if mode != SandboxOff && runtime.GOOS == "linux" {
		if bwrap, err := lookPath("bwrap"); err == nil {
			arguments = append([]string{"--die-with-parent", "--new-session", "--unshare-all", "--ro-bind", "/", "/", "--bind", workspace, workspace, "--chdir", workspace, program}, arguments...)
			program, isolation = bwrap, IsolationBubblewrap
		} else if mode == SandboxRequired {
			return nil, "", fmt.Errorf("%w: bubblewrap is unavailable", ErrSandboxUnavailable)
		}
	} else if mode == SandboxRequired {
		return nil, "", fmt.Errorf("%w: strong sandbox is unsupported on this platform", ErrSandboxUnavailable)
	}
	command := exec.Command(program, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command, isolation, nil
}

type unixGuard struct{}

func buildCommand(request ExecRequest, workspace string, environment []string, lookPath func(string) (string, error)) (*exec.Cmd, Isolation, error) {
	isolation := IsolationProcessGroup
	program := "/bin/sh"
	arguments := []string{"-c", request.Command}
	if request.Sandbox {
		isolation = IsolationPolicyOnly
		if runtime.GOOS == "linux" {
			if bwrap, err := lookPath("bwrap"); err == nil {
				program = bwrap
				arguments = []string{"--die-with-parent", "--new-session", "--unshare-all", "--ro-bind", "/", "/", "--bind", workspace, workspace, "--chdir", workspace, "/bin/sh", "-c", request.Command}
				isolation = IsolationBubblewrap
			}
		}
	}
	command := exec.Command(program, arguments...)
	command.Dir = workspace
	command.Env = environment
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command, isolation, nil
}

func attachProcess(*exec.Cmd) (unixGuard, error) { return unixGuard{}, nil }
func (unixGuard) kill(command *exec.Cmd) {
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Process.Kill()
	}
}
func (unixGuard) close() {}
