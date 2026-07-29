package protocol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
)

type Server struct {
	runtime     Runtime
	codec       Codec
	mu          sync.Mutex
	cancel      context.CancelFunc
	running     bool
	runs        sync.WaitGroup
	permissions *PermissionBroker
}

type ServerOptions struct {
	MaxMessageBytes int
	Permissions     *PermissionBroker
}

func NewServer(runtime Runtime, maxMessageBytes int) (*Server, error) {
	return NewServerWithOptions(runtime, ServerOptions{MaxMessageBytes: maxMessageBytes})
}

func NewServerWithOptions(runtime Runtime, options ServerOptions) (*Server, error) {
	if runtime == nil {
		return nil, errors.New("protocol runtime is required")
	}
	return &Server{runtime: runtime, codec: NewCodec(options.MaxMessageBytes), permissions: options.Permissions}, nil
}

// Serve handles newline-delimited JSON requests until the peer disconnects.
// Active turns are canceled on disconnect or server shutdown.
func (server *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := bufio.NewReader(input)
	responses := newConnectionOutput(output, server.codec, 64)
	defer responses.Close()
	connectionCtx, disconnect := context.WithCancel(ctx)
	defer disconnect()
	if server.permissions != nil {
		go server.forwardPermissions(connectionCtx, responses)
	}
	for {
		var request Request
		if err := server.codec.Decode(reader, &request); err != nil {
			if errors.Is(err, io.EOF) {
				server.cancelActive()
				disconnect()
				if server.permissions != nil {
					server.permissions.Disconnect()
				}
				server.runs.Wait()
				return nil
			}
			_ = responses.Send(Response{Type: "error", Error: err.Error()})
			return err
		}
		select {
		case <-ctx.Done():
			server.cancelActive()
			return ctx.Err()
		default:
		}
		if err := server.handle(ctx, responses, request); err != nil {
			_ = responses.Send(Response{ID: request.ID, Type: "error", Error: err.Error()})
		}
	}
}

func (server *Server) handle(parent context.Context, output *connectionOutput, request Request) error {
	switch request.Type {
	case "status":
		server.mu.Lock()
		running := server.running
		server.mu.Unlock()
		return output.Send(Response{ID: request.ID, Type: "status", Status: &Status{SessionID: server.runtime.SessionID(), Running: running, History: len(server.runtime.History())}})
	case "cancel":
		server.cancelActive()
		return output.Send(Response{ID: request.ID, Type: "accepted"})
	case "start", "input":
		if request.Prompt == "" {
			return errors.New("prompt is required")
		}
		server.mu.Lock()
		if server.running {
			server.mu.Unlock()
			return errors.New("a turn is already running")
		}
		turnCtx, cancel := context.WithCancel(parent)
		server.cancel, server.running = cancel, true
		server.mu.Unlock()
		server.runs.Add(1)
		go func() {
			defer server.runs.Done()
			server.forwardEvents(output, request.ID, turnCtx, request.Prompt)
		}()
		return output.Send(Response{ID: request.ID, Type: "accepted"})
	case "permission":
		if server.permissions == nil {
			return errors.New("permission responses are unavailable")
		}
		decision := permissions.Decision{Reason: request.Reason}
		switch request.Decision {
		case "allow":
			decision.Behavior = permissions.PermissionBehaviorAllow
		case "deny":
			decision.Behavior = permissions.PermissionBehaviorDeny
		default:
			return errors.New("permission decision must be allow or deny")
		}
		if err := server.permissions.Respond(request.PermissionID, decision); err != nil {
			return err
		}
		return output.Send(Response{ID: request.ID, Type: "accepted"})
	default:
		return fmt.Errorf("unsupported protocol message type %q", request.Type)
	}
}

func (server *Server) forwardPermissions(ctx context.Context, output *connectionOutput) {
	for {
		select {
		case <-ctx.Done():
			return
		case prompt := <-server.permissions.Requests():
			if err := output.Send(Response{Type: "permission", Permission: &prompt}); err != nil {
				server.permissions.Disconnect()
				server.cancelActive()
				return
			}
		}
	}
}

func (server *Server) forwardEvents(output *connectionOutput, id string, ctx context.Context, prompt string) {
	defer func() {
		server.mu.Lock()
		server.running = false
		server.cancel = nil
		server.mu.Unlock()
	}()
	for event := range server.runtime.Run(ctx, prompt) {
		if err := output.Send(Response{ID: id, Type: "event", Event: &event}); err != nil {
			server.cancelActive()
			return
		}
	}
	_ = output.Send(Response{ID: id, Type: "turn_finished", Canceled: ctx.Err() != nil})
}

func (server *Server) cancelActive() {
	server.mu.Lock()
	cancel := server.cancel
	server.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

var _ Runtime = (interface {
	Run(context.Context, string) <-chan core.Event
	SessionID() string
	History() []core.Message
})(nil)
