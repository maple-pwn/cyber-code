package bridge

import "encoding/json"

// IDEMessage is the stable, editor-neutral envelope used by IDE adapters.
type IDEMessage struct {
	Version int             `json:"version"`
	Type    string          `json:"type"`
	Path    string          `json:"path,omitempty"`
	Start   Position        `json:"start,omitempty"`
	End     Position        `json:"end,omitempty"`
	Text    string          `json:"text,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Diagnostic struct {
	Path     string   `json:"path"`
	Message  string   `json:"message"`
	Severity string   `json:"severity,omitempty"`
	Start    Position `json:"start,omitempty"`
	End      Position `json:"end,omitempty"`
}

type Diff struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}
