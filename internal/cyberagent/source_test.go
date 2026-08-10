package cyberagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"cyber-code/internal/runtimeapi"
)

func TestRemoteSourcePersistsBindingAndRecoversLaggingCursor(t *testing.T) {
	root := t.TempDir()
	client := snapshotClient(t, 3)
	store, err := runtimeapi.NewStore(filepath.Join(root, "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := NewBindingStore(filepath.Join(root, "bindings"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewRemoteSource(RemoteSourceOptions{
		Client: client, Store: store, Bindings: bindings, TaskID: "task-1", SessionID: "session-1", RuntimeID: "cyber-agent:local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Attach(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, event := range []EventEnvelope{
		testCyberAgentEvent(1, "session.created", map[string]any{"schema_version": 1, "revision": 1, "session_id": "session-1", "task_id": "task-1", "status": "active"}),
		testCyberAgentEvent(2, "plan.created", map[string]any{"run_id": "run-1", "runtime_revision": 1, "plan_revision": 1, "step_ids": []string{"recon"}}),
	} {
		if _, err := source.Apply(context.Background(), event); err != nil {
			t.Fatalf("Apply(%d): %v", event.Sequence, err)
		}
	}
	binding, ok, err := bindings.Load(context.Background(), "task-1")
	if err != nil || !ok || binding.Cursor.Sequence != 2 || binding.Cursor.EventID != "source-event-b" {
		t.Fatalf("binding = %#v, ok=%t, err=%v", binding, ok, err)
	}

	// Simulate a crash after the runtime transaction is durable but before its
	// binding sidecar is replaced. Attach must trust and reconcile the durable
	// transaction origin.
	third := testCyberAgentEvent(3, "runtime.future", map[string]any{"future": true})
	mapped, err := ProjectEvent(third, "cyber-agent:local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitExternal(context.Background(), mapped); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewRemoteSource(RemoteSourceOptions{
		Client: client, Store: store, Bindings: bindings, TaskID: "task-1", SessionID: "session-1", RuntimeID: "cyber-agent:local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Attach(context.Background()); err != nil {
		t.Fatal(err)
	}
	binding, _, _ = bindings.Load(context.Background(), "task-1")
	if binding.Cursor.Sequence != 3 || binding.Cursor.EventID != third.EventID {
		t.Fatalf("reconciled binding = %#v", binding)
	}
}

func TestRemoteSourceRejectsGapsConflictsAndBindingChanges(t *testing.T) {
	root := t.TempDir()
	store, _ := runtimeapi.NewStore(filepath.Join(root, "runtime"))
	bindings, _ := NewBindingStore(filepath.Join(root, "bindings"))
	source, err := NewRemoteSource(RemoteSourceOptions{
		Client: snapshotClient(t, 5), Store: store, Bindings: bindings,
		TaskID: "task-1", SessionID: "session-1", RuntimeID: "cyber-agent:local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Attach(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := testCyberAgentEvent(1, "session.created", map[string]any{"schema_version": 1, "revision": 1, "session_id": "session-1", "task_id": "task-1", "status": "active"})
	if _, err := source.Apply(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Apply(context.Background(), testCyberAgentEvent(3, "runtime.future", map[string]any{})); !errors.Is(err, ErrSourceCursorGap) {
		t.Fatalf("gap error = %v", err)
	}
	conflict := first
	conflict.EventID = "different-event"
	if _, err := source.Apply(context.Background(), conflict); !errors.Is(err, ErrSourceCursorConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	duplicate, err := source.Apply(context.Background(), first)
	if err != nil || duplicate.CommittedCursor != 1 {
		t.Fatalf("duplicate state cursor = %d, err=%v", duplicate.CommittedCursor, err)
	}

	other, _ := NewRemoteSource(RemoteSourceOptions{
		Client: snapshotClient(t, 5), Store: store, Bindings: bindings,
		TaskID: "task-1", SessionID: "other-session", RuntimeID: "cyber-agent:local",
	})
	if _, err := other.Attach(context.Background()); !errors.Is(err, ErrSourceBindingConflict) {
		t.Fatalf("binding conflict error = %v", err)
	}
}

func TestBindingStoreUsesPrivateAtomicFiles(t *testing.T) {
	root := t.TempDir()
	store, err := NewBindingStore(root)
	if err != nil {
		t.Fatal(err)
	}
	binding := SourceBinding{SchemaVersion: 1, TaskID: "task-1", SessionID: "session-1", Cursor: EventCursor{SessionID: "session-1", Sequence: 1, EventID: "event-1"}}
	if err := store.Save(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("binding permissions = %o", info.Mode().Perm())
	}
}

func snapshotClient(t *testing.T, sequence int) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/sessions/session-1" && request.URL.Path != "/v1/sessions/other-session" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		sessionID := filepath.Base(request.URL.Path)
		payload := testSnapshotJSON("active", 1)
		payload = []byte(string(payload))
		var snapshot map[string]any
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			t.Fatal(err)
		}
		snapshot["session_id"] = sessionID
		snapshot["task_id"] = "task-1"
		eventID := ""
		if sequence > 0 {
			eventID = "source-event-" + string(rune('a'+sequence-1))
		}
		snapshot["event_cursor"] = map[string]any{"session_id": sessionID, "sequence": sequence, "event_id": eventID}
		_ = json.NewEncoder(response).Encode(snapshot)
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(ClientOptions{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
