package runtimeapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
)

// The local bridge must carry editor.save payloads up to the 64 MiB file limit
// plus JSON/base64 framing, while retaining a bounded scanner allocation.
const defaultLocalMessageLimit = 68 * 1024 * 1024

type LocalServerOptions struct {
	Service          *Service
	Bearer           string
	Role             string
	Workspace        string
	Source           SourceMetadata
	MaxMessageBytes  int
	StateFileName    string
	TaskPrefix       string
	ClientID         string
	TerminalProfiles map[string]TerminalProfile
	TerminalBackend  TerminalBackend
}

type TerminalProfile struct {
	ID               string
	RequiredAction   string
	Risk             string
	ApprovalRequired bool
	Program          string
	Arguments        []string
}

type LocalRequest struct {
	ID                    string            `json:"id"`
	Type                  string            `json:"type"`
	Bearer                string            `json:"bearer"`
	Handshake             *HandshakeRequest `json:"handshake,omitempty"`
	Command               *CommandEnvelope  `json:"command,omitempty"`
	TaskID                string            `json:"taskId,omitempty"`
	AfterCursor           int               `json:"afterCursor,omitempty"`
	DraftID               string            `json:"draftId,omitempty"`
	ExpectedLeaseRevision int               `json:"expectedLeaseRevision,omitempty"`
}

type RuntimeSnapshot struct {
	Cursor int                `json:"cursor"`
	State  productstate.State `json:"state"`
}

type LocalResponse struct {
	ID        string                  `json:"id"`
	Type      string                  `json:"type"`
	ErrorCode string                  `json:"errorCode,omitempty"`
	Handshake *HandshakeResponse      `json:"handshake,omitempty"`
	Receipt   *CommandReceipt         `json:"receipt,omitempty"`
	Events    []productprotocol.Event `json:"events,omitempty"`
	Snapshot  *RuntimeSnapshot        `json:"snapshot,omitempty"`
	Ready     *bool                   `json:"ready,omitempty"`
	Editor    *EditorReadResponse     `json:"editor,omitempty"`
}

type EditorReadResponse struct {
	DraftID    string `json:"draftId"`
	Revision   int    `json:"revision"`
	BaseSHA256 string `json:"baseSha256"`
	Data       string `json:"data"`
	ByteLength int    `json:"byteLength"`
	Encoding   string `json:"encoding"`
}

func (response LocalResponse) MarshalJSON() ([]byte, error) {
	type responseAlias LocalResponse
	if response.Type != "events" {
		return json.Marshal(responseAlias(response))
	}
	return json.Marshal(struct {
		responseAlias
		Events []productprotocol.Event `json:"events"`
	}{
		responseAlias: responseAlias(response),
		Events:        response.Events,
	})
}

type localReceipt struct {
	Digest  string         `json:"digest"`
	Receipt CommandReceipt `json:"receipt"`
}

type localPersistentState struct {
	RuntimeID  string                  `json:"runtimeId"`
	NextTask   int                     `json:"nextTask"`
	ActiveTask string                  `json:"activeTask,omitempty"`
	Receipts   map[string]localReceipt `json:"receipts"`
}

type LocalServer struct {
	service          *Service
	bearerDigest     [sha256.Size]byte
	role             string
	workspace        string
	source           SourceMetadata
	maxMessageBytes  int
	statePath        string
	taskPrefix       string
	clientID         string
	terminalProfiles map[string]TerminalProfile
	terminalManager  *TerminalManager
	editorManager    *editorManager

	mu         sync.Mutex
	receipts   map[string]localReceipt
	nextTask   int
	activeTask string
}

func NewLocalServer(options LocalServerOptions) (*LocalServer, error) {
	return newRuntimeServer(options, SourceModeLocal)
}

func newRuntimeServer(options LocalServerOptions, requiredMode SourceMode) (*LocalServer, error) {
	if options.Service == nil {
		return nil, fmt.Errorf("runtime service is required")
	}
	if strings.TrimSpace(options.Bearer) == "" {
		return nil, fmt.Errorf("local runtime bearer is required")
	}
	if !validIdentityText(options.Role) || !validMetadata(options.Source) || options.Source.Mode != requiredMode {
		return nil, fmt.Errorf("valid runtime identity is required")
	}
	if options.Source.RuntimeID != options.Service.runtimeID || options.Source.Principal != options.Service.principal {
		return nil, fmt.Errorf("local runtime source identity must match its authority")
	}
	limit := options.MaxMessageBytes
	if limit == 0 {
		limit = defaultLocalMessageLimit
	}
	if limit < 1 {
		return nil, fmt.Errorf("local runtime message limit must be positive")
	}
	stateFileName := options.StateFileName
	if stateFileName == "" {
		stateFileName = "local-runtime-state.json"
	}
	if filepath.Base(stateFileName) != stateFileName || !strings.HasSuffix(stateFileName, ".json") {
		return nil, fmt.Errorf("runtime state file name is invalid")
	}
	taskPrefix := options.TaskPrefix
	if taskPrefix == "" {
		taskPrefix = "task-"
	}
	if !validRuntimeIdentifier(taskPrefix) {
		return nil, fmt.Errorf("runtime task prefix is invalid")
	}
	clientID := options.ClientID
	if clientID == "" {
		clientID = options.Source.Principal
	}
	if !validText(clientID) {
		return nil, fmt.Errorf("runtime client identity is invalid")
	}
	server := &LocalServer{
		service: options.Service, bearerDigest: sha256.Sum256([]byte(options.Bearer)), role: options.Role,
		workspace: options.Workspace, source: options.Source, maxMessageBytes: limit,
		receipts:  make(map[string]localReceipt),
		statePath: filepath.Join(options.Service.Store().Root(), stateFileName), taskPrefix: taskPrefix, clientID: clientID,
		terminalProfiles: make(map[string]TerminalProfile, len(options.TerminalProfiles)),
	}
	for id, profile := range options.TerminalProfiles {
		if id != profile.ID || !validRuntimeIdentifier(id) || !validIdentityText(profile.RequiredAction) ||
			!slices.Contains([]string{"low", "medium", "high", "critical"}, profile.Risk) ||
			(options.TerminalBackend != nil && !validIdentityText(profile.Program)) {
			return nil, fmt.Errorf("terminal profile is invalid")
		}
		for _, argument := range profile.Arguments {
			if !validIdentityText(argument) {
				return nil, fmt.Errorf("terminal profile argument is invalid")
			}
		}
		server.terminalProfiles[id] = profile
	}
	if options.TerminalBackend != nil {
		if len(server.terminalProfiles) == 0 || !slices.Contains(server.source.Capabilities, "terminal.input") {
			return nil, fmt.Errorf("terminal backend requires advertised profiles and input capability")
		}
		server.terminalManager = NewTerminalManager(server.service, options.TerminalBackend)
	}
	if slices.Contains(server.source.Capabilities, "editor.read") {
		server.editorManager = NewEditorManager(server.service)
	}
	if err := server.loadState(); err != nil {
		return nil, err
	}
	return server, nil
}

func validRuntimeIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func (s *LocalServer) Handle(ctx context.Context, request LocalRequest) LocalResponse {
	if !s.authorized(request.Bearer) {
		return localError(request.ID, "unauthorized")
	}
	return s.handleAuthorizedForClient(ctx, request, s.clientID)
}

func (s *LocalServer) handleAuthorizedForClient(ctx context.Context, request LocalRequest, clientID string) LocalResponse {
	response := LocalResponse{ID: request.ID}
	if ctx == nil {
		ctx = context.Background()
	}
	switch request.Type {
	case "health":
		ready := true
		response.Type = "health"
		response.Ready = &ready
	case "handshake":
		if request.Handshake == nil {
			return localError(request.ID, "invalid_handshake")
		}
		handshake := HandshakeResponse{
			ProtocolVersion: ProtocolVersion, RuntimeID: s.source.RuntimeID, Principal: s.source.Principal,
			Role: s.role, Capabilities: append([]string(nil), s.source.Capabilities...), Source: s.source,
		}
		if _, err := NegotiateHandshake(*request.Handshake, handshake); err != nil {
			return localError(request.ID, err.Error())
		}
		response.Type = "handshake"
		response.Handshake = &handshake
	case "events":
		request.TaskID = s.resolveTaskID(request.TaskID)
		if request.TaskID == "" {
			response.Type = "events"
			response.Events = []productprotocol.Event{}
			break
		}
		events, err := s.service.Store().EventsAfter(ctx, request.TaskID, request.AfterCursor)
		if err != nil {
			return localError(request.ID, localErrorCode(err))
		}
		response.Type = "events"
		response.Events = events
	case "snapshot":
		request.TaskID = s.resolveTaskID(request.TaskID)
		if request.TaskID == "" {
			state := productstate.Initial()
			response.Type = "snapshot"
			response.Snapshot = &RuntimeSnapshot{Cursor: 0, State: state}
			break
		}
		_, state, err := s.service.Store().Load(ctx, request.TaskID)
		if err != nil {
			return localError(request.ID, localErrorCode(err))
		}
		response.Type = "snapshot"
		response.Snapshot = &RuntimeSnapshot{Cursor: state.CommittedCursor, State: state}
	case "command":
		if request.Command == nil {
			return localError(request.ID, "invalid_command_envelope")
		}
		response.Type = "command"
		response.Receipt = s.handleCommand(ctx, request.TaskID, *request.Command, clientID)
	case "editor":
		request.TaskID = s.resolveTaskID(request.TaskID)
		if request.TaskID == "" || !validRuntimeIdentifier(request.DraftID) || request.ExpectedLeaseRevision < 1 {
			return localError(request.ID, "invalid_editor_request")
		}
		if err := s.requireEditorCapability("editor.read"); err != nil {
			return localError(request.ID, localErrorCode(err))
		}
		result, err := s.editorManager.Read(ctx, request.TaskID, clientID, request.DraftID, request.ExpectedLeaseRevision)
		if err != nil {
			return localError(request.ID, localErrorCode(err))
		}
		response.Type = "editor"
		response.Editor = &EditorReadResponse{DraftID: result.DraftID, Revision: result.Revision, BaseSHA256: result.BaseSHA256, Data: base64.StdEncoding.EncodeToString(result.Data), ByteLength: result.ByteLength, Encoding: result.Encoding}
	case "close":
		if s.terminalManager != nil {
			_ = s.terminalManager.Close(ctx)
		}
		if s.editorManager != nil {
			s.editorManager.Close()
		}
		response.Type = "closed"
	default:
		return localError(request.ID, "unknown_request")
	}
	return response
}

func (s *LocalServer) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if input == nil || output == nil {
		return fmt.Errorf("local runtime stdio is required")
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), s.maxMessageBytes)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var request LocalRequest
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return fmt.Errorf("decode local runtime request: %w", err)
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return fmt.Errorf("decode local runtime request: trailing JSON value")
		}
		if err := encoder.Encode(s.Handle(ctx, request)); err != nil {
			return fmt.Errorf("encode local runtime response: %w", err)
		}
		if request.Type == "close" {
			return nil
		}
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read local runtime request: %w", err)
	}
	return nil
}

func (s *LocalServer) authorized(bearer string) bool {
	digest := sha256.Sum256([]byte(bearer))
	return subtle.ConstantTimeCompare(digest[:], s.bearerDigest[:]) == 1
}

func (s *LocalServer) handleCommand(ctx context.Context, taskID string, envelope CommandEnvelope, clientID string) *CommandReceipt {
	if err := ValidateCommandEnvelope(envelope); err != nil {
		return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	digest := s.idempotencyDigest(taskID, envelope.Command, clientID)
	if existing, ok := s.receipts[envelope.IdempotencyKey]; ok {
		if subtle.ConstantTimeCompare([]byte(existing.Digest), []byte(digest)) != 1 {
			return rejectedReceipt(envelope.IdempotencyKey, "idempotency_conflict")
		}
		copy := existing.Receipt
		return &copy
	}
	receipt := s.dispatchCommand(ctx, taskID, envelope, clientID)
	s.receipts[envelope.IdempotencyKey] = localReceipt{Digest: digest, Receipt: *receipt}
	if err := s.saveState(); err != nil {
		delete(s.receipts, envelope.IdempotencyKey)
		return rejectedReceipt(envelope.IdempotencyKey, "persistence_failed")
	}
	return receipt
}

func (s *LocalServer) idempotencyDigest(taskID string, command json.RawMessage, clientID string) string {
	var commandType struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(command, &commandType)
	if taskID == "" && commandType.Type != "task.create" {
		taskID = s.activeTask
	}
	bound, _ := json.Marshal(struct {
		Command    json.RawMessage `json:"command"`
		TaskID     string          `json:"taskId"`
		Controller string          `json:"controller"`
		RuntimeID  string          `json:"runtimeId"`
		Principal  string          `json:"principal"`
		Role       string          `json:"role"`
	}{
		Command: command, TaskID: taskID, Controller: clientID,
		RuntimeID: s.source.RuntimeID, Principal: s.source.Principal, Role: s.role,
	})
	digest := sha256.Sum256(bound)
	return hex.EncodeToString(digest[:])
}

func (s *LocalServer) loadState() error {
	data, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read local runtime state: %w", err)
	}
	var state localPersistentState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return fmt.Errorf("decode local runtime state: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode local runtime state: trailing JSON value")
	}
	if state.RuntimeID != s.source.RuntimeID || state.NextTask < 0 || state.Receipts == nil {
		return fmt.Errorf("local runtime state identity is invalid")
	}
	for key, receipt := range state.Receipts {
		if len(receipt.Digest) != sha256.Size*2 || ValidateCommandReceipt(receipt.Receipt, CommandEnvelope{IdempotencyKey: key, Command: json.RawMessage(`{"type":"task.pause"}`)}) != nil {
			return fmt.Errorf("local runtime receipt state is invalid")
		}
	}
	s.nextTask = state.NextTask
	s.activeTask = state.ActiveTask
	s.receipts = state.Receipts
	return nil
}

func (s *LocalServer) saveState() error {
	state := localPersistentState{RuntimeID: s.source.RuntimeID, NextTask: s.nextTask, ActiveTask: s.activeTask, Receipts: s.receipts}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary := s.statePath + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := replaceLocalStateFile(temporary, s.statePath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(s.statePath))
}

func (s *LocalServer) dispatchCommand(ctx context.Context, taskID string, envelope CommandEnvelope, clientID string) *CommandReceipt {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(envelope.Command, &base); err != nil {
		return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
	}
	var err error
	if taskID == "" && base.Type != "task.create" {
		taskID = s.activeTask
	}
	switch base.Type {
	case "task.create":
		var command struct {
			Objective string `json:"objective"`
			RuntimeID string `json:"runtimeId"`
			Workspace string `json:"workspace"`
		}
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		if command.RuntimeID != s.source.RuntimeID {
			return rejectedReceipt(envelope.IdempotencyKey, "runtime_mismatch")
		}
		workspace := command.Workspace
		if workspace == "" {
			workspace = s.workspace
		}
		s.nextTask++
		taskID = fmt.Sprintf("%s%d", s.taskPrefix, s.nextTask)
		if _, err = s.service.CreateTask(ctx, taskID, command.Objective); err == nil {
			scope := productprotocol.ScopeSnapshot{
				ID: "scope-1", Principal: s.source.Principal, Workspace: workspace, Validity: "task",
				Targets: []string{workspace}, AllowedActions: defaultScopeActions(s.source.Capabilities), DeniedActions: []string{}, RiskCeiling: "low",
			}
			_, err = s.service.ProposeScope(ctx, taskID, scope)
			if err == nil {
				s.activeTask = taskID
			}
		}
	case "scope.confirm":
		var command struct {
			ScopeID string `json:"scopeId"`
		}
		_ = json.Unmarshal(envelope.Command, &command)
		_, err = s.service.ConfirmScope(ctx, taskID, command.ScopeID)
	case "task.pause":
		_, err = s.service.emit(ctx, taskID, "task.paused", map[string]any{}, productprotocol.EventSourceRef{})
	case "task.resume":
		_, err = s.service.emit(ctx, taskID, "task.resumed", map[string]any{}, productprotocol.EventSourceRef{})
	case "task.cancel":
		_, err = s.service.emit(ctx, taskID, "task.cancel.requested", map[string]any{}, productprotocol.EventSourceRef{})
	case "approval.respond":
		var command struct {
			ChallengeID string `json:"challengeId"`
			Decision    string `json:"decision"`
		}
		_ = json.Unmarshal(envelope.Command, &command)
		_, err = s.service.ResolveApproval(ctx, taskID, command.ChallengeID, command.Decision)
	case "control.take":
		var command struct {
			ExpectedRevision int `json:"expectedRevision"`
		}
		_ = json.Unmarshal(envelope.Command, &command)
		_, err = s.service.TakeControl(ctx, taskID, clientID, command.ExpectedRevision)
	case "instruction.send":
		return rejectedReceipt(envelope.IdempotencyKey, "unsupported_command")
	case "terminal.open":
		var command terminalOpenCommand
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		if err = s.authorizeTerminalOpen(ctx, taskID, clientID, command); err == nil {
			if s.terminalManager == nil {
				err = ErrTerminalUnavailable
			} else {
				err = s.terminalManager.Open(ctx, taskID, clientID, command, s.terminalProfiles[command.ProfileID])
			}
		}
	case "terminal.input":
		var command terminalInputCommand
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		err = s.dispatchTerminalMutation(func(manager *TerminalManager) error { return manager.Input(ctx, taskID, clientID, command) })
	case "terminal.resize":
		var command terminalResizeCommand
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		err = s.dispatchTerminalMutation(func(manager *TerminalManager) error { return manager.Resize(ctx, taskID, clientID, command) })
	case "terminal.cancel":
		var command terminalCancelCommand
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		err = s.dispatchTerminalMutation(func(manager *TerminalManager) error { return manager.Cancel(ctx, taskID, clientID, command) })
	case "editor.open":
		var command editorOpenCommand
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		if err = s.requireEditorCapability("editor.read"); err == nil {
			_, err = s.editorManager.Open(ctx, taskID, clientID, command)
		}
	case "editor.save":
		var command editorSaveCommand
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		if err = s.requireEditorCapability("editor.write"); err == nil {
			_, err = s.editorManager.Save(ctx, taskID, clientID, command)
		}
	case "editor.apply":
		var command editorApplyCommand
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		if err = s.requireEditorCapability("editor.write"); err == nil {
			err = s.editorManager.Apply(ctx, taskID, clientID, command)
		}
	case "editor.discard":
		var command editorDiscardCommand
		if json.Unmarshal(envelope.Command, &command) != nil {
			return rejectedReceipt(envelope.IdempotencyKey, "invalid_command_envelope")
		}
		if err = s.requireEditorCapability("editor.write"); err == nil {
			err = s.editorManager.Discard(ctx, taskID, clientID, command)
		}
	default:
		return rejectedReceipt(envelope.IdempotencyKey, "unsupported_command")
	}
	if err != nil {
		return rejectedReceipt(envelope.IdempotencyKey, localErrorCode(err))
	}
	return &CommandReceipt{IdempotencyKey: envelope.IdempotencyKey, Status: "accepted"}
}

func defaultScopeActions(capabilities []string) []string {
	actions := []string{"read"}
	if slices.Contains(capabilities, "terminal.input") {
		actions = append(actions, "terminal.open")
	}
	if slices.Contains(capabilities, "editor.read") {
		actions = append(actions, "editor.open")
	}
	if slices.Contains(capabilities, "editor.write") {
		actions = append(actions, "editor.write")
	}
	return actions
}

func (s *LocalServer) resolveTaskID(taskID string) string {
	if taskID != "" {
		return taskID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeTask
}

func rejectedReceipt(key, code string) *CommandReceipt {
	return &CommandReceipt{IdempotencyKey: key, Status: "rejected", ErrorCode: code}
}

func localError(id, code string) LocalResponse {
	return LocalResponse{ID: id, Type: "error", ErrorCode: code}
}

func localErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for sentinel, code := range map[error]string{
		ErrScopeNotConfirmed: "scope_not_confirmed", ErrApprovalOutOfScope: "approval_out_of_scope",
		ErrApprovalExpired: "approval_expired", ErrApprovalReplay: "approval_replay",
		ErrUnknownApproval: "unknown_approval", ErrStaleLease: "stale_control_revision",
		ErrInvalidReportState:               "invalid_report_state",
		ErrTerminalCapabilityRequired:       "terminal_capability_required",
		ErrTerminalProfileNotAllowed:        "terminal_profile_not_allowed",
		ErrTerminalScopeMismatch:            "terminal_scope_mismatch",
		ErrTerminalWorkspaceOutOfScope:      "terminal_workspace_out_of_scope",
		ErrTerminalControlRequired:          "terminal_control_required",
		ErrTerminalUnavailable:              "terminal_unavailable",
		ErrTerminalApprovalRequired:         "terminal_approval_required",
		ErrTerminalSessionExists:            "terminal_session_exists",
		ErrTerminalSessionNotFound:          "terminal_session_not_found",
		ErrTerminalInputSequence:            "terminal_input_sequence",
		ErrEditorCapabilityRequired:         "editor_capability_required",
		ErrEditorScopeMismatch:              "editor_scope_mismatch",
		ErrEditorWorkspaceOutOfScope:        "editor_workspace_out_of_scope",
		ErrEditorControlRequired:            "editor_control_required",
		ErrEditorDraftNotFound:              "editor_draft_not_found",
		ErrEditorUnsupportedFile:            "editor_unsupported_file",
		productstate.ErrEditorDraftRevision: "editor_draft_revision",
		productstate.ErrEditorPatchStale:    "editor_patch_stale",
	} {
		if errors.Is(err, sentinel) {
			return code
		}
	}
	return "runtime_command_failed"
}

func (s *LocalServer) requireEditorCapability(capability string) error {
	if !slices.Contains(s.source.Capabilities, capability) || s.editorManager == nil {
		return ErrEditorCapabilityRequired
	}
	return nil
}

func (s *LocalServer) dispatchTerminalMutation(run func(*TerminalManager) error) error {
	if !slices.Contains(s.source.Capabilities, "terminal.input") {
		return ErrTerminalCapabilityRequired
	}
	if s.terminalManager == nil {
		return ErrTerminalUnavailable
	}
	return run(s.terminalManager)
}
