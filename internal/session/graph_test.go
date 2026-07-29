package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"cyber-code/internal/core"
)

func TestCheckpointGraphValidationAndCancellation(t *testing.T) {
	if validCheckpointID("cp-zzzzzzzzzzzzzzzzzzzzzzzz") {
		t.Fatal("invalid checkpoint hex accepted")
	}
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ListCheckpoints(ctx, "session"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if _, err := store.CreateCheckpoint(ctx, "session", "name"); !errors.Is(err, context.Canceled) {
		t.Fatalf("create cancel error = %v", err)
	}
	if _, err := store.Rewind(ctx, "session", "cp-missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("rewind cancel error = %v", err)
	}
	if _, err := store.Branch(ctx, "session", "cp-missing", "branch"); !errors.Is(err, context.Canceled) {
		t.Fatalf("branch cancel error = %v", err)
	}
	if checkpoints, err := store.ListCheckpoints(context.Background(), "empty"); err != nil || len(checkpoints) != 0 {
		t.Fatalf("empty checkpoints=%v err=%v", checkpoints, err)
	}
	invalid := sessionGraph{Version: 1, SessionID: "bad", Checkpoints: []Checkpoint{{ID: "invalid", SessionID: "bad", CreatedAt: time.Now()}}}
	encoded, _ := json.Marshal(invalid)
	if err := os.MkdirAll(store.sessionDir("bad"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.graphPath("bad"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCheckpoints(context.Background(), "bad"); err == nil || !strings.Contains(err.Error(), "invalid checkpoint") {
		t.Fatalf("validation error = %v", err)
	}
	metadata, _ := json.Marshal(sessionGraph{Version: 2, SessionID: "bad"})
	if err := os.WriteFile(store.graphPath("bad"), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCheckpoints(context.Background(), "bad"); err == nil || !strings.Contains(err.Error(), "invalid metadata") {
		t.Fatalf("metadata error=%v", err)
	}
}

func TestCheckpointRejectsInvalidMetadataSnapshotAndExistingBranch(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.SaveSnapshot(ctx, Snapshot{SessionID: "source", LastSequence: 1}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := store.CreateCheckpoint(ctx, "source", "point")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.checkpointPath("source", checkpoint.ID), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rewind(ctx, "source", checkpoint.ID); err == nil || !strings.Contains(err.Error(), "decode checkpoint snapshot") {
		t.Fatalf("snapshot error=%v", err)
	}
	if err := os.WriteFile(store.checkpointPath("source", checkpoint.ID), []byte(`{"session_id":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rewind(ctx, "source", checkpoint.ID); err == nil || !strings.Contains(err.Error(), "invalid session") {
		t.Fatalf("snapshot validation=%v", err)
	}
	if err := os.Remove(store.checkpointPath("source", checkpoint.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rewind(ctx, "source", checkpoint.ID); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("missing snapshot=%v", err)
	}

	valid := sessionGraph{Version: 1, SessionID: "source", Checkpoints: []Checkpoint{{ID: checkpoint.ID, SessionID: "source", CreatedAt: time.Now()}, {ID: checkpoint.ID, SessionID: "source", CreatedAt: time.Now()}}}
	encoded, _ := json.Marshal(valid)
	if err := os.WriteFile(store.graphPath("source"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCheckpoints(ctx, "source"); err == nil || !strings.Contains(err.Error(), "duplicate checkpoint") {
		t.Fatalf("duplicate error=%v", err)
	}
}

func TestCheckpointListRewindAndBranch(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	history := []core.Message{{Role: core.RoleUser, Content: []core.ContentBlock{{Type: core.ContentText, Text: "first"}}}}
	if err := store.SaveSnapshot(ctx, Snapshot{SessionID: "graph", LastSequence: 2, History: history}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := store.CreateCheckpoint(ctx, "graph", "before-edit")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.ID == "" || checkpoint.Name != "before-edit" || checkpoint.Sequence != 2 {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := store.loadCheckpointSnapshot(canceled, "graph", checkpoint.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot cancel=%v", err)
	}
	history[0].Content[0].Text = "mutated caller"
	checkpoints, err := store.ListCheckpoints(ctx, "graph")
	if err != nil || len(checkpoints) != 1 {
		t.Fatalf("checkpoints = %#v, error = %v", checkpoints, err)
	}
	rewound, err := store.Rewind(ctx, "graph", checkpoint.ID)
	if err != nil || rewound.History[0].Content[0].Text != "first" {
		t.Fatalf("rewound = %#v, error = %v", rewound, err)
	}
	rewound.History[0].Content[0].Text = "mutated rewind"
	again, err := store.Rewind(ctx, "graph", checkpoint.ID)
	if err != nil || again.History[0].Content[0].Text != "first" {
		t.Fatalf("checkpoint was mutable = %#v, error = %v", again, err)
	}
	branch, err := store.Branch(ctx, "graph", checkpoint.ID, "graph-branch")
	if err != nil {
		t.Fatal(err)
	}
	if branch.SessionID != "graph-branch" || branch.ParentSessionID != "graph" {
		t.Fatalf("branch = %#v", branch)
	}
	if _, err := store.Branch(ctx, "graph", checkpoint.ID, "graph-branch"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate branch error = %v", err)
	}
	branched, err := store.Resume(ctx, branch.SessionID)
	if err != nil || branched.History[0].Content[0].Text != "first" {
		t.Fatalf("branch snapshot = %#v, error = %v", branched, err)
	}
}

func TestCheckpointRequiresSnapshotAndRejectsUnknownIDs(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.CreateCheckpoint(ctx, "missing", strings.Repeat("x", 257)); err == nil {
		t.Fatal("oversized checkpoint name was accepted")
	}
	if _, err := store.CreateCheckpoint(ctx, "missing", "checkpoint"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing checkpoint error = %v", err)
	}
	if err := store.SaveSnapshot(ctx, Snapshot{SessionID: "known", LastSequence: 1, History: []core.Message{{Role: core.RoleUser}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rewind(ctx, "known", "missing"); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("missing rewind error = %v", err)
	}
	if _, err := store.Branch(ctx, "known", "missing", "new"); !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("missing branch error = %v", err)
	}
}

func TestCheckpointRejectsCorruptGraph(t *testing.T) {
	store, err := NewStore(t.TempDir(), StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(context.Background(), Snapshot{SessionID: "corrupt-graph", LastSequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.graphPath("corrupt-graph"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCheckpoints(context.Background(), "corrupt-graph"); err == nil || !strings.Contains(err.Error(), "decode session graph") {
		t.Fatalf("corrupt graph error = %v", err)
	}
}
