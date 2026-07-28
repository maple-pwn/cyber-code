package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

type ExportOptions struct {
	Secrets []string
}

type sessionExport struct {
	SessionID string        `json:"session_id"`
	Events    []EventRecord `json:"events"`
	Snapshot  *Snapshot     `json:"snapshot,omitempty"`
}

var (
	exportSecretAssignment = regexp.MustCompile(`(?i)\b(api[_-]?key|authorization|access[_-]?token|token|secret)(\s*[:=]\s*)([^\s,;]+)`)
	exportBearer           = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	exportOpenAIToken      = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{4,}\b`)
)

func (store *Store) Export(ctx context.Context, sessionID string, destination io.Writer, options ExportOptions) error {
	if destination == nil {
		return fmt.Errorf("export destination is required")
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	state, err := store.state(sessionID)
	if err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	events, _, _, err := readEventLog(store.eventLogPath(sessionID), sessionID)
	if err != nil {
		return err
	}
	exported := sessionExport{SessionID: sessionID, Events: events}
	snapshot, err := store.loadSnapshot(ctx, sessionID)
	if err == nil {
		exported.Snapshot = &snapshot
	} else if !errors.Is(err, ErrSessionNotFound) {
		return err
	}
	encoded, err := json.Marshal(exported)
	if err != nil {
		return fmt.Errorf("encode session export: %w", err)
	}
	var structured any
	if err := json.Unmarshal(encoded, &structured); err != nil {
		return fmt.Errorf("prepare session export: %w", err)
	}
	secrets := append([]string(nil), options.Secrets...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	redactJSONStrings(structured, secrets)
	if err := contextError(ctx); err != nil {
		return err
	}
	encoder := json.NewEncoder(destination)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(structured); err != nil {
		return fmt.Errorf("write session export: %w", err)
	}
	return nil
}

func redactJSONStrings(value any, secrets []string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if text, ok := child.(string); ok {
				typed[key] = redactExportText(text, secrets)
				continue
			}
			redactJSONStrings(child, secrets)
		}
	case []any:
		for index, child := range typed {
			if text, ok := child.(string); ok {
				typed[index] = redactExportText(text, secrets)
				continue
			}
			redactJSONStrings(child, secrets)
		}
	}
}

func redactExportText(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	text = exportBearer.ReplaceAllString(text, "Bearer [REDACTED]")
	text = exportSecretAssignment.ReplaceAllString(text, "$1$2[REDACTED]")
	return exportOpenAIToken.ReplaceAllString(text, "[REDACTED]")
}
