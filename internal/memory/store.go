// Package memory provides bounded, explicitly scoped agent memory.
package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/filelock"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeSession Scope = "session"
	ScopeAgent   Scope = "agent"
	ScopeTeam    Scope = "team"
)

type Entry struct {
	ID        string    `json:"id"`
	Scope     Scope     `json:"scope"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	Source    string    `json:"source,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Options struct {
	MaxEntries      int
	MaxContentBytes int
}
type Store struct {
	path                        string
	lockPath                    string
	maxEntries, maxContentBytes int
	mu                          sync.Mutex
}

func NewStore(directory string, options Options) (*Store, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("memory directory is required")
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = 1024
	}
	if options.MaxContentBytes <= 0 {
		options.MaxContentBytes = 16 << 10
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(directory, "memory.json"), lockPath: filepath.Join(directory, ".memory.lock"), maxEntries: options.MaxEntries, maxContentBytes: options.MaxContentBytes}, nil
}

func (store *Store) Add(ctx context.Context, entry Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry.Content = strings.TrimSpace(entry.Content)
	if entry.ID == "" {
		var bytes [8]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return err
		}
		entry.ID = "memory-" + hex.EncodeToString(bytes[:])
	}
	if !validScope(entry.Scope) {
		return errors.New("invalid memory scope")
	}
	if entry.Content == "" || len([]byte(entry.Content)) > store.maxContentBytes {
		return fmt.Errorf("memory content must be 1 to %d bytes", store.maxContentBytes)
	}
	if looksSecret(entry.Content) {
		return errors.New("memory content resembles a secret")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	release, err := filelock.Acquire(store.lockPath)
	if err != nil {
		return err
	}
	defer release()
	entries, err := store.read()
	if err != nil {
		return err
	}
	for _, existing := range entries {
		if existing.ID == entry.ID {
			return fmt.Errorf("memory ID %q already exists", entry.ID)
		}
	}
	entries = append(entries, entry)
	if len(entries) > store.maxEntries {
		return fmt.Errorf("memory exceeds %d entries", store.maxEntries)
	}
	return store.write(entries)
}

func (store *Store) Remove(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	release, err := filelock.Acquire(store.lockPath)
	if err != nil {
		return err
	}
	defer release()
	entries, err := store.read()
	if err != nil {
		return err
	}
	filtered := entries[:0]
	found := false
	for _, entry := range entries {
		if entry.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, entry)
	}
	if !found {
		return fmt.Errorf("memory ID %q was not found", id)
	}
	return store.write(filtered)
}

func (store *Store) List(ctx context.Context, scope Scope) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	release, err := filelock.Acquire(store.lockPath)
	if err != nil {
		return nil, err
	}
	defer release()
	entries, err := store.read()
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(entries))
	now := time.Now().UTC()
	for _, entry := range entries {
		if validScope(scope) && entry.Scope != scope {
			continue
		}
		if !entry.ExpiresAt.IsZero() && !entry.ExpiresAt.After(now) {
			continue
		}
		result = append(result, cloneEntry(entry))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (store *Store) Retrieve(ctx context.Context, scope Scope, query string, limit int) ([]Entry, error) {
	entries, err := store.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	terms := strings.Fields(strings.ToLower(query))
	type scored struct {
		entry Entry
		score int
	}
	ranked := make([]scored, 0, len(entries))
	for _, entry := range entries {
		haystack := strings.ToLower(entry.Content + " " + strings.Join(entry.Tags, " "))
		score := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				score++
			}
		}
		if score > 0 || len(terms) == 0 {
			ranked = append(ranked, scored{entry, score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].entry.ID < ranked[j].entry.ID
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	result := make([]Entry, len(ranked))
	for i := range ranked {
		result[i] = ranked[i].entry
	}
	return result, nil
}

func (store *Store) read() ([]Entry, error) {
	data, err := os.ReadFile(store.path)
	if os.IsNotExist(err) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode memory: %w", err)
	}
	return entries, nil
}
func (store *Store) write(entries []Entry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(store.path), ".memory-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceMemoryFile(tempPath, store.path)
}
func validScope(scope Scope) bool {
	switch scope {
	case ScopeUser, ScopeProject, ScopeSession, ScopeAgent, ScopeTeam:
		return true
	default:
		return false
	}
}
func looksSecret(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"api_key", "apikey", "authorization:", "bearer ", "private_key", "secret="} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
func cloneEntry(entry Entry) Entry { entry.Tags = append([]string(nil), entry.Tags...); return entry }
