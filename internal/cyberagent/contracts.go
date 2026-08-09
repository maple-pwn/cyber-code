package cyberagent

import (
	"encoding/json"
	"time"
)

type RuntimeCapabilities struct {
	Product         string   `json:"product"`
	RuntimeVersion  string   `json:"runtime_version"`
	ProtocolVersion int      `json:"protocol_version"`
	Capabilities    []string `json:"capabilities"`
}

type TaskSubmission struct {
	Kind    string `json:"kind"`
	TaskID  string `json:"task_id"`
	Format  string `json:"format,omitempty"`
	Content string `json:"content"`
}

type CreateSessionOptions struct {
	AgentMode    string
	Autonomy     string
	HarnessRef   string
	Scope        []string
	MaxRounds    int
	MaxToolCalls int
	MaxLLMCalls  int
}

type EventCursor struct {
	SessionID string `json:"session_id"`
	Sequence  int    `json:"sequence"`
	EventID   string `json:"event_id"`
}

type InteractionRequest struct {
	InteractionID   string          `json:"interaction_id"`
	SessionID       string          `json:"session_id"`
	Kind            string          `json:"kind"`
	Prompt          string          `json:"prompt"`
	ActionRef       *string         `json:"action_ref"`
	Options         []string        `json:"options"`
	RequestedAt     time.Time       `json:"requested_at"`
	ExpiresAt       *time.Time      `json:"expires_at"`
	ApprovalRequest json.RawMessage `json:"approval_request"`
}

type InteractionResponse struct {
	InteractionID    string    `json:"interaction_id"`
	SessionID        string    `json:"session_id"`
	Answer           *string   `json:"answer,omitempty"`
	Approved         *bool     `json:"approved,omitempty"`
	ApprovalRevision *int      `json:"approval_revision,omitempty"`
	RespondedAt      time.Time `json:"responded_at"`
}

type SessionSnapshot struct {
	SchemaVersion             int                 `json:"schema_version"`
	SessionID                 string              `json:"session_id"`
	TaskID                    string              `json:"task_id"`
	Revision                  int                 `json:"revision"`
	Status                    string              `json:"status"`
	HarnessRef                string              `json:"harness_ref"`
	HarnessVersion            string              `json:"harness_version"`
	SkillRefs                 []string            `json:"skill_refs"`
	SkillVersions             []string            `json:"skill_versions"`
	SkillDigests              []string            `json:"skill_digests"`
	CapabilityLease           json.RawMessage     `json:"capability_lease"`
	WorkingPlan               []string            `json:"working_plan"`
	ChildRunRefs              []string            `json:"child_run_refs"`
	GraphRef                  *string             `json:"graph_ref"`
	ArtifactRefs              []string            `json:"artifact_refs"`
	EventCursor               EventCursor         `json:"event_cursor"`
	UnifiedStateRef           *string             `json:"unified_state_ref"`
	UnifiedStateVersion       *int                `json:"unified_state_version"`
	PendingInteraction        *InteractionRequest `json:"pending_interaction"`
	PendingAction             json.RawMessage     `json:"pending_action"`
	PendingActionState        *string             `json:"pending_action_state"`
	InFlightActionRef         *string             `json:"in_flight_action_ref"`
	PendingSubagent           json.RawMessage     `json:"pending_subagent"`
	BoundaryApprovals         []json.RawMessage   `json:"boundary_approvals"`
	ResumeTurn                *string             `json:"resume_turn"`
	FinishConfirmationPending bool                `json:"finish_confirmation_pending"`
	InteractionOutcomes       []string            `json:"interaction_outcomes"`
	MemorySummary             json.RawMessage     `json:"memory_summary"`
	ConversationHistory       []json.RawMessage   `json:"conversation_history"`
	RawToolOutputs            []json.RawMessage   `json:"raw_tool_outputs"`
	LastResponse              *string             `json:"last_response"`
	ActionsUsed               int                 `json:"actions_used"`
	LLMCallsUsed              int                 `json:"llm_calls_used"`
	ToolCallsUsed             int                 `json:"tool_calls_used"`
	UpdatedAt                 time.Time           `json:"updated_at"`
}

type EventEnvelope struct {
	EventID     string          `json:"event_id"`
	TaskID      string          `json:"task_id"`
	SessionID   string          `json:"session_id"`
	Sequence    int             `json:"sequence"`
	Topic       string          `json:"topic"`
	Payload     json.RawMessage `json:"payload"`
	EmittedBy   string          `json:"emitted_by"`
	EmittedAt   time.Time       `json:"emitted_at"`
	CausationID *string         `json:"causation_id"`
}

type SkillRecord struct {
	SkillRef      string          `json:"skill_ref"`
	Version       string          `json:"version"`
	Publisher     string          `json:"publisher,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	Source        string          `json:"source,omitempty"`
	ArchiveURL    string          `json:"archive_url,omitempty"`
	ArchiveSHA256 string          `json:"archive_sha256"`
	ContentDigest string          `json:"content_digest"`
	Signature     json.RawMessage `json:"signature,omitempty"`
	Revoked       bool            `json:"revoked,omitempty"`
	Trust         string          `json:"trust,omitempty"`
	RequiredTools []string        `json:"required_tools,omitempty"`
	MissingTools  []string        `json:"missing_tools,omitempty"`
	Active        bool            `json:"active,omitempty"`
}

type SkillList struct {
	Skills []SkillRecord `json:"skills"`
}

type SkillRemoveReceipt struct {
	SkillRef string `json:"skill_ref"`
	Removed  bool   `json:"removed"`
}
