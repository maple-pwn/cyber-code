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
