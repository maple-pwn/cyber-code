package protocol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"cyber-code/internal/core"
)

type Server struct {
	runtime Runtime
	codec   Codec
	writeMu sync.Mutex
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	runs    sync.WaitGroup
}

func NewServer(runtime Runtime, maxMessageBytes int) (*Server, error) {
	if runtime == nil {
		return nil, errors.New("protocol runtime is required")
	}
	return &Server{runtime: runtime, codec: NewCodec(maxMessageBytes)}, nil
}

// Serve handles newline-delimited JSON requests until the peer disconnects.
// Active turns are canceled on disconnect or server shutdown.
func (server *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := bufio.NewReader(input)
	for {
		var request Request
		if err := server.codec.Decode(reader, &request); err != nil {
			if errors.Is(err, io.EOF) {
				server.cancelActive()
				server.runs.Wait()
				return nil
			}
			_ = server.send(output, Response{Type: "error", Error: err.Error()})
			return err
		}
		select {
		case <-ctx.Done():
			server.cancelActive()
			return ctx.Err()
		default:
		}
		if err := server.handle(ctx, output, request); err != nil {
			_ = server.send(output, Response{ID: request.ID, Type: "error", Error: err.Error()})
		}
	}
}

func (server *Server) handle(parent context.Context, output io.Writer, request Request) error {
	switch request.Type {
	case "status":
		server.mu.Lock()
		running := server.running
		server.mu.Unlock()
		return server.send(output, Response{ID: request.ID, Type: "status", Status: &Status{SessionID: server.runtime.SessionID(), Running: running, History: len(server.runtime.History())}})
	case "cancel":
		server.cancelActive()
		return server.send(output, Response{ID: request.ID, Type: "accepted"})
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
		return server.send(output, Response{ID: request.ID, Type: "accepted"})
	case "permission":
		return errors.New("permission responses are runtime-owned and cannot be injected")
	default:
		return fmt.Errorf("unsupported protocol message type %q", request.Type)
	}
}

func (server *Server) forwardEvents(output io.Writer, id string, ctx context.Context, prompt string) {
	defer func() {
		server.mu.Lock()
		server.running = false
		server.cancel = nil
		server.mu.Unlock()
	}()
	for event := range server.runtime.Run(ctx, prompt) {
		if err := server.send(output, Response{ID: id, Type: "event", Event: &event}); err != nil {
			return
		}
	}
}

func (server *Server) cancelActive() {
	server.mu.Lock()
	cancel := server.cancel
	server.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (server *Server) send(output io.Writer, response Response) error {
	server.writeMu.Lock()
	defer server.writeMu.Unlock()
	return server.codec.Encode(output, response)
}

var _ Runtime = (interface {
	Run(context.Context, string) <-chan core.Event
	SessionID() string
	History() []core.Message
})(nil)
