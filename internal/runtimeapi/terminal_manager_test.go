package runtimeapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

func TestTerminalManagerOwnsLifecycleAndAuditsInputWithoutContent(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	process := newFakeTerminalProcess()
	manager := NewTerminalManager(server.service, &fakeTerminalBackend{process: process})
	command := terminalOpenCommand{SessionID: "terminal-managed", ProfileID: "default-shell", WorkingDirectory: workspace, ScopeID: "scope-terminal", Columns: 80, Rows: 24, OutputLimitBytes: 1024, ExpectedLeaseRevision: 1}
	profile := server.terminalProfiles[command.ProfileID]
	if err := manager.Open(context.Background(), taskID, "client-1", command, profile); err != nil {
		t.Fatal(err)
	}

	input := terminalInputCommand{SessionID: command.SessionID, Sequence: 1, Data: base64.StdEncoding.EncodeToString([]byte("ls\n")), ByteLength: 3, ExpectedLeaseRevision: 1}
	if err := manager.Input(context.Background(), taskID, "client-1", input); err != nil {
		t.Fatal(err)
	}
	if err := manager.Resize(context.Background(), taskID, "client-1", terminalResizeCommand{SessionID: command.SessionID, Columns: 100, Rows: 30, ExpectedLeaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	process.output <- []byte("ok\n")
	waitForTerminalState(t, server.service, taskID, func(state terminalStateView) bool { return state.OutputBytes == 3 })
	if err := manager.Cancel(context.Background(), taskID, "client-1", terminalCancelCommand{SessionID: command.SessionID, ExpectedLeaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	waitForTerminalState(t, server.service, taskID, func(state terminalStateView) bool { return state.Status == "exited" })

	if got := process.input.String(); got != "ls\n" {
		t.Fatalf("process input = %q", got)
	}
	if process.columns != 100 || process.rows != 30 || !process.killed {
		t.Fatalf("process state = columns %d rows %d killed %v", process.columns, process.rows, process.killed)
	}
	events, _, err := server.service.Store().Load(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type != "terminal.input.accepted" {
			continue
		}
		if bytes.Contains(event.Payload, []byte("ls")) || bytes.Contains(event.Payload, []byte(input.Data)) {
			t.Fatalf("input audit leaked terminal content: %s", event.Payload)
		}
		var payload struct {
			ByteLength int    `json:"byteLength"`
			SHA256     string `json:"sha256"`
		}
		if json.Unmarshal(event.Payload, &payload) != nil || payload.ByteLength != 3 || len(payload.SHA256) != 64 {
			t.Fatalf("input audit payload = %s", event.Payload)
		}
		return
	}
	t.Fatal("missing terminal input audit event")
}

func TestTerminalManagerKillsProcessAtOutputLimit(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	process := newFakeTerminalProcess()
	manager := NewTerminalManager(server.service, &fakeTerminalBackend{process: process})
	command := terminalOpenCommand{SessionID: "terminal-capped", ProfileID: "default-shell", WorkingDirectory: workspace, ScopeID: "scope-terminal", Columns: 80, Rows: 24, OutputLimitBytes: 4, ExpectedLeaseRevision: 1}
	if err := manager.Open(context.Background(), taskID, "client-1", command, server.terminalProfiles[command.ProfileID]); err != nil {
		t.Fatal(err)
	}
	process.output <- []byte("12345")
	waitForTerminalState(t, server.service, taskID, func(state terminalStateView) bool { return state.Status == "exited" })
	if !process.killed {
		t.Fatal("output-capped process was not killed")
	}
	state := currentTerminalState(t, server.service, taskID)
	if state.OutputBytes > 4 || state.ExitReason != "output_limit" {
		t.Fatalf("capped terminal state = %+v", state)
	}
}

func TestTerminalCommandsDispatchThroughRuntimeOwnedManager(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	process := newFakeTerminalProcess()
	server.terminalManager = NewTerminalManager(server.service, &fakeTerminalBackend{process: process})
	commands := []map[string]any{
		{"type": "terminal.open", "sessionId": "terminal-dispatch", "profileId": "default-shell", "workingDirectory": workspace, "scopeId": "scope-terminal", "columns": 80, "rows": 24, "outputLimitBytes": 1024, "expectedLeaseRevision": 1},
		{"type": "terminal.input", "sessionId": "terminal-dispatch", "sequence": 1, "data": "bHMK", "byteLength": 3, "expectedLeaseRevision": 1},
		{"type": "terminal.resize", "sessionId": "terminal-dispatch", "columns": 100, "rows": 30, "expectedLeaseRevision": 1},
		{"type": "terminal.cancel", "sessionId": "terminal-dispatch", "expectedLeaseRevision": 1},
	}
	for index, command := range commands {
		envelope := mustLocalEnvelope(t, fmt.Sprintf("terminal-dispatch-%d", index), command)
		response := server.handleAuthorizedForClient(context.Background(), LocalRequest{ID: "dispatch", Type: "command", TaskID: taskID, Command: &envelope}, "client-1")
		if response.Receipt == nil || response.Receipt.Status != "accepted" {
			t.Fatalf("command %v receipt = %+v", command["type"], response.Receipt)
		}
	}
}

func TestTerminalManagerRevokesMutationAfterControlTransfer(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	process := newFakeTerminalProcess()
	manager := NewTerminalManager(server.service, &fakeTerminalBackend{process: process})
	command := terminalOpenCommand{SessionID: "terminal-revoked", ProfileID: "default-shell", WorkingDirectory: workspace, ScopeID: "scope-terminal", Columns: 80, Rows: 24, OutputLimitBytes: 1024, ExpectedLeaseRevision: 1}
	if err := manager.Open(context.Background(), taskID, "client-1", command, server.terminalProfiles[command.ProfileID]); err != nil {
		t.Fatal(err)
	}
	if _, err := server.service.TakeControl(context.Background(), taskID, "client-2", 1); err != nil {
		t.Fatal(err)
	}
	err := manager.Input(context.Background(), taskID, "client-1", terminalInputCommand{SessionID: command.SessionID, Sequence: 1, Data: "bHMK", ByteLength: 3, ExpectedLeaseRevision: 1})
	if !errors.Is(err, ErrTerminalControlRequired) {
		t.Fatalf("revoked terminal input error = %v", err)
	}
	waitForTerminalState(t, server.service, taskID, func(state terminalStateView) bool { return state.Status == "exited" })
}

func TestTerminalManagerCloseTerminatesAndCleansLiveSessions(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	process := newFakeTerminalProcess()
	manager := NewTerminalManager(server.service, &fakeTerminalBackend{process: process})
	command := terminalOpenCommand{SessionID: "terminal-close", ProfileID: "default-shell", WorkingDirectory: workspace, ScopeID: "scope-terminal", Columns: 80, Rows: 24, OutputLimitBytes: 1024, ExpectedLeaseRevision: 1}
	if err := manager.Open(context.Background(), taskID, "client-1", command, server.terminalProfiles[command.ProfileID]); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForTerminalState(t, server.service, taskID, func(state terminalStateView) bool {
		return state.Status == "exited" && state.ExitReason == "runtime_closed"
	})
	manager.mu.RLock()
	live := len(manager.sessions)
	manager.mu.RUnlock()
	if live != 0 || !process.killed {
		t.Fatalf("manager cleanup live=%d killed=%v", live, process.killed)
	}
}

func TestTerminalManagerDiscardsUncommittedSessionIDAfterOpenAuditFailure(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	firstProcess := newFakeTerminalProcess()
	backend := &fakeTerminalBackend{process: firstProcess}
	manager := NewTerminalManager(server.service, backend)
	command := terminalOpenCommand{SessionID: "terminal-retry", ProfileID: "default-shell", WorkingDirectory: workspace, ScopeID: "scope-terminal", Columns: 80, Rows: 24, OutputLimitBytes: 1024, ExpectedLeaseRevision: 1}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Open(cancelled, taskID, "client-1", command, server.terminalProfiles[command.ProfileID]); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled terminal open error = %v", err)
	}
	if !firstProcess.killed {
		t.Fatal("uncommitted terminal process was not killed")
	}

	secondProcess := newFakeTerminalProcess()
	backend.process = secondProcess
	if err := manager.Open(context.Background(), taskID, "client-1", command, server.terminalProfiles[command.ProfileID]); err != nil {
		t.Fatalf("retry terminal open = %v", err)
	}
	if err := manager.Cancel(context.Background(), taskID, "client-1", terminalCancelCommand{SessionID: command.SessionID, ExpectedLeaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	waitForTerminalState(t, server.service, taskID, func(state terminalStateView) bool { return state.Status == "exited" })
}

func TestTerminalManagerRevokesSessionWhenResizeAuditFails(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	process := newFakeTerminalProcess()
	manager := NewTerminalManager(server.service, &fakeTerminalBackend{process: process})
	command := terminalOpenCommand{SessionID: "terminal-resize-audit", ProfileID: "default-shell", WorkingDirectory: workspace, ScopeID: "scope-terminal", Columns: 80, Rows: 24, OutputLimitBytes: 1024, ExpectedLeaseRevision: 1}
	if err := manager.Open(context.Background(), taskID, "client-1", command, server.terminalProfiles[command.ProfileID]); err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err := manager.Resize(cancelled, taskID, "client-1", terminalResizeCommand{SessionID: command.SessionID, Columns: 100, Rows: 30, ExpectedLeaseRevision: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resize error = %v", err)
	}
	waitForTerminalState(t, server.service, taskID, func(state terminalStateView) bool {
		return state.Status == "exited" && state.ExitReason == "audit_failed"
	})
	if !process.killed {
		t.Fatal("terminal survived resize audit failure")
	}
}

type fakeTerminalBackend struct{ process *fakeTerminalProcess }

func (backend *fakeTerminalBackend) Start(context.Context, TerminalLaunch) (TerminalProcess, error) {
	return backend.process, nil
}

type fakeTerminalProcess struct {
	mu      sync.Mutex
	input   bytes.Buffer
	output  chan []byte
	done    chan struct{}
	killed  bool
	columns int
	rows    int
}

func newFakeTerminalProcess() *fakeTerminalProcess {
	return &fakeTerminalProcess{output: make(chan []byte, 4), done: make(chan struct{})}
}
func (process *fakeTerminalProcess) PID() string { return "fake-42" }
func (process *fakeTerminalProcess) Read(buffer []byte) (int, error) {
	select {
	case data := <-process.output:
		return copy(buffer, data), nil
	case <-process.done:
		return 0, io.EOF
	}
}
func (process *fakeTerminalProcess) Write(data []byte) (int, error) {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.input.Write(data)
}
func (process *fakeTerminalProcess) Resize(columns, rows int) error {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.columns, process.rows = columns, rows
	return nil
}
func (process *fakeTerminalProcess) Kill() error {
	process.mu.Lock()
	process.killed = true
	process.mu.Unlock()
	select {
	case <-process.done:
	default:
		close(process.done)
	}
	return nil
}
func (process *fakeTerminalProcess) Wait() (int, error) { <-process.done; return 130, nil }
func (process *fakeTerminalProcess) Close() error       { return process.Kill() }

type terminalStateView struct {
	Status      string
	OutputBytes int
	ExitReason  string
}

func currentTerminalState(t *testing.T, service *Service, taskID string) terminalStateView {
	t.Helper()
	_, state, err := service.Store().Load(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, terminal := range state.Terminals {
		return terminalStateView{Status: terminal.Status, OutputBytes: terminal.OutputBytes, ExitReason: terminal.ExitReason}
	}
	return terminalStateView{}
}

func waitForTerminalState(t *testing.T, service *Service, taskID string, predicate func(terminalStateView) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate(currentTerminalState(t, service, taskID)) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("terminal state did not converge: %+v", currentTerminalState(t, service, taskID))
}
