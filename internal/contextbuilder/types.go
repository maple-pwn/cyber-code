// Package contextbuilder assembles provider context from bounded, attributed
// instruction sources without changing authorization state.
package contextbuilder

import (
	"errors"

	"cyber-code/internal/core"
)

const IdentitySourceID = "cyber-code:identity"

const (
	DefaultMaxFileBytes     int64 = 256 << 10
	DefaultMaxTotalBytes    int64 = 1 << 20
	DefaultWarningThreshold       = 0.80
	DefaultCompactThreshold       = 0.90
)

var (
	ErrInvalidSource       = errors.New("invalid context source")
	ErrBudgetExceeded      = errors.New("context budget exceeded")
	ErrPathOutsideRoot     = errors.New("instruction path is outside its root")
	ErrInstructionTooLarge = errors.New("instruction content is too large")
)

type SourceKind string

const (
	SourceIdentity SourceKind = "identity"
	SourceUser     SourceKind = "user"
	SourceProject  SourceKind = "project"
	SourceAgent    SourceKind = "agent"
	SourceSkill    SourceKind = "skill"
	SourceSession  SourceKind = "session"
	SourceMemory   SourceKind = "memory"
	SourceRuntime  SourceKind = "runtime"
)

// Source is one attributed system-instruction segment. Trusted indicates
// whether the source belongs to cyber-code's fixed runtime policy; it does not
// grant permissions and is never derived from source content.
type Source struct {
	ID       string
	Kind     SourceKind
	Path     string
	Priority int
	Trusted  bool
	Required bool
	Content  string
}

type SourceMetadata struct {
	ID              string     `json:"id"`
	Kind            SourceKind `json:"kind"`
	Path            string     `json:"path,omitempty"`
	Priority        int        `json:"priority"`
	Trusted         bool       `json:"trusted"`
	Required        bool       `json:"required,omitempty"`
	Bytes           int        `json:"bytes"`
	EstimatedTokens int        `json:"estimated_tokens"`
	Digest          string     `json:"digest"`
	Truncated       bool       `json:"truncated,omitempty"`
}

type Diagnostic struct {
	SourceID string `json:"source_id,omitempty"`
	Kind     string `json:"kind"`
	Message  string `json:"message"`
}

type BuildInput struct {
	Model           string
	Messages        []core.Message
	Tools           []core.ToolDefinition
	Sources         []Source
	AllowOverBudget bool
}

type Plan struct {
	System          []core.ContentBlock
	Messages        []core.Message
	Tools           []core.ToolDefinition
	Sources         []SourceMetadata
	Diagnostics     []Diagnostic
	EstimatedTokens int
	MaxOutputTokens int
	Budget          BudgetMetadata
}

// BudgetMetadata describes immutable context utilization for one plan.
type BudgetMetadata struct {
	ContextWindow    int
	ReservedOutput   int
	InputLimit       int
	UtilizationRatio float64
	WarningThreshold float64
	CompactThreshold float64
	WarningExceeded  bool
	CompactExceeded  bool
}

type Options struct {
	Sources          []Source
	ContextWindow    int
	ReservedOutput   int
	EstimateText     func(string) int
	WarningThreshold float64
	CompactThreshold float64
}
