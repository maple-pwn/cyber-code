package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunnerStartsManagedProcessWithLiteralArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process, err := NewRunner(Options{}).Start(context.Background(), ProcessRequest{
		Command: executable,
		Args: []string{
			"-test.run=^TestManagedProcessHelper$", "--", "echo", "argument with spaces", "$(must-not-execute)",
		},
		Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	line, err := bufio.NewReader(process.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "argument with spaces|$(must-not-execute)" {
		t.Fatalf("stdout = %q", line)
	}
	if _, err := io.WriteString(process.Stdin(), "exit\n"); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedProcessCancellationReapsProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	process, err := NewRunner(Options{}).Start(ctx, ProcessRequest{
		Command:   executable,
		Args:      []string{"-test.run=^TestManagedProcessHelper$", "--", "block"},
		Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled process was not reaped")
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedProcessPassesExplicitEnvironmentWithoutInheritingSecrets(t *testing.T) {
	t.Setenv("UNRELATED_SECRET", "must-not-inherit")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process, err := NewRunner(Options{}).Start(context.Background(), ProcessRequest{
		Command:   executable,
		Args:      []string{"-test.run=^TestManagedProcessHelper$", "--", "environment"},
		Workspace: t.TempDir(),
		Environment: map[string]string{
			"MCP_API_KEY": "explicit-secret",
			"LD_PRELOAD":  "/must/not/load",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	line, err := bufio.NewReader(process.Stdout()).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "explicit-secret|" {
		t.Fatalf("environment output = %q", line)
	}
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedProcessHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	switch os.Args[separator+1] {
	case "echo":
		fmt.Printf("%s|%s\n", os.Args[separator+2], os.Args[separator+3])
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	case "block":
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "environment":
		fmt.Printf("%s|%s\n", os.Getenv("MCP_API_KEY"), os.Getenv("UNRELATED_SECRET"))
	}
	os.Exit(0)
}
