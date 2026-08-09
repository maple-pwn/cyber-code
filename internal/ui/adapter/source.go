// Package adapter connects Tactical Ops semantic actions to product event sources.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/productprotocol"
	"cyber-code/internal/productstate"
	"cyber-code/internal/runtimeapi"
	"cyber-code/internal/ui/mission"
)

type CommandType string

const (
	CommandTaskCreate      CommandType = "task.create"
	CommandScopeConfirm    CommandType = "scope.confirm"
	CommandTaskPause       CommandType = "task.pause"
	CommandTaskResume      CommandType = "task.resume"
	CommandTaskCancel      CommandType = "task.cancel"
	CommandApprovalRespond CommandType = "approval.respond"
	CommandControlTake     CommandType = "control.take"
	CommandInstructionSend CommandType = "instruction.send"
)

type Command struct {
	Type             CommandType
	Objective        string
	RuntimeID        string
	ScopeID          string
	ChallengeID      string
	Decision         string
	ExpectedRevision int
	Content          string
}

var ErrUnsupportedAction = errors.New("unsupported tactical action")

var ErrWritesDisabled = errors.New("tactical source is read-only")

type Source interface {
	Handshake(context.Context, runtimeapi.HandshakeRequest) (runtimeapi.HandshakeResponse, error)
	Subscribe(context.Context, int, func(json.RawMessage)) (func(), error)
	Snapshot(context.Context) (Snapshot, error)
	Send(context.Context, Command) error
	Close(context.Context) error
}

type Snapshot struct {
	Cursor int
	State  productstate.State
}

type ConnectionStatus string

const (
	ConnectionConnecting   ConnectionStatus = "connecting"
	ConnectionHealthy      ConnectionStatus = "healthy"
	ConnectionDegraded     ConnectionStatus = "degraded"
	ConnectionResyncing    ConnectionStatus = "resyncing"
	ConnectionOffline      ConnectionStatus = "offline"
	ConnectionIncompatible ConnectionStatus = "incompatible"
)

type Connection struct {
	Status            ConnectionStatus
	LastTrustedCursor int
	ErrorCode         string
}

type View struct {
	State      productstate.State
	Connection Connection
	ReadOnly   bool
	Source     *runtimeapi.SourceMetadata
	Role       string
}

type ClientOptions struct {
	ClientID     string
	ExpectedMode runtimeapi.SourceMode
	Capabilities []string
}

type Client struct {
	mu            sync.Mutex
	source        Source
	clientID      string
	expectedMode  runtimeapi.SourceMode
	capabilities  []string
	sourceInfo    *runtimeapi.SourceMetadata
	role          string
	state         productstate.State
	connection    Connection
	unsubscribe   func()
	establishing  bool
	pendingResync bool
	recovering    bool
}

func NewClient(source Source, options ClientOptions) *Client {
	if len(options.Capabilities) == 0 {
		options.Capabilities = []string{"events", "snapshot", "commands", "terminal.observe", "terminal.input", "editor.read", "editor.write"}
	}
	return &Client{
		source: source, clientID: options.ClientID, expectedMode: options.ExpectedMode, capabilities: append([]string(nil), options.Capabilities...), state: productstate.Initial(),
		connection: Connection{Status: ConnectionOffline},
	}
}

func (client *Client) Connect(ctx context.Context) error {
	if err := client.handshake(ctx); err != nil {
		return err
	}
	return client.subscribe(ctx, ConnectionConnecting)
}

func (client *Client) Disconnect(ctx context.Context) error {
	client.mu.Lock()
	client.stopSubscriptionLocked()
	client.connection.Status = ConnectionOffline
	client.mu.Unlock()
	return client.source.Close(ctx)
}

func (client *Client) Dispatch(ctx context.Context, command Command) error {
	client.mu.Lock()
	writable := client.writableLocked(command)
	client.mu.Unlock()
	if !writable {
		return ErrWritesDisabled
	}
	return client.source.Send(ctx, command)
}

func (client *Client) View() View {
	client.mu.Lock()
	defer client.mu.Unlock()
	var source *runtimeapi.SourceMetadata
	if client.sourceInfo != nil {
		copy := *client.sourceInfo
		copy.Capabilities = append([]string(nil), copy.Capabilities...)
		source = &copy
	}
	return View{
		State: client.state, Connection: client.connection,
		ReadOnly: !client.writableLocked(Command{Type: CommandTaskPause}), Source: source, Role: client.role,
	}
}

func (client *Client) handshake(ctx context.Context) error {
	client.mu.Lock()
	after := client.connection.LastTrustedCursor
	client.mu.Unlock()
	request := runtimeapi.HandshakeRequest{
		SupportedProtocolVersions: []int{runtimeapi.ProtocolVersion}, SupportedCapabilities: append([]string(nil), client.capabilities...), AfterCursor: after,
	}
	response, err := client.source.Handshake(ctx, request)
	if err != nil {
		client.mu.Lock()
		client.connection.Status = ConnectionDegraded
		client.connection.ErrorCode = err.Error()
		client.mu.Unlock()
		return err
	}
	metadata, err := runtimeapi.NegotiateHandshake(request, response)
	if err == nil && client.expectedMode != "" && metadata.Mode != client.expectedMode {
		err = fmt.Errorf("runtime source mode mismatch: expected %s, got %s", client.expectedMode, metadata.Mode)
	}
	if err != nil {
		client.mu.Lock()
		client.connection.Status = ConnectionIncompatible
		client.connection.ErrorCode = err.Error()
		client.mu.Unlock()
		return err
	}
	client.mu.Lock()
	client.sourceInfo = &metadata
	client.role = response.Role
	client.mu.Unlock()
	return nil
}

func (client *Client) subscribe(ctx context.Context, status ConnectionStatus) error {
	client.mu.Lock()
	client.stopSubscriptionLocked()
	client.connection.Status = status
	after := client.connection.LastTrustedCursor
	client.establishing = true
	client.pendingResync = false
	client.mu.Unlock()

	unsubscribe, err := client.source.Subscribe(ctx, after, client.receive)
	client.mu.Lock()
	client.establishing = false
	if err != nil {
		client.connection.Status = ConnectionDegraded
		client.connection.ErrorCode = err.Error()
		client.mu.Unlock()
		return err
	}
	if client.connection.Status == ConnectionIncompatible {
		client.mu.Unlock()
		if unsubscribe != nil {
			unsubscribe()
		}
		return nil
	}
	client.unsubscribe = unsubscribe
	needsRecovery := client.pendingResync
	if !needsRecovery && client.connection.Status == status {
		client.connection.Status = ConnectionHealthy
		client.connection.ErrorCode = ""
	}
	client.mu.Unlock()
	if needsRecovery {
		return client.recover(ctx)
	}
	return nil
}

func (client *Client) receive(raw json.RawMessage) {
	event, err := productprotocol.Validate(raw)
	client.mu.Lock()
	if client.connection.Status == ConnectionIncompatible || client.connection.Status == ConnectionOffline {
		client.mu.Unlock()
		return
	}
	if err != nil {
		client.connection.Status = ConnectionIncompatible
		client.connection.ErrorCode = err.Error()
		client.stopSubscriptionLocked()
		client.mu.Unlock()
		return
	}
	result, err := productstate.Project(client.state, event)
	if err != nil {
		client.connection.Status = ConnectionIncompatible
		client.connection.ErrorCode = err.Error()
		client.stopSubscriptionLocked()
		client.mu.Unlock()
		return
	}
	if result.Kind == productstate.ProjectionResyncRequired {
		client.connection.Status = ConnectionResyncing
		client.pendingResync = true
		shouldRecover := !client.establishing && !client.recovering
		client.mu.Unlock()
		if shouldRecover {
			go func() { _ = client.recover(context.Background()) }()
		}
		return
	}
	client.state = result.State
	client.connection.LastTrustedCursor = result.State.CommittedCursor
	client.mu.Unlock()
}

func (client *Client) recover(ctx context.Context) error {
	client.mu.Lock()
	if client.recovering {
		client.mu.Unlock()
		return nil
	}
	client.recovering = true
	client.pendingResync = false
	client.connection.Status = ConnectionResyncing
	trustedCursor := client.connection.LastTrustedCursor
	client.mu.Unlock()

	snapshot, err := client.source.Snapshot(ctx)
	if err == nil && (snapshot.Cursor < trustedCursor || snapshot.State.CommittedCursor != snapshot.Cursor) {
		err = errors.New("invalid source snapshot cursor")
	}
	if err != nil {
		client.mu.Lock()
		client.connection.Status = ConnectionOffline
		client.connection.ErrorCode = err.Error()
		client.recovering = false
		client.mu.Unlock()
		return err
	}
	client.mu.Lock()
	client.state = snapshot.State
	client.connection.LastTrustedCursor = snapshot.Cursor
	client.recovering = false
	client.mu.Unlock()
	return client.subscribe(ctx, ConnectionResyncing)
}

func (client *Client) writableLocked(command Command) bool {
	switch client.connection.Status {
	case ConnectionHealthy:
	default:
		return false
	}
	if command.Type == CommandControlTake {
		return true
	}
	return client.state.ControlLease == nil || client.state.ControlLease.ClientID == client.clientID
}

func (client *Client) stopSubscriptionLocked() {
	if client.unsubscribe != nil {
		client.unsubscribe()
		client.unsubscribe = nil
	}
}

type ScenarioOptions struct {
	RuntimeID       string
	InjectFailureAt string
}

type ScenarioSource struct {
	mu                sync.Mutex
	options           ScenarioOptions
	state             productstate.State
	events            []productprotocol.Event
	rawEvents         []json.RawMessage
	listener          func(json.RawMessage)
	closed            bool
	taskCreated       bool
	scopeConfirmed    bool
	approvalRequested bool
	approvalResolved  bool
	scopeRevision     int
	currentScope      productprotocol.ScopeSnapshot
}

var scenarioBaseTime = time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)

const scenarioTimestampLayout = "2006-01-02T15:04:05.000Z"

func NewScenarioSource(options ScenarioOptions) (*ScenarioSource, error) {
	if options.RuntimeID != "scenario-local" && options.RuntimeID != "scenario-remote" {
		return nil, errors.New("scenario runtime must be scenario-local or scenario-remote")
	}
	if options.InjectFailureAt != "" && options.InjectFailureAt != "recon" && options.InjectFailureAt != "verification" {
		return nil, errors.New("invalid scenario failure stage")
	}
	return &ScenarioSource{
		options: options, state: productstate.Initial(), scopeRevision: 1,
		currentScope: productprotocol.ScopeSnapshot{
			ID: "scope-1", Principal: "authorized-operator", Workspace: "/labs/juice-shop", Validity: "single-task",
			Targets:        []string{"juice-shop.lab"},
			AllowedActions: []string{"passive-recon", "route-enumeration", "bounded-login-verification"},
			DeniedActions:  []string{"destructive", "persistence", "credential-stuffing"}, RiskCeiling: "medium",
		},
	}, nil
}

func (source *ScenarioSource) Handshake(_ context.Context, request runtimeapi.HandshakeRequest) (runtimeapi.HandshakeResponse, error) {
	metadata := runtimeapi.SourceMetadata{
		Mode: runtimeapi.SourceModeDemo, RuntimeID: source.options.RuntimeID, Principal: "authorized-operator",
		Capabilities: []string{"events", "snapshot", "commands", "deterministic"},
	}
	metadata.Capabilities = runtimeapi.SelectCapabilities(request.SupportedCapabilities, metadata.Capabilities)
	response := runtimeapi.HandshakeResponse{
		ProtocolVersion: runtimeapi.ProtocolVersion, RuntimeID: metadata.RuntimeID, Principal: metadata.Principal,
		Role: "operator", Capabilities: append([]string(nil), metadata.Capabilities...), Source: metadata,
	}
	if _, err := runtimeapi.NegotiateHandshake(request, response); err != nil {
		return runtimeapi.HandshakeResponse{}, err
	}
	return response, nil
}

func (source *ScenarioSource) Subscribe(_ context.Context, after int, receive func(json.RawMessage)) (func(), error) {
	if after < 0 {
		return nil, errors.New("invalid scenario cursor")
	}
	source.mu.Lock()
	source.closed = false
	source.listener = receive
	replay := make([]json.RawMessage, 0)
	for index, event := range source.events {
		if event.Cursor > after {
			replay = append(replay, append(json.RawMessage(nil), source.rawEvents[index]...))
		}
	}
	source.mu.Unlock()
	for _, raw := range replay {
		receive(raw)
	}
	return func() {
		source.mu.Lock()
		source.listener = nil
		source.mu.Unlock()
	}, nil
}

func (source *ScenarioSource) Snapshot(context.Context) (Snapshot, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	data, err := json.Marshal(source.state)
	if err != nil {
		return Snapshot{}, err
	}
	var state productstate.State
	if err := json.Unmarshal(data, &state); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Cursor: state.CommittedCursor, State: state}, nil
}

func (source *ScenarioSource) Send(_ context.Context, command Command) error {
	source.mu.Lock()
	start := len(source.rawEvents)
	err := source.sendLocked(command)
	listener := source.listener
	closed := source.closed
	created := make([]json.RawMessage, len(source.rawEvents)-start)
	for index, raw := range source.rawEvents[start:] {
		created[index] = append(json.RawMessage(nil), raw...)
	}
	source.mu.Unlock()
	if err != nil {
		return err
	}
	if listener != nil && !closed {
		for _, raw := range created {
			listener(raw)
		}
	}
	return nil
}

func (source *ScenarioSource) Close(context.Context) error {
	source.mu.Lock()
	source.closed = true
	source.listener = nil
	source.mu.Unlock()
	return nil
}

func (source *ScenarioSource) Events() []productprotocol.Event {
	source.mu.Lock()
	defer source.mu.Unlock()
	result := make([]productprotocol.Event, len(source.events))
	copy(result, source.events)
	return result
}

func (source *ScenarioSource) sendLocked(command Command) error {
	switch command.Type {
	case CommandTaskCreate:
		return source.createTaskLocked(command)
	case CommandScopeConfirm:
		return source.confirmScopeLocked(command.ScopeID)
	case CommandTaskPause:
		return source.emitLocked("task.paused", map[string]any{})
	case CommandTaskResume:
		return source.emitLocked("task.resumed", map[string]any{})
	case CommandTaskCancel:
		if err := source.emitLocked("task.cancel.requested", map[string]any{}); err != nil {
			return err
		}
		return source.emitLocked("task.cancelled", map[string]any{})
	case CommandApprovalRespond:
		return source.resolveApprovalLocked(command.ChallengeID, command.Decision)
	case CommandControlTake:
		return source.takeControlLocked(command.ExpectedRevision)
	case CommandInstructionSend:
		if command.Content == "request_scope_revision" {
			return source.reviseScopeLocked()
		}
		return source.emitLocked("question.resolved", map[string]any{
			"questionId": fmt.Sprintf("instruction-%d", len(source.events)+1), "answer": command.Content,
		})
	default:
		return errors.New("unsupported scenario command")
	}
}

func (source *ScenarioSource) createTaskLocked(command Command) error {
	if source.taskCreated {
		return errors.New("task_already_created")
	}
	if command.RuntimeID != source.options.RuntimeID {
		return errors.New("runtime_mismatch")
	}
	if !strings.Contains(command.Objective, "juice-shop.lab") {
		return errors.New("unsupported_target")
	}
	source.taskCreated = true
	if err := source.emitLocked("task.created", map[string]any{"title": command.Objective}); err != nil {
		return err
	}
	return source.emitLocked("scope.proposed", map[string]any{"scope": source.currentScope})
}

func (source *ScenarioSource) confirmScopeLocked(scopeID string) error {
	if !source.taskCreated {
		return errors.New("task_required")
	}
	if scopeID != source.currentScope.ID {
		return errors.New("unknown_scope")
	}
	if source.scopeConfirmed {
		return errors.New("scope_already_confirmed")
	}
	source.scopeConfirmed = true
	progress := 0.0
	steps := []struct {
		typeName string
		payload  any
	}{
		{"scope.confirmed", map[string]any{"scope": source.currentScope}},
		{"task.started", map[string]any{"title": "Authorized juice-shop.lab assessment"}},
		{"agent.started", map[string]any{"agent": productprotocol.AgentState{ID: "agent-recon", Name: "Recon Agent", Status: "running", Progress: &progress}}},
		{"tool.started", map[string]any{"callId": "tool-recon", "name": "bounded-recon"}},
	}
	for _, step := range steps {
		if err := source.emitLocked(step.typeName, step.payload); err != nil {
			return err
		}
	}
	if source.options.InjectFailureAt == "recon" {
		return source.failStageLocked("agent-recon", "tool-recon", "recon_failed")
	}
	for _, evidence := range scenarioReconEvidence() {
		if err := source.emitLocked("evidence.committed", map[string]any{"evidence": evidence}); err != nil {
			return err
		}
	}
	if err := source.emitLocked("tool.completed", map[string]any{
		"callId": "tool-recon", "success": true,
		"evidenceIds": []string{"evidence-runtime", "evidence-framework", "evidence-route-count", "evidence-login-route", "evidence-product-route", "evidence-security-headers", "evidence-auth-shape", "evidence-scope"},
	}); err != nil {
		return err
	}
	if err := source.emitLocked("agent.completed", map[string]any{"agentId": "agent-recon"}); err != nil {
		return err
	}
	source.approvalRequested = true
	return source.emitLocked("approval.requested", map[string]any{"challenge": productprotocol.ApprovalChallenge{
		ID: "approval-1", AgentID: "agent-verification", Action: "bounded-login-verification",
		Target: "juice-shop.lab", ParameterDigest: "sha256:scenario-login-v1", Risk: "medium",
		ExpiresAt: scenarioBaseTime.Add(25 * time.Second).Format(scenarioTimestampLayout),
	}})
}

func (source *ScenarioSource) resolveApprovalLocked(challengeID, decision string) error {
	if !source.approvalRequested || challengeID != "approval-1" {
		return errors.New("unknown_approval")
	}
	if source.approvalResolved {
		return errors.New("approval_already_resolved")
	}
	if decision != "allow_once" && decision != "deny" {
		return errors.New("invalid_approval_decision")
	}
	source.approvalResolved = true
	if err := source.emitLocked("approval.resolved", map[string]any{"challengeId": challengeID, "decision": decision}); err != nil {
		return err
	}
	finding := productprotocol.FindingState{
		ID: "finding-login-injection", Title: "Potential login injection", Severity: "high",
		Status: "candidate", Confidence: "medium", EvidenceIDs: []string{"evidence-login-route", "evidence-auth-shape"},
	}
	if err := source.emitLocked("finding.created", map[string]any{"finding": finding}); err != nil {
		return err
	}
	if decision == "deny" {
		if err := source.emitLocked("finding.rejected", map[string]any{
			"findingId": finding.ID, "reason": "Verification denied; candidate retained as an explicit limitation.",
		}); err != nil {
			return err
		}
		return source.emitLocked("task.completed", map[string]any{})
	}
	if err := source.emitLocked("finding.verifying", map[string]any{"findingId": finding.ID}); err != nil {
		return err
	}
	progress := 0.0
	if err := source.emitLocked("agent.started", map[string]any{"agent": productprotocol.AgentState{ID: "agent-verification", Name: "Verification Agent", Status: "running", Progress: &progress}}); err != nil {
		return err
	}
	if err := source.emitLocked("tool.started", map[string]any{"callId": "tool-verification", "name": "bounded-login-verification"}); err != nil {
		return err
	}
	if source.options.InjectFailureAt == "verification" {
		return source.failStageLocked("agent-verification", "tool-verification", "verification_failed")
	}
	verified := productprotocol.ImmutableEvidence{
		ID: "evidence-bounded-verification", TaskID: "task-1", Kind: "verification",
		Summary: "Bounded login verification confirmed impact",
		Data:    map[string]any{"target": "juice-shop.lab", "attempts": 1, "impact": "authentication bypass"},
	}
	for _, step := range []struct {
		typeName string
		payload  any
	}{
		{"evidence.committed", map[string]any{"evidence": verified}},
		{"tool.completed", map[string]any{"callId": "tool-verification", "success": true, "evidenceIds": []string{verified.ID}}},
		{"finding.confirmed", map[string]any{"findingId": finding.ID}},
		{"agent.completed", map[string]any{"agentId": "agent-verification"}},
		{"task.completed", map[string]any{}},
	} {
		if err := source.emitLocked(step.typeName, step.payload); err != nil {
			return err
		}
	}
	return nil
}

func (source *ScenarioSource) takeControlLocked(expectedRevision int) error {
	if expectedRevision != source.state.HighestCommittedLeaseRevision {
		return errors.New("stale_control_revision")
	}
	revision := expectedRevision + 1
	typeName := "control.acquired"
	if expectedRevision > 0 {
		typeName = "control.transferred"
	}
	return source.emitLocked(typeName, map[string]any{"lease": productprotocol.ControlLease{ClientID: "tui-client", Revision: revision}})
}

func (source *ScenarioSource) reviseScopeLocked() error {
	if !source.taskCreated {
		return errors.New("task_required")
	}
	source.scopeRevision++
	source.scopeConfirmed = false
	source.currentScope.ID = fmt.Sprintf("scope-%d", source.scopeRevision)
	return source.emitLocked("scope.proposed", map[string]any{"scope": source.currentScope})
}

func (source *ScenarioSource) failStageLocked(agentID, callID, reason string) error {
	for _, step := range []struct {
		typeName string
		payload  any
	}{
		{"tool.failed", map[string]any{"callId": callID, "reason": reason}},
		{"agent.failed", map[string]any{"agentId": agentID, "reason": reason}},
		{"task.blocked", map[string]any{"reason": reason}},
	} {
		if err := source.emitLocked(step.typeName, step.payload); err != nil {
			return err
		}
	}
	return nil
}

func (source *ScenarioSource) emitLocked(eventType string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	cursor := len(source.events) + 1
	event := productprotocol.Event{
		SchemaVersion: productprotocol.SchemaVersion,
		EventID:       fmt.Sprintf("%s-%d-%s", source.options.RuntimeID, cursor, eventType),
		TaskID:        "task-1", Cursor: cursor,
		OccurredAt: scenarioBaseTime.Add(time.Duration(cursor) * time.Second).Format(scenarioTimestampLayout),
		Type:       eventType, Source: productprotocol.EventSourceRef{RuntimeID: source.options.RuntimeID}, Payload: payloadJSON,
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	validated, err := productprotocol.Validate(raw)
	if err != nil {
		return err
	}
	result, err := productstate.Project(source.state, validated)
	if err != nil {
		return err
	}
	if result.Kind != productstate.ProjectionApplied {
		return errors.New("scenario event was not applied")
	}
	source.state = result.State
	source.events = append(source.events, validated)
	source.rawEvents = append(source.rawEvents, raw)
	return nil
}

func scenarioReconEvidence() []productprotocol.ImmutableEvidence {
	return []productprotocol.ImmutableEvidence{
		{ID: "evidence-runtime", TaskID: "task-1", Kind: "technology", Summary: "Node.js runtime identified", Data: map[string]any{"runtime": "Node.js"}},
		{ID: "evidence-framework", TaskID: "task-1", Kind: "technology", Summary: "Express framework identified", Data: map[string]any{"framework": "Express"}},
		{ID: "evidence-route-count", TaskID: "task-1", Kind: "route-inventory", Summary: "24 API routes enumerated", Data: map[string]any{"count": 24}},
		{ID: "evidence-login-route", TaskID: "task-1", Kind: "route", Summary: "Login API route observed", Data: map[string]any{"method": "POST", "path": "/rest/user/login"}},
		{ID: "evidence-product-route", TaskID: "task-1", Kind: "route", Summary: "Product API route observed", Data: map[string]any{"method": "GET", "path": "/api/Products"}},
		{ID: "evidence-security-headers", TaskID: "task-1", Kind: "headers", Summary: "Security header baseline recorded", Data: map[string]any{"server": "Express"}},
		{ID: "evidence-auth-shape", TaskID: "task-1", Kind: "schema", Summary: "Authentication request shape recorded", Data: map[string]any{"fields": []string{"email", "password"}}},
		{ID: "evidence-scope", TaskID: "task-1", Kind: "scope", Summary: "Evidence collected within juice-shop.lab", Data: map[string]any{"target": "juice-shop.lab"}},
	}
}

func CommandForAction(action mission.Action, state productstate.State, runtimeID string) (Command, error) {
	switch action.Kind {
	case mission.ActionCreateTask:
		return Command{Type: CommandTaskCreate, Objective: action.Text, RuntimeID: runtimeID}, nil
	case mission.ActionConfirmScope:
		if state.Scope == nil {
			return Command{}, errors.New("scope confirmation requires a proposed scope")
		}
		return Command{Type: CommandScopeConfirm, ScopeID: state.Scope.ID}, nil
	case mission.ActionPauseTask:
		return Command{Type: CommandTaskPause}, nil
	case mission.ActionResumeTask:
		return Command{Type: CommandTaskResume}, nil
	case mission.ActionCancelTask:
		return Command{Type: CommandTaskCancel}, nil
	case mission.ActionResolveApproval:
		return Command{Type: CommandApprovalRespond, ChallengeID: action.ApprovalID, Decision: action.Decision}, nil
	case mission.ActionTakeControl:
		return Command{Type: CommandControlTake, ExpectedRevision: state.HighestCommittedLeaseRevision}, nil
	case mission.ActionSendInstruction:
		return Command{Type: CommandInstructionSend, Content: action.Text}, nil
	default:
		return Command{}, ErrUnsupportedAction
	}
}
