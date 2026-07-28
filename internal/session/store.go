// Package session persists canonical runtime events and conversation snapshots.
package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrSessionNotFound = errors.New("session not found")

type StoreOptions struct {
	Now func() time.Time
}

type Store struct {
	root string
	now  func() time.Time

	mu       sync.Mutex
	sessions map[string]*sessionState
}

type sessionState struct {
	mu          sync.Mutex
	initialized bool
	next        uint64
	validBytes  int64
}

func NewStore(root string, options StoreOptions) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("session store root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve session store root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create session store root: %w", err)
	}
	if err := restrictDirectory(absolute); err != nil {
		return nil, fmt.Errorf("restrict session store root: %w", err)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Store{root: absolute, now: options.Now, sessions: make(map[string]*sessionState)}, nil
}

func (store *Store) state(sessionID string) (*sessionState, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state := store.sessions[sessionID]
	if state == nil {
		state = &sessionState{}
		store.sessions[sessionID] = state
	}
	return state, nil
}

func (store *Store) ensureSessionDir(sessionID string) (string, error) {
	directory := store.sessionDir(sessionID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create session directory: %w", err)
	}
	if err := restrictDirectory(directory); err != nil {
		return "", fmt.Errorf("restrict session directory: %w", err)
	}
	return directory, nil
}

func (store *Store) sessionDir(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return filepath.Join(store.root, hex.EncodeToString(digest[:]))
}

func (store *Store) eventLogPath(sessionID string) string {
	return filepath.Join(store.sessionDir(sessionID), "events.jsonl")
}

func (store *Store) snapshotPath(sessionID string) string {
	return filepath.Join(store.sessionDir(sessionID), "snapshot.json")
}

func validateSessionID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session ID is required")
	}
	if len(sessionID) > 512 || !utf8.ValidString(sessionID) {
		return fmt.Errorf("session ID is invalid")
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// Delete removes all persisted data for one validated session.
func (store *Store) Delete(ctx context.Context, sessionID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state := store.sessions[sessionID]
	if state == nil {
		state = &sessionState{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := os.RemoveAll(store.sessionDir(sessionID)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	delete(store.sessions, sessionID)
	return nil
}
