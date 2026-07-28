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

	"cyber-code/internal/filelock"
)

var ErrSessionNotFound = errors.New("session not found")
var ErrSessionActive = errors.New("session is active in another runtime")

type StoreOptions struct {
	Now func() time.Time
}

type Store struct {
	root string
	now  func() time.Time

	mu       sync.Mutex
	sessions map[string]*sessionState
	leases   map[string]*Lease
}

type sessionState struct {
	mu sync.Mutex
}

// Lease holds exclusive ownership of an active session until Close.
type Lease struct {
	store   *Store
	id      string
	release filelock.Release
	once    sync.Once
	err     error
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
	return &Store{root: absolute, now: options.Now, sessions: make(map[string]*sessionState), leases: make(map[string]*Lease)}, nil
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
	return filepath.Join(store.root, sessionStorageKey(sessionID))
}

func (store *Store) lockSession(sessionID string) (filelock.Release, error) {
	store.mu.Lock()
	_, leased := store.leases[sessionID]
	store.mu.Unlock()
	if leased {
		return func() error { return nil }, nil
	}
	release, err := filelock.TryAcquire(filepath.Join(store.root, "."+sessionStorageKey(sessionID)+".lock"))
	if err != nil {
		if errors.Is(err, filelock.ErrLocked) {
			return nil, ErrSessionActive
		}
		return nil, fmt.Errorf("lock session: %w", err)
	}
	return release, nil
}

// AcquireLease claims one session for a Runtime without waiting for another
// active process. Store operations performed through this Store reuse it.
func (store *Store) AcquireLease(sessionID string) (*Lease, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	store.mu.Lock()
	if _, exists := store.leases[sessionID]; exists {
		store.mu.Unlock()
		return nil, ErrSessionActive
	}
	store.mu.Unlock()
	release, err := filelock.TryAcquire(filepath.Join(store.root, "."+sessionStorageKey(sessionID)+".lock"))
	if err != nil {
		if errors.Is(err, filelock.ErrLocked) {
			return nil, ErrSessionActive
		}
		return nil, fmt.Errorf("acquire session lease: %w", err)
	}
	lease := &Lease{store: store, id: sessionID, release: release}
	store.mu.Lock()
	store.leases[sessionID] = lease
	store.mu.Unlock()
	return lease, nil
}

func (lease *Lease) Close() error {
	if lease == nil {
		return nil
	}
	lease.once.Do(func() {
		lease.store.mu.Lock()
		delete(lease.store.leases, lease.id)
		lease.store.mu.Unlock()
		lease.err = lease.release()
	})
	return lease.err
}

func sessionStorageKey(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(digest[:])
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
	state, err := store.state(sessionID)
	if err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	release, err := store.lockSession(sessionID)
	if err != nil {
		return err
	}
	defer release()
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := os.RemoveAll(store.sessionDir(sessionID)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	store.mu.Lock()
	delete(store.sessions, sessionID)
	store.mu.Unlock()
	return nil
}
