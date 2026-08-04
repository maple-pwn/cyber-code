package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	toolpkg "cyber-code/internal/tool"
)

func TestStdioTransportRunsManagerProtocolAndClosesProcess(t *testing.T) {
	starter := &pipeStarter{serve: serveMCPStdio}
	registry := toolpkg.NewRegistry()
	factory := NewDefaultTransportFactory(DefaultTransportOptions{StdioStarter: starter})
	manager, err := NewManager(ManagerOptions{
		Registry: registry, Authorizer: allowAllAuthorizer(), TransportFactory: factory, CallTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	config := ServerConfig{
		Name: "local", Transport: TransportStdio, Command: "fake-mcp", Args: []string{"--stdio"},
		Workspace: t.TempDir(), Environment: map[string]string{"LANG": "C"}, ReadOnlyTools: map[string]bool{"echo": true},
	}
	if err := manager.Connect(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	bridge, ok := registry.Get("mcp__local__echo")
	if !ok {
		t.Fatal("stdio tool was not registered")
	}
	result, err := bridge.Run(context.Background(), json.RawMessage(`{"value":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content[0].Text != "hello" {
		t.Fatalf("result = %#v", result)
	}
	read, err := manager.ReadResource(context.Background(), "local", "test://stdio")
	if err != nil || read.Contents[0].Text != "stdio resource" {
		t.Fatalf("read = %#v, error = %v", read, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	process := starter.lastProcess()
	if process == nil || !process.isClosed() {
		t.Fatal("stdio process was not closed")
	}
	started := starter.lastConfig()
	if started.Command != "fake-mcp" || len(started.Args) != 1 || started.Args[0] != "--stdio" || started.Environment["LANG"] != "C" {
		t.Fatalf("process config = %#v", started)
	}
}

func TestStdioTransportMatchesConcurrentResponsesByID(t *testing.T) {
	starter := &pipeStarter{serve: func(scanner *bufio.Scanner, output io.Writer) {
		var requests []jsonRPCRequest
		for scanner.Scan() {
			var request jsonRPCRequest
			if json.Unmarshal(scanner.Bytes(), &request) == nil {
				requests = append(requests, request)
			}
			if len(requests) == 2 {
				for index := len(requests) - 1; index >= 0; index-- {
					request := requests[index]
					writeRPCResponse(output, request.ID, map[string]any{"value": request.Method})
				}
				return
			}
		}
	}}
	transport := newTestStdioTransport(t, starter)
	defer transport.Close()
	type response struct {
		Value string `json:"value"`
	}
	results := make(chan response, 2)
	errorsFound := make(chan error, 2)
	for _, method := range []string{"first", "second"} {
		method := method
		go func() {
			var result response
			err := transport.Call(context.Background(), method, map[string]any{}, &result)
			results <- result
			errorsFound <- err
		}()
	}
	seen := map[string]bool{}
	for range 2 {
		if err := <-errorsFound; err != nil {
			t.Fatal(err)
		}
		seen[(<-results).Value] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Fatalf("results = %#v", seen)
	}
}

func TestStdioTransportCancellationRemovesPendingCall(t *testing.T) {
	received := make(chan struct{})
	starter := &pipeStarter{serve: func(scanner *bufio.Scanner, _ io.Writer) {
		if scanner.Scan() {
			close(received)
			time.Sleep(100 * time.Millisecond)
		}
	}}
	transport := newTestStdioTransport(t, starter)
	defer transport.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := transport.Call(ctx, "slow", nil, &map[string]any{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	<-received
	if pending := transport.pendingCount(); pending != 0 {
		t.Fatalf("pending calls = %d", pending)
	}
}

func TestStdioTransportIgnoresLateResponseForCanceledCall(t *testing.T) {
	starter := &pipeStarter{serve: func(scanner *bufio.Scanner, output io.Writer) {
		if !scanner.Scan() {
			return
		}
		var first jsonRPCRequest
		_ = json.Unmarshal(scanner.Bytes(), &first)
		time.Sleep(20 * time.Millisecond)
		writeRPCResponse(output, first.ID, map[string]any{"value": "late"})
		if !scanner.Scan() {
			return
		}
		var second jsonRPCRequest
		_ = json.Unmarshal(scanner.Bytes(), &second)
		writeRPCResponse(output, second.ID, map[string]any{"value": "healthy"})
	}}
	transport := newTestStdioTransport(t, starter)
	defer transport.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := transport.Call(ctx, "first", nil, &map[string]any{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first error = %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	var result struct {
		Value string `json:"value"`
	}
	if err := transport.Call(context.Background(), "second", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result.Value != "healthy" {
		t.Fatalf("result = %#v", result)
	}
}

func TestStdioTransportDoesNotWriteCanceledCall(t *testing.T) {
	received := make(chan struct{}, 1)
	starter := &pipeStarter{serve: func(scanner *bufio.Scanner, _ io.Writer) {
		if scanner.Scan() {
			received <- struct{}{}
		}
	}}
	transport := newTestStdioTransport(t, starter)
	defer transport.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := transport.Call(ctx, "canceled", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	select {
	case <-received:
		t.Fatal("canceled call was written to process")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestStdioTransportInvalidJSONFailsConnectionAndWaitingCalls(t *testing.T) {
	starter := &pipeStarter{serve: func(scanner *bufio.Scanner, output io.Writer) {
		if scanner.Scan() {
			_, _ = io.WriteString(output, "not-json\n")
		}
	}}
	transport := newTestStdioTransport(t, starter)
	defer transport.Close()
	err := transport.Call(context.Background(), "initialize", nil, &InitializeResult{})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("error = %v", err)
	}
	err = transport.Call(context.Background(), "initialize", nil, &InitializeResult{})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("subsequent error = %v", err)
	}
}

func TestStdioTransportRejectsOversizedMessage(t *testing.T) {
	starter := &pipeStarter{serve: func(scanner *bufio.Scanner, output io.Writer) {
		if scanner.Scan() {
			_, _ = io.WriteString(output, strings.Repeat("x", 128)+"\n")
		}
	}}
	transport, err := NewStdioTransport(context.Background(), StdioOptions{Starter: starter, Config: StdioProcessConfig{Command: "fake"}, MaxMessageBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	err = transport.Call(context.Background(), "initialize", nil, &InitializeResult{})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestDefaultFactoryStartsRealManagedStdioProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	registry := toolpkg.NewRegistry()
	manager, err := NewManager(ManagerOptions{
		Registry: registry, Authorizer: allowAllAuthorizer(),
		TransportFactory: NewDefaultTransportFactory(DefaultTransportOptions{}), CallTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.Connect(context.Background(), ServerConfig{
		Name: "real", Transport: TransportStdio, Command: executable,
		Args: []string{"-test.run=^TestMCPStdioProcessHelper$", "--", "mcp-helper"}, Workspace: workspace,
	}); err != nil {
		t.Fatal(err)
	}
	bridge, ok := registry.Get("mcp__real__echo")
	if !ok {
		t.Fatal("real stdio process tool was not registered")
	}
	result, err := bridge.Run(context.Background(), json.RawMessage(`{"value":"managed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content[0].Text != "managed" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMCPStdioProcessHelper(t *testing.T) {
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) && os.Args[index+1] == "mcp-helper" {
			serveMCPStdio(bufio.NewScanner(os.Stdin), os.Stdout)
			return
		}
	}
}

func newTestStdioTransport(t *testing.T, starter StdioStarter) *StdioTransport {
	t.Helper()
	transport, err := NewStdioTransport(context.Background(), StdioOptions{Starter: starter, Config: StdioProcessConfig{Command: "fake"}})
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func serveMCPStdio(scanner *bufio.Scanner, output io.Writer) {
	for scanner.Scan() {
		var request jsonRPCRequest
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = InitializeResult{ProtocolVersion: ProtocolVersion, ServerInfo: Implementation{Name: "stdio-test", Version: "1"}}
		case "tools/list":
			result = ListToolsResult{Tools: []Tool{{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
		case "resources/list":
			result = ListResourcesResult{Resources: []Resource{{URI: "test://stdio", Name: "stdio"}}}
		case "tools/call":
			var params CallToolParams
			_ = assignJSON(&params, request.Params)
			var arguments map[string]string
			_ = json.Unmarshal(params.Arguments, &arguments)
			result = CallToolResult{Content: []Content{{Type: "text", Text: arguments["value"]}}}
		case "resources/read":
			result = ReadResourceResult{Contents: []ResourceContent{{URI: "test://stdio", Text: "stdio resource"}}}
		default:
			return
		}
		writeRPCResponse(output, request.ID, result)
	}
}

func writeRPCResponse(output io.Writer, id int64, result any) {
	data, _ := json.Marshal(result)
	_ = json.NewEncoder(output).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: data})
}

type pipeStarter struct {
	mu      sync.Mutex
	serve   func(*bufio.Scanner, io.Writer)
	configs []StdioProcessConfig
	process []*pipeProcess
}

func (starter *pipeStarter) Start(_ context.Context, config StdioProcessConfig) (StdioProcess, error) {
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	process := &pipeProcess{stdin: clientInput, stdout: clientOutput, closeServerInput: serverInput, closeServerOutput: serverOutput}
	starter.mu.Lock()
	starter.configs = append(starter.configs, config)
	starter.process = append(starter.process, process)
	starter.mu.Unlock()
	go func() {
		starter.serve(bufio.NewScanner(serverInput), serverOutput)
		_ = serverOutput.Close()
		_ = serverInput.Close()
	}()
	return process, nil
}

func (starter *pipeStarter) lastProcess() *pipeProcess {
	starter.mu.Lock()
	defer starter.mu.Unlock()
	if len(starter.process) == 0 {
		return nil
	}
	return starter.process[len(starter.process)-1]
}

func (starter *pipeStarter) lastConfig() StdioProcessConfig {
	starter.mu.Lock()
	defer starter.mu.Unlock()
	return starter.configs[len(starter.configs)-1]
}

type pipeProcess struct {
	mu                sync.Mutex
	stdin             *io.PipeWriter
	stdout            *io.PipeReader
	closeServerInput  *io.PipeReader
	closeServerOutput *io.PipeWriter
	closed            bool
}

func (process *pipeProcess) Stdin() io.WriteCloser { return process.stdin }
func (process *pipeProcess) Stdout() io.ReadCloser { return process.stdout }
func (process *pipeProcess) Close() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return nil
	}
	process.closed = true
	_ = process.stdin.Close()
	_ = process.stdout.Close()
	_ = process.closeServerInput.Close()
	_ = process.closeServerOutput.Close()
	return nil
}
func (process *pipeProcess) isClosed() bool {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.closed
}
