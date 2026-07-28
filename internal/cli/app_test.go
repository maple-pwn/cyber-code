package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"claude-code-go/internal/core"
	"claude-code-go/internal/frontend"
)

func TestPrintModeUsesInjectedRunner(t *testing.T) {
	runner := &cliTestRunner{events: []core.Event{
		{Type: core.EventTextDelta, Text: "deepseek"},
		{Type: core.EventCompleted, FinishReason: "stop"},
	}}
	var stdout, stderr bytes.Buffer
	app := NewApp(&Config{Runtime: runner, PrintMode: true, Stdout: &stdout, Stderr: &stderr}, "test")
	app.initialPrompt = "hello"

	if err := app.runPrintMode(); err != nil {
		t.Fatal(err)
	}
	if runner.prompt != "hello" || stdout.String() != "deepseek\n" || stderr.Len() != 0 {
		t.Fatalf("prompt = %q, stdout = %q, stderr = %q", runner.prompt, stdout.String(), stderr.String())
	}
}

func TestPrintModeJSONAndExitError(t *testing.T) {
	runner := &cliTestRunner{events: []core.Event{{
		Type: core.EventError,
		Err:  &core.Error{Kind: core.ErrorKindAuthentication, Message: "invalid key"},
	}}}
	var stdout, stderr bytes.Buffer
	app := NewApp(&Config{Runtime: runner, PrintMode: true, PrintJSON: true, Stdout: &stdout, Stderr: &stderr}, "test")
	app.initialPrompt = "hello"

	err := app.runPrintMode()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != frontend.ExitAuthentication {
		t.Fatalf("error = %#v", err)
	}
	if !strings.Contains(stdout.String(), `"type":"error"`) || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestRunWithPromptUsesInjectedRunner(t *testing.T) {
	runner := &cliTestRunner{events: []core.Event{
		{Type: core.EventTextDelta, Text: "hello"},
		{Type: core.EventTextDelta, Text: " world"},
		{Type: core.EventCompleted, FinishReason: "stop"},
	}}
	app := NewApp(&Config{Runtime: runner}, "test")

	output, err := app.RunWithPrompt("prompt")
	if err != nil || output != "hello world" || runner.prompt != "prompt" {
		t.Fatalf("output = %q, prompt = %q, error = %v", output, runner.prompt, err)
	}
}

func TestAppInheritsConfiguredContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app := NewApp(&Config{Context: ctx}, "test")
	cancel()
	select {
	case <-app.ctx.Done():
	default:
		t.Fatal("app context was not canceled")
	}
}

type cliTestRunner struct {
	prompt string
	events []core.Event
}

func (runner *cliTestRunner) Run(_ context.Context, prompt string) <-chan core.Event {
	runner.prompt = prompt
	events := make(chan core.Event, len(runner.events))
	for _, event := range runner.events {
		events <- event
	}
	close(events)
	return events
}
