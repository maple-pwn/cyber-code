package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrQueueClosed    = errors.New("task queue is closed")
	ErrQueueEmpty     = errors.New("task queue is empty")
	ErrQueueFull      = errors.New("task queue capacity reached")
	ErrQueueItem      = errors.New("task queue item was not found")
	ErrQueueLeaseLost = errors.New("task queue lease was lost")
)

type QueueStatus string

const (
	QueuePending   QueueStatus = "pending"
	QueueLeased    QueueStatus = "leased"
	QueueCompleted QueueStatus = "completed"
	QueueCancelled QueueStatus = "cancelled"
	QueueDead      QueueStatus = "dead"
)

const (
	queueStateVersion = 1
	maxQueueFileBytes = 16 << 20
	maxQueuePayload   = 1 << 20
)

type QueueItem struct {
	ID             string          `json:"id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
	Status         QueueStatus     `json:"status"`
	Attempts       int             `json:"attempts"`
	AvailableAt    time.Time       `json:"available_at"`
	LeaseOwner     string          `json:"lease_owner,omitempty"`
	LeaseUntil     time.Time       `json:"lease_until,omitempty"`
	LastError      string          `json:"last_error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type QueueOptions struct {
	Capacity      int
	MaxAttempts   int
	LeaseDuration time.Duration
	BaseBackoff   time.Duration
	MaxBackoff    time.Duration
	Clock         func() time.Time
	Jitter        func(time.Duration) time.Duration
}

type queueState struct {
	Version int         `json:"version"`
	Items   []QueueItem `json:"items"`
}

type Queue struct {
	path    string
	options QueueOptions
	mu      sync.Mutex
	items   []QueueItem
	closed  bool
}

func OpenQueue(path string, options QueueOptions) (*Queue, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("task queue path is required")
	}
	options = normalizeQueueOptions(options)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create task queue directory: %w", err)
	}
	queue := &Queue{path: path, options: options}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return queue, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task queue: %w", err)
	}
	if len(data) > maxQueueFileBytes {
		return nil, fmt.Errorf("task queue exceeds %d bytes", maxQueueFileBytes)
	}
	var state queueState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode task queue: %w", err)
	}
	if state.Version != queueStateVersion {
		return nil, fmt.Errorf("unsupported task queue version %d", state.Version)
	}
	if err := validateQueueItems(state.Items); err != nil {
		return nil, err
	}
	queue.items = cloneQueueItems(state.Items)
	return queue, nil
}

func normalizeQueueOptions(options QueueOptions) QueueOptions {
	if options.Capacity <= 0 {
		options.Capacity = 128
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 3
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = 5 * time.Minute
	}
	if options.BaseBackoff <= 0 {
		options.BaseBackoff = time.Second
	}
	if options.MaxBackoff <= 0 {
		options.MaxBackoff = time.Minute
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Jitter == nil {
		options.Jitter = queueJitter
	}
	return options
}

func (q *Queue) Enqueue(ctx context.Context, idempotencyKey string, payload json.RawMessage) (QueueItem, bool, error) {
	if err := contextError(ctx); err != nil {
		return QueueItem{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 256 || strings.ContainsAny(idempotencyKey, "\x00\r\n") {
		return QueueItem{}, false, fmt.Errorf("task queue idempotency key is invalid")
	}
	if len(payload) > maxQueuePayload || !json.Valid(payload) {
		return QueueItem{}, false, fmt.Errorf("task queue payload must be valid JSON within %d bytes", maxQueuePayload)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return QueueItem{}, false, ErrQueueClosed
	}
	for _, item := range q.items {
		if item.IdempotencyKey == idempotencyKey {
			return cloneQueueItem(item), false, nil
		}
	}
	active := 0
	for _, item := range q.items {
		if item.Status == QueuePending || item.Status == QueueLeased {
			active++
		}
	}
	if active >= q.options.Capacity {
		return QueueItem{}, false, ErrQueueFull
	}
	id, err := newQueueID()
	if err != nil {
		return QueueItem{}, false, err
	}
	now := q.now()
	item := QueueItem{ID: id, IdempotencyKey: idempotencyKey, Payload: append(json.RawMessage(nil), payload...), Status: QueuePending, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	previous := cloneQueueItems(q.items)
	q.items = append(q.items, item)
	if err := q.persistLocked(); err != nil {
		q.items = previous
		return QueueItem{}, false, err
	}
	return cloneQueueItem(item), true, nil
}

func (q *Queue) Claim(ctx context.Context, workerID string) (QueueItem, error) {
	if err := contextError(ctx); err != nil {
		return QueueItem{}, err
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || len(workerID) > 128 || strings.ContainsAny(workerID, "\x00\r\n") {
		return QueueItem{}, fmt.Errorf("task queue worker ID is invalid")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return QueueItem{}, ErrQueueClosed
	}
	previous := cloneQueueItems(q.items)
	now := q.now()
	q.reconcileLocked(now)
	index := -1
	for candidate := range q.items {
		item := q.items[candidate]
		if item.Status != QueuePending || item.AvailableAt.After(now) {
			continue
		}
		if index < 0 || item.AvailableAt.Before(q.items[index].AvailableAt) || (item.AvailableAt.Equal(q.items[index].AvailableAt) && item.CreatedAt.Before(q.items[index].CreatedAt)) {
			index = candidate
		}
	}
	if index < 0 {
		q.items = previous
		return QueueItem{}, ErrQueueEmpty
	}
	q.items[index].Status = QueueLeased
	q.items[index].Attempts++
	q.items[index].LeaseOwner = workerID
	q.items[index].LeaseUntil = now.Add(q.options.LeaseDuration)
	q.items[index].UpdatedAt = now
	if err := q.persistLocked(); err != nil {
		q.items = previous
		return QueueItem{}, err
	}
	return cloneQueueItem(q.items[index]), nil
}

func (q *Queue) Complete(ctx context.Context, itemID, workerID string) error {
	return q.finish(ctx, itemID, workerID, nil)
}

func (q *Queue) CancelByKey(ctx context.Context, idempotencyKey string) (QueueItem, error) {
	if err := contextError(ctx); err != nil {
		return QueueItem{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return QueueItem{}, ErrQueueClosed
	}
	index := -1
	for candidate := range q.items {
		if q.items[candidate].IdempotencyKey == idempotencyKey {
			index = candidate
			break
		}
	}
	if index < 0 {
		return QueueItem{}, ErrQueueItem
	}
	if q.items[index].Status == QueueCompleted || q.items[index].Status == QueueCancelled || q.items[index].Status == QueueDead {
		return cloneQueueItem(q.items[index]), nil
	}
	previous := cloneQueueItems(q.items)
	now := q.now()
	q.items[index].Status = QueueCancelled
	q.items[index].LeaseOwner = ""
	q.items[index].LeaseUntil = time.Time{}
	q.items[index].AvailableAt = time.Time{}
	q.items[index].LastError = "cancelled"
	q.items[index].UpdatedAt = now
	if err := q.persistLocked(); err != nil {
		q.items = previous
		return QueueItem{}, err
	}
	return cloneQueueItem(q.items[index]), nil
}

func (q *Queue) Fail(ctx context.Context, itemID, workerID string, cause error) (QueueItem, error) {
	var updated QueueItem
	err := q.mutateLease(ctx, itemID, workerID, func(item *QueueItem, now time.Time) {
		item.LastError = boundedQueueError(cause)
		item.LeaseOwner = ""
		item.LeaseUntil = time.Time{}
		item.UpdatedAt = now
		if item.Attempts >= q.options.MaxAttempts {
			item.Status = QueueDead
			item.AvailableAt = time.Time{}
		} else {
			item.Status = QueuePending
			delay := q.retryDelay(item.Attempts)
			item.AvailableAt = now.Add(delay + nonNegativeDuration(q.options.Jitter(delay)))
		}
		updated = cloneQueueItem(*item)
	})
	return updated, err
}

func (q *Queue) finish(ctx context.Context, itemID, workerID string, cause error) error {
	return q.mutateLease(ctx, itemID, workerID, func(item *QueueItem, now time.Time) {
		item.Status = QueueCompleted
		item.LastError = boundedQueueError(cause)
		item.LeaseOwner = ""
		item.LeaseUntil = time.Time{}
		item.AvailableAt = time.Time{}
		item.UpdatedAt = now
	})
}

func (q *Queue) mutateLease(ctx context.Context, itemID, workerID string, mutate func(*QueueItem, time.Time)) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrQueueClosed
	}
	index := q.itemIndex(itemID)
	if index < 0 {
		return ErrQueueItem
	}
	if q.items[index].Status != QueueLeased || q.items[index].LeaseOwner != workerID {
		return ErrQueueLeaseLost
	}
	previous := cloneQueueItems(q.items)
	mutate(&q.items[index], q.now())
	if err := q.persistLocked(); err != nil {
		q.items = previous
		return err
	}
	return nil
}

func (q *Queue) Reconcile(ctx context.Context) (int, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return 0, ErrQueueClosed
	}
	previous := cloneQueueItems(q.items)
	recovered := q.reconcileLocked(q.now())
	if recovered == 0 {
		return 0, nil
	}
	if err := q.persistLocked(); err != nil {
		q.items = previous
		return 0, err
	}
	return recovered, nil
}

func (q *Queue) reconcileLocked(now time.Time) int {
	recovered := 0
	for index := range q.items {
		item := &q.items[index]
		if item.Status != QueueLeased || item.LeaseUntil.After(now) {
			continue
		}
		item.LeaseOwner = ""
		item.LeaseUntil = time.Time{}
		item.UpdatedAt = now
		if item.Attempts >= q.options.MaxAttempts {
			item.Status = QueueDead
			item.AvailableAt = time.Time{}
			item.LastError = "worker lease expired after final attempt"
		} else {
			item.Status = QueuePending
			item.AvailableAt = now
			item.LastError = "worker lease expired"
		}
		recovered++
	}
	return recovered
}

func (q *Queue) CleanupTerminal(ctx context.Context, olderThan time.Duration) (int, error) {
	if err := contextError(ctx); err != nil {
		return 0, err
	}
	if olderThan < 0 {
		return 0, fmt.Errorf("task queue cleanup duration cannot be negative")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return 0, ErrQueueClosed
	}
	cutoff := q.now().Add(-olderThan)
	kept := make([]QueueItem, 0, len(q.items))
	removed := 0
	for _, item := range q.items {
		terminal := item.Status == QueueCompleted || item.Status == QueueCancelled || item.Status == QueueDead
		if terminal && !item.UpdatedAt.After(cutoff) {
			removed++
			continue
		}
		kept = append(kept, item)
	}
	if removed == 0 {
		return 0, nil
	}
	previous := q.items
	q.items = kept
	if err := q.persistLocked(); err != nil {
		q.items = previous
		return 0, err
	}
	return removed, nil
}

func (q *Queue) Snapshot() []QueueItem {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	items := cloneQueueItems(q.items)
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items
}

func (q *Queue) Close() error {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil
	}
	if err := q.persistLocked(); err != nil {
		return err
	}
	q.closed = true
	return nil
}

func (q *Queue) persistLocked() error {
	data, err := json.Marshal(queueState{Version: queueStateVersion, Items: q.items})
	if err != nil {
		return fmt.Errorf("encode task queue: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(q.path), ".task-queue-*.tmp")
	if err != nil {
		return fmt.Errorf("create task queue temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure task queue temporary file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write task queue: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync task queue: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close task queue: %w", err)
	}
	if err := replaceTaskQueueFile(temporaryPath, q.path); err != nil {
		return fmt.Errorf("replace task queue: %w", err)
	}
	return nil
}

func (q *Queue) itemIndex(id string) int {
	for index := range q.items {
		if q.items[index].ID == id {
			return index
		}
	}
	return -1
}

func (q *Queue) retryDelay(attempt int) time.Duration {
	delay := q.options.BaseBackoff
	for current := 1; current < attempt && delay < q.options.MaxBackoff; current++ {
		if delay > q.options.MaxBackoff/2 {
			return q.options.MaxBackoff
		}
		delay *= 2
	}
	if delay > q.options.MaxBackoff {
		return q.options.MaxBackoff
	}
	return delay
}

func (q *Queue) now() time.Time { return q.options.Clock().UTC() }

func validateQueueItems(items []QueueItem) error {
	seenIDs := make(map[string]struct{}, len(items))
	seenKeys := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.ID == "" || item.IdempotencyKey == "" || len(item.Payload) > maxQueuePayload || !json.Valid(item.Payload) {
			return fmt.Errorf("task queue contains an invalid item")
		}
		if _, exists := seenIDs[item.ID]; exists {
			return fmt.Errorf("task queue contains duplicate item ID %q", item.ID)
		}
		if _, exists := seenKeys[item.IdempotencyKey]; exists {
			return fmt.Errorf("task queue contains duplicate idempotency key")
		}
		seenIDs[item.ID] = struct{}{}
		seenKeys[item.IdempotencyKey] = struct{}{}
		switch item.Status {
		case QueuePending, QueueLeased, QueueCompleted, QueueCancelled, QueueDead:
		default:
			return fmt.Errorf("task queue contains invalid status %q", item.Status)
		}
	}
	return nil
}

func cloneQueueItems(items []QueueItem) []QueueItem {
	result := make([]QueueItem, len(items))
	for index, item := range items {
		result[index] = cloneQueueItem(item)
	}
	return result
}

func cloneQueueItem(item QueueItem) QueueItem {
	item.Payload = append(json.RawMessage(nil), item.Payload...)
	return item
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func newQueueID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate task queue ID: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func queueJitter(delay time.Duration) time.Duration {
	maximum := delay / 4
	if maximum <= 0 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(maximum)+1))
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64())
}

func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

func boundedQueueError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}

type QueueHandler func(context.Context, QueueItem) error

type QueueWorkerOptions struct {
	ID           string
	PollInterval time.Duration
}

type QueueWorker struct {
	queue   *Queue
	options QueueWorkerOptions
	handler QueueHandler
	start   sync.Once
	stop    sync.Once
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func NewQueueWorker(queue *Queue, options QueueWorkerOptions, handler QueueHandler) (*QueueWorker, error) {
	options.ID = strings.TrimSpace(options.ID)
	if queue == nil || handler == nil || options.ID == "" {
		return nil, fmt.Errorf("task queue, worker ID, and handler are required")
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 100 * time.Millisecond
	}
	return &QueueWorker{queue: queue, options: options, handler: handler, stopCh: make(chan struct{}), doneCh: make(chan struct{})}, nil
}

func (worker *QueueWorker) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	worker.start.Do(func() { go worker.run(ctx) })
}

func (worker *QueueWorker) run(ctx context.Context) {
	defer close(worker.doneCh)
	_, _ = worker.queue.Reconcile(context.Background())
	for {
		select {
		case <-worker.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		item, err := worker.queue.Claim(ctx, worker.options.ID)
		if errors.Is(err, ErrQueueEmpty) {
			timer := time.NewTimer(worker.options.PollInterval)
			select {
			case <-worker.stopCh:
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
			continue
		}
		if err != nil {
			return
		}
		if handleErr := worker.handler(ctx, item); handleErr != nil {
			_, _ = worker.queue.Fail(context.Background(), item.ID, worker.options.ID, handleErr)
		} else {
			_ = worker.queue.Complete(context.Background(), item.ID, worker.options.ID)
		}
	}
}

func (worker *QueueWorker) Drain(ctx context.Context) error {
	if worker == nil {
		return nil
	}
	worker.stop.Do(func() { close(worker.stopCh) })
	worker.start.Do(func() { close(worker.doneCh) })
	select {
	case <-worker.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
