package credential

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore keeps one opaque credential value in a private file.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// RestrictPrivateDirectory limits a credential directory to the current user.
func RestrictPrivateDirectory(path string) error {
	return restrictDirectory(path)
}

// NewFileStore validates a concrete credential-file path.
func NewFileStore(path string) (*FileStore, error) {
	if filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) || path == "" {
		return nil, fmt.Errorf("credential file path is required")
	}
	return &FileStore{path: filepath.Clean(path)}, nil
}

func (store *FileStore) Load(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]byte(nil), value...), nil
}

func (store *FileStore) Save(ctx context.Context, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	if err := restrictDirectory(directory); err != nil {
		return fmt.Errorf("restrict credential directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".credentials-*")
	if err != nil {
		return fmt.Errorf("create temporary credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = temporary.Close(); _ = os.Remove(temporaryPath) }
	if err := restrictFile(temporaryPath); err != nil {
		cleanup()
		return fmt.Errorf("restrict temporary credential file: %w", err)
	}
	if _, err := temporary.Write(value); err != nil {
		cleanup()
		return fmt.Errorf("write credential: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync credential: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close credential: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := replaceFile(temporaryPath, store.path); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace credential atomically: %w", err)
	}
	if err := restrictFile(store.path); err != nil {
		return fmt.Errorf("restrict credential file: %w", err)
	}
	return nil
}
