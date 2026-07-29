package protocol

import (
	"context"

	"cyber-code/internal/core"
)

const Version = 1

type Request struct {
	Version      int    `json:"version"`
	ID           string `json:"id,omitempty"`
	Type         string `json:"type"`
	Prompt       string `json:"prompt,omitempty"`
	Reason       string `json:"reason,omitempty"`
	PermissionID string `json:"permission_id,omitempty"`
	Decision     string `json:"decision,omitempty"`
}

type Response struct {
	Version    int               `json:"version"`
	ID         string            `json:"id,omitempty"`
	Type       string            `json:"type"`
	Event      *core.Event       `json:"event,omitempty"`
	Status     *Status           `json:"status,omitempty"`
	Error      string            `json:"error,omitempty"`
	Permission *PermissionPrompt `json:"permission,omitempty"`
	Canceled   bool              `json:"canceled,omitempty"`
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
