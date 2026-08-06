package runtimeapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
)

func TestEditorManagerOpenRejectsUnsafeAndUnsupportedFiles(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newEditorAuthorityServer(t)
	manager := NewEditorManager(server.service)

	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "binary.bin"), []byte{0xff, 0xfe, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"traversal":      filepath.Join(workspace, "..", "outside.txt"),
		"symlink escape": filepath.Join(workspace, "escape.txt"),
		"binary":         filepath.Join(workspace, "binary.bin"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := manager.Open(context.Background(), taskID, "client-1", editorOpenCommand{DraftID: "draft-" + name, Path: path, ScopeID: "scope-editor", EvidenceReferences: []editorEvidenceReference{{FindingID: "finding-1", EvidenceID: "evidence-1", StartLine: 1, EndLine: 1}}, ExpectedLeaseRevision: 1})
			if err == nil {
				t.Fatal("unsafe or unsupported file was opened")
			}
		})
	}
}

func TestEditorManagerOpenStoresDigestAndSaveApplyRequiresFreshOwnerLease(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newEditorAuthorityServer(t)
	path := filepath.Join(workspace, "notes.txt")
	content := []byte("hello\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewEditorManager(server.service)
	draft, err := manager.Open(context.Background(), taskID, "client-1", editorOpenCommand{DraftID: "draft-1", Path: path, ScopeID: "scope-editor", EvidenceReferences: []editorEvidenceReference{{FindingID: "finding-1", EvidenceID: "evidence-1", StartLine: 1, EndLine: 1}}, ExpectedLeaseRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(content)
	if draft.BaseSHA256 != hex.EncodeToString(want[:]) || draft.BaseByteLength != len(content) || draft.Status != "open" {
		t.Fatalf("draft = %+v", draft)
	}

	proposed := []byte("updated\n")
	if _, err := manager.Save(context.Background(), taskID, "client-1", editorSaveCommand{DraftID: "draft-1", Revision: 1, BaseSHA256: draft.BaseSHA256, Data: proposed, ByteLength: len(proposed), ExpectedLeaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Apply(context.Background(), taskID, "client-1", editorApplyCommand{DraftID: "draft-1", Revision: 1, ProposedSHA256: digestBytes(proposed), ExpectedLeaseRevision: 1}); !errors.Is(err, productstate.ErrEditorPatchStale) {
		t.Fatalf("stale apply error = %v", err)
	}

	if _, err := server.service.TakeControl(context.Background(), taskID, "client-2", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Save(context.Background(), taskID, "client-1", editorSaveCommand{DraftID: "draft-1", Revision: 1, BaseSHA256: draft.BaseSHA256, Data: proposed, ByteLength: len(proposed), ExpectedLeaseRevision: 1}); !errors.Is(err, ErrEditorControlRequired) {
		t.Fatalf("revoked save error = %v", err)
	}
}

func TestEditorManagerAppliesAtomicallyWithoutLeakingContentIntoEvents(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newEditorAuthorityServer(t)
	path := filepath.Join(workspace, "app.go")
	if err := os.WriteFile(path, []byte("package old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	manager := NewEditorManager(server.service)
	draft, err := manager.Open(context.Background(), taskID, "client-1", editorOpenCommand{DraftID: "draft-apply", Path: path, ScopeID: "scope-editor", EvidenceReferences: []editorEvidenceReference{{FindingID: "finding-1", EvidenceID: "evidence-1", StartLine: 1, EndLine: 1}}, ExpectedLeaseRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	proposed := []byte("package confidentialmarker\n")
	if _, err := manager.Save(context.Background(), taskID, "client-1", editorSaveCommand{DraftID: draft.ID, Revision: 1, BaseSHA256: draft.BaseSHA256, Data: proposed, ByteLength: len(proposed), ExpectedLeaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Apply(context.Background(), taskID, "client-1", editorApplyCommand{DraftID: draft.ID, Revision: 1, ProposedSHA256: digestBytes(proposed), ExpectedLeaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(proposed) {
		t.Fatalf("applied file = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("applied mode = %v, %v", info.Mode().Perm(), err)
	}
	events, state, err := server.service.Store().Load(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if state.EditorDrafts[draft.ID].Status != "applied" {
		t.Fatalf("draft state = %+v", state.EditorDrafts[draft.ID])
	}
	for _, event := range events {
		if strings.Contains(string(event.Payload), "confidentialmarker") {
			t.Fatalf("event leaked editor content: %s", event.Payload)
		}
	}
}

func TestEditorCommandsDispatchAndRuntimeCloseClearsPendingDrafts(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newEditorAuthorityServer(t)
	path := filepath.Join(workspace, "dispatch.txt")
	base := []byte("before\n")
	if err := os.WriteFile(path, base, 0o600); err != nil {
		t.Fatal(err)
	}
	commands := []map[string]any{
		{"type": "editor.open", "draftId": "draft-dispatch", "path": path, "scopeId": "scope-editor", "evidenceReferences": []map[string]any{{"findingId": "finding-1", "evidenceId": "evidence-1", "startLine": 1, "endLine": 1}}, "expectedLeaseRevision": 1},
		{"type": "editor.save", "draftId": "draft-dispatch", "revision": 1, "baseSha256": digestBytes(base), "data": []byte("after\n"), "byteLength": 6, "expectedLeaseRevision": 1},
	}
	for index, command := range commands {
		envelope := mustLocalEnvelope(t, fmt.Sprintf("editor-%d", index), command)
		response := server.handleAuthorizedForClient(context.Background(), LocalRequest{ID: "dispatch", Type: "command", TaskID: taskID, Command: &envelope}, "client-1")
		if response.Receipt == nil || response.Receipt.Status != "accepted" {
			t.Fatalf("%s receipt = %+v", command["type"], response.Receipt)
		}
	}
	read := server.handleAuthorizedForClient(context.Background(), LocalRequest{ID: "read", Type: "editor", TaskID: taskID, DraftID: "draft-dispatch", ExpectedLeaseRevision: 1}, "client-1")
	if read.Editor == nil || read.Editor.DraftID != "draft-dispatch" || read.Editor.Data != "YWZ0ZXIK" || read.Editor.ByteLength != 6 {
		t.Fatalf("editor read response = %+v", read)
	}
	denied := server.handleAuthorizedForClient(context.Background(), LocalRequest{ID: "read-other", Type: "editor", TaskID: taskID, DraftID: "draft-dispatch", ExpectedLeaseRevision: 1}, "client-2")
	if denied.Type != "error" || denied.ErrorCode != "editor_control_required" {
		t.Fatalf("cross-client editor read = %+v", denied)
	}
	if server.editorManager == nil || server.editorManager.pendingCount() != 1 {
		t.Fatalf("pending editor drafts = %d", server.editorManager.pendingCount())
	}
	server.handleAuthorizedForClient(context.Background(), LocalRequest{ID: "close", Type: "close"}, "client-1")
	if server.editorManager.pendingCount() != 0 {
		t.Fatalf("pending editor drafts after close = %d", server.editorManager.pendingCount())
	}
}

func newEditorAuthorityServer(t *testing.T) (*LocalServer, string, string) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-1", "operator", nil)
	server, err := NewLocalServer(LocalServerOptions{Service: service, Bearer: "secret", Role: "owner", Workspace: workspace, Source: SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-1", Principal: "operator", Capabilities: []string{"events", "commands", "snapshot", "editor.read", "editor.write"}}})
	if err != nil {
		t.Fatal(err)
	}
	taskID := "task-editor"
	if _, err := service.CreateTask(context.Background(), taskID, "edit"); err != nil {
		t.Fatal(err)
	}
	scope := productprotocol.ScopeSnapshot{ID: "scope-editor", Principal: "operator", Workspace: workspace, Validity: "task", Targets: []string{workspace}, AllowedActions: []string{"read", "editor.open", "editor.write"}, DeniedActions: []string{}, RiskCeiling: "low"}
	if _, err := service.ProposeScope(context.Background(), taskID, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmScope(context.Background(), taskID, scope.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CommitEvidence(context.Background(), productprotocol.ImmutableEvidence{ID: "evidence-1", TaskID: taskID, Kind: "file", Summary: "notes", Data: map[string]any{}}, productprotocol.EventSourceRef{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateFinding(context.Background(), taskID, productprotocol.FindingState{ID: "finding-1", Title: "edit", Severity: "low", Status: "candidate", Confidence: "high", EvidenceIDs: []string{"evidence-1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TakeControl(context.Background(), taskID, "client-1", 0); err != nil {
		t.Fatal(err)
	}
	return server, taskID, workspace
}
