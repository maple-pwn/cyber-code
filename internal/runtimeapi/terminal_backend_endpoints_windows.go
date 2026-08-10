//go:build windows

package runtimeapi

import (
	"errors"
	"os"

	pty "github.com/aymanbagabas/go-pty"
)

func readTerminal(terminal pty.Pty, data []byte) (int, error) {
	return terminal.Read(data)
}

func killTerminalProcess(command *pty.Cmd) error {
	err := command.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func closeTerminalChildEndpoint(pty.Pty) error { return nil }

func closeTerminalParentEndpoint(terminal pty.Pty) error { return terminal.Close() }
