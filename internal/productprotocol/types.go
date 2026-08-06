// Package productprotocol defines the cross-client CYBER product event schema.
package productprotocol

import "encoding/json"

const SchemaVersion = 1

type EventKind string

const (
	EventKindKnown   EventKind = "known"
	EventKindUnknown EventKind = "unknown"
)

type EventSourceRef struct {
	RuntimeID  string `json:"runtimeId"`
	AgentID    string `json:"agentId,omitempty"`
	ToolCallID string `json:"toolCallId,omitempty"`
}

type Event struct {
	SchemaVersion int             `json:"schemaVersion"`
	EventID       string          `json:"eventId"`
	TaskID        string          `json:"taskId"`
	Cursor        int             `json:"cursor"`
	OccurredAt    string          `json:"occurredAt"`
	Type          string          `json:"type"`
	Source        EventSourceRef  `json:"source"`
	Payload       json.RawMessage `json:"payload"`
	Kind          EventKind       `json:"kind"`
}

type ScopeSnapshot struct {
	ID             string   `json:"id"`
	Principal      string   `json:"principal"`
	Workspace      string   `json:"workspace"`
	Validity       string   `json:"validity"`
	Targets        []string `json:"targets"`
	AllowedActions []string `json:"allowedActions"`
	DeniedActions  []string `json:"deniedActions"`
	RiskCeiling    string   `json:"riskCeiling"`
}

type AgentState struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	Progress      *float64 `json:"progress,omitempty"`
	CurrentAction string   `json:"currentAction,omitempty"`
	AgentID       string   `json:"agentId,omitempty"`
}

type ImmutableEvidence struct {
	ID      string         `json:"id"`
	TaskID  string         `json:"taskId"`
	Kind    string         `json:"kind"`
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data"`
}

type FindingState struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Severity        string   `json:"severity"`
	Status          string   `json:"status"`
	Confidence      string   `json:"confidence"`
	EvidenceIDs     []string `json:"evidenceIds"`
	RejectionReason string   `json:"rejectionReason,omitempty"`
}

type ApprovalChallenge struct {
	ID              string `json:"id"`
	AgentID         string `json:"agentId"`
	Action          string `json:"action"`
	Target          string `json:"target"`
	ParameterDigest string `json:"parameterDigest"`
	Risk            string `json:"risk"`
	ExpiresAt       string `json:"expiresAt"`
}

type ApprovalState struct {
	ApprovalChallenge
	Decision string `json:"decision,omitempty"`
}

type ControlLease struct {
	ClientID string `json:"clientId"`
	Revision int    `json:"revision"`
}

type TerminalOutputChunk struct {
	Sequence   int    `json:"sequence"`
	Data       string `json:"data"`
	ByteLength int    `json:"byteLength"`
}

type TerminalSessionState struct {
	ID                 string                `json:"id"`
	ProfileID          string                `json:"profileId"`
	ProcessID          string                `json:"processId"`
	WorkingDirectory   string                `json:"workingDirectory"`
	ScopeID            string                `json:"scopeId"`
	OwnerClientID      string                `json:"ownerClientId"`
	LeaseRevision      int                   `json:"leaseRevision"`
	Columns            int                   `json:"columns"`
	Rows               int                   `json:"rows"`
	OutputLimitBytes   int                   `json:"outputLimitBytes"`
	OutputBytes        int                   `json:"outputBytes"`
	NextInputSequence  int                   `json:"nextInputSequence"`
	NextOutputSequence int                   `json:"nextOutputSequence"`
	Status             string                `json:"status"`
	Output             []TerminalOutputChunk `json:"output"`
	ExitCode           *int                  `json:"exitCode,omitempty"`
	ExitReason         string                `json:"exitReason,omitempty"`
}

type ReportFinding struct {
	Finding         FindingState        `json:"finding"`
	Evidence        []ImmutableEvidence `json:"evidence"`
	Included        bool                `json:"included"`
	ExclusionReason string              `json:"exclusionReason,omitempty"`
}

type ReportState struct {
	ID              string          `json:"id"`
	TaskID          string          `json:"taskId"`
	Version         int             `json:"version"`
	Status          string          `json:"status"`
	Narrative       string          `json:"narrative"`
	Recommendations string          `json:"recommendations"`
	HumanNotes      string          `json:"humanNotes"`
	Findings        []ReportFinding `json:"findings"`
}
