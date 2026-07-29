package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/core"
)

func TestSnapshotMetadataDerivesBoundedRedactedFirstUserSummary(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	secret := "sk-session-secret"
	snapshot := Snapshot{
		SessionID: "summary-session", LastSequence: 9, UpdatedAt: time.Unix(100, 0).UTC(),
		History: []core.Message{
			{Role: core.RoleAssistant, Content: []core.ContentBlock{{Type: core.ContentText, Text: "ignore assistant"}}},
			{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "Please inspect api_key=" + secret + " " + strings.Repeat("long context ", 40)}}},
		},
	}
	if err := store.SaveSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.SnapshotMetadata(context.Background(), "summary-session")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.MessageCount != 2 || metadata.LastSequence != 9 || metadata.UpdatedAt != snapshot.UpdatedAt {
		t.Fatalf("metadata = %#v", metadata)
	}
	if metadata.Summary == "" || strings.Contains(metadata.Summary, secret) || len([]rune(metadata.Summary)) > MaxSessionSummaryRunes {
		t.Fatalf("summary = %q", metadata.Summary)
	}

	if err := os.WriteFile(store.snapshotPath("summary-session"), []byte("corrupt full snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SnapshotMetadata(context.Background(), "summary-session"); err != nil {
		t.Fatalf("metadata read depended on the full transcript: %v", err)
	}
}

func TestSnapshotMetadataHandlesCanceledMissingAndInvalidReads(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.SnapshotMetadata(canceled, "canceled-session"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read error = %v", err)
	}
	if _, err := store.SnapshotMetadata(context.Background(), "missing-session"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing read error = %v", err)
	}
	if err := store.SaveSnapshot(context.Background(), Snapshot{SessionID: "invalid-sidecar"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.snapshotMetadataPath("invalid-sidecar"), []byte(`{"session_id":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SnapshotMetadata(context.Background(), "invalid-sidecar"); err == nil || !strings.Contains(err.Error(), "invalid metadata") {
		t.Fatalf("invalid metadata error = %v", err)
	}
	if err := os.WriteFile(store.snapshotMetadataPath("invalid-sidecar"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SnapshotMetadata(context.Background(), "invalid-sidecar"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("corrupt metadata error = %v", err)
	}
}

func TestSaveSnapshotBoundsExplicitSummary(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	secret := "sk-explicit-summary-secret"
	if err := store.SaveSnapshot(context.Background(), Snapshot{
		SessionID: "explicit-summary", Summary: " api_key=" + secret + "\n" + strings.Repeat("summary ", 100),
	}); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.SnapshotMetadata(context.Background(), "explicit-summary")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(metadata.Summary, secret) || strings.Contains(metadata.Summary, "\n") || len([]rune(metadata.Summary)) > MaxSessionSummaryRunes {
		t.Fatalf("summary = %q", metadata.Summary)
	}
}
