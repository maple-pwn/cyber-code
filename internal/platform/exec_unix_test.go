//go:build !windows

package platform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessCancellationTerminatesChildProcess(t *testing.T) {
	workspace := t.TempDir()
	pidFile := filepath.Join(workspace, "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := NewRunner(Options{SandboxMode: SandboxOff}).Run(ctx, ExecRequest{
			Command: fmt.Sprintf("sleep 30 & echo $! > %q; wait", pidFile), Workspace: workspace,
		})
		done <- err
	}()
	var childPID int
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(pidFile)
		if err == nil {
			childPID, _ = strconv.Atoi(strings.TrimSpace(string(content)))
			if childPID > 0 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if childPID == 0 {
		cancel()
		t.Fatal("child process did not start")
	}
	cancel()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("error = %v", err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(childPID, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("child process %d survived cancellation", childPID)
}
