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
	"testing"
	"time"
)

func TestClientFramesRequestsAndCorrelatesOutOfOrderResponses(t *testing.T) {
	process, server := newPipeProcess()
	client, err := NewClient(ClientOptions{Process: process})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	go func() {
		first := server.read(t)
		second := server.read(t)
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": second.ID, "result": second.Method})
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": first.ID, "result": first.Method})
	}()

	results := make(chan string, 2)
	for _, method := range []string{"first", "second"} {
		method := method
		go func() {
			var result string
			if err := client.Call(context.Background(), method, map[string]string{"value": method}, &result); err != nil {
				results <- "error: " + err.Error()
				return
			}
			results <- result
		}()
	}
	got := map[string]bool{<-results: true, <-results: true}
	if !got["first"] || !got["second"] {
		t.Fatalf("results = %#v", got)
	}
}

func TestClientCancellationSendsNotificationAndIgnoresLateResponse(t *testing.T) {
	process, server := newPipeProcess()
	client, err := NewClient(ClientOptions{Process: process})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cancelled := make(chan rpcTestMessage, 1)
	go func() {
		request := server.read(t)
		cancelled <- server.read(t)
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": "late"})
		next := server.read(t)
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": next.ID, "result": "ok"})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var ignored string
	if err := client.Call(ctx, "slow", nil, &ignored); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow error = %v", err)
	}
	notification := <-cancelled
	if notification.Method != "$/cancelRequest" || notification.ID != 0 || string(notification.Params) == "" {
		t.Fatalf("cancel notification = %#v", notification)
	}
	var result string
	if err := client.Call(context.Background(), "next", nil, &result); err != nil || result != "ok" {
		t.Fatalf("next result = %q, error = %v", result, err)
	}
}

func TestClientIgnoresResponseAfterCancellationDuringBlockedWrite(t *testing.T) {
	process, server := newPipeProcess()
	client, err := NewClient(ClientOptions{Process: process})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var ignored string
	if err := client.Call(ctx, "blocked-write", nil, &ignored); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write error = %v", err)
	}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		request := server.read(t)
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": "late"})
		for {
			message := server.read(t)
			if message.Method == "next" {
				server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": message.ID, "result": "ok"})
				return
			}
		}
	}()
	var result string
	if err := client.Call(context.Background(), "next", nil, &result); err != nil || result != "ok" {
		t.Fatalf("next result = %q, error = %v", result, err)
	}
	<-serverDone
}

func TestClientStoresDiagnosticNotificationSnapshots(t *testing.T) {
	process, server := newPipeProcess()
	client, err := NewClient(ClientOptions{Process: process})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server.write(t, map[string]interface{}{
		"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
		"params": map[string]interface{}{"uri": "file:///workspace/main.go", "diagnostics": []interface{}{
			map[string]interface{}{"message": "broken", "severity": 1, "range": map[string]interface{}{
				"start": map[string]int{"line": 2, "character": 3}, "end": map[string]int{"line": 2, "character": 4},
			}},
		}},
	})
	deadline := time.Now().Add(time.Second)
	for len(client.Diagnostics("file:///workspace/main.go")) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	first := client.Diagnostics("file:///workspace/main.go")
	if len(first) != 1 || first[0].Message != "broken" || first[0].Range.Start.Line != 2 {
		t.Fatalf("diagnostics = %#v", first)
	}
	first[0].Message = "changed"
	if client.Diagnostics("file:///workspace/main.go")[0].Message != "broken" {
		t.Fatal("diagnostics exposed mutable client state")
	}
}

func TestClientFailsPendingCallsWhenProcessEnds(t *testing.T) {
	process, server := newPipeProcess()
	client, err := NewClient(ClientOptions{Process: process})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		var result string
		done <- client.Call(context.Background(), "blocked", nil, &result)
	}()
	_ = server.read(t)
	server.closeOutput()
	select {
	case err := <-done:
		if !errors.Is(err, ErrClientClosed) {
			t.Fatalf("pending error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending call did not fail after process exit")
	}
}

func TestClientRespondsToServerRequestWithoutStealingPendingResponse(t *testing.T) {
	process, server := newPipeProcess()
	client, err := NewClient(ClientOptions{Process: process})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	go func() {
		request := server.read(t)
		server.write(t, map[string]interface{}{
			"jsonrpc": "2.0", "id": request.ID, "method": "workspace/configuration",
			"params": map[string]interface{}{"items": []interface{}{map[string]string{"section": "gopls"}}},
		})
		response := server.read(t)
		if response.ID != request.ID || response.Method != "" {
			t.Errorf("client response = %#v", response)
			return
		}
		server.write(t, map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": "complete"})
	}()
	var result string
	if err := client.Call(context.Background(), "client-request", nil, &result); err != nil || result != "complete" {
		t.Fatalf("result = %q, error = %v", result, err)
	}
}

type rpcTestMessage struct {
	ID     int64
	Method string
	Params json.RawMessage
}

type pipeProcess struct {
	stdinWriter  *io.PipeWriter
	stdoutReader *io.PipeReader
	closeOnce    sync.Once
}

func (process *pipeProcess) Stdin() io.WriteCloser { return process.stdinWriter }
func (process *pipeProcess) Stdout() io.ReadCloser { return process.stdoutReader }
func (process *pipeProcess) Close() error {
	process.closeOnce.Do(func() {
		_ = process.stdinWriter.Close()
		_ = process.stdoutReader.Close()
	})
	return nil
}

type pipeServer struct {
	input  *bufio.Reader
	output *io.PipeWriter
	mu     sync.Mutex
}

func newPipeProcess() (*pipeProcess, *pipeServer) {
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	return &pipeProcess{stdinWriter: clientInput, stdoutReader: clientOutput}, &pipeServer{input: bufio.NewReader(serverInput), output: serverOutput}
}

func (server *pipeServer) read(t *testing.T) rpcTestMessage {
	t.Helper()
	length := 0
	for {
		line, err := server.input.ReadString('\n')
		if err != nil {
			t.Errorf("read frame header: %v", err)
			return rpcTestMessage{}
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		name, value, found := strings.Cut(line, ":")
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") && found {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				t.Errorf("parse content length: %v", err)
				return rpcTestMessage{}
			}
		}
	}
	if length <= 0 {
		t.Errorf("missing Content-Length")
		return rpcTestMessage{}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(server.input, body); err != nil {
		t.Errorf("read frame body: %v", err)
		return rpcTestMessage{}
	}
	var message rpcTestMessage
	if err := json.Unmarshal(body, &message); err != nil {
		t.Errorf("decode frame: %v", err)
	}
	return message
}

func (server *pipeServer) write(t *testing.T, message interface{}) {
	t.Helper()
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if _, err := fmt.Fprintf(server.output, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		t.Errorf("write header: %v", err)
		return
	}
	if _, err := server.output.Write(payload); err != nil {
		t.Errorf("write payload: %v", err)
	}
}

func (server *pipeServer) closeOutput() { _ = server.output.Close() }
