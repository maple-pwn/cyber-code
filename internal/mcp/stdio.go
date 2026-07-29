package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"cyber-code/internal/platform"
)

const defaultMaxMessageBytes = 4 << 20

var errStdioClosed = errors.New("MCP stdio transport is closed")

type StdioProcessConfig struct {
	Command     string
	Args        []string
	Workspace   string
	Environment map[string]string
}

type StdioProcess interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Close() error
}

type StdioStarter interface {
	Start(context.Context, StdioProcessConfig) (StdioProcess, error)
}

type platformStdioStarter struct{ runner *platform.Runner }

func newPlatformStdioStarter(runner *platform.Runner) StdioStarter {
	if runner == nil {
		runner = platform.NewRunner(platform.Options{})
	}
	return &platformStdioStarter{runner: runner}
}

func (starter *platformStdioStarter) Start(ctx context.Context, config StdioProcessConfig) (StdioProcess, error) {
	return starter.runner.Start(ctx, platform.ProcessRequest{
		Command: config.Command, Args: config.Args, Workspace: config.Workspace, Environment: config.Environment,
	})
}

type StdioOptions struct {
	Starter         StdioStarter
	Config          StdioProcessConfig
	MaxMessageBytes int
}

type StdioTransport struct {
	process  StdioProcess
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	maxBytes int
	nextID   atomic.Int64

	writeMu   sync.Mutex
	mu        sync.Mutex
	pending   map[int64]chan rpcReply
	abandoned map[int64]struct{}
	failure   error
	done      chan struct{}

	closeOnce sync.Once
}

type rpcReply struct {
	response jsonRPCResponse
	err      error
}

func NewStdioTransport(ctx context.Context, options StdioOptions) (*StdioTransport, error) {
	if options.Starter == nil {
		return nil, fmt.Errorf("MCP stdio starter is required")
	}
	if options.Config.Command == "" {
		return nil, fmt.Errorf("MCP stdio command is required")
	}
	if options.MaxMessageBytes <= 0 {
		options.MaxMessageBytes = defaultMaxMessageBytes
	}
	process, err := options.Starter.Start(ctx, cloneStdioConfig(options.Config))
	if err != nil {
		return nil, fmt.Errorf("start MCP stdio process: %w", err)
	}
	if process == nil || process.Stdin() == nil || process.Stdout() == nil {
		if process != nil {
			_ = process.Close()
		}
		return nil, fmt.Errorf("MCP stdio process returned invalid pipes")
	}
	transport := &StdioTransport{
		process: process, stdin: process.Stdin(), stdout: process.Stdout(), maxBytes: options.MaxMessageBytes,
		pending: make(map[int64]chan rpcReply), abandoned: make(map[int64]struct{}), done: make(chan struct{}),
	}
	go transport.readResponses()
	return transport, nil
}

func (transport *StdioTransport) Call(ctx context.Context, method string, params, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := transport.nextID.Add(1)
	reply := make(chan rpcReply, 1)
	transport.mu.Lock()
	if transport.failure != nil {
		err := transport.failure
		transport.mu.Unlock()
		return err
	}
	transport.pending[id] = reply
	transport.mu.Unlock()

	payload, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		transport.removePending(id, false)
		return fmt.Errorf("encode MCP JSON-RPC request: %w", err)
	}
	if len(payload) > transport.maxBytes {
		transport.removePending(id, false)
		return ErrResponseTooLarge
	}
	transport.writeMu.Lock()
	if err := ctx.Err(); err != nil {
		transport.writeMu.Unlock()
		transport.removePending(id, false)
		return err
	}
	_, writeErr := transport.stdin.Write(append(payload, '\n'))
	transport.writeMu.Unlock()
	if writeErr != nil {
		transport.fail(fmt.Errorf("write MCP stdio request: %w", writeErr))
	}

	select {
	case received := <-reply:
		return decodeRPCReply(received, result)
	case <-ctx.Done():
		transport.removePending(id, true)
		return ctx.Err()
	case <-transport.done:
		// A response can arrive immediately before the reader observes EOF.
		// Prefer that response over the transport-wide failure notification.
		select {
		case received := <-reply:
			return decodeRPCReply(received, result)
		default:
		}
		transport.mu.Lock()
		err := transport.failure
		transport.mu.Unlock()
		return err
	}
}

func decodeRPCReply(received rpcReply, result any) error {
	if received.err != nil {
		return received.err
	}
	if received.response.Error != nil {
		return &RPCError{Code: received.response.Error.Code, Message: received.response.Error.Message}
	}
	if len(received.response.Result) == 0 {
		return fmt.Errorf("%w: response omitted result", ErrProtocol)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(received.response.Result, result); err != nil {
		return fmt.Errorf("%w: malformed result", ErrProtocol)
	}
	return nil
}

func (transport *StdioTransport) readResponses() {
	reader := bufio.NewReaderSize(transport.stdout, transport.maxBytes+1)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) || len(line) > transport.maxBytes+1 {
			transport.fail(ErrResponseTooLarge)
			return
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) == 0 {
				transport.fail(fmt.Errorf("%w: stdio stream ended", ErrProtocol))
			} else {
				transport.fail(fmt.Errorf("%w: read stdio response", ErrProtocol))
			}
			return
		}
		var response jsonRPCResponse
		if json.Unmarshal(line, &response) != nil {
			transport.fail(fmt.Errorf("%w: malformed response", ErrProtocol))
			return
		}
		if response.JSONRPC != "2.0" || response.ID <= 0 {
			transport.fail(fmt.Errorf("%w: invalid response envelope", ErrProtocol))
			return
		}
		transport.mu.Lock()
		waiting, ok := transport.pending[response.ID]
		if ok {
			delete(transport.pending, response.ID)
		}
		_, wasAbandoned := transport.abandoned[response.ID]
		if wasAbandoned {
			delete(transport.abandoned, response.ID)
		}
		transport.mu.Unlock()
		if wasAbandoned {
			continue
		}
		if !ok {
			transport.fail(fmt.Errorf("%w: response ID is not pending", ErrProtocol))
			return
		}
		waiting <- rpcReply{response: response}
	}
}

func (transport *StdioTransport) removePending(id int64, abandon bool) {
	transport.mu.Lock()
	if _, exists := transport.pending[id]; exists {
		delete(transport.pending, id)
		if abandon {
			transport.abandoned[id] = struct{}{}
		}
	}
	transport.mu.Unlock()
}

func (transport *StdioTransport) fail(cause error) {
	transport.mu.Lock()
	if transport.failure != nil {
		transport.mu.Unlock()
		return
	}
	transport.failure = cause
	pending := transport.pending
	transport.pending = make(map[int64]chan rpcReply)
	transport.abandoned = make(map[int64]struct{})
	close(transport.done)
	transport.mu.Unlock()
	for _, waiting := range pending {
		waiting <- rpcReply{err: cause}
	}
}

func (transport *StdioTransport) Close() error {
	var closeErr error
	transport.closeOnce.Do(func() {
		transport.fail(errStdioClosed)
		closeErr = transport.process.Close()
	})
	return closeErr
}

func (transport *StdioTransport) pendingCount() int {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return len(transport.pending)
}

func cloneStdioConfig(config StdioProcessConfig) StdioProcessConfig {
	config.Args = append([]string(nil), config.Args...)
	config.Environment = cloneMap(config.Environment)
	return config
}
