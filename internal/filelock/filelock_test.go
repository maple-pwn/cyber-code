package filelock

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireSerializesIndependentHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan Release, 1)
	errors := make(chan error, 1)
	go func() {
		release, err := Acquire(path)
		if err != nil {
			errors <- err
			return
		}
		acquired <- release
	}()
	select {
	case release := <-acquired:
		release()
		t.Fatal("second lock was acquired before the first was released")
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := first(); err != nil {
		t.Fatal(err)
	}
	select {
	case release := <-acquired:
		if err := release(); err != nil {
			t.Fatal(err)
		}
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not acquire after release")
	}
}

func TestTryAcquireReportsHeldLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if competing, err := TryAcquire(path); !errors.Is(err, ErrLocked) {
		if competing != nil {
			_ = competing()
		}
		t.Fatalf("TryAcquire error = %v", err)
	}
}
