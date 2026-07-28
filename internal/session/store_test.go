package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-code-go/internal/core"
)

func TestStoreAppendsSequencedEventsAndRecoversIncompleteTail(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store, err := NewStore(root, StoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := store.Append(ctx, "session-one", core.Event{Type: core.EventTextDelta, Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	second, err := store.Append(ctx, "session-one", core.Event{Type: core.EventCompleted, FinishReason: "stop"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || second.Sequence != 2 || first.SessionID != "session-one" || !second.Time.Equal(now) {
		t.Fatalf("records = %#v, %#v", first, second)
	}
	if err := appendRaw(store.eventLogPath("session-one"), []byte(`{"sequence":3,"session_id":"session-one"`)); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(root, StoreOptions{Now: func() time.Time { return now.Add(time.Second) }})
	if err != nil {
		t.Fatal(err)
	}
	records, err := reopened.Events(ctx, "session-one")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Event.Text != "first" || records[1].Event.FinishReason != "stop" {
		t.Fatalf("recovered records = %#v", records)
	}
	third, err := reopened.Append(ctx, "session-one", core.Event{Type: core.EventTextDelta, Text: "third"})
	if err != nil {
		t.Fatal(err)
	}
	if third.Sequence != 3 {
		t.Fatalf("third sequence = %d", third.Sequence)
	}
	records, err = reopened.Events(ctx, "session-one")
	if err != nil || len(records) != 3 || records[2].Event.Text != "third" {
		t.Fatalf("records after recovery = %#v, error = %v", records, err)
	}
}

func TestStoreRejectsCorruptMiddleRecord(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.Append(ctx, "corrupt-session", core.Event{Type: core.EventTextDelta, Text: "first"}); err != nil {
		t.Fatal(err)
	}
	valid := EventRecord{Sequence: 2, SessionID: "corrupt-session", Time: time.Now().UTC(), Event: core.Event{Type: core.EventCompleted}}
	line, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendRaw(store.eventLogPath("corrupt-session"), append([]byte("not-json\n"), append(line, '\n')...)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(ctx, "corrupt-session"); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("Events error = %v", err)
	}
}

func TestStoreDeleteRemovesSessionSnapshotAndEvents(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.Append(ctx, "delete-session", core.Event{Type: core.EventCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, Snapshot{SessionID: "delete-session", LastSequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, "delete-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resume(ctx, "delete-session"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("resume error = %v", err)
	}
	events, err := store.Events(ctx, "delete-session")
	if err != nil || len(events) != 0 {
		t.Fatalf("events = %v, error = %v", events, err)
	}
}

func TestStoreWritesAtomicSnapshotAndResumesHistory(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	history := []core.Message{
		{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "question"}}},
		{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentText, Text: "answer"}}},
	}
	want := Snapshot{SessionID: "resume-session", LastSequence: 7, History: history}
	if err := store.SaveSnapshot(ctx, want); err != nil {
		t.Fatal(err)
	}
	history[0].Content[0].Text = "mutated"
	resumed, err := store.Resume(ctx, "resume-session")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.SessionID != want.SessionID || resumed.LastSequence != 7 || resumed.History[0].Content[0].Text != "question" {
		t.Fatalf("resumed snapshot = %#v", resumed)
	}
	entries, err := os.ReadDir(store.sessionDir("resume-session"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".snapshot-") {
			t.Fatalf("temporary snapshot remains: %s", entry.Name())
		}
	}
}

func TestStoreIsolatesConcurrentSessions(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const eventsPerSession = 40
	ids := []string{"alpha", "beta"}
	var wait sync.WaitGroup
	for _, id := range ids {
		id := id
		for index := 0; index < eventsPerSession; index++ {
			index := index
			wait.Add(1)
			go func() {
				defer wait.Done()
				if _, err := store.Append(context.Background(), id, core.Event{Type: core.EventTextDelta, Text: id + "-" + string(rune(index))}); err != nil {
					t.Errorf("Append(%s): %v", id, err)
				}
			}()
		}
	}
	wait.Wait()
	for _, id := range ids {
		records, err := store.Events(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != eventsPerSession {
			t.Fatalf("session %s has %d records", id, len(records))
		}
		for index, record := range records {
			if record.SessionID != id || record.Sequence != uint64(index+1) {
				t.Fatalf("session %s record %d = %#v", id, index, record)
			}
		}
	}
}

func TestStoreExportRedactsCredentials(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	secret := "top-secret-value"
	text := "Authorization: Bearer " + secret + " api_key=" + secret + " sk-exporttoken"
	if _, err := store.Append(ctx, "export-session", core.Event{Type: core.EventTextDelta, Text: text}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, Snapshot{SessionID: "export-session", History: []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: text}}}}}); err != nil {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	if err := store.Export(ctx, "export-session", &exported, ExportOptions{Secrets: []string{secret}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported.String(), secret) || strings.Contains(exported.String(), "sk-exporttoken") {
		t.Fatalf("export contains credentials: %s", exported.String())
	}
	if !strings.Contains(exported.String(), "[REDACTED]") || !json.Valid(exported.Bytes()) {
		t.Fatalf("export is not valid redacted JSON: %s", exported.String())
	}
}

func appendRaw(path string, content []byte) error {
	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(content)
	return err
}
