package runtimeapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
)

var (
	ErrScopeNotConfirmed  = errors.New("scope_not_confirmed")
	ErrApprovalOutOfScope = errors.New("approval_out_of_scope")
	ErrApprovalExpired    = errors.New("approval_expired")
	ErrApprovalReplay     = errors.New("approval_replay")
	ErrUnknownApproval    = errors.New("unknown_approval")
	ErrStaleLease         = errors.New("stale_control_revision")
	ErrInvalidReportState = errors.New("invalid_report_state")
	ErrRuntimeDraining    = errors.New("runtime_is_draining")
)

type RuntimeDrainer interface {
	Drain(context.Context) error
}

type ApprovalRequest struct {
	TaskID      string
	ChallengeID string
	AgentID     string
	Action      string
	Target      string
	Parameters  any
	Risk        string
	ExpiresAt   time.Time
}

type Service struct {
	store     *Store
	runtimeID string
	principal string
	mu        sync.RWMutex
	clock     func() time.Time
	drainers  []RuntimeDrainer
	draining  bool
	drained   bool
	drainDone chan struct{}
	drainErr  error
}

func NewService(store *Store, runtimeID, principal string, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{store: store, runtimeID: runtimeID, principal: principal, clock: clock}
}

func (s *Service) Store() *Store { return s.store }

func (s *Service) SetClock(clock func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if clock == nil {
		clock = time.Now
	}
	s.clock = clock
}

func (s *Service) now() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clock().UTC()
}

func (s *Service) RegisterDrainer(drainer RuntimeDrainer) error {
	if s == nil || drainer == nil {
		return fmt.Errorf("runtime drainer is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining || s.drained {
		return ErrRuntimeDraining
	}
	s.drainers = append(s.drainers, drainer)
	return nil
}

// Drain stops registered worker lifecycles in reverse registration order.
// Concurrent callers share the same terminal result.
func (s *Service) Drain(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.drained {
		err := s.drainErr
		s.mu.Unlock()
		return err
	}
	if s.draining {
		done := s.drainDone
		s.mu.Unlock()
		select {
		case <-done:
			s.mu.RLock()
			err := s.drainErr
			s.mu.RUnlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.draining = true
	s.drainDone = make(chan struct{})
	done := s.drainDone
	drainers := append([]RuntimeDrainer(nil), s.drainers...)
	s.mu.Unlock()

	var drainErrors []error
	for index := len(drainers) - 1; index >= 0; index-- {
		if err := drainers[index].Drain(ctx); err != nil {
			drainErrors = append(drainErrors, err)
		}
	}
	result := errors.Join(drainErrors...)
	s.mu.Lock()
	s.draining = false
	s.drained = true
	s.drainErr = result
	close(done)
	s.mu.Unlock()
	return result
}

func (s *Service) Emit(ctx context.Context, draft DraftEvent) (productprotocol.Event, error) {
	draft.Source.RuntimeID = s.runtimeID
	draft.OccurredAt = s.now()
	event, _, err := s.store.Commit(ctx, draft)
	return event, err
}

func (s *Service) CreateTask(ctx context.Context, taskID, title string) (productprotocol.Event, error) {
	return s.emit(ctx, taskID, "task.created", map[string]any{"title": title}, productprotocol.EventSourceRef{})
}

func (s *Service) StartTask(ctx context.Context, taskID, title string) (productprotocol.Event, error) {
	return s.emit(ctx, taskID, "task.started", map[string]any{"title": title}, productprotocol.EventSourceRef{})
}

func (s *Service) ProposeScope(ctx context.Context, taskID string, scope productprotocol.ScopeSnapshot) (productprotocol.Event, error) {
	if scope.Principal != s.principal {
		return productprotocol.Event{}, ErrApprovalOutOfScope
	}
	return s.emit(ctx, taskID, "scope.proposed", map[string]any{"scope": scope}, productprotocol.EventSourceRef{})
}

func (s *Service) ConfirmScope(ctx context.Context, taskID, scopeID string) (productprotocol.Event, error) {
	_, state, err := s.store.Load(ctx, taskID)
	if err != nil {
		return productprotocol.Event{}, err
	}
	if state.Scope == nil || state.Scope.ID != scopeID {
		return productprotocol.Event{}, ErrScopeNotConfirmed
	}
	return s.emit(ctx, taskID, "scope.confirmed", map[string]any{"scope": *state.Scope}, productprotocol.EventSourceRef{})
}

func (s *Service) RequestApproval(ctx context.Context, request ApprovalRequest) (productprotocol.Event, error) {
	events, state, err := s.store.Load(ctx, request.TaskID)
	if err != nil {
		return productprotocol.Event{}, err
	}
	if state.Scope == nil || !scopeWasConfirmed(events, state.Scope.ID) {
		return productprotocol.Event{}, ErrScopeNotConfirmed
	}
	if !scopeAllows(*state.Scope, request.Action, request.Target, request.Risk) {
		return productprotocol.Event{}, ErrApprovalOutOfScope
	}
	if !request.ExpiresAt.After(s.now()) {
		return productprotocol.Event{}, ErrApprovalExpired
	}
	digest, err := ParameterDigest(request.Action, request.Target, request.Parameters)
	if err != nil {
		return productprotocol.Event{}, err
	}
	challenge := productprotocol.ApprovalChallenge{
		ID: request.ChallengeID, AgentID: request.AgentID, Action: request.Action, Target: request.Target,
		ParameterDigest: digest, Risk: request.Risk, ExpiresAt: request.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	return s.emit(ctx, request.TaskID, "approval.requested", map[string]any{"challenge": challenge}, productprotocol.EventSourceRef{AgentID: request.AgentID})
}

func (s *Service) ResolveApproval(ctx context.Context, taskID, challengeID, decision string) (productprotocol.Event, error) {
	events, state, err := s.store.Load(ctx, taskID)
	if err != nil {
		return productprotocol.Event{}, err
	}
	approval, exists := state.Approvals[challengeID]
	if !exists {
		return productprotocol.Event{}, ErrUnknownApproval
	}
	if approval.Decision != "" {
		return productprotocol.Event{}, ErrApprovalReplay
	}
	if !approvalScopeActive(events, challengeID) {
		return productprotocol.Event{}, ErrApprovalOutOfScope
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
	if err != nil || s.now().After(expiresAt) {
		return productprotocol.Event{}, ErrApprovalExpired
	}
	event, err := s.emit(ctx, taskID, "approval.resolved", map[string]any{"challengeId": challengeID, "decision": decision}, productprotocol.EventSourceRef{AgentID: approval.AgentID})
	if errors.Is(err, productstate.ErrApprovalAlreadyResolved) {
		return productprotocol.Event{}, ErrApprovalReplay
	}
	if errors.Is(err, productstate.ErrApprovalExpired) {
		return productprotocol.Event{}, ErrApprovalExpired
	}
	return event, err
}

func (s *Service) CommitEvidence(ctx context.Context, evidence productprotocol.ImmutableEvidence, source productprotocol.EventSourceRef) (productprotocol.Event, error) {
	return s.emit(ctx, evidence.TaskID, "evidence.committed", map[string]any{"evidence": evidence}, source)
}

func (s *Service) CreateFinding(ctx context.Context, taskID string, finding productprotocol.FindingState) (productprotocol.Event, error) {
	_, state, err := s.store.Load(ctx, taskID)
	if err != nil {
		return productprotocol.Event{}, err
	}
	for _, evidenceID := range finding.EvidenceIDs {
		if _, exists := state.Evidence[evidenceID]; !exists {
			return productprotocol.Event{}, fmt.Errorf("unknown evidence %q", evidenceID)
		}
	}
	return s.emit(ctx, taskID, "finding.created", map[string]any{"finding": finding}, productprotocol.EventSourceRef{})
}

func (s *Service) TransitionFinding(ctx context.Context, taskID, findingID, status, reason string) (productprotocol.Event, error) {
	payload := map[string]any{"findingId": findingID}
	if status == "rejected" {
		payload["reason"] = reason
	}
	return s.emit(ctx, taskID, "finding."+status, payload, productprotocol.EventSourceRef{})
}

func (s *Service) DraftReport(ctx context.Context, report productprotocol.ReportState) (productprotocol.Event, error) {
	return s.emit(ctx, report.TaskID, "report.drafted", map[string]any{"report": report}, productprotocol.EventSourceRef{})
}

func (s *Service) EditReport(ctx context.Context, report productprotocol.ReportState) (productprotocol.Event, error) {
	_, state, err := s.store.Load(ctx, report.TaskID)
	if err != nil {
		return productprotocol.Event{}, err
	}
	if state.Report == nil || state.Report.ID != report.ID || state.Report.Status == "frozen" || report.Version <= state.Report.Version {
		return productprotocol.Event{}, ErrInvalidReportState
	}
	return s.emit(ctx, report.TaskID, "report.edited", map[string]any{"report": report}, productprotocol.EventSourceRef{})
}

func (s *Service) FreezeReport(ctx context.Context, taskID, reportID string, version int) (productprotocol.Event, error) {
	_, state, err := s.store.Load(ctx, taskID)
	if err != nil {
		return productprotocol.Event{}, err
	}
	if state.Report == nil || state.Report.ID != reportID || state.Report.Status == "frozen" || version <= state.Report.Version {
		return productprotocol.Event{}, ErrInvalidReportState
	}
	return s.emit(ctx, taskID, "report.frozen", map[string]any{"reportId": reportID, "version": version}, productprotocol.EventSourceRef{})
}

func (s *Service) TakeControl(ctx context.Context, taskID, clientID string, expectedRevision int) (productprotocol.Event, error) {
	_, state, err := s.store.Load(ctx, taskID)
	if err != nil {
		return productprotocol.Event{}, err
	}
	if state.HighestCommittedLeaseRevision != expectedRevision {
		return productprotocol.Event{}, ErrStaleLease
	}
	eventType := "control.acquired"
	if expectedRevision > 0 {
		eventType = "control.transferred"
	}
	lease := productprotocol.ControlLease{ClientID: clientID, Revision: expectedRevision + 1}
	event, err := s.emit(ctx, taskID, eventType, map[string]any{"lease": lease}, productprotocol.EventSourceRef{})
	if errors.Is(err, productstate.ErrNonMonotonicLeaseRevision) {
		return productprotocol.Event{}, ErrStaleLease
	}
	return event, err
}

func ParameterDigest(action, target string, parameters any) (string, error) {
	canonical, err := productprotocol.CanonicalJSON(map[string]any{"action": action, "target": target, "parameters": parameters})
	if err != nil {
		return "", fmt.Errorf("canonicalize approval parameters: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (s *Service) emit(ctx context.Context, taskID, eventType string, payload any, source productprotocol.EventSourceRef) (productprotocol.Event, error) {
	source.RuntimeID = s.runtimeID
	return s.Emit(ctx, DraftEvent{TaskID: taskID, Type: eventType, Source: source, Payload: payload})
}

func scopeWasConfirmed(events []productprotocol.Event, scopeID string) bool {
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type == "scope.proposed" {
			return false
		}
		if events[index].Type == "scope.confirmed" {
			var payload struct {
				Scope productprotocol.ScopeSnapshot `json:"scope"`
			}
			return json.Unmarshal(events[index].Payload, &payload) == nil && payload.Scope.ID == scopeID
		}
	}
	return false
}

func approvalScopeActive(events []productprotocol.Event, challengeID string) bool {
	found := false
	for _, event := range events {
		if event.Type == "approval.requested" {
			var payload struct {
				Challenge productprotocol.ApprovalChallenge `json:"challenge"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && payload.Challenge.ID == challengeID {
				found = true
			}
			continue
		}
		if found && (event.Type == "scope.proposed" || event.Type == "scope.confirmed") {
			return false
		}
	}
	return found
}

func scopeAllows(scope productprotocol.ScopeSnapshot, action, target, risk string) bool {
	if !slices.Contains(scope.Targets, target) || !slices.Contains(scope.AllowedActions, action) || slices.Contains(scope.DeniedActions, action) {
		return false
	}
	levels := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
	requested, requestedOK := levels[risk]
	ceiling, ceilingOK := levels[scope.RiskCeiling]
	return requestedOK && ceilingOK && requested <= ceiling
}
