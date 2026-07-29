package collaboration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoordinatorSerializesWorkspaceWrites(t *testing.T) {
	coordinator, err := NewCoordinator(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var active, maximum atomic.Int32
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := coordinator.WithWorkspaceWrite(context.Background(), func() error {
				current := active.Add(1)
				for {
					previous := maximum.Load()
					if current <= previous || maximum.CompareAndSwap(previous, current) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				active.Add(-1)
				return nil
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent writes=%d", maximum.Load())
	}
}

func TestCoordinatorUsesExplicitMailboxMessages(t *testing.T) {
	coordinator, err := NewCoordinator(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Send(context.Background(), Message{From: "leader", To: "worker", Body: "run tests"}); err != nil {
		t.Fatal(err)
	}
	messages, err := coordinator.Receive(context.Background(), "worker", 0, 10)
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
}
