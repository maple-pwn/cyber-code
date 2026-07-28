//go:build windows

package filelock

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Acquire blocks until an exclusive lock for path is held.
func Acquire(path string) (Release, error) {
	return acquire(path, windows.LOCKFILE_EXCLUSIVE_LOCK)
}

// TryAcquire obtains an exclusive lock without waiting.
func TryAcquire(path string) (Release, error) {
	return acquire(path, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY)
}

func acquire(path string, flags uint32) (Release, error) {
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, overlapped); err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return func() error {
		unlockErr := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
		closeErr := file.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}, nil
}
