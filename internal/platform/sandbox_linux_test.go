//go:build linux

package platform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinuxCapabilityDetectionDoesNotExecuteBubblewrap(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "executed")
	bwrap := filepath.Join(directory, "bwrap")
	script := "#!/bin/sh\ntouch '" + marker + "'\n"
	if err := os.WriteFile(bwrap, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	capability := detectSandboxCapability(SandboxBestEffort, func(string) (string, error) { return bwrap, nil })
	if !capability.Strong || capability.Backend != "bubblewrap" {
		t.Fatalf("capability = %#v", capability)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("normal capability detection executed bwrap: %v", err)
	}
}

func TestLinuxFunctionalProbeHasABoundedTimeout(t *testing.T) {
	directory := t.TempDir()
	bwrap := filepath.Join(directory, "bwrap")
	if err := os.WriteFile(bwrap, []byte("#!/bin/sh\nsleep 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(Options{LookPath: func(string) (string, error) { return bwrap, nil }})
	started := time.Now()
	capability := runner.ProbeSandboxCapability(context.Background(), 50*time.Millisecond)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("sandbox probe exceeded its bound: %s", elapsed)
	}
	if capability.Strong || !strings.Contains(capability.DegradedReason, "timed out") {
		t.Fatalf("timed-out capability = %#v", capability)
	}
}
