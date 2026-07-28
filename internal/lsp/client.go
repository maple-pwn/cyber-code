package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const defaultMaxMessageBytes = 4 << 20

var (
	ErrClientClosed    = errors.New("LSP client is closed")
	ErrProtocol        = errors.New("invalid LSP protocol message")
	ErrMessageTooLarge = errors.New("LSP message exceeds size limit")
)

type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Close() error
}

type ClientOptions struct {
	Process         Process
	MaxMessageBytes int
}

type Client struct {
	process  Process
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	maxBytes int
	nextID   atomic.Int64
	outbound chan outboundMessage

	mu          sync.Mutex
	pending     map[int64]chan clientReply
	abandoned   map[int64]struct{}
	diagnostics map[string][]Diagnostic
	failure     error
	done        chan struct{}
	closeOnce   sync.Once
}

type clientReply struct {
	message rpcMessage
	err     error
}

type outboundMessage struct {
	payload []byte
	done    chan error
}

func NewClient(options ClientOptions) (*Client, error) {
	if options.Process == nil || options.Process.Stdin() == nil || options.Process.Stdout() == nil {
		return nil, fmt.Errorf("LSP process with stdin and stdout is required")
	}
	if options.MaxMessageBytes <= 0 {
		options.MaxMessageBytes = defaultMaxMessageBytes
	}
	client := &Client{
		process: options.Process, stdin: options.Process.Stdin(), stdout: options.Process.Stdout(), maxBytes: options.MaxMessageBytes,
		pending: make(map[int64]chan clientReply), abandoned: make(map[int64]struct{}),
		diagnostics: make(map[string][]Diagnostic), outbound: make(chan outboundMessage, 128), done: make(chan struct{}),
	}
	go client.writeLoop()
	go client.readLoop()
	return client, nil
}

func (client *Client) Call(ctx context.Context, method string, params, result any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(method) == "" {
		return fmt.Errorf("LSP method is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := client.nextID.Add(1)
	waiting := make(chan clientReply, 1)
	client.mu.Lock()
	if client.failure != nil {
		err := client.failure
		client.mu.Unlock()
		return err
	}
	client.pending[id] = waiting
	client.mu.Unlock()
	if err := client.send(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		abandoned := ctx.Err() != nil
		client.removePending(id, abandoned)
		if abandoned {
			client.notifyBestEffort("$/cancelRequest", map[string]int64{"id": id})
		}
		return err
	}
	select {
	case reply := <-waiting:
		if reply.err != nil {
			return reply.err
		}
		if reply.message.Error != nil {
			return &RPCError{Code: reply.message.Error.Code, Message: reply.message.Error.Message}
		}
		if reply.message.Result == nil {
			return fmt.Errorf("%w: response omitted result", ErrProtocol)
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(reply.message.Result, result); err != nil {
			return fmt.Errorf("%w: malformed result: %v", ErrProtocol, err)
		}
		return nil
	case <-ctx.Done():
		client.removePending(id, true)
		client.notifyBestEffort("$/cancelRequest", map[string]int64{"id": id})
		return ctx.Err()
	case <-client.done:
		return client.currentFailure()
	}
}

func (client *Client) notifyBestEffort(method string, params any) {
	client.enqueueBestEffort(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (client *Client) enqueueBestEffort(message any) {
	payload, err := json.Marshal(message)
	if err != nil || len(payload) > client.maxBytes {
		return
	}
	request := outboundMessage{payload: payload, done: make(chan error, 1)}
	select {
	case client.outbound <- request:
	case <-client.done:
	default:
	}
}

func (client *Client) Notify(ctx context.Context, method string, params any) error {
	if strings.TrimSpace(method) == "" {
		return fmt.Errorf("LSP method is required")
	}
	return client.send(ctx, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (client *Client) Diagnostics(uri string) []Diagnostic {
	client.mu.Lock()
	defer client.mu.Unlock()
	return append([]Diagnostic(nil), client.diagnostics[uri]...)
}

func (client *Client) Close() error {
	var closeErr error
	client.closeOnce.Do(func() {
		client.fail(ErrClientClosed)
		closeErr = client.process.Close()
	})
	return closeErr
}

func (client *Client) send(ctx context.Context, message any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode LSP message: %w", err)
	}
	if len(payload) > client.maxBytes {
		return ErrMessageTooLarge
	}
	request := outboundMessage{payload: payload, done: make(chan error, 1)}
	select {
	case client.outbound <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-client.done:
		return client.currentFailure()
	}
	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-client.done:
		return client.currentFailure()
	}
}

func (client *Client) writeLoop() {
	for {
		select {
		case request := <-client.outbound:
			if _, err := fmt.Fprintf(client.stdin, "Content-Length: %d\r\n\r\n", len(request.payload)); err != nil {
				failure := fmt.Errorf("%w: write header: %v", ErrClientClosed, err)
				request.done <- failure
				client.fail(failure)
				return
			}
			if _, err := client.stdin.Write(request.payload); err != nil {
				failure := fmt.Errorf("%w: write payload: %v", ErrClientClosed, err)
				request.done <- failure
				client.fail(failure)
				return
			}
			request.done <- nil
		case <-client.done:
			return
		}
	}
}

func (client *Client) readLoop() {
	reader := bufio.NewReaderSize(client.stdout, min(client.maxBytes, 64<<10))
	for {
		payload, err := readFrame(reader, client.maxBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = fmt.Errorf("%w: stdout ended", ErrClientClosed)
			}
			client.fail(err)
			return
		}
		var message rpcMessage
		if err := json.Unmarshal(payload, &message); err != nil || message.JSONRPC != "2.0" {
			client.fail(fmt.Errorf("%w: malformed JSON-RPC envelope", ErrProtocol))
			return
		}
		if message.Method != "" {
			if len(message.ID) == 0 {
				client.handleNotification(message)
			} else {
				client.handleServerRequest(message)
			}
			continue
		}
		if len(message.ID) == 0 {
			client.fail(fmt.Errorf("%w: message omitted method and ID", ErrProtocol))
			return
		}
		var id int64
		if err := json.Unmarshal(message.ID, &id); err != nil || id <= 0 {
			client.fail(fmt.Errorf("%w: invalid response ID", ErrProtocol))
			return
		}
		client.mu.Lock()
		waiting, pending := client.pending[id]
		if pending {
			delete(client.pending, id)
		}
		_, abandoned := client.abandoned[id]
		if abandoned {
			delete(client.abandoned, id)
		}
		client.mu.Unlock()
		if abandoned {
			continue
		}
		if !pending {
			client.fail(fmt.Errorf("%w: response ID %d is not pending", ErrProtocol, id))
			return
		}
		waiting <- clientReply{message: message}
	}
}

func (client *Client) handleServerRequest(message rpcMessage) {
	response := map[string]any{"jsonrpc": "2.0", "id": message.ID}
	switch message.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(message.Params, &params)
		response["result"] = make([]any, len(params.Items))
	case "workspace/workspaceFolders":
		response["result"] = []any{}
	case "window/workDoneProgress/create", "client/registerCapability", "client/unregisterCapability":
		response["result"] = nil
	default:
		response["error"] = map[string]any{"code": -32601, "message": "method not supported"}
	}
	client.enqueueBestEffort(response)
}

func readFrame(reader *bufio.Reader, maximum int) ([]byte, error) {
	contentLength := -1
	headerBytes := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		headerBytes += len(line)
		if headerBytes > 16<<10 {
			return nil, fmt.Errorf("%w: headers too large", ErrProtocol)
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		name, value, found := strings.Cut(trimmed, ":")
		if !found {
			return nil, fmt.Errorf("%w: malformed header", ErrProtocol)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			if contentLength >= 0 {
				return nil, fmt.Errorf("%w: duplicate Content-Length", ErrProtocol)
			}
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || contentLength < 0 {
				return nil, fmt.Errorf("%w: invalid Content-Length", ErrProtocol)
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("%w: missing Content-Length", ErrProtocol)
	}
	if contentLength > maximum {
		return nil, ErrMessageTooLarge
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (client *Client) handleNotification(message rpcMessage) {
	if message.Method != "textDocument/publishDiagnostics" {
		return
	}
	var params struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if json.Unmarshal(message.Params, &params) != nil || params.URI == "" {
		return
	}
	client.mu.Lock()
	client.diagnostics[params.URI] = append([]Diagnostic(nil), params.Diagnostics...)
	client.mu.Unlock()
}

func (client *Client) removePending(id int64, abandon bool) {
	client.mu.Lock()
	if _, exists := client.pending[id]; exists {
		delete(client.pending, id)
		if abandon {
			client.abandoned[id] = struct{}{}
		}
	}
	client.mu.Unlock()
}

func (client *Client) fail(cause error) {
	client.mu.Lock()
	if client.failure != nil {
		client.mu.Unlock()
		return
	}
	client.failure = cause
	pending := client.pending
	client.pending = make(map[int64]chan clientReply)
	client.abandoned = make(map[int64]struct{})
	close(client.done)
	client.mu.Unlock()
	for _, waiting := range pending {
		waiting <- clientReply{err: cause}
	}
}

func (client *Client) currentFailure() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.failure == nil {
		return ErrClientClosed
	}
	return client.failure
}
