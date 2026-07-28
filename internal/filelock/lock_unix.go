//go:build !windows

package filelock

import (
	"errors"

	"golang.org/x/sys/unix"
)

// Acquire blocks until an exclusive lock for path is held.
func Acquire(path string) (Release, error) {
	return acquire(path, unix.LOCK_EX)
}

// TryAcquire obtains an exclusive lock without waiting.
func TryAcquire(path string) (Release, error) {
	return acquire(path, unix.LOCK_EX|unix.LOCK_NB)
}

func acquire(path string, flags int) (Release, error) {
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), flags); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return func() error {
		unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
		closeErr := file.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}, nil
}
