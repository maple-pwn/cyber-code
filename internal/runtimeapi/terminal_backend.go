package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"

	pty "github.com/aymanbagabas/go-pty"
)

type PortableTerminalBackend struct{}

func NewPortableTerminalBackend() *PortableTerminalBackend { return &PortableTerminalBackend{} }

func (*PortableTerminalBackend) Start(ctx context.Context, launch TerminalLaunch) (TerminalProcess, error) {
	if launch.Program == "" || launch.WorkingDirectory == "" || launch.Columns < 1 || launch.Rows < 1 {
		return nil, ErrInvalidCommandEnvelope
	}
	program, err := exec.LookPath(launch.Program)
	if err != nil {
		return nil, fmt.Errorf("resolve terminal profile program: %w", err)
	}
	terminal, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("open PTY: %w", err)
	}
	if err := terminal.Resize(launch.Columns, launch.Rows); err != nil {
		_ = terminal.Close()
		return nil, fmt.Errorf("set PTY size: %w", err)
	}
	command := terminal.CommandContext(ctx, program, launch.Arguments...)
	command.Dir = launch.WorkingDirectory
	if err := command.Start(); err != nil {
		_ = terminal.Close()
		return nil, fmt.Errorf("start PTY process: %w", err)
	}
	if err := closeTerminalChildEndpoint(terminal); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = terminal.Close()
		return nil, fmt.Errorf("release PTY child endpoint: %w", err)
	}
	return &portableTerminalProcess{terminal: terminal, command: command}, nil
}

type portableTerminalProcess struct {
	terminal  pty.Pty
	command   *pty.Cmd
	closeOnce sync.Once
	closeErr  error
}

func (process *portableTerminalProcess) PID() string {
	if process.command.Process == nil {
		return ""
	}
	return strconv.Itoa(process.command.Process.Pid)
}

func (process *portableTerminalProcess) Read(data []byte) (int, error) {
	return readTerminal(process.terminal, data)
}
func (process *portableTerminalProcess) Write(data []byte) (int, error) {
	return process.terminal.Write(data)
}
func (process *portableTerminalProcess) Resize(columns, rows int) error {
	return process.terminal.Resize(columns, rows)
}
func (process *portableTerminalProcess) Close() error {
	process.closeOnce.Do(func() { process.closeErr = closeTerminalParentEndpoint(process.terminal) })
	return process.closeErr
}

func (process *portableTerminalProcess) Kill() error {
	if process.command.Process == nil {
		return nil
	}
	return killTerminalProcess(process.command)
}

func (process *portableTerminalProcess) Wait() (int, error) {
	err := process.command.Wait()
	if process.command.ProcessState == nil {
		return -1, err
	}
	exitCode := process.command.ProcessState.ExitCode()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitCode, nil
	}
	return exitCode, err
}
