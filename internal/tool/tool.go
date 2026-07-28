// Package tool defines the canonical, permission-gated tool contract.
package tool

import (
	"context"
	"encoding/json"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
)

type Spec struct {
	Name            string
	Description     string
	Schema          json.RawMessage
	ReadOnly        bool
	ConcurrencySafe bool
}

type Tool interface {
	Spec() Spec
	Authorize(context.Context, json.RawMessage) (permissions.Request, error)
	Run(context.Context, json.RawMessage) (core.ToolResult, error)
}
