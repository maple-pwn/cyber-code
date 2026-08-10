//go:build windows

package cyberagent

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsSupervisorGuard struct {
	job  windows.Handle
	once sync.Once
}

func configureSupervisorCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func attachSupervisorProcess(command *exec.Cmd) (supervisorProcessGuard, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&information)), uint32(unsafe.Sizeof(information))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &windowsSupervisorGuard{job: job}, nil
}

func (guard *windowsSupervisorGuard) graceful(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Signal(os.Interrupt)
}

func (guard *windowsSupervisorGuard) kill(process *os.Process) error {
	err := guard.close()
	if process != nil {
		if killErr := process.Kill(); err == nil {
			err = killErr
		}
	}
	return err
}

func (guard *windowsSupervisorGuard) close() (err error) {
	guard.once.Do(func() {
		if guard.job != 0 {
			err = windows.CloseHandle(guard.job)
		}
	})
	return err
}
