//go:build windows

package platform

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureDirectCommand(program string, arguments []string, _ string, mode SandboxMode, _ func(string) (string, error)) (*exec.Cmd, Isolation, error) {
	if mode == SandboxRequired {
		return nil, "", ErrSandboxUnavailable
	}
	command := exec.Command(program, arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return command, IsolationJobObject, nil
}

type windowsGuard struct {
	job  windows.Handle
	once sync.Once
}

func buildCommand(request ExecRequest, workspace string, environment []string, _ func(string) (string, error)) (*exec.Cmd, Isolation, error) {
	command := exec.Command("cmd.exe", "/d", "/s", "/c", request.Command)
	command.Dir = workspace
	command.Env = environment
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return command, IsolationJobObject, nil
}

func attachProcess(command *exec.Cmd) (*windowsGuard, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&information)), uint32(unsafe.Sizeof(information))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(process)
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	return &windowsGuard{job: job}, nil
}

func (guard *windowsGuard) kill(command *exec.Cmd) {
	guard.close()
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
func (guard *windowsGuard) close() {
	guard.once.Do(func() {
		if guard.job != 0 {
			_ = windows.CloseHandle(guard.job)
		}
	})
}
