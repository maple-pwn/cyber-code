package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cyber-code/internal/platform"
)

type RunnerOptions struct {
	Registry       *Registry
	Executor       platform.Executor
	Workspace      string
	Timeout        time.Duration
	MaxOutputBytes int
}

type Outcome struct {
	Denied            bool
	Reason            string
	Changed           bool
	UpdatedInput      map[string]any
	AdditionalContext []string
}

type Runner struct {
	registry       *Registry
	executor       platform.Executor
	workspace      string
	timeout        time.Duration
	maxOutputBytes int
}

func NewRunner(options RunnerOptions) (*Runner, error) {
	if options.Registry == nil {
		options.Registry = NewRegistry()
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	if options.MaxOutputBytes <= 0 {
		options.MaxOutputBytes = 64 << 10
	}
	return &Runner{
		registry: options.Registry, executor: options.Executor, workspace: options.Workspace,
		timeout: options.Timeout, maxOutputBytes: options.MaxOutputBytes,
	}, nil
}

func (runner *Runner) RegisterCommand(event HookEvent, command string) error {
	if !IsHookEvent(string(event)) {
		return fmt.Errorf("unsupported hook event %q", event)
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("hook command is required")
	}
	if runner.executor == nil {
		return fmt.Errorf("hook command executor is required")
	}
	runner.registry.Register(event, runner.commandHandler(command))
	return nil
}

func (runner *Runner) Run(ctx context.Context, input HookInput) (Outcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !IsHookEvent(string(input.EventName)) {
		return Outcome{}, fmt.Errorf("unsupported hook event %q", input.EventName)
	}
	ctx, cancel := context.WithTimeout(ctx, runner.timeout)
	defer cancel()
	outputs, err := runner.registry.Execute(ctx, input)
	if err != nil {
		return Outcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{UpdatedInput: inputMap(input.ToolInput)}
	contextBytes := 0
	for _, output := range outputs {
		if len(output.UpdatedInput) > 0 {
			outcome.Changed = true
		}
		for key, value := range output.UpdatedInput {
			outcome.UpdatedInput[key] = value
		}
		if output.AdditionalContext != "" {
			contextBytes += len(output.AdditionalContext)
			if contextBytes > runner.maxOutputBytes {
				return Outcome{}, fmt.Errorf("hook output exceeds %d bytes", runner.maxOutputBytes)
			}
			outcome.AdditionalContext = append(outcome.AdditionalContext, output.AdditionalContext)
		}
		if output.Decision == "block" || !output.Continue {
			outcome.Denied = true
			outcome.Reason = output.Reason
			if outcome.Reason == "" {
				outcome.Reason = output.StopReason
			}
			if outcome.Reason == "" {
				outcome.Reason = "hook requested execution to stop"
			}
			break
		}
	}
	return outcome, nil
}

func (runner *Runner) commandHandler(command string) HookHandler {
	return func(ctx context.Context, input HookInput) (HookOutput, error) {
		encoded, err := json.Marshal(input)
		if err != nil {
			return HookOutput{}, fmt.Errorf("encode hook input: %w", err)
		}
		result, err := runner.executor.Run(ctx, platform.ExecRequest{
			Command: command, Workspace: runner.workspace, Stdin: string(encoded), Timeout: runner.timeout,
		})
		if err != nil {
			return HookOutput{}, fmt.Errorf("run hook command: %w", err)
		}
		if len(result.Stdout) > runner.maxOutputBytes {
			return HookOutput{}, fmt.Errorf("hook output exceeds %d bytes", runner.maxOutputBytes)
		}
		if strings.TrimSpace(result.Stdout) == "" {
			return HookOutput{Continue: true}, nil
		}
		decoder := json.NewDecoder(bytes.NewBufferString(result.Stdout))
		decoder.DisallowUnknownFields()
		var output HookOutput
		if err := decoder.Decode(&output); err != nil {
			return HookOutput{}, fmt.Errorf("decode hook output: %w", err)
		}
		if decoder.More() {
			return HookOutput{}, fmt.Errorf("decode hook output: multiple values")
		}
		return output, nil
	}
}

func inputMap(input any) map[string]any {
	result := make(map[string]any)
	if values, ok := input.(map[string]any); ok {
		for key, value := range values {
			result[key] = value
		}
	}
	return result
}
