package authorization

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type FileStore struct{ path string }

func NewFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, fmt.Errorf("authorization store path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve authorization store path: %w", err)
	}
	return &FileStore{path: absolute}, nil
}

func (s *FileStore) Save(policy *Policy) (returnErr error) {
	if s == nil || policy == nil {
		return fmt.Errorf("authorization store and policy are required")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create authorization store directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".authorization-*.tmp")
	if err != nil {
		return fmt.Errorf("create authorization temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		returnErr = err
		return
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(policy.Snapshot()); err != nil {
		returnErr = fmt.Errorf("encode authorization snapshot: %w", err)
		return
	}
	if err := temporary.Sync(); err != nil {
		returnErr = fmt.Errorf("sync authorization snapshot: %w", err)
		return
	}
	if err := temporary.Close(); err != nil {
		returnErr = fmt.Errorf("close authorization snapshot: %w", err)
		return
	}
	if err := replaceAuthorizationFile(temporaryPath, s.path); err != nil {
		returnErr = fmt.Errorf("replace authorization snapshot: %w", err)
		return
	}
	return nil
}

func (s *FileStore) Load() (*Policy, error) {
	if s == nil {
		return nil, fmt.Errorf("authorization store is required")
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read authorization snapshot: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode authorization snapshot: %w", err)
	}
	return NewPolicyFromSnapshot(snapshot)
}
