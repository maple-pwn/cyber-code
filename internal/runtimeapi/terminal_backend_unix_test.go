//go:build !windows

package runtimeapi

import (
	"bufio"
	"context"
	"io"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPortableTerminalBackendRunsAllowlistedProgramInPTY(t *testing.T) {
	t.Parallel()
	backend := NewPortableTerminalBackend()
	process, err := backend.Start(context.Background(), TerminalLaunch{
		ProfileID: "test-shell", Program: "/bin/sh", Arguments: []string{"-c", "printf 'ready\\n'"},
		WorkingDirectory: t.TempDir(), Columns: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	if process.PID() == "" {
		t.Fatal("PTY process has no identity")
	}
	if err := process.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	output, readErr := io.ReadAll(process)
	exitCode, waitErr := process.Wait()
	if readErr != nil || waitErr != nil || exitCode != 0 {
		t.Fatalf("PTY result output=%q read=%v wait=%v exit=%d", output, readErr, waitErr, exitCode)
	}
	if !strings.Contains(string(output), "ready") {
		t.Fatalf("PTY output = %q", output)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPortableTerminalBackendKillTerminatesProcessGroup(t *testing.T) {
	t.Parallel()
	process, err := NewPortableTerminalBackend().Start(context.Background(), TerminalLaunch{
		ProfileID: "test-shell", Program: "/bin/sh", Arguments: []string{"-c", "sleep 30 & child=$!; printf '%s\\n' \"$child\"; wait"},
		WorkingDirectory: t.TempDir(), Columns: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(process)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = process.Kill()
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		_ = process.Kill()
		t.Fatalf("child PID output = %q", line)
	}
	t.Cleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })
	if err := process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = process.Wait()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(childPID, 0); err == syscall.ESRCH {
			_ = process.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child process %d survived PTY kill", childPID)
}
