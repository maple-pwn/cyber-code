package runtimeapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
)

var (
	ErrEvidenceConflict      = errors.New("evidence_conflict")
	ErrEventIdentityConflict = errors.New("event_identity_conflict")
	ErrCorruptTransactionLog = errors.New("corrupt_transaction_log")
)

type DraftEvent struct {
	TaskID     string
	Type       string
	OccurredAt time.Time
	Source     productprotocol.EventSourceRef
	Payload    any
}

type CommitOption func(*commitOptions)

type commitOptions struct {
	eventID string
}

func WithEventID(eventID string) CommitOption {
	return func(options *commitOptions) { options.eventID = eventID }
}

type transactionRecord struct {
	Event    productprotocol.Event `json:"event"`
	Snapshot productstate.State    `json:"snapshot"`
}

// Store appends one event and its resulting snapshot as a single durable
// transaction record. A partial final record is never acknowledged or loaded.
type Store struct {
	root string
	mu   sync.Mutex
}

func NewStore(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("runtime store root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create runtime store: %w", err)
	}
	return &Store{root: root}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) Commit(ctx context.Context, draft DraftEvent, options ...CommitOption) (productprotocol.Event, productstate.State, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return productprotocol.Event{}, productstate.State{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	events, previous, err := s.loadLocked(ctx, draft.TaskID)
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, err
	}
	settings := commitOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	cursor := previous.CommittedCursor + 1
	eventID := settings.eventID
	if eventID == "" {
		eventID = generatedEventID(draft.Source.RuntimeID, draft.TaskID, cursor, draft.Type)
	}
	for _, existing := range events {
		if existing.EventID == eventID {
			return productprotocol.Event{}, productstate.State{}, ErrEventIdentityConflict
		}
	}
	payload, err := json.Marshal(draft.Payload)
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, fmt.Errorf("encode event payload: %w", err)
	}
	occurredAt := draft.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	candidate := productprotocol.Event{
		SchemaVersion: productprotocol.SchemaVersion, EventID: eventID, TaskID: draft.TaskID,
		Cursor: cursor, OccurredAt: occurredAt.UTC().Format(time.RFC3339Nano), Type: draft.Type,
		Source: draft.Source, Payload: payload,
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, err
	}
	event, err := productprotocol.Validate(raw)
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, fmt.Errorf("validate product event: %w", err)
	}
	if err := rejectEvidenceMutation(previous, event); err != nil {
		return productprotocol.Event{}, productstate.State{}, err
	}
	projected, err := productstate.Project(previous, event)
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, err
	}
	if projected.Kind != productstate.ProjectionApplied {
		return productprotocol.Event{}, productstate.State{}, fmt.Errorf("unexpected projection result %q", projected.Kind)
	}
	record := transactionRecord{Event: event, Snapshot: projected.State}
	encoded, err := json.Marshal(record)
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, err
	}
	path, err := s.transactionPath(draft.TaskID)
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, fmt.Errorf("open transaction log: %w", err)
	}
	if _, err = file.Write(append(encoded, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return productprotocol.Event{}, productstate.State{}, fmt.Errorf("commit transaction: %w", err)
	}
	if closeErr != nil {
		return productprotocol.Event{}, productstate.State{}, fmt.Errorf("close transaction log: %w", closeErr)
	}
	if err := syncDirectory(s.root); err != nil {
		return productprotocol.Event{}, productstate.State{}, fmt.Errorf("sync transaction directory: %w", err)
	}
	return event, projected.State, nil
}

func (s *Store) Load(ctx context.Context, taskID string) ([]productprotocol.Event, productstate.State, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, productstate.State{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(ctx, taskID)
}

func (s *Store) EventsAfter(ctx context.Context, taskID string, afterCursor int) ([]productprotocol.Event, error) {
	if afterCursor < 0 {
		return nil, fmt.Errorf("after cursor must be non-negative")
	}
	events, _, err := s.Load(ctx, taskID)
	if err != nil {
		return nil, err
	}
	index := len(events)
	for candidate, event := range events {
		if event.Cursor > afterCursor {
			index = candidate
			break
		}
	}
	return append([]productprotocol.Event(nil), events[index:]...), nil
}

func (s *Store) loadLocked(ctx context.Context, taskID string) ([]productprotocol.Event, productstate.State, error) {
	path, err := s.transactionPath(taskID)
	if err != nil {
		return nil, productstate.State{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []productprotocol.Event{}, productstate.Initial(), nil
	}
	if err != nil {
		return nil, productstate.State{}, fmt.Errorf("read transaction log: %w", err)
	}
	completeLength := len(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		if index := bytes.LastIndexByte(data, '\n'); index >= 0 {
			completeLength = index + 1
		} else {
			completeLength = 0
		}
		if err := os.Truncate(path, int64(completeLength)); err != nil {
			return nil, productstate.State{}, fmt.Errorf("discard incomplete transaction: %w", err)
		}
		data = data[:completeLength]
	}
	events := make([]productprotocol.Event, 0)
	state := productstate.Initial()
	reader := bufio.NewReader(bytes.NewReader(data))
	for line := 1; ; line++ {
		if err := ctx.Err(); err != nil {
			return nil, productstate.State{}, err
		}
		encoded, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(encoded)) > 0 {
			var record transactionRecord
			if err := json.Unmarshal(bytes.TrimSpace(encoded), &record); err != nil {
				return nil, productstate.State{}, fmt.Errorf("%w at line %d: %v", ErrCorruptTransactionLog, line, err)
			}
			canonical, err := json.Marshal(record.Event)
			if err != nil {
				return nil, productstate.State{}, err
			}
			validated, err := productprotocol.Validate(canonical)
			if err != nil {
				return nil, productstate.State{}, fmt.Errorf("%w at line %d: %v", ErrCorruptTransactionLog, line, err)
			}
			projected, err := productstate.Project(state, validated)
			if err != nil || projected.Kind != productstate.ProjectionApplied {
				return nil, productstate.State{}, fmt.Errorf("%w at line %d: projection failed", ErrCorruptTransactionLog, line)
			}
			want, _ := productprotocol.CanonicalJSON(projected.State)
			got, _ := productprotocol.CanonicalJSON(record.Snapshot)
			if !bytes.Equal(want, got) {
				return nil, productstate.State{}, fmt.Errorf("%w at line %d: snapshot mismatch", ErrCorruptTransactionLog, line)
			}
			events = append(events, validated)
			state = projected.State
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, productstate.State{}, readErr
		}
	}
	return events, state, nil
}

func (s *Store) transactionPath(taskID string) (string, error) {
	if strings.TrimSpace(taskID) == "" {
		return "", fmt.Errorf("task ID is required")
	}
	digest := sha256.Sum256([]byte(taskID))
	return filepath.Join(s.root, hex.EncodeToString(digest[:])+".transactions.jsonl"), nil
}

func generatedEventID(runtimeID, taskID string, cursor int, eventType string) string {
	digest := sha256.Sum256([]byte(runtimeID + "\x00" + taskID + "\x00" + fmt.Sprint(cursor) + "\x00" + eventType))
	return "evt-" + hex.EncodeToString(digest[:16])
}

func rejectEvidenceMutation(state productstate.State, event productprotocol.Event) error {
	if event.Type != "evidence.committed" {
		return nil
	}
	var payload struct {
		Evidence productprotocol.ImmutableEvidence `json:"evidence"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	existing, exists := state.Evidence[payload.Evidence.ID]
	if !exists {
		return nil
	}
	left, _ := productprotocol.CanonicalJSON(existing)
	right, _ := productprotocol.CanonicalJSON(payload.Evidence)
	if !bytes.Equal(left, right) {
		return ErrEvidenceConflict
	}
	return nil
}

func syncDirectory(path string) error {
	if goruntime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		// Windows does not support syncing directory handles. The transaction
		// file itself has already been flushed before this portability fallback.
		if errors.Is(err, os.ErrInvalid) {
			return nil
		}
		return err
	}
	return nil
}
