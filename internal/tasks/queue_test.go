package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDurableQueueBoundsAndDeduplicatesEnqueue(t *testing.T) {
	now := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC)
	queue := openTestQueue(t, QueueOptions{Capacity: 1, Clock: func() time.Time { return now }})
	first, created, err := queue.Enqueue(context.Background(), "request-1", json.RawMessage(`{"prompt":"inspect"}`))
	if err != nil || !created || first.ID == "" {
		t.Fatalf("first enqueue=%#v created=%t err=%v", first, created, err)
	}
	again, created, err := queue.Enqueue(context.Background(), "request-1", json.RawMessage(`{"prompt":"different"}`))
	if err != nil || created || again.ID != first.ID || string(again.Payload) != string(first.Payload) {
		t.Fatalf("deduplicated enqueue=%#v created=%t err=%v", again, created, err)
	}
	if _, _, err := queue.Enqueue(context.Background(), "request-2", json.RawMessage(`{}`)); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue overflow error=%v", err)
	}
}

func TestDurableQueueRetriesWithBackoffThenDeadLetters(t *testing.T) {
	now := time.Date(2026, 8, 9, 2, 0, 0, 0, time.UTC)
	queue := openTestQueue(t, QueueOptions{
		Capacity: 4, MaxAttempts: 2, BaseBackoff: time.Second,
		Clock: func() time.Time { return now }, Jitter: func(delay time.Duration) time.Duration { return delay / 2 },
	})
	item, _, err := queue.Enqueue(context.Background(), "retry", json.RawMessage(`{"task":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.Claim(context.Background(), "worker-a")
	if err != nil || claimed.ID != item.ID || claimed.Attempts != 1 {
		t.Fatalf("first claim=%#v err=%v", claimed, err)
	}
	retried, err := queue.Fail(context.Background(), claimed.ID, "worker-a", errors.New("temporary failure"))
	if err != nil || retried.Status != QueuePending || !retried.AvailableAt.Equal(now.Add(1500*time.Millisecond)) {
		t.Fatalf("retry=%#v err=%v", retried, err)
	}
	if _, err := queue.Claim(context.Background(), "worker-a"); !errors.Is(err, ErrQueueEmpty) {
		t.Fatalf("early claim error=%v", err)
	}
	now = retried.AvailableAt
	claimed, err = queue.Claim(context.Background(), "worker-b")
	if err != nil || claimed.Attempts != 2 {
		t.Fatalf("second claim=%#v err=%v", claimed, err)
	}
	dead, err := queue.Fail(context.Background(), claimed.ID, "worker-b", errors.New("permanent failure"))
	if err != nil || dead.Status != QueueDead || dead.LastError != "permanent failure" {
		t.Fatalf("dead letter=%#v err=%v", dead, err)
	}
}

func TestDurableQueueRecoversExpiredLeaseAfterRestart(t *testing.T) {
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "queue.json")
	queue, err := OpenQueue(path, QueueOptions{Capacity: 4, MaxAttempts: 3, LeaseDuration: time.Second, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := queue.Enqueue(context.Background(), "restart", json.RawMessage(`{"task":"recover"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue.Claim(context.Background(), "worker-before-restart"); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	restarted, err := OpenQueue(path, QueueOptions{Capacity: 4, MaxAttempts: 3, LeaseDuration: time.Second, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if recovered, err := restarted.Reconcile(context.Background()); err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	claimed, err := restarted.Claim(context.Background(), "worker-after-restart")
	if err != nil || claimed.ID != item.ID || claimed.Attempts != 2 {
		t.Fatalf("recovered claim=%#v err=%v", claimed, err)
	}
}

func TestQueueWorkerDrainWaitsForActiveAndLeavesPending(t *testing.T) {
	queue := openTestQueue(t, QueueOptions{Capacity: 4})
	for _, key := range []string{"active", "pending"} {
		if _, _, err := queue.Enqueue(context.Background(), key, json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	worker, err := NewQueueWorker(queue, QueueWorkerOptions{ID: "worker", PollInterval: time.Millisecond}, func(context.Context, QueueItem) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	worker.Start(context.Background())
	<-started
	drained := make(chan error, 1)
	go func() { drained <- worker.Drain(context.Background()) }()
	select {
	case err := <-drained:
		t.Fatalf("drain returned before active handler completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("worker claimed new work while draining: calls=%d", calls.Load())
	}
	items := queue.Snapshot()
	if len(items) != 2 || items[0].Status != QueueCompleted || items[1].Status != QueuePending {
		t.Fatalf("queue after drain=%#v", items)
	}
}

func TestQueueWorkerDrainBeforeStartReturns(t *testing.T) {
	queue := openTestQueue(t, QueueOptions{})
	worker, err := NewQueueWorker(queue, QueueWorkerOptions{ID: "idle"}, func(context.Context, QueueItem) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := worker.Drain(ctx); err != nil {
		t.Fatalf("drain before start: %v", err)
	}
}

func TestDurableQueueCleansTerminalOrphans(t *testing.T) {
	now := time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)
	queue := openTestQueue(t, QueueOptions{Capacity: 2, Clock: func() time.Time { return now }})
	item, _, err := queue.Enqueue(context.Background(), "done", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.Claim(context.Background(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete(context.Background(), claimed.ID, "worker"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	removed, err := queue.CleanupTerminal(context.Background(), time.Hour)
	if err != nil || removed != 1 || len(queue.Snapshot()) != 0 {
		t.Fatalf("removed=%d items=%#v err=%v original=%s", removed, queue.Snapshot(), err, item.ID)
	}
}

func openTestQueue(t *testing.T, options QueueOptions) *Queue {
	t.Helper()
	queue, err := OpenQueue(filepath.Join(t.TempDir(), "queue.json"), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	return queue
}
