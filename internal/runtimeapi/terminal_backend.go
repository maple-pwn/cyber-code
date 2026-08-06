package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

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
	return &portableTerminalProcess{terminal: terminal, command: command}, nil
}

type portableTerminalProcess struct {
	terminal pty.Pty
	command  *pty.Cmd
}

func (process *portableTerminalProcess) PID() string {
	if process.command.Process == nil {
		return ""
	}
	return strconv.Itoa(process.command.Process.Pid)
}

func (process *portableTerminalProcess) Read(data []byte) (int, error) {
	return process.terminal.Read(data)
}
func (process *portableTerminalProcess) Write(data []byte) (int, error) {
	return process.terminal.Write(data)
}
func (process *portableTerminalProcess) Resize(columns, rows int) error {
	return process.terminal.Resize(columns, rows)
}
func (process *portableTerminalProcess) Close() error { return process.terminal.Close() }

func (process *portableTerminalProcess) Kill() error {
	if process.command.Process == nil {
		return nil
	}
	err := process.command.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
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
