// Package filelock provides cooperative cross-process exclusive file locks.
package filelock

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrLocked indicates that another process currently owns a non-blocking lock.
var ErrLocked = errors.New("file lock is already held")

// Release unlocks and closes an acquired lock file.
type Release func() error

func open(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}
