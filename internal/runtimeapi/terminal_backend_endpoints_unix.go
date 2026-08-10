//go:build !windows

package runtimeapi

import (
	"errors"
	"fmt"
	"io"
	"syscall"

	pty "github.com/aymanbagabas/go-pty"
)

func readTerminal(terminal pty.Pty, data []byte) (int, error) {
	count, err := terminal.Read(data)
	if errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return count, err
}

func killTerminalProcess(command *pty.Cmd) error {
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func closeTerminalChildEndpoint(terminal pty.Pty) error {
	unixTerminal, ok := terminal.(pty.UnixPty)
	if !ok {
		return fmt.Errorf("PTY does not expose Unix endpoints")
	}
	return unixTerminal.Slave().Close()
}

func closeTerminalParentEndpoint(terminal pty.Pty) error {
	unixTerminal, ok := terminal.(pty.UnixPty)
	if !ok {
		return terminal.Close()
	}
	return unixTerminal.Master().Close()
}
