package cyberagent

import (
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
	"runtime"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/productstate"
	"cyber-code/internal/runtimeapi"
)

var (
	ErrSourceBindingConflict = errors.New("cyber_agent_source_binding_conflict")
	ErrSourceCursorGap       = errors.New("cyber_agent_source_cursor_gap")
	ErrSourceCursorConflict  = errors.New("cyber_agent_source_cursor_conflict")
)

type SourceBinding struct {
	SchemaVersion int         `json:"schema_version"`
	TaskID        string      `json:"task_id"`
	SessionID     string      `json:"session_id"`
	Cursor        EventCursor `json:"cursor"`
}

type BindingStore struct {
	root string
	mu   sync.Mutex
}

func NewBindingStore(root string) (*BindingStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("cyber-agent binding store root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create cyber-agent binding store: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure cyber-agent binding store: %w", err)
	}
	return &BindingStore{root: root}, nil
}

func (store *BindingStore) Load(ctx context.Context, taskID string) (SourceBinding, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SourceBinding{}, false, err
	}
	path, err := store.path(taskID)
	if err != nil {
		return SourceBinding{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return SourceBinding{}, false, nil
	}
	if err != nil {
		return SourceBinding{}, false, err
	}
	var binding SourceBinding
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&binding); err != nil {
		return SourceBinding{}, false, fmt.Errorf("decode cyber-agent source binding: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return SourceBinding{}, false, fmt.Errorf("decode cyber-agent source binding: trailing JSON content")
	}
	if err := validateBinding(binding); err != nil {
		return SourceBinding{}, false, err
	}
	return binding, true, nil
}

func (store *BindingStore) Save(ctx context.Context, binding SourceBinding) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateBinding(binding); err != nil {
		return err
	}
	path, err := store.path(binding.TaskID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	file, err := os.CreateTemp(store.root, ".binding-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err == nil {
		_, err = file.Write(encoded)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write cyber-agent source binding: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace cyber-agent source binding: %w", err)
	}
	return syncBindingDirectory(store.root)
}

func (store *BindingStore) path(taskID string) (string, error) {
	if strings.TrimSpace(taskID) == "" {
		return "", fmt.Errorf("cyber-agent binding task ID is required")
	}
	digest := sha256.Sum256([]byte(taskID))
	return filepath.Join(store.root, hex.EncodeToString(digest[:])+".json"), nil
}

func validateBinding(binding SourceBinding) error {
	if binding.SchemaVersion != 1 || binding.TaskID == "" || binding.SessionID == "" || binding.Cursor.SessionID != binding.SessionID || binding.Cursor.Sequence < 0 || (binding.Cursor.Sequence == 0) != (binding.Cursor.EventID == "") {
		return fmt.Errorf("cyber-agent source binding is invalid")
	}
	return nil
}

type RemoteSourceOptions struct {
	Client    *Client
	Store     *runtimeapi.Store
	Bindings  *BindingStore
	TaskID    string
	SessionID string
	RuntimeID string
}

type RemoteSource struct {
	client    *Client
	store     *runtimeapi.Store
	bindings  *BindingStore
	taskID    string
	sessionID string
	runtimeID string
	mu        sync.Mutex
}

func NewRemoteSource(options RemoteSourceOptions) (*RemoteSource, error) {
	if options.Client == nil || options.Store == nil || options.Bindings == nil {
		return nil, fmt.Errorf("cyber-agent client, runtime store, and binding store are required")
	}
	if options.TaskID == "" || options.SessionID == "" || options.RuntimeID == "" {
		return nil, fmt.Errorf("cyber-agent source identities are required")
	}
	return &RemoteSource{client: options.Client, store: options.Store, bindings: options.Bindings, taskID: options.TaskID, sessionID: options.SessionID, runtimeID: options.RuntimeID}, nil
}

func (source *RemoteSource) Attach(ctx context.Context) (SessionSnapshot, error) {
	snapshot, err := source.client.Snapshot(ctx, source.sessionID)
	if err != nil {
		return SessionSnapshot{}, err
	}
	if snapshot.SessionID != source.sessionID || snapshot.TaskID != source.taskID || snapshot.EventCursor.SessionID != source.sessionID ||
		snapshot.EventCursor.Sequence < 0 || (snapshot.EventCursor.Sequence == 0) != (snapshot.EventCursor.EventID == "") {
		return SessionSnapshot{}, ErrSourceBindingConflict
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	binding, exists, err := source.bindings.Load(ctx, source.taskID)
	if err != nil {
		return SessionSnapshot{}, err
	}
	if exists && (binding.TaskID != source.taskID || binding.SessionID != source.sessionID) {
		return SessionSnapshot{}, ErrSourceBindingConflict
	}
	events, state, err := source.store.Load(ctx, source.taskID)
	if err != nil {
		return SessionSnapshot{}, err
	}
	durable := SourceBinding{SchemaVersion: 1, TaskID: source.taskID, SessionID: source.sessionID, Cursor: EventCursor{SessionID: source.sessionID}}
	if len(events) > 0 {
		last := events[len(events)-1]
		if last.Origin == nil || last.Origin.SourceSessionID != source.sessionID || last.Origin.SourceSequence != state.CommittedCursor {
			return SessionSnapshot{}, ErrSourceBindingConflict
		}
		durable.Cursor.Sequence = last.Origin.SourceSequence
		durable.Cursor.EventID = last.Origin.SourceEventID
	}
	if exists && binding.Cursor.Sequence > durable.Cursor.Sequence {
		return SessionSnapshot{}, ErrSourceCursorConflict
	}
	if snapshot.EventCursor.Sequence < durable.Cursor.Sequence || (snapshot.EventCursor.Sequence == durable.Cursor.Sequence && durable.Cursor.Sequence > 0 && snapshot.EventCursor.EventID != durable.Cursor.EventID) {
		return SessionSnapshot{}, ErrSourceCursorConflict
	}
	if !exists || binding.Cursor != durable.Cursor {
		if err := source.bindings.Save(ctx, durable); err != nil {
			return SessionSnapshot{}, err
		}
	}
	return snapshot, nil
}

func syncBindingDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}

func (source *RemoteSource) Apply(ctx context.Context, event EventEnvelope) (productstate.State, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if event.TaskID != source.taskID || event.SessionID != source.sessionID {
		return productstate.State{}, ErrSourceBindingConflict
	}
	binding, exists, err := source.bindings.Load(ctx, source.taskID)
	if err != nil {
		return productstate.State{}, err
	}
	if !exists || binding.SessionID != source.sessionID {
		return productstate.State{}, ErrSourceBindingConflict
	}
	events, state, err := source.store.Load(ctx, source.taskID)
	if err != nil {
		return productstate.State{}, err
	}
	if event.Sequence <= binding.Cursor.Sequence {
		for _, existing := range events {
			if existing.Cursor == event.Sequence && existing.Origin != nil {
				if existing.Origin.SourceEventID == event.EventID {
					return state, nil
				}
				return productstate.State{}, ErrSourceCursorConflict
			}
		}
		return productstate.State{}, ErrSourceCursorConflict
	}
	if event.Sequence != binding.Cursor.Sequence+1 {
		return productstate.State{}, fmt.Errorf("%w: expected %d, got %d", ErrSourceCursorGap, binding.Cursor.Sequence+1, event.Sequence)
	}
	mapped, err := ProjectEvent(event, source.runtimeID)
	if err != nil {
		return productstate.State{}, err
	}
	state, err = source.store.CommitExternal(ctx, mapped)
	if err != nil {
		return productstate.State{}, err
	}
	binding.Cursor = EventCursor{SessionID: source.sessionID, Sequence: event.Sequence, EventID: event.EventID}
	if err := source.bindings.Save(ctx, binding); err != nil {
		return productstate.State{}, err
	}
	return state, nil
}

// Run follows the authoritative event stream until the context is cancelled.
// Cursor conflicts trigger a snapshot check followed by a replay from sequence
// zero; Apply makes exact replay idempotent.
func (source *RemoteSource) Run(ctx context.Context, onEvent func(productstate.State)) error {
	if _, err := source.Attach(ctx); err != nil {
		return err
	}
	replayFromStart := false
	backoff := 100 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		binding, _, err := source.bindings.Load(ctx, source.taskID)
		if err != nil {
			return err
		}
		var after *EventCursor
		if !replayFromStart {
			cursor := binding.Cursor
			after = &cursor
		}
		stream, err := source.client.Events(ctx, source.sessionID, after)
		if err != nil {
			var apiError *APIError
			if errors.As(err, &apiError) && apiError.StatusCode == 409 {
				if _, snapshotErr := source.Attach(ctx); snapshotErr != nil {
					return snapshotErr
				}
				replayFromStart = true
				continue
			}
			return err
		}
		for {
			event, nextErr := stream.Next(ctx)
			if nextErr != nil {
				_ = stream.Close()
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if !errors.Is(nextErr, io.EOF) {
					return nextErr
				}
				break
			}
			state, applyErr := source.Apply(ctx, event)
			if applyErr != nil {
				_ = stream.Close()
				if errors.Is(applyErr, ErrSourceCursorGap) || errors.Is(applyErr, ErrSourceCursorConflict) {
					replayFromStart = true
					break
				}
				return applyErr
			}
			replayFromStart = false
			backoff = 100 * time.Millisecond
			if onEvent != nil {
				onEvent(state)
			}
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}
