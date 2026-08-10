package runtimeapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"cyber-code/internal/productprotocol"
)

func TestStoreCommitsEventAndSnapshotAsOneDurableRecord(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	event, snapshot, err := store.Commit(context.Background(), DraftEvent{
		TaskID: "task-1", Type: "task.created", OccurredAt: time.Unix(1, 0).UTC(),
		Source:  productprotocol.EventSourceRef{RuntimeID: "runtime-1"},
		Payload: map[string]any{"title": "Audit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Cursor != 1 || snapshot.CommittedCursor != 1 || snapshot.Task == nil || snapshot.Task.Title != "Audit" {
		t.Fatalf("event/snapshot mismatch: event=%+v snapshot=%+v", event, snapshot)
	}

	reopened, err := NewStore(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	events, recovered, err := reopened.Load(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventID != event.EventID || recovered.CommittedCursor != 1 {
		t.Fatalf("recovered events=%+v snapshot=%+v", events, recovered)
	}
}

func TestStoreIgnoresIncompleteUnacknowledgedTail(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Commit(context.Background(), DraftEvent{
		TaskID: "task-1", Type: "task.created", OccurredAt: time.Unix(1, 0).UTC(),
		Source: productprotocol.EventSourceRef{RuntimeID: "runtime-1"}, Payload: map[string]any{"title": "Audit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.transactionPath("task-1")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"event":{"cursor":2}`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	events, snapshot, err := reopened.Load(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || snapshot.CommittedCursor != 1 {
		t.Fatalf("partial tail advanced durable state: events=%d cursor=%d", len(events), snapshot.CommittedCursor)
	}
}

func TestStoreRejectsConflictingEvidenceAndDuplicateIdentity(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	commit := func(summary string, options ...CommitOption) error {
		_, _, err := store.Commit(context.Background(), DraftEvent{
			TaskID: "task-1", Type: "evidence.committed", OccurredAt: time.Unix(1, 0).UTC(),
			Source: productprotocol.EventSourceRef{RuntimeID: "runtime-1"},
			Payload: map[string]any{"evidence": map[string]any{
				"id": "evidence-1", "taskId": "task-1", "kind": "http", "summary": summary, "data": map[string]any{"status": 200},
			}},
		}, options...)
		return err
	}
	if err := commit("first", WithEventID("event-1")); err != nil {
		t.Fatal(err)
	}
	if err := commit("changed"); err != ErrEvidenceConflict {
		t.Fatalf("conflicting evidence error = %v", err)
	}
	if err := commit("first", WithEventID("event-1")); err != ErrEventIdentityConflict {
		t.Fatalf("duplicate event identity error = %v", err)
	}
}

func TestStoreRecordContainsMatchingCommittedSnapshot(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Commit(context.Background(), DraftEvent{
		TaskID: "task-1", Type: "task.created", OccurredAt: time.Unix(1, 0).UTC(),
		Source: productprotocol.EventSourceRef{RuntimeID: "runtime-1"}, Payload: map[string]any{"title": "Audit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mustTransactionPath(t, store, "task-1"))
	if err != nil {
		t.Fatal(err)
	}
	var record transactionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Event.Cursor != record.Snapshot.CommittedCursor {
		t.Fatalf("record cursor=%d snapshot cursor=%d", record.Event.Cursor, record.Snapshot.CommittedCursor)
	}
}

func TestStoreReplaysAcknowledgedEventsAfterDeliveryCrash(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	committed, _, err := store.Commit(context.Background(), DraftEvent{
		TaskID: "task-1", Type: "task.created", OccurredAt: time.Unix(1, 0).UTC(),
		Source: productprotocol.EventSourceRef{RuntimeID: "runtime-1"}, Payload: map[string]any{"title": "Audit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after Commit acknowledged durability but before the
	// transport delivered the event to its subscriber.
	reopened, err := NewStore(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := reopened.EventsAfter(context.Background(), "task-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 || replayed[0].EventID != committed.EventID {
		t.Fatalf("replayed = %+v, want acknowledged event %q", replayed, committed.EventID)
	}
	if none, err := reopened.EventsAfter(context.Background(), "task-1", committed.Cursor); err != nil || len(none) != 0 {
		t.Fatalf("events after committed cursor = %+v, %v", none, err)
	}
}

func mustTransactionPath(t *testing.T, store *Store, taskID string) string {
	t.Helper()
	path, err := store.transactionPath(taskID)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
