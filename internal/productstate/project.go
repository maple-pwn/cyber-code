package productstate

import (
	"encoding/json"
	"errors"
	"time"

	"cyber-code/internal/productprotocol"
)

var (
	ErrEventIDConflict           = errors.New("event_id_conflict")
	ErrStaleCursor               = errors.New("stale_cursor")
	ErrInvalidFindingTransition  = errors.New("invalid_finding_transition")
	ErrApprovalChallengeConflict = errors.New("approval_challenge_conflict")
	ErrApprovalAlreadyResolved   = errors.New("approval_already_resolved")
	ErrApprovalExpired           = errors.New("approval_expired")
	ErrNonMonotonicLeaseRevision = errors.New("non_monotonic_lease_revision")
	ErrTerminalSessionConflict   = errors.New("terminal_session_conflict")
	ErrTerminalSessionNotOpen    = errors.New("terminal_session_not_open")
	ErrTerminalOutputSequence    = errors.New("terminal_output_sequence")
	ErrTerminalInputSequence     = errors.New("terminal_input_sequence")
	ErrTerminalOutputLimit       = errors.New("terminal_output_limit")
	ErrEditorDraftConflict       = errors.New("editor_draft_conflict")
	ErrEditorProvenanceMissing   = errors.New("editor_provenance_missing")
	ErrEditorDraftNotEditable    = errors.New("editor_draft_not_editable")
	ErrEditorDraftRevision       = errors.New("editor_draft_revision")
	ErrEditorPatchStale          = errors.New("editor_patch_stale")
	ErrEditorPatchNotApplied     = errors.New("editor_patch_not_applied")
)

type ProjectionKind string

const (
	ProjectionApplied        ProjectionKind = "applied"
	ProjectionDuplicate      ProjectionKind = "duplicate"
	ProjectionResyncRequired ProjectionKind = "resync-required"
)

type ProjectionResult struct {
	Kind           ProjectionKind
	State          State
	ExpectedCursor int
}

var findingTransitions = map[string]map[string]struct{}{
	"candidate": {"verifying": {}, "rejected": {}},
	"verifying": {"confirmed": {}, "rejected": {}},
	"confirmed": {"mitigated": {}},
	"rejected":  {},
	"mitigated": {},
}

func Project(previous State, event productprotocol.Event) (ProjectionResult, error) {
	canonical, err := productprotocol.CanonicalJSON(event)
	if err != nil {
		return ProjectionResult{}, err
	}
	if existing, ok := previous.CanonicalEvents[event.EventID]; ok {
		if existing != string(canonical) {
			return ProjectionResult{}, ErrEventIDConflict
		}
		state, err := cloneState(previous)
		return ProjectionResult{Kind: ProjectionDuplicate, State: state}, err
	}
	if event.Cursor <= previous.CommittedCursor {
		return ProjectionResult{}, ErrStaleCursor
	}
	if event.Cursor > previous.CommittedCursor+1 {
		state, err := cloneState(previous)
		return ProjectionResult{
			Kind: ProjectionResyncRequired, State: state, ExpectedCursor: previous.CommittedCursor + 1,
		}, err
	}

	state, err := cloneState(previous)
	if err != nil {
		return ProjectionResult{}, err
	}
	if state.EditorDrafts == nil {
		state.EditorDrafts = make(map[string]productprotocol.EditorDraftState)
	}
	storedEvent := event
	storedEvent.Payload = append(json.RawMessage(nil), event.Payload...)
	state.CommittedCursor = event.Cursor
	state.CanonicalEvents[event.EventID] = string(canonical)
	if event.Kind == productprotocol.EventKindUnknown {
		state.RawEvents = append(state.RawEvents, storedEvent)
		return ProjectionResult{Kind: ProjectionApplied, State: state}, nil
	}
	state.Timeline = append(state.Timeline, storedEvent)
	if err := applyKnownEvent(&state, event); err != nil {
		return ProjectionResult{}, err
	}
	return ProjectionResult{Kind: ProjectionApplied, State: state}, nil
}

func applyKnownEvent(state *State, event productprotocol.Event) error {
	switch event.Type {
	case "task.created", "task.started":
		var payload struct {
			Title string `json:"title"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		status := "created"
		if event.Type == "task.started" {
			status = "running"
		}
		state.Task = &TaskState{ID: event.TaskID, Title: payload.Title, Status: status}
	case "task.paused", "task.resumed", "task.cancel.requested", "task.cancelled", "task.completed", "task.failed", "task.blocked":
		if state.Task != nil {
			state.Task.Status = event.Type[len("task."):]
		}
	case "scope.proposed", "scope.confirmed":
		var payload struct {
			Scope productprotocol.ScopeSnapshot `json:"scope"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		state.Scope = &payload.Scope
	case "agent.started":
		var payload struct {
			Agent productprotocol.AgentState `json:"agent"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		state.Agents[payload.Agent.ID] = payload.Agent
	case "agent.progressed":
		var payload struct {
			AgentID       string  `json:"agentId"`
			Progress      float64 `json:"progress"`
			CurrentAction string  `json:"currentAction"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		if agent, ok := state.Agents[payload.AgentID]; ok {
			agent.AgentID = payload.AgentID
			agent.Progress = &payload.Progress
			if payload.CurrentAction != "" {
				agent.CurrentAction = payload.CurrentAction
			}
			state.Agents[payload.AgentID] = agent
		}
	case "agent.completed", "agent.failed":
		var payload struct {
			AgentID string `json:"agentId"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		if agent, ok := state.Agents[payload.AgentID]; ok {
			agent.Status = event.Type[len("agent."):]
			state.Agents[payload.AgentID] = agent
		}
	case "evidence.committed":
		var payload struct {
			Evidence productprotocol.ImmutableEvidence `json:"evidence"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		state.Evidence[payload.Evidence.ID] = payload.Evidence
	case "finding.created":
		var payload struct {
			Finding productprotocol.FindingState `json:"finding"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		state.Findings[payload.Finding.ID] = payload.Finding
	case "finding.verifying", "finding.confirmed", "finding.rejected", "finding.mitigated":
		var payload struct {
			FindingID string `json:"findingId"`
			Reason    string `json:"reason"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		finding, ok := state.Findings[payload.FindingID]
		status := event.Type[len("finding."):]
		if !ok {
			return ErrInvalidFindingTransition
		}
		if _, allowed := findingTransitions[finding.Status][status]; !allowed {
			return ErrInvalidFindingTransition
		}
		finding.Status = status
		if status == "rejected" {
			finding.RejectionReason = payload.Reason
		}
		state.Findings[payload.FindingID] = finding
	case "approval.requested":
		var payload struct {
			Challenge productprotocol.ApprovalChallenge `json:"challenge"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		if existing, ok := state.Approvals[payload.Challenge.ID]; ok {
			existingCanonical, err := productprotocol.CanonicalJSON(existing)
			if err != nil {
				return err
			}
			challengeCanonical, err := productprotocol.CanonicalJSON(payload.Challenge)
			if err != nil {
				return err
			}
			if string(existingCanonical) != string(challengeCanonical) {
				return ErrApprovalChallengeConflict
			}
		} else {
			state.Approvals[payload.Challenge.ID] = productprotocol.ApprovalState{ApprovalChallenge: payload.Challenge}
		}
	case "approval.resolved":
		var payload struct {
			ChallengeID string `json:"challengeId"`
			Decision    string `json:"decision"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		approval, ok := state.Approvals[payload.ChallengeID]
		if !ok || approval.Decision != "" {
			return ErrApprovalAlreadyResolved
		}
		occurredAt, _ := time.Parse(time.RFC3339Nano, event.OccurredAt)
		expiresAt, _ := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
		if occurredAt.After(expiresAt) {
			return ErrApprovalExpired
		}
		approval.Decision = payload.Decision
		state.Approvals[payload.ChallengeID] = approval
	case "control.acquired", "control.transferred":
		var payload struct {
			Lease productprotocol.ControlLease `json:"lease"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		if payload.Lease.Revision <= state.HighestCommittedLeaseRevision {
			return ErrNonMonotonicLeaseRevision
		}
		state.ControlLease = &payload.Lease
		state.HighestCommittedLeaseRevision = payload.Lease.Revision
	case "control.released":
		var payload struct {
			ClientID string `json:"clientId"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		if state.ControlLease != nil && state.ControlLease.ClientID == payload.ClientID {
			state.ControlLease = nil
		}
	case "report.drafted", "report.edited":
		var payload struct {
			Report productprotocol.ReportState `json:"report"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		state.Report = &payload.Report
	case "report.frozen":
		var payload struct {
			Version int `json:"version"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		if state.Report != nil {
			state.Report.Version = payload.Version
			state.Report.Status = "frozen"
		}
	case "terminal.opened":
		var payload struct {
			Session productprotocol.TerminalSessionState `json:"session"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		if _, exists := state.Terminals[payload.Session.ID]; exists {
			return ErrTerminalSessionConflict
		}
		payload.Session.Status = "open"
		payload.Session.NextInputSequence = 1
		payload.Session.NextOutputSequence = 1
		payload.Session.Output = make([]productprotocol.TerminalOutputChunk, 0)
		state.Terminals[payload.Session.ID] = payload.Session
	case "terminal.output":
		var payload struct {
			SessionID  string `json:"sessionId"`
			Sequence   int    `json:"sequence"`
			Data       string `json:"data"`
			ByteLength int    `json:"byteLength"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		session, ok := state.Terminals[payload.SessionID]
		if !ok || session.Status != "open" {
			return ErrTerminalSessionNotOpen
		}
		if payload.Sequence != session.NextOutputSequence {
			return ErrTerminalOutputSequence
		}
		if session.OutputBytes+payload.ByteLength > session.OutputLimitBytes {
			return ErrTerminalOutputLimit
		}
		session.OutputBytes += payload.ByteLength
		session.NextOutputSequence++
		session.Output = append(session.Output, productprotocol.TerminalOutputChunk{Sequence: payload.Sequence, Data: payload.Data, ByteLength: payload.ByteLength})
		state.Terminals[session.ID] = session
	case "terminal.input.accepted":
		var payload struct {
			SessionID string `json:"sessionId"`
			Sequence  int    `json:"sequence"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		session, ok := state.Terminals[payload.SessionID]
		if !ok || session.Status != "open" {
			return ErrTerminalSessionNotOpen
		}
		if payload.Sequence != session.NextInputSequence {
			return ErrTerminalInputSequence
		}
		session.NextInputSequence++
		state.Terminals[session.ID] = session
	case "terminal.resized":
		var payload struct {
			SessionID string `json:"sessionId"`
			Columns   int    `json:"columns"`
			Rows      int    `json:"rows"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		session, ok := state.Terminals[payload.SessionID]
		if !ok || session.Status != "open" {
			return ErrTerminalSessionNotOpen
		}
		session.Columns, session.Rows = payload.Columns, payload.Rows
		state.Terminals[session.ID] = session
	case "terminal.exited":
		var payload struct {
			SessionID string `json:"sessionId"`
			ExitCode  int    `json:"exitCode"`
			Reason    string `json:"reason"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		session, ok := state.Terminals[payload.SessionID]
		if !ok || session.Status != "open" {
			return ErrTerminalSessionNotOpen
		}
		session.Status = "exited"
		session.ExitCode = &payload.ExitCode
		session.ExitReason = payload.Reason
		state.Terminals[session.ID] = session
	case "editor.draft.opened":
		var payload struct {
			Draft productprotocol.EditorDraftState `json:"draft"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		if _, exists := state.EditorDrafts[payload.Draft.ID]; exists {
			return ErrEditorDraftConflict
		}
		for _, reference := range payload.Draft.EvidenceReferences {
			finding, findingOK := state.Findings[reference.FindingID]
			evidence, evidenceOK := state.Evidence[reference.EvidenceID]
			if !findingOK || !evidenceOK || evidence.TaskID != event.TaskID || !containsString(finding.EvidenceIDs, reference.EvidenceID) {
				return ErrEditorProvenanceMissing
			}
		}
		payload.Draft.Status = "open"
		payload.Draft.NextRevision = 1
		state.EditorDrafts[payload.Draft.ID] = payload.Draft
	case "editor.draft.saved":
		var payload struct {
			DraftID            string `json:"draftId"`
			Revision           int    `json:"revision"`
			BaseSHA256         string `json:"baseSha256"`
			ProposedSHA256     string `json:"proposedSha256"`
			ProposedByteLength int    `json:"proposedByteLength"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		draft, ok := state.EditorDrafts[payload.DraftID]
		if !ok || (draft.Status != "open" && draft.Status != "saved") {
			return ErrEditorDraftNotEditable
		}
		if payload.Revision != draft.NextRevision || payload.BaseSHA256 != draft.BaseSHA256 {
			return ErrEditorDraftRevision
		}
		draft.Status = "saved"
		draft.NextRevision++
		draft.ProposedSHA256 = payload.ProposedSHA256
		draft.ProposedByteLength = payload.ProposedByteLength
		state.EditorDrafts[draft.ID] = draft
	case "editor.patch.applied":
		var payload struct {
			DraftID        string `json:"draftId"`
			Revision       int    `json:"revision"`
			BaseSHA256     string `json:"baseSha256"`
			ProposedSHA256 string `json:"proposedSha256"`
			ResultSHA256   string `json:"resultSha256"`
			Reviewer       string `json:"reviewer"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		draft, ok := state.EditorDrafts[payload.DraftID]
		if !ok || draft.Status != "saved" || payload.Revision != draft.NextRevision-1 || payload.BaseSHA256 != draft.BaseSHA256 || payload.ProposedSHA256 != draft.ProposedSHA256 {
			return ErrEditorPatchStale
		}
		draft.Status = "applied"
		draft.ResultSHA256 = payload.ResultSHA256
		draft.Reviewer = payload.Reviewer
		state.EditorDrafts[draft.ID] = draft
	case "editor.patch.verified":
		var payload struct {
			DraftID        string   `json:"draftId"`
			Revision       int      `json:"revision"`
			VerificationID string   `json:"verificationId"`
			Success        bool     `json:"success"`
			EvidenceIDs    []string `json:"evidenceIds"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		draft, ok := state.EditorDrafts[payload.DraftID]
		if !ok || draft.Status != "applied" || payload.Revision != draft.NextRevision-1 {
			return ErrEditorPatchNotApplied
		}
		for _, evidenceID := range payload.EvidenceIDs {
			if _, exists := state.Evidence[evidenceID]; !exists {
				return ErrEditorProvenanceMissing
			}
		}
		draft.Status = "verified"
		draft.Verification = &productprotocol.EditorVerificationState{ID: payload.VerificationID, Revision: payload.Revision, Success: payload.Success, EvidenceIDs: append([]string(nil), payload.EvidenceIDs...)}
		state.EditorDrafts[draft.ID] = draft
	case "editor.draft.discarded":
		var payload struct {
			DraftID string `json:"draftId"`
			Reason  string `json:"reason"`
		}
		if err := decodePayload(event, &payload); err != nil {
			return err
		}
		draft, ok := state.EditorDrafts[payload.DraftID]
		if !ok || (draft.Status != "open" && draft.Status != "saved") {
			return ErrEditorDraftNotEditable
		}
		draft.Status = "discarded"
		draft.DiscardReason = payload.Reason
		state.EditorDrafts[draft.ID] = draft
	}
	return nil
}

func cloneState(state State) (State, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return State{}, err
	}
	var cloned State
	if err := json.Unmarshal(data, &cloned); err != nil {
		return State{}, err
	}
	return cloned, nil
}

func decodePayload(event productprotocol.Event, target any) error {
	return json.Unmarshal(event.Payload, target)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
