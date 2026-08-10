package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/credential"
	"cyber-code/internal/filelock"
)

// Credential contains an MCP access token and its optional refresh metadata.
// Tokens are persisted only in the local credential store and are never logged.
type Credential struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

type CredentialStore struct {
	directory string
	path      string
	lockPath  string
	mu        sync.Mutex
}

type CredentialRefresher interface {
	Refresh(context.Context, string, Credential) (Credential, error)
}

func (store *CredentialStore) Resolve(ctx context.Context, server string, now time.Time, refresher CredentialRefresher) (Credential, error) {
	value, ok, err := store.Get(ctx, server)
	if err != nil {
		return Credential{}, err
	}
	if !ok {
		return Credential{}, errors.New("MCP credential is unavailable")
	}
	if value.ExpiresAt == 0 || now.Unix() < value.ExpiresAt {
		return value, nil
	}
	if refresher == nil || value.RefreshToken == "" {
		return Credential{}, errors.New("MCP credential has expired")
	}
	refreshed, err := refresher.Refresh(ctx, server, value)
	if err != nil {
		return Credential{}, errors.New("refresh MCP credential")
	}
	if strings.TrimSpace(refreshed.AccessToken) == "" {
		return Credential{}, errors.New("refreshed MCP credential omitted access token")
	}
	if err := store.Put(ctx, server, refreshed); err != nil {
		return Credential{}, err
	}
	return refreshed, nil
}

func NewCredentialStore(directory string) (*CredentialStore, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("credential directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create credential directory: %w", err)
	}
	if err := credential.RestrictPrivateDirectory(directory); err != nil {
		return nil, fmt.Errorf("restrict credential directory: %w", err)
	}
	return &CredentialStore{directory: filepath.Clean(directory), path: filepath.Join(directory, "credentials.json"), lockPath: filepath.Join(directory, ".credentials.lock")}, nil
}

func (store *CredentialStore) Put(ctx context.Context, server string, credentialValue Credential) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(server) == "" {
		return errors.New("MCP server name is required")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	release, err := filelock.Acquire(store.lockPath)
	if err != nil {
		return err
	}
	defer release()
	values, err := store.read()
	if err != nil {
		return err
	}
	values[server] = credentialValue
	return store.write(ctx, values)
}

func (store *CredentialStore) Get(ctx context.Context, server string) (Credential, bool, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, false, err
	}
	if strings.TrimSpace(server) == "" {
		return Credential{}, false, errors.New("MCP server name is required")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	release, err := filelock.Acquire(store.lockPath)
	if err != nil {
		return Credential{}, false, err
	}
	defer release()
	values, err := store.read()
	if err != nil {
		return Credential{}, false, err
	}
	value, ok := values[server]
	return value, ok, nil
}

func (store *CredentialStore) Delete(ctx context.Context, server string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(server) == "" {
		return errors.New("MCP server name is required")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	release, err := filelock.Acquire(store.lockPath)
	if err != nil {
		return err
	}
	defer release()
	values, err := store.read()
	if err != nil {
		return err
	}
	delete(values, server)
	return store.write(ctx, values)
}

func (store *CredentialStore) read() (map[string]Credential, error) {
	fileStore, err := credential.NewFileStore(store.path)
	if err != nil {
		return nil, err
	}
	data, err := fileStore.Load(context.Background())
	if errors.Is(err, credential.ErrNotFound) {
		return map[string]Credential{}, nil
	}
	if err != nil {
		return nil, errors.New("read MCP credentials")
	}
	values := map[string]Credential{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, errors.New("decode MCP credentials")
	}
	return values, nil
}

func (store *CredentialStore) write(ctx context.Context, values map[string]Credential) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(values)
	if err != nil {
		return errors.New("encode MCP credentials")
	}
	fileStore, err := credential.NewFileStore(store.path)
	if err != nil {
		return err
	}
	if err := fileStore.Save(ctx, data); err != nil {
		return errors.New("write MCP credentials")
	}
	return nil
}
