package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCheckpointNotFound = errors.New("checkpoint not found")

type Checkpoint struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Name      string    `json:"name,omitempty"`
	Sequence  uint64    `json:"sequence"`
	CreatedAt time.Time `json:"created_at"`
}

type BranchInfo struct {
	SessionID       string    `json:"session_id"`
	ParentSessionID string    `json:"parent_session_id"`
	CheckpointID    string    `json:"checkpoint_id"`
	CreatedAt       time.Time `json:"created_at"`
}

type sessionGraph struct {
	Version          int          `json:"version"`
	SessionID        string       `json:"session_id"`
	ParentSessionID  string       `json:"parent_session_id,omitempty"`
	ParentCheckpoint string       `json:"parent_checkpoint,omitempty"`
	Checkpoints      []Checkpoint `json:"checkpoints"`
}

func (store *Store) graphPath(sessionID string) string {
	return filepath.Join(store.sessionDir(sessionID), "graph.json")
}

func (store *Store) checkpointPath(sessionID, checkpointID string) string {
	return filepath.Join(store.sessionDir(sessionID), "checkpoint-"+checkpointID+".json")
}

func (store *Store) CreateCheckpoint(ctx context.Context, sessionID, name string) (Checkpoint, error) {
	if err := contextError(ctx); err != nil {
		return Checkpoint{}, err
	}
	if err := validateSessionID(sessionID); err != nil {
		return Checkpoint{}, err
	}
	name = strings.TrimSpace(name)
	if len(name) > 256 || !utf8.ValidString(name) {
		return Checkpoint{}, fmt.Errorf("checkpoint name is invalid")
	}
	state, err := store.state(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	release, err := store.lockSession(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	defer release()
	snapshot, err := store.loadSnapshot(ctx, sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	graph, err := store.loadGraph(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	created := store.now().UTC()
	checkpoint := Checkpoint{
		ID: checkpointID(sessionID, name, snapshot.LastSequence, created), SessionID: sessionID,
		Name: name, Sequence: snapshot.LastSequence, CreatedAt: created,
	}
	checkpointSnapshot := cloneSnapshot(snapshot)
	if err := store.writeJSONAtomically(store.checkpointPath(sessionID, checkpoint.ID), checkpointSnapshot); err != nil {
		return Checkpoint{}, fmt.Errorf("write checkpoint snapshot: %w", err)
	}
	graph.Checkpoints = append(graph.Checkpoints, checkpoint)
	if err := store.writeJSONAtomically(store.graphPath(sessionID), graph); err != nil {
		_ = os.Remove(store.checkpointPath(sessionID, checkpoint.ID))
		return Checkpoint{}, fmt.Errorf("write session graph: %w", err)
	}
	return checkpoint, nil
}

func (store *Store) ListCheckpoints(ctx context.Context, sessionID string) ([]Checkpoint, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	state, err := store.state(sessionID)
	if err != nil {
		return nil, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	release, err := store.lockSession(sessionID)
	if err != nil {
		return nil, err
	}
	defer release()
	graph, err := store.loadGraph(sessionID)
	if err != nil {
		return nil, err
	}
	result := append([]Checkpoint(nil), graph.Checkpoints...)
	return result, nil
}

func (store *Store) Rewind(ctx context.Context, sessionID, checkpointID string) (Snapshot, error) {
	if err := contextError(ctx); err != nil {
		return Snapshot{}, err
	}
	state, err := store.state(sessionID)
	if err != nil {
		return Snapshot{}, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	release, err := store.lockSession(sessionID)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	if _, err := store.findCheckpoint(sessionID, checkpointID); err != nil {
		return Snapshot{}, err
	}
	return store.loadCheckpointSnapshot(ctx, sessionID, checkpointID)
}

func (store *Store) Branch(ctx context.Context, sessionID, checkpointID, branchID string) (BranchInfo, error) {
	if err := contextError(ctx); err != nil {
		return BranchInfo{}, err
	}
	if err := validateSessionID(branchID); err != nil {
		return BranchInfo{}, err
	}
	state, err := store.state(sessionID)
	if err != nil {
		return BranchInfo{}, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	release, err := store.lockSession(sessionID)
	if err != nil {
		return BranchInfo{}, err
	}
	defer release()
	branchRelease, err := store.lockSession(branchID)
	if err != nil {
		return BranchInfo{}, err
	}
	defer branchRelease()
	checkpoint, err := store.findCheckpoint(sessionID, checkpointID)
	if err != nil {
		return BranchInfo{}, err
	}
	if _, err := os.Stat(store.sessionDir(branchID)); err == nil {
		return BranchInfo{}, fmt.Errorf("branch session %q already exists", branchID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return BranchInfo{}, err
	}
	snapshot, err := store.loadCheckpointSnapshot(ctx, sessionID, checkpoint.ID)
	if err != nil {
		return BranchInfo{}, err
	}
	snapshot.SessionID, snapshot.LastSequence = branchID, 0
	if _, err := store.ensureSessionDir(branchID); err != nil {
		return BranchInfo{}, err
	}
	if err := store.writeJSONAtomically(store.snapshotPath(branchID), snapshot); err != nil {
		return BranchInfo{}, fmt.Errorf("write branch snapshot: %w", err)
	}
	created := store.now().UTC()
	info := BranchInfo{SessionID: branchID, ParentSessionID: sessionID, CheckpointID: checkpoint.ID, CreatedAt: created}
	graph := sessionGraph{Version: 1, SessionID: branchID, ParentSessionID: sessionID, ParentCheckpoint: checkpoint.ID}
	if err := store.writeJSONAtomically(store.graphPath(branchID), graph); err != nil {
		_ = os.RemoveAll(store.sessionDir(branchID))
		return BranchInfo{}, fmt.Errorf("write branch graph: %w", err)
	}
	return info, nil
}

func (store *Store) loadGraph(sessionID string) (sessionGraph, error) {
	encoded, err := os.ReadFile(filepath.Clean(store.graphPath(sessionID)))
	if errors.Is(err, os.ErrNotExist) {
		return sessionGraph{Version: 1, SessionID: sessionID, Checkpoints: []Checkpoint{}}, nil
	}
	if err != nil {
		return sessionGraph{}, fmt.Errorf("read session graph: %w", err)
	}
	var graph sessionGraph
	if err := json.Unmarshal(encoded, &graph); err != nil {
		return sessionGraph{}, fmt.Errorf("decode session graph: %w", err)
	}
	if graph.Version != 1 || graph.SessionID != sessionID {
		return sessionGraph{}, fmt.Errorf("validate session graph: invalid metadata")
	}
	seen := make(map[string]struct{}, len(graph.Checkpoints))
	for _, checkpoint := range graph.Checkpoints {
		if checkpoint.SessionID != sessionID || !validCheckpointID(checkpoint.ID) || checkpoint.CreatedAt.IsZero() {
			return sessionGraph{}, fmt.Errorf("validate session graph: invalid checkpoint")
		}
		if _, exists := seen[checkpoint.ID]; exists {
			return sessionGraph{}, fmt.Errorf("validate session graph: duplicate checkpoint")
		}
		seen[checkpoint.ID] = struct{}{}
	}
	return graph, nil
}

func (store *Store) findCheckpoint(sessionID, checkpointID string) (Checkpoint, error) {
	graph, err := store.loadGraph(sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	for _, checkpoint := range graph.Checkpoints {
		if checkpoint.ID == checkpointID {
			return checkpoint, nil
		}
	}
	return Checkpoint{}, ErrCheckpointNotFound
}

func (store *Store) loadCheckpointSnapshot(ctx context.Context, sessionID, checkpointID string) (Snapshot, error) {
	encoded, err := os.ReadFile(filepath.Clean(store.checkpointPath(sessionID, checkpointID)))
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, ErrCheckpointNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read checkpoint snapshot: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode checkpoint snapshot: %w", err)
	}
	if snapshot.SessionID != sessionID {
		return Snapshot{}, fmt.Errorf("validate checkpoint snapshot: invalid session")
	}
	return cloneSnapshot(snapshot), nil
}

func (store *Store) writeJSONAtomically(path string, value any) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := restrictDirectory(directory); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(directory, ".graph-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = temporary.Close(); _ = os.Remove(temporaryPath) }
	if err := restrictFile(temporaryPath); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := replaceSnapshot(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return syncDirectory(directory)
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	return Snapshot{SessionID: snapshot.SessionID, LastSequence: snapshot.LastSequence, UpdatedAt: snapshot.UpdatedAt, History: cloneSessionMessages(snapshot.History)}
}

func checkpointID(sessionID, name string, sequence uint64, created time.Time) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d", sessionID, name, sequence, created.UnixNano())))
	return "cp-" + hex.EncodeToString(digest[:12])
}

func validCheckpointID(id string) bool {
	if !strings.HasPrefix(id, "cp-") || len(id) != 27 {
		return false
	}
	_, err := hex.DecodeString(id[3:])
	return err == nil
}
