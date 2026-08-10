package runtimeapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"

	"cyber-code/internal/productprotocol"
)

var (
	ErrTerminalSessionExists   = errors.New("terminal_session_exists")
	ErrTerminalSessionNotFound = errors.New("terminal_session_not_found")
	ErrTerminalInputSequence   = errors.New("terminal_input_sequence")
)

type TerminalLaunch struct {
	ProfileID        string
	Program          string
	Arguments        []string
	WorkingDirectory string
	Columns          int
	Rows             int
}

type TerminalProcess interface {
	io.ReadWriteCloser
	PID() string
	Resize(columns, rows int) error
	Kill() error
	Wait() (exitCode int, err error)
}

type TerminalBackend interface {
	Start(ctx context.Context, launch TerminalLaunch) (TerminalProcess, error)
}

type terminalInputCommand struct {
	SessionID             string `json:"sessionId"`
	Sequence              int    `json:"sequence"`
	Data                  string `json:"data"`
	ByteLength            int    `json:"byteLength"`
	ExpectedLeaseRevision int    `json:"expectedLeaseRevision"`
}

type terminalResizeCommand struct {
	SessionID             string `json:"sessionId"`
	Columns               int    `json:"columns"`
	Rows                  int    `json:"rows"`
	ExpectedLeaseRevision int    `json:"expectedLeaseRevision"`
}

type terminalCancelCommand struct {
	SessionID             string `json:"sessionId"`
	ExpectedLeaseRevision int    `json:"expectedLeaseRevision"`
}

type managedTerminal struct {
	mu                  sync.Mutex
	process             TerminalProcess
	taskID              string
	sessionID           string
	ownerClientID       string
	scopeID             string
	leaseRevision       int
	nextInputSequence   int
	nextOutputSequence  int
	outputBytes         int
	outputReceivedBytes int
	outputLimitBytes    int
	sanitizer           *terminalOutputSanitizer
	closed              bool
	exitReason          string
	done                chan struct{}
}

type TerminalManager struct {
	service  *Service
	backend  TerminalBackend
	mu       sync.RWMutex
	sessions map[string]*managedTerminal
	used     map[string]struct{}
}

func NewTerminalManager(service *Service, backend TerminalBackend) *TerminalManager {
	return &TerminalManager{service: service, backend: backend, sessions: make(map[string]*managedTerminal), used: make(map[string]struct{})}
}

func (manager *TerminalManager) Open(ctx context.Context, taskID, clientID string, command terminalOpenCommand, profile TerminalProfile) error {
	if manager == nil || manager.service == nil || manager.backend == nil {
		return ErrTerminalUnavailable
	}
	manager.mu.Lock()
	if _, exists := manager.used[command.SessionID]; exists {
		manager.mu.Unlock()
		return ErrTerminalSessionExists
	}
	manager.mu.Unlock()
	process, err := manager.backend.Start(context.Background(), TerminalLaunch{
		ProfileID: command.ProfileID, Program: profile.Program, Arguments: append([]string(nil), profile.Arguments...),
		WorkingDirectory: command.WorkingDirectory, Columns: command.Columns, Rows: command.Rows,
	})
	if err != nil {
		return fmt.Errorf("start terminal profile: %w", err)
	}
	session := &managedTerminal{
		process: process, taskID: taskID, sessionID: command.SessionID, ownerClientID: clientID, scopeID: command.ScopeID,
		leaseRevision: command.ExpectedLeaseRevision, nextInputSequence: 1, nextOutputSequence: 1,
		outputLimitBytes: command.OutputLimitBytes, sanitizer: newTerminalOutputSanitizer(), exitReason: "exited", done: make(chan struct{}),
	}
	manager.mu.Lock()
	if _, exists := manager.used[command.SessionID]; exists {
		manager.mu.Unlock()
		_ = process.Kill()
		_ = process.Close()
		return ErrTerminalSessionExists
	}
	manager.sessions[command.SessionID] = session
	manager.used[command.SessionID] = struct{}{}
	manager.mu.Unlock()
	_, err = manager.service.emit(ctx, taskID, "terminal.opened", map[string]any{"session": map[string]any{
		"id": command.SessionID, "profileId": command.ProfileID, "processId": process.PID(),
		"workingDirectory": command.WorkingDirectory, "scopeId": command.ScopeID, "ownerClientId": clientID,
		"leaseRevision": command.ExpectedLeaseRevision, "columns": command.Columns, "rows": command.Rows,
		"outputLimitBytes": command.OutputLimitBytes,
	}}, productprotocol.EventSourceRef{})
	if err != nil {
		manager.discard(command.SessionID)
		_ = process.Kill()
		_ = process.Close()
		return err
	}
	go manager.readOutput(session)
	go manager.wait(session)
	return nil
}

func (manager *TerminalManager) Input(ctx context.Context, taskID, clientID string, command terminalInputCommand) error {
	session, err := manager.session(command.SessionID, taskID, clientID, command.ExpectedLeaseRevision)
	if err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(command.Data)
	if err != nil || len(data) != command.ByteLength {
		return ErrInvalidCommandEnvelope
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return ErrTerminalSessionNotFound
	}
	if command.Sequence != session.nextInputSequence {
		return ErrTerminalInputSequence
	}
	if _, err := session.process.Write(data); err != nil {
		return fmt.Errorf("write terminal input: %w", err)
	}
	digest := sha256.Sum256(data)
	_, err = manager.service.emit(ctx, taskID, "terminal.input.accepted", map[string]any{
		"sessionId": command.SessionID, "sequence": command.Sequence, "byteLength": len(data), "sha256": hex.EncodeToString(digest[:]),
	}, productprotocol.EventSourceRef{})
	if err != nil {
		session.exitReason = "audit_failed"
		_ = session.process.Kill()
		return err
	}
	session.nextInputSequence++
	return nil
}

func (manager *TerminalManager) Resize(ctx context.Context, taskID, clientID string, command terminalResizeCommand) error {
	session, err := manager.session(command.SessionID, taskID, clientID, command.ExpectedLeaseRevision)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return ErrTerminalSessionNotFound
	}
	if err := session.process.Resize(command.Columns, command.Rows); err != nil {
		return fmt.Errorf("resize terminal: %w", err)
	}
	_, err = manager.service.emit(ctx, taskID, "terminal.resized", map[string]any{"sessionId": command.SessionID, "columns": command.Columns, "rows": command.Rows}, productprotocol.EventSourceRef{})
	if err != nil {
		session.exitReason = "audit_failed"
		_ = session.process.Kill()
	}
	return err
}

func (manager *TerminalManager) Cancel(_ context.Context, taskID, clientID string, command terminalCancelCommand) error {
	session, err := manager.session(command.SessionID, taskID, clientID, command.ExpectedLeaseRevision)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return ErrTerminalSessionNotFound
	}
	session.exitReason = "cancelled"
	return session.process.Kill()
}

func (manager *TerminalManager) readOutput(session *managedTerminal) {
	buffer := make([]byte, 32*1024)
	for {
		count, err := session.process.Read(buffer)
		if count > 0 {
			manager.commitOutput(session, buffer[:count])
		}
		if err != nil {
			manager.flushOutput(session)
			return
		}
	}
}

func (manager *TerminalManager) commitOutput(session *managedTerminal, data []byte) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return
	}
	remaining := session.outputLimitBytes - session.outputReceivedBytes
	if remaining <= 0 {
		session.exitReason = "output_limit"
		_ = session.process.Kill()
		return
	}
	if len(data) > remaining {
		data = data[:remaining]
	}
	session.outputReceivedBytes += len(data)
	clean := session.sanitizer.Push(data)
	if len(clean) > 0 && !manager.emitOutputLocked(session, clean) {
		return
	}
	if session.outputReceivedBytes >= session.outputLimitBytes {
		session.exitReason = "output_limit"
		_ = session.process.Kill()
	}
}

func (manager *TerminalManager) flushOutput(session *managedTerminal) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return
	}
	clean := session.sanitizer.Flush()
	if len(clean) > 0 {
		manager.emitOutputLocked(session, clean)
	}
}

func (manager *TerminalManager) emitOutputLocked(session *managedTerminal, data []byte) bool {
	sequence := session.nextOutputSequence
	_, err := manager.service.emit(context.Background(), session.taskID, "terminal.output", map[string]any{
		"sessionId": session.sessionID, "sequence": sequence, "data": base64.StdEncoding.EncodeToString(data), "byteLength": len(data),
	}, productprotocol.EventSourceRef{})
	if err != nil {
		session.exitReason = "audit_failed"
		_ = session.process.Kill()
		return false
	}
	session.outputBytes += len(data)
	session.nextOutputSequence++
	return true
}

func (manager *TerminalManager) wait(session *managedTerminal) {
	exitCode, waitErr := session.process.Wait()
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return
	}
	session.closed = true
	reason := session.exitReason
	if waitErr != nil && reason == "exited" {
		reason = "process_error"
	}
	session.mu.Unlock()
	_, _ = manager.service.emit(context.Background(), session.taskID, "terminal.exited", map[string]any{
		"sessionId": session.sessionID, "exitCode": exitCode, "reason": reason,
	}, productprotocol.EventSourceRef{})
	_ = session.process.Close()
	manager.remove(session.sessionID)
	close(session.done)
}

func (manager *TerminalManager) Close(ctx context.Context) error {
	if manager == nil {
		return nil
	}
	manager.mu.RLock()
	sessions := make([]*managedTerminal, 0, len(manager.sessions))
	for _, session := range manager.sessions {
		sessions = append(sessions, session)
	}
	manager.mu.RUnlock()
	var closeErrors []error
	for _, session := range sessions {
		session.mu.Lock()
		if !session.closed {
			session.exitReason = "runtime_closed"
			if err := session.process.Kill(); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
		session.mu.Unlock()
	}
	for _, session := range sessions {
		select {
		case <-session.done:
		case <-ctx.Done():
			closeErrors = append(closeErrors, ctx.Err())
			return errors.Join(closeErrors...)
		}
	}
	return errors.Join(closeErrors...)
}

func (manager *TerminalManager) session(sessionID, taskID, clientID string, leaseRevision int) (*managedTerminal, error) {
	manager.mu.RLock()
	session := manager.sessions[sessionID]
	manager.mu.RUnlock()
	if session == nil || session.taskID != taskID {
		return nil, ErrTerminalSessionNotFound
	}
	events, state, err := manager.service.Store().Load(context.Background(), taskID)
	if err != nil {
		return nil, err
	}
	if state.Scope == nil || state.Scope.ID != session.scopeID || !scopeWasConfirmed(events, session.scopeID) {
		revokeTerminal(session, "scope_revoked")
		return nil, ErrScopeNotConfirmed
	}
	if state.ControlLease == nil || state.ControlLease.ClientID != clientID || session.ownerClientID != clientID {
		revokeTerminal(session, "control_revoked")
		return nil, ErrTerminalControlRequired
	}
	if state.ControlLease.Revision != leaseRevision || session.leaseRevision != leaseRevision {
		revokeTerminal(session, "control_revoked")
		return nil, ErrStaleLease
	}
	return session, nil
}

func revokeTerminal(session *managedTerminal, reason string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return
	}
	session.exitReason = reason
	_ = session.process.Kill()
}

func (manager *TerminalManager) remove(sessionID string) {
	manager.mu.Lock()
	delete(manager.sessions, sessionID)
	manager.mu.Unlock()
}

func (manager *TerminalManager) discard(sessionID string) {
	manager.mu.Lock()
	delete(manager.sessions, sessionID)
	delete(manager.used, sessionID)
	manager.mu.Unlock()
}
