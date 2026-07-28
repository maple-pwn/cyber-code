package frontend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"cyber-code/internal/core"
)

const (
	ExitOK             = 0
	ExitRuntime        = 1
	ExitConfiguration  = 2
	ExitAuthentication = 3
	ExitPermission     = 4
	ExitCanceled       = 130
)

type Runner interface {
	Run(context.Context, string) <-chan core.Event
}

type PrintOptions struct {
	JSON   bool
	Stdout io.Writer
	Stderr io.Writer
}

func Run(ctx context.Context, runner Runner, prompt string, options PrintOptions) int {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := options.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := options.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	if runner == nil || strings.TrimSpace(prompt) == "" {
		_, _ = fmt.Fprintln(stderr, "runtime and prompt are required")
		return ExitConfiguration
	}
	events := runner.Run(ctx, prompt)
	if events == nil {
		_, _ = fmt.Fprintln(stderr, "runtime returned no event stream")
		return ExitRuntime
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	wroteText := false
	for {
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(stderr, ctx.Err())
			return ExitCanceled
		case event, open := <-events:
			if !open {
				if !options.JSON && wroteText {
					_, _ = fmt.Fprintln(stdout)
				}
				return ExitOK
			}
			if options.JSON {
				if err := encoder.Encode(event); err != nil {
					_, _ = fmt.Fprintln(stderr, "encode event:", err)
					return ExitRuntime
				}
			} else {
				switch event.Type {
				case core.EventTextDelta:
					_, _ = io.WriteString(stdout, event.Text)
					wroteText = true
				case core.EventAssistantMessage:
					text := messageText(event.Message)
					if text != "" {
						_, _ = io.WriteString(stdout, text)
						wroteText = true
					}
				}
			}
			if event.Type == core.EventError {
				if event.Err == nil {
					_, _ = fmt.Fprintln(stderr, "runtime failed")
					return ExitRuntime
				}
				if !options.JSON {
					_, _ = fmt.Fprintln(stderr, event.Err.UserMessage())
				}
				return exitCode(event.Err.Kind)
			}
		}
	}
}

func exitCode(kind core.ErrorKind) int {
	switch kind {
	case core.ErrorKindConfiguration:
		return ExitConfiguration
	case core.ErrorKindAuthentication:
		return ExitAuthentication
	case core.ErrorKindPermission:
		return ExitPermission
	case core.ErrorKindCanceled:
		return ExitCanceled
	default:
		return ExitRuntime
	}
}

func messageText(message *core.Message) string {
	if message == nil {
		return ""
	}
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type == core.ContentText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}
