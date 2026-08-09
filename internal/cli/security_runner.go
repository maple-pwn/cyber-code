package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"cyber-code/internal/core"
	"cyber-code/internal/productprotocol"
	"cyber-code/internal/runtimeapi"
	"cyber-code/internal/ui/adapter"
)

type securityRunner struct {
	source    adapter.Source
	runtimeID string
	inputs    []string
}

func newSecurityRunner(source adapter.Source, runtimeID string, inputs ...string) *securityRunner {
	return &securityRunner{source: source, runtimeID: runtimeID, inputs: append([]string(nil), inputs...)}
}

func (runner *securityRunner) Run(ctx context.Context, prompt string) <-chan core.Event {
	events := make(chan core.Event, 64)
	go func() {
		defer close(events)
		emitError := func(err error) {
			events <- core.Event{Type: core.EventError, Err: &core.Error{Kind: core.ErrorKindPlatform, Op: "cyber-agent.run", Message: "security runtime failed", Cause: err}}
		}
		request := runtimeapi.HandshakeRequest{SupportedProtocolVersions: []int{runtimeapi.ProtocolVersion}, SupportedCapabilities: []string{"events", "snapshot", "commands"}}
		response, err := runner.source.Handshake(ctx, request)
		if err != nil {
			emitError(err)
			return
		}
		if _, err := runtimeapi.NegotiateHandshake(request, response); err != nil {
			emitError(err)
			return
		}
		rawEvents := make(chan json.RawMessage, 64)
		unsubscribe, err := runner.source.Subscribe(ctx, 0, func(raw json.RawMessage) {
			select {
			case rawEvents <- append(json.RawMessage(nil), raw...):
			case <-ctx.Done():
			}
		})
		if err != nil {
			emitError(err)
			return
		}
		defer unsubscribe()
		command := adapter.Command{Type: adapter.CommandTaskCreate, Objective: prompt, RuntimeID: runner.runtimeID, InputPaths: append([]string(nil), runner.inputs...)}
		if identity, ok := runner.source.(adapter.IdentitySource); ok && identity.RuntimeIdentity().SessionID != "" {
			command = adapter.Command{Type: adapter.CommandInstructionSend, Content: prompt, RuntimeID: runner.runtimeID}
		}
		if err := runner.source.Send(ctx, command); err != nil {
			emitError(err)
			return
		}
		for {
			select {
			case <-ctx.Done():
				emitError(ctx.Err())
				return
			case raw := <-rawEvents:
				event, err := productprotocol.Validate(raw)
				if err != nil {
					emitError(fmt.Errorf("validate security event: %w", err))
					return
				}
				if event.Type == "task.failed" || event.Type == "task.cancelled" {
					emitError(fmt.Errorf("security task ended with %s", event.Type))
					return
				}
				if event.Type != "task.completed" {
					events <- core.Event{Type: core.EventTextDelta, Text: formatSecurityEvent(event)}
					continue
				}
				events <- core.Event{Type: core.EventTextDelta, Text: formatSecurityEvent(event)}
				events <- core.Event{Type: core.EventCompleted, FinishReason: "security_session_completed"}
				return
			}
		}
	}()
	return events
}

func formatSecurityEvent(event productprotocol.Event) string {
	return fmt.Sprintf("[%s] task=%s cursor=%d\n", event.Type, event.TaskID, event.Cursor)
}
