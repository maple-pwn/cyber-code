package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cyber-code/internal/core"
)

type Snapshot struct {
	SessionID    string         `json:"session_id"`
	LastSequence uint64         `json:"last_sequence"`
	UpdatedAt    time.Time      `json:"updated_at"`
	History      []core.Message `json:"history"`
}

func (store *Store) SaveSnapshot(ctx context.Context, snapshot Snapshot) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	state, err := store.state(snapshot.SessionID)
	if err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	release, err := store.lockSession(snapshot.SessionID)
	if err != nil {
		return err
	}
	defer release()
	directory, err := store.ensureSessionDir(snapshot.SessionID)
	if err != nil {
		return err
	}
	if snapshot.UpdatedAt.IsZero() {
		snapshot.UpdatedAt = store.now().UTC()
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode session snapshot: %w", err)
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(directory, ".snapshot-*")
	if err != nil {
		return fmt.Errorf("create temporary session snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := restrictFile(temporaryPath); err != nil {
		cleanup()
		return fmt.Errorf("restrict temporary session snapshot: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		cleanup()
		return fmt.Errorf("write session snapshot: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync session snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close session snapshot: %w", err)
	}
	if err := contextError(ctx); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := replaceSnapshot(temporaryPath, store.snapshotPath(snapshot.SessionID)); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace session snapshot: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync session directory: %w", err)
	}
	return nil
}

func (store *Store) Resume(ctx context.Context, sessionID string) (Snapshot, error) {
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
	return store.loadSnapshot(ctx, sessionID)
}

func (store *Store) loadSnapshot(ctx context.Context, sessionID string) (Snapshot, error) {
	encoded, err := os.ReadFile(filepath.Clean(store.snapshotPath(sessionID)))
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, ErrSessionNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read session snapshot: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode session snapshot: %w", err)
	}
	if snapshot.SessionID != sessionID || snapshot.UpdatedAt.IsZero() {
		return Snapshot{}, fmt.Errorf("validate session snapshot: invalid metadata")
	}
	return snapshot, nil
}
