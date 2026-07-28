//go:build !windows

package platform

import (
	"os/exec"
	"runtime"
	"syscall"
)

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
				arguments = []string{"--die-with-parent", "--new-session", "--ro-bind", "/", "/", "--bind", workspace, workspace, "--chdir", workspace, "/bin/sh", "-c", request.Command}
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
