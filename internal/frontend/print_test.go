package frontend

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/core"
)

func TestPrintTextOutputsOnlyAssistantText(t *testing.T) {
	runner := printTestRunner{events: []core.Event{
		{Type: core.EventTextDelta, Text: "hello"},
		{Type: core.EventToolCall, ToolCall: &core.ToolCall{Name: "read_file"}},
		{Type: core.EventTextDelta, Text: " world"},
		{Type: core.EventCompleted, FinishReason: "stop"},
	}}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), runner, "prompt", PrintOptions{Stdout: &stdout, Stderr: &stderr})
	if code != ExitOK || stdout.String() != "hello world\n" || stderr.Len() != 0 || strings.Contains(stdout.String(), "\x1b[") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestPrintJSONEmitsOneStableObjectPerEvent(t *testing.T) {
	runner := printTestRunner{events: []core.Event{{Type: core.EventTextDelta, Text: "hello"}, {Type: core.EventCompleted, FinishReason: "stop"}}}
	var stdout bytes.Buffer
	code := Run(context.Background(), runner, "prompt", PrintOptions{JSON: true, Stdout: &stdout})
	if code != ExitOK {
		t.Fatalf("exit code = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("JSON lines = %q", stdout.String())
	}
	for _, line := range lines {
		var event core.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Type == "" {
			t.Fatalf("invalid JSON event %q: %v", line, err)
		}
	}
}

func TestPrintMapsAuthenticationAndConfigurationErrorsToNonzeroExit(t *testing.T) {
	for _, test := range []struct {
		kind core.ErrorKind
		want int
	}{{core.ErrorKindAuthentication, ExitAuthentication}, {core.ErrorKindConfiguration, ExitConfiguration}} {
		var stderr bytes.Buffer
		runner := printTestRunner{events: []core.Event{{Type: core.EventError, Err: &core.Error{Kind: test.kind, Message: "failed"}}}}
		if got := Run(context.Background(), runner, "prompt", PrintOptions{Stderr: &stderr}); got != test.want || !strings.Contains(stderr.String(), "failed") {
			t.Fatalf("kind = %s, code = %d, stderr = %q", test.kind, got, stderr.String())
		}
	}
}

func TestPrintCancelsRunnerAfterTerminalError(t *testing.T) {
	runner := &cancelAwareRunner{canceled: make(chan struct{})}
	if code := Run(context.Background(), runner, "prompt", PrintOptions{Stderr: &bytes.Buffer{}}); code != ExitRuntime {
		t.Fatalf("exit code = %d", code)
	}
	select {
	case <-runner.canceled:
	case <-time.After(time.Second):
		t.Fatal("runner context was not canceled")
	}
}

type printTestRunner struct{ events []core.Event }

func (runner printTestRunner) Run(context.Context, string) <-chan core.Event {
	events := make(chan core.Event, len(runner.events))
	for _, event := range runner.events {
		events <- event
	}
	close(events)
	return events
}

type cancelAwareRunner struct{ canceled chan struct{} }

func (runner *cancelAwareRunner) Run(ctx context.Context, _ string) <-chan core.Event {
	events := make(chan core.Event, 1)
	events <- core.Event{Type: core.EventError, Err: &core.Error{Kind: core.ErrorKindProvider, Message: "failed"}}
	go func() {
		<-ctx.Done()
		close(runner.canceled)
	}()
	return events
}
