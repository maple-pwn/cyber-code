package runtimeapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"unicode/utf8"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
	"cyber-code/internal/security"
)

const maxEditorBytes = 64 * 1024 * 1024

var (
	ErrEditorCapabilityRequired  = errors.New("editor_capability_required")
	ErrEditorScopeMismatch       = errors.New("editor_scope_mismatch")
	ErrEditorWorkspaceOutOfScope = errors.New("editor_workspace_out_of_scope")
	ErrEditorControlRequired     = errors.New("editor_control_required")
	ErrEditorDraftNotFound       = errors.New("editor_draft_not_found")
	ErrEditorUnsupportedFile     = errors.New("editor_unsupported_file")
)

type editorEvidenceReference = productprotocol.EditorEvidenceReference

type editorOpenCommand struct {
	DraftID               string                    `json:"draftId"`
	Path                  string                    `json:"path"`
	ScopeID               string                    `json:"scopeId"`
	EvidenceReferences    []editorEvidenceReference `json:"evidenceReferences"`
	ExpectedLeaseRevision int                       `json:"expectedLeaseRevision"`
}

type editorSaveCommand struct {
	DraftID               string `json:"draftId"`
	Revision              int    `json:"revision"`
	BaseSHA256            string `json:"baseSha256"`
	Data                  []byte `json:"data"`
	ByteLength            int    `json:"byteLength"`
	ExpectedLeaseRevision int    `json:"expectedLeaseRevision"`
}

type editorApplyCommand struct {
	DraftID               string `json:"draftId"`
	Revision              int    `json:"revision"`
	ProposedSHA256        string `json:"proposedSha256"`
	ExpectedLeaseRevision int    `json:"expectedLeaseRevision"`
}

type editorDiscardCommand struct {
	DraftID               string `json:"draftId"`
	ExpectedLeaseRevision int    `json:"expectedLeaseRevision"`
}

type EditorReadResult struct {
	DraftID    string `json:"draftId"`
	Revision   int    `json:"revision"`
	BaseSHA256 string `json:"baseSha256"`
	Data       []byte `json:"-"`
	ByteLength int    `json:"byteLength"`
	Encoding   string `json:"encoding"`
}

type editorManager struct {
	service *Service
	mu      sync.Mutex
	pending map[string][]byte
}

func NewEditorManager(service *Service) *editorManager {
	return &editorManager{service: service, pending: make(map[string][]byte)}
}

func (manager *editorManager) Close() {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	clear(manager.pending)
}

func (manager *editorManager) pendingCount() int {
	if manager == nil {
		return 0
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return len(manager.pending)
}

func (manager *editorManager) Open(ctx context.Context, taskID, clientID string, command editorOpenCommand) (productprotocol.EditorDraftState, error) {
	if manager == nil || manager.service == nil {
		return productprotocol.EditorDraftState{}, ErrEditorCapabilityRequired
	}
	events, state, err := manager.service.Store().Load(ctx, taskID)
	if err != nil {
		return productprotocol.EditorDraftState{}, err
	}
	if err := authorizeEditor(state, events, clientID, command.ScopeID, command.ExpectedLeaseRevision, "editor.open"); err != nil {
		return productprotocol.EditorDraftState{}, err
	}
	if !slices.Contains(state.Scope.AllowedActions, "editor.open") {
		return productprotocol.EditorDraftState{}, ErrApprovalOutOfScope
	}
	if !pathWithinWorkspace(state.Scope.Workspace, command.Path) {
		return productprotocol.EditorDraftState{}, ErrEditorWorkspaceOutOfScope
	}
	file, info, err := security.OpenVerified(state.Scope.Workspace, command.Path)
	if err != nil {
		return productprotocol.EditorDraftState{}, err
	}
	defer file.Close()
	if info.Size() < 0 || info.Size() > maxEditorBytes {
		return productprotocol.EditorDraftState{}, ErrEditorUnsupportedFile
	}
	data, err := io.ReadAll(io.LimitReader(file, maxEditorBytes+1))
	if err != nil {
		return productprotocol.EditorDraftState{}, err
	}
	if len(data) > maxEditorBytes || !utf8.Valid(data) {
		return productprotocol.EditorDraftState{}, ErrEditorUnsupportedFile
	}
	draft := productprotocol.EditorDraftState{ID: command.DraftID, Path: filepath.Clean(command.Path), ScopeID: command.ScopeID, OwnerClientID: clientID, LeaseRevision: command.ExpectedLeaseRevision, BaseSHA256: digestBytes(data), BaseByteLength: len(data), Encoding: "utf-8", EvidenceReferences: append([]productprotocol.EditorEvidenceReference(nil), command.EvidenceReferences...)}
	opened := map[string]any{
		"id": draft.ID, "path": draft.Path, "scopeId": draft.ScopeID, "ownerClientId": draft.OwnerClientID,
		"leaseRevision": draft.LeaseRevision, "baseSha256": draft.BaseSHA256, "baseByteLength": draft.BaseByteLength,
		"encoding": draft.Encoding, "evidenceReferences": draft.EvidenceReferences,
	}
	if _, err := manager.service.Emit(ctx, DraftEvent{TaskID: taskID, Type: "editor.draft.opened", Payload: map[string]any{"draft": opened}}); err != nil {
		return productprotocol.EditorDraftState{}, err
	}
	manager.mu.Lock()
	manager.pending[editorKey(taskID, draft.ID)] = nil
	manager.mu.Unlock()
	draft.Status, draft.NextRevision = "open", 1
	return draft, nil
}

func (manager *editorManager) Save(ctx context.Context, taskID, clientID string, command editorSaveCommand) (productprotocol.Event, error) {
	events, state, err := manager.service.Store().Load(ctx, taskID)
	if err != nil {
		return productprotocol.Event{}, err
	}
	draft, ok := state.EditorDrafts[command.DraftID]
	if !ok {
		return productprotocol.Event{}, ErrEditorDraftNotFound
	}
	if err := authorizeEditor(state, events, clientID, draft.ScopeID, command.ExpectedLeaseRevision, "editor.write"); err != nil {
		return productprotocol.Event{}, err
	}
	if draft.Status != "open" && draft.Status != "saved" || draft.NextRevision != command.Revision || draft.BaseSHA256 != command.BaseSHA256 {
		return productprotocol.Event{}, productstate.ErrEditorDraftRevision
	}
	if len(command.Data) != command.ByteLength || len(command.Data) > maxEditorBytes || !utf8.Valid(command.Data) {
		return productprotocol.Event{}, ErrEditorUnsupportedFile
	}
	if _, err := manager.verifyBase(state.Scope.Workspace, draft.Path, draft.BaseSHA256); err != nil {
		return productprotocol.Event{}, err
	}
	event, err := manager.service.Emit(ctx, DraftEvent{TaskID: taskID, Type: "editor.draft.saved", Payload: map[string]any{"draftId": draft.ID, "revision": command.Revision, "baseSha256": draft.BaseSHA256, "proposedSha256": digestBytes(command.Data), "proposedByteLength": len(command.Data)}})
	if err != nil {
		return productprotocol.Event{}, err
	}
	manager.mu.Lock()
	manager.pending[editorKey(taskID, draft.ID)] = append([]byte(nil), command.Data...)
	manager.mu.Unlock()
	return event, nil
}

func (manager *editorManager) Read(ctx context.Context, taskID, clientID, draftID string, expectedLeaseRevision int) (EditorReadResult, error) {
	events, state, err := manager.service.Store().Load(ctx, taskID)
	if err != nil {
		return EditorReadResult{}, err
	}
	draft, ok := state.EditorDrafts[draftID]
	if !ok {
		return EditorReadResult{}, ErrEditorDraftNotFound
	}
	if err := authorizeEditor(state, events, clientID, draft.ScopeID, expectedLeaseRevision, "editor.open"); err != nil {
		return EditorReadResult{}, err
	}
	if draft.OwnerClientID != clientID {
		return EditorReadResult{}, ErrEditorControlRequired
	}
	if draft.Status != "open" && draft.Status != "saved" {
		return EditorReadResult{}, productstate.ErrEditorDraftNotEditable
	}
	if _, err := manager.verifyBase(state.Scope.Workspace, draft.Path, draft.BaseSHA256); err != nil {
		return EditorReadResult{}, err
	}
	var data []byte
	if draft.Status == "saved" {
		manager.mu.Lock()
		pending, exists := manager.pending[editorKey(taskID, draft.ID)]
		data = append([]byte(nil), pending...)
		manager.mu.Unlock()
		if !exists || digestBytes(data) != draft.ProposedSHA256 {
			return EditorReadResult{}, productstate.ErrEditorPatchStale
		}
	} else {
		file, _, err := security.OpenVerified(state.Scope.Workspace, draft.Path)
		if err != nil {
			return EditorReadResult{}, err
		}
		data, err = io.ReadAll(io.LimitReader(file, maxEditorBytes+1))
		closeErr := file.Close()
		if err != nil {
			return EditorReadResult{}, err
		}
		if closeErr != nil {
			return EditorReadResult{}, closeErr
		}
	}
	return EditorReadResult{DraftID: draft.ID, Revision: draft.NextRevision - 1, BaseSHA256: draft.BaseSHA256, Data: data, ByteLength: len(data), Encoding: "utf-8"}, nil
}

func (manager *editorManager) Apply(ctx context.Context, taskID, clientID string, command editorApplyCommand) error {
	events, state, err := manager.service.Store().Load(ctx, taskID)
	if err != nil {
		return err
	}
	draft, ok := state.EditorDrafts[command.DraftID]
	if !ok {
		return ErrEditorDraftNotFound
	}
	if err := authorizeEditor(state, events, clientID, draft.ScopeID, command.ExpectedLeaseRevision, "editor.write"); err != nil {
		return err
	}
	if draft.Status != "saved" || draft.NextRevision-1 != command.Revision || draft.ProposedSHA256 != command.ProposedSHA256 {
		return productstate.ErrEditorPatchStale
	}
	manager.mu.Lock()
	data, exists := manager.pending[editorKey(taskID, draft.ID)]
	data = append([]byte(nil), data...)
	manager.mu.Unlock()
	if !exists || digestBytes(data) != command.ProposedSHA256 {
		return productstate.ErrEditorPatchStale
	}
	mode, err := manager.verifyBase(state.Scope.Workspace, draft.Path, draft.BaseSHA256)
	if err != nil {
		return err
	}
	temporary, err := prepareEditorWrite(draft.Path, data, mode)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	if _, err := manager.service.Emit(ctx, DraftEvent{TaskID: taskID, Type: "editor.patch.applied", Payload: map[string]any{"draftId": draft.ID, "revision": command.Revision, "baseSha256": draft.BaseSHA256, "proposedSha256": draft.ProposedSHA256, "resultSha256": digestBytes(data), "reviewer": clientID}}); err != nil {
		return err
	}
	if err := replaceLocalStateFile(temporary, draft.Path); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(draft.Path)); err != nil {
		return err
	}
	manager.mu.Lock()
	delete(manager.pending, editorKey(taskID, draft.ID))
	manager.mu.Unlock()
	return nil
}

func (manager *editorManager) Discard(ctx context.Context, taskID, clientID string, command editorDiscardCommand) error {
	events, state, err := manager.service.Store().Load(ctx, taskID)
	if err != nil {
		return err
	}
	draft, ok := state.EditorDrafts[command.DraftID]
	if !ok {
		return ErrEditorDraftNotFound
	}
	if err := authorizeEditor(state, events, clientID, draft.ScopeID, command.ExpectedLeaseRevision, "editor.write"); err != nil {
		return err
	}
	if _, err := manager.service.Emit(ctx, DraftEvent{TaskID: taskID, Type: "editor.draft.discarded", Payload: map[string]any{"draftId": draft.ID, "reason": "operator_discarded"}}); err != nil {
		return err
	}
	manager.mu.Lock()
	delete(manager.pending, editorKey(taskID, draft.ID))
	manager.mu.Unlock()
	return nil
}

func (manager *editorManager) verifyBase(root, path, expected string) (os.FileMode, error) {
	file, _, err := security.OpenVerified(root, path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxEditorBytes+1))
	if err != nil {
		return 0, err
	}
	if len(data) > maxEditorBytes || digestBytes(data) != expected {
		return 0, productstate.ErrEditorPatchStale
	}
	return info.Mode().Perm(), nil
}

func authorizeEditor(state productstate.State, events []productprotocol.Event, clientID, scopeID string, revision int, action string) error {
	if state.Scope == nil || state.Scope.ID != scopeID || !scopeWasConfirmed(events, scopeID) {
		return ErrEditorScopeMismatch
	}
	if !scopeAllows(*state.Scope, action, state.Scope.Workspace, "low") {
		return ErrApprovalOutOfScope
	}
	if state.ControlLease == nil || state.ControlLease.ClientID != clientID {
		return ErrEditorControlRequired
	}
	if state.ControlLease.Revision != revision {
		return ErrStaleLease
	}
	return nil
}

func prepareEditorWrite(path string, data []byte, mode os.FileMode) (string, error) {
	temporary := path + ".cyber-code-editor.tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return "", err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	return temporary, nil
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func editorKey(taskID, draftID string) string { return fmt.Sprintf("%s:%s", taskID, draftID) }
