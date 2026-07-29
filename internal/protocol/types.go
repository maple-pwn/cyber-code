package protocol

import (
	"context"

	"cyber-code/internal/core"
)

const Version = 1

type Request struct {
	Version      int         `json:"version"`
	ID           string      `json:"id,omitempty"`
	Type         string      `json:"type"`
	Prompt       string      `json:"prompt,omitempty"`
	Reason       string      `json:"reason,omitempty"`
	PermissionID string      `json:"permission_id,omitempty"`
	Decision     string      `json:"decision,omitempty"`
	IDEContext   *IDEContext `json:"ide_context,omitempty"`
}

type Response struct {
	Version    int               `json:"version"`
	ID         string            `json:"id,omitempty"`
	Type       string            `json:"type"`
	Event      *core.Event       `json:"event,omitempty"`
	Status     *Status           `json:"status,omitempty"`
	Error      string            `json:"error,omitempty"`
	Permission *PermissionPrompt `json:"permission,omitempty"`
	Diff       *IDEDiff          `json:"diff,omitempty"`
	Canceled   bool              `json:"canceled,omitempty"`
}

// IDEContext carries optional editor state without making the protocol depend
// on a specific editor API. Line and character positions are zero-based.
type IDEContext struct {
	Workspace   string          `json:"workspace,omitempty"`
	Focus       string          `json:"focus,omitempty"`
	Selection   *IDESelection   `json:"selection,omitempty"`
	Diagnostics []IDEDiagnostic `json:"diagnostics,omitempty"`
}

type IDEPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type IDESelection struct {
	Path  string      `json:"path"`
	Start IDEPosition `json:"start"`
	End   IDEPosition `json:"end"`
	Text  string      `json:"text,omitempty"`
}

type IDEDiagnostic struct {
	Path     string      `json:"path"`
	Message  string      `json:"message"`
	Severity string      `json:"severity,omitempty"`
	Start    IDEPosition `json:"start,omitempty"`
	End      IDEPosition `json:"end,omitempty"`
}

type IDEDiff struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type Status struct {
	SessionID string `json:"session_id,omitempty"`
	Running   bool   `json:"running"`
	History   int    `json:"history_messages"`
}

type Runtime interface {
	Run(ctx context.Context, prompt string) <-chan core.Event
	SessionID() string
	History() []core.Message
}

// IDERuntime is an optional extension implemented by runtimes that can use
// editor context. Servers fall back to Runtime.Run for older implementations.
type IDERuntime interface {
	RunWithIDEContext(ctx context.Context, prompt string, ide *IDEContext) <-chan core.Event
}
