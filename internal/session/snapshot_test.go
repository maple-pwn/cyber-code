package session

import (
	"context"
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
