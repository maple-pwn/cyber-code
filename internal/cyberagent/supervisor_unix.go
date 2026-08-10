//go:build !windows

package cyberagent

import (
	"os"
	"os/exec"
	"syscall"
)

type unixSupervisorGuard struct{ pid int }

func configureSupervisorCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachSupervisorProcess(command *exec.Cmd) (supervisorProcessGuard, error) {
	return &unixSupervisorGuard{pid: command.Process.Pid}, nil
}

func (guard *unixSupervisorGuard) graceful(*os.Process) error {
	return syscall.Kill(-guard.pid, syscall.SIGTERM)
}

func (guard *unixSupervisorGuard) kill(process *os.Process) error {
	err := syscall.Kill(-guard.pid, syscall.SIGKILL)
	if err != nil && process != nil {
		return process.Kill()
	}
	return err
}

func (*unixSupervisorGuard) close() error { return nil }
