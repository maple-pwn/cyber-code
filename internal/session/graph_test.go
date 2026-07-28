package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"cyber-code/internal/core"
)

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
