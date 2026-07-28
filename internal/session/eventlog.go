package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"cyber-code/internal/core"
)

type EventRecord struct {
	Sequence  uint64     `json:"sequence"`
	SessionID string     `json:"session_id"`
	Time      time.Time  `json:"time"`
	Event     core.Event `json:"event"`
}

func (store *Store) Append(ctx context.Context, sessionID string, event core.Event) (EventRecord, error) {
	if err := contextError(ctx); err != nil {
		return EventRecord{}, err
	}
	state, err := store.state(sessionID)
	if err != nil {
		return EventRecord{}, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	release, err := store.lockSession(sessionID)
	if err != nil {
		return EventRecord{}, err
	}
	defer release()
	if _, err := store.ensureSessionDir(sessionID); err != nil {
		return EventRecord{}, err
	}
	path := store.eventLogPath(sessionID)
	records, validBytes, incomplete, err := readEventLog(path, sessionID)
	if err != nil {
		return EventRecord{}, err
	}
	if incomplete {
		if err := os.Truncate(path, validBytes); err != nil {
			return EventRecord{}, fmt.Errorf("truncate incomplete event log tail: %w", err)
		}
	}
	next := uint64(len(records) + 1)
	if event.Type == core.EventCompacted && event.CoveredSequence == 0 && next > 1 {
		event.CoveredSequence = next - 1
	}
	record := EventRecord{Sequence: next, SessionID: sessionID, Time: store.now().UTC(), Event: event}
	encoded, err := json.Marshal(record)
	if err != nil {
		return EventRecord{}, fmt.Errorf("encode session event: %w", err)
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return EventRecord{}, fmt.Errorf("open session event log: %w", err)
	}
	if err := restrictFile(path); err != nil {
		_ = file.Close()
		return EventRecord{}, fmt.Errorf("restrict session event log: %w", err)
	}
	written, writeErr := file.Write(encoded)
	if writeErr == nil && written != len(encoded) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return EventRecord{}, fmt.Errorf("append session event: %w", errors.Join(writeErr, syncErr, closeErr))
	}
	return record, nil
}

func (store *Store) Events(ctx context.Context, sessionID string) ([]EventRecord, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	state, err := store.state(sessionID)
	if err != nil {
		return nil, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	release, err := store.lockSession(sessionID)
	if err != nil {
		return nil, err
	}
	defer release()
	records, _, _, err := readEventLog(store.eventLogPath(sessionID), sessionID)
	if err != nil {
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return records, nil
}

func readEventLog(path, sessionID string) ([]EventRecord, int64, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("read session event log: %w", err)
	}
	var records []EventRecord
	offset := 0
	lineNumber := 0
	for offset < len(data) {
		newline := bytes.IndexByte(data[offset:], '\n')
		if newline < 0 {
			return records, int64(offset), true, nil
		}
		lineNumber++
		line := data[offset : offset+newline]
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, 0, false, fmt.Errorf("decode session event log line %d: empty record", lineNumber)
		}
		var record EventRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, 0, false, fmt.Errorf("decode session event log line %d: %w", lineNumber, err)
		}
		wantSequence := uint64(len(records) + 1)
		if record.Sequence != wantSequence || record.SessionID != sessionID || record.Time.IsZero() || record.Event.Type == "" {
			return nil, 0, false, fmt.Errorf("validate session event log line %d: invalid record metadata", lineNumber)
		}
		records = append(records, record)
		offset += newline + 1
	}
	return records, int64(offset), false, nil
}
