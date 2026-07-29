package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cyber-code/internal/core"
	"cyber-code/internal/security"
)

const MaxSessionSummaryRunes = 160

type Snapshot struct {
	SessionID    string         `json:"session_id"`
	LastSequence uint64         `json:"last_sequence"`
	UpdatedAt    time.Time      `json:"updated_at"`
	History      []core.Message `json:"history"`
	Summary      string         `json:"summary,omitempty"`
}

type SnapshotMetadata struct {
	SessionID    string    `json:"session_id"`
	LastSequence uint64    `json:"last_sequence"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	Summary      string    `json:"summary,omitempty"`
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
	if snapshot.Summary == "" {
		snapshot.Summary = snapshotSummary(snapshot.History)
	} else {
		snapshot.Summary = boundedSummary(snapshot.Summary)
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
	metadata := SnapshotMetadata{
		SessionID: snapshot.SessionID, LastSequence: snapshot.LastSequence,
		UpdatedAt: snapshot.UpdatedAt, MessageCount: len(snapshot.History), Summary: snapshot.Summary,
	}
	if err := store.writeJSONAtomically(store.snapshotMetadataPath(snapshot.SessionID), metadata); err != nil {
		return fmt.Errorf("write session snapshot metadata: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync session directory: %w", err)
	}
	return nil
}

func (store *Store) SnapshotMetadata(ctx context.Context, sessionID string) (SnapshotMetadata, error) {
	if err := contextError(ctx); err != nil {
		return SnapshotMetadata{}, err
	}
	if err := validateSessionID(sessionID); err != nil {
		return SnapshotMetadata{}, err
	}
	encoded, err := os.ReadFile(filepath.Clean(store.snapshotMetadataPath(sessionID)))
	if errors.Is(err, os.ErrNotExist) {
		return SnapshotMetadata{}, ErrSessionNotFound
	}
	if err != nil {
		return SnapshotMetadata{}, fmt.Errorf("read session snapshot metadata: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return SnapshotMetadata{}, err
	}
	var metadata SnapshotMetadata
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		return SnapshotMetadata{}, fmt.Errorf("decode session snapshot metadata: %w", err)
	}
	if metadata.SessionID != sessionID || metadata.UpdatedAt.IsZero() || metadata.MessageCount < 0 {
		return SnapshotMetadata{}, fmt.Errorf("validate session snapshot metadata: invalid metadata")
	}
	return metadata, nil
}

func snapshotSummary(history []core.Message) string {
	for _, message := range history {
		if message.Role != core.RoleUser {
			continue
		}
		parts := make([]string, 0, len(message.Content))
		for _, block := range message.Content {
			if block.Type == core.ContentText && strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		}
		return boundedSummary(strings.Join(parts, " "))
	}
	return ""
}

func boundedSummary(value string) string {
	value = security.NewRedactor().Text(strings.Join(strings.Fields(value), " "))
	runes := []rune(value)
	if len(runes) > MaxSessionSummaryRunes {
		value = string(runes[:MaxSessionSummaryRunes-1]) + "…"
	}
	return value
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
