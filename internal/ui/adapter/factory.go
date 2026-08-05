package adapter

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"cyber-code/internal/runtimeapi"
)

type SourceFactoryOptions struct {
	StateDir  string
	Workspace string
	ClientID  string
}

type SourceOption struct {
	Name        string
	Mode        string
	Available   bool
	SetupStatus string
}

type SourceSelection struct {
	Source    Source
	RuntimeID string
	Mode      string
	Demo      bool
}

type SourceFactory struct {
	options SourceFactoryOptions
}

func NewSourceFactory(options SourceFactoryOptions) (*SourceFactory, error) {
	if strings.TrimSpace(options.StateDir) == "" {
		return nil, fmt.Errorf("runtime source state directory is required")
	}
	if strings.TrimSpace(options.Workspace) == "" {
		return nil, fmt.Errorf("runtime source workspace is required")
	}
	if options.ClientID == "" {
		options.ClientID = "tui-client"
	}
	return &SourceFactory{options: options}, nil
}

func (factory *SourceFactory) Options() []SourceOption {
	return []SourceOption{
		{Name: "demo", Mode: "demo", Available: true},
		{Name: "local", Mode: "local", Available: true},
	}
}

func (factory *SourceFactory) Create(name string) (SourceSelection, error) {
	if name == "" {
		name = "demo"
	}
	switch name {
	case "demo":
		source, err := NewScenarioSource(ScenarioOptions{RuntimeID: "scenario-local"})
		return SourceSelection{Source: source, RuntimeID: "scenario-local", Mode: "demo", Demo: true}, err
	case "local":
		return factory.createLocal()
	case "remote":
		return SourceSelection{}, fmt.Errorf("runtime source remote is unavailable; configure a remote runtime endpoint first")
	default:
		return SourceSelection{}, fmt.Errorf("unknown runtime source %q; choose demo or local", name)
	}
}

func (factory *SourceFactory) createLocal() (SourceSelection, error) {
	root := filepath.Join(factory.options.StateDir, "tactical-runtime-events")
	store, err := runtimeapi.NewStore(root)
	if err != nil {
		return SourceSelection{}, err
	}
	digest := sha256.Sum256([]byte(filepath.Clean(factory.options.StateDir)))
	runtimeID := "runtime-" + hex.EncodeToString(digest[:8])
	service := runtimeapi.NewService(store, runtimeID, "local-user", nil)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return SourceSelection{}, fmt.Errorf("generate local runtime credential: %w", err)
	}
	bearer := hex.EncodeToString(secretBytes)
	server, err := runtimeapi.NewLocalServer(runtimeapi.LocalServerOptions{
		Service: service, Bearer: bearer, Role: "owner", Workspace: factory.options.Workspace,
		ClientID: factory.options.ClientID,
		Source: runtimeapi.SourceMetadata{
			Mode: runtimeapi.SourceModeLocal, RuntimeID: runtimeID, Principal: "local-user",
			Capabilities: []string{"events", "snapshot", "commands"},
		},
	})
	if err != nil {
		return SourceSelection{}, err
	}
	return SourceSelection{
		Source:    &localRuntimeSource{server: server, bearer: bearer},
		RuntimeID: runtimeID, Mode: "local",
	}, nil
}

type localRuntimeSource struct {
	mu       sync.Mutex
	server   *runtimeapi.LocalServer
	bearer   string
	listener func(json.RawMessage)
	cursor   int
	sequence int
	closed   bool
}

func (source *localRuntimeSource) Handshake(ctx context.Context, request runtimeapi.HandshakeRequest) (runtimeapi.HandshakeResponse, error) {
	response := source.server.Handle(ctx, runtimeapi.LocalRequest{
		ID: source.nextID(), Type: "handshake", Bearer: source.bearer, Handshake: &request,
	})
	if response.Type == "error" {
		return runtimeapi.HandshakeResponse{}, fmt.Errorf("%s", response.ErrorCode)
	}
	if response.Type != "handshake" || response.Handshake == nil {
		return runtimeapi.HandshakeResponse{}, fmt.Errorf("invalid local runtime handshake")
	}
	if _, err := runtimeapi.NegotiateHandshake(request, *response.Handshake); err != nil {
		return runtimeapi.HandshakeResponse{}, err
	}
	return *response.Handshake, nil
}

func (source *localRuntimeSource) Subscribe(ctx context.Context, after int, receive func(json.RawMessage)) (func(), error) {
	if after < 0 || receive == nil {
		return nil, fmt.Errorf("invalid local runtime subscription")
	}
	events, err := source.eventsAfter(ctx, after)
	if err != nil {
		return nil, err
	}
	source.mu.Lock()
	source.listener = receive
	source.cursor = after
	source.closed = false
	source.mu.Unlock()
	for _, event := range events {
		receive(event.raw)
		source.mu.Lock()
		if event.cursor > source.cursor {
			source.cursor = event.cursor
		}
		source.mu.Unlock()
	}
	return func() {
		source.mu.Lock()
		source.listener = nil
		source.mu.Unlock()
	}, nil
}

func (source *localRuntimeSource) Snapshot(ctx context.Context) (Snapshot, error) {
	response := source.server.Handle(ctx, runtimeapi.LocalRequest{
		ID: source.nextID(), Type: "snapshot", Bearer: source.bearer,
	})
	if response.Type == "error" {
		return Snapshot{}, fmt.Errorf("%s", response.ErrorCode)
	}
	if response.Snapshot == nil {
		return Snapshot{}, fmt.Errorf("invalid local runtime snapshot")
	}
	return Snapshot{Cursor: response.Snapshot.Cursor, State: response.Snapshot.State}, nil
}

func (source *localRuntimeSource) Send(ctx context.Context, command Command) error {
	payload, err := localCommandPayload(command)
	if err != nil {
		return err
	}
	sequence := source.nextID()
	response := source.server.Handle(ctx, runtimeapi.LocalRequest{
		ID: sequence, Type: "command", Bearer: source.bearer,
		Command: &runtimeapi.CommandEnvelope{IdempotencyKey: "tui-" + sequence, Command: payload},
	})
	if response.Type == "error" {
		return fmt.Errorf("%s", response.ErrorCode)
	}
	if response.Receipt == nil || response.Receipt.Status != "accepted" {
		code := "command_rejected"
		if response.Receipt != nil && response.Receipt.ErrorCode != "" {
			code = response.Receipt.ErrorCode
		}
		return fmt.Errorf("%s", code)
	}
	source.mu.Lock()
	after := source.cursor
	listener := source.listener
	source.mu.Unlock()
	if listener == nil {
		return nil
	}
	events, err := source.eventsAfter(ctx, after)
	if err != nil {
		return err
	}
	for _, event := range events {
		listener(event.raw)
		source.mu.Lock()
		if event.cursor > source.cursor {
			source.cursor = event.cursor
		}
		source.mu.Unlock()
	}
	return nil
}

func (source *localRuntimeSource) Close(context.Context) error {
	source.mu.Lock()
	source.closed = true
	source.listener = nil
	source.mu.Unlock()
	return nil
}

type localRuntimeEvent struct {
	cursor int
	raw    json.RawMessage
}

func (source *localRuntimeSource) eventsAfter(ctx context.Context, after int) ([]localRuntimeEvent, error) {
	response := source.server.Handle(ctx, runtimeapi.LocalRequest{
		ID: source.nextID(), Type: "events", Bearer: source.bearer, AfterCursor: after,
	})
	if response.Type == "error" {
		return nil, fmt.Errorf("%s", response.ErrorCode)
	}
	events := make([]localRuntimeEvent, 0, len(response.Events))
	for _, event := range response.Events {
		raw, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		events = append(events, localRuntimeEvent{cursor: event.Cursor, raw: raw})
	}
	return events, nil
}

func (source *localRuntimeSource) nextID() string {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.sequence++
	return fmt.Sprintf("tui-%d", source.sequence)
}

func localCommandPayload(command Command) (json.RawMessage, error) {
	var value any
	switch command.Type {
	case CommandTaskCreate:
		value = struct {
			Type, Objective, RuntimeID string
		}{string(command.Type), command.Objective, command.RuntimeID}
	case CommandScopeConfirm:
		value = struct{ Type, ScopeID string }{string(command.Type), command.ScopeID}
	case CommandTaskPause, CommandTaskResume, CommandTaskCancel:
		value = struct{ Type string }{string(command.Type)}
	case CommandApprovalRespond:
		value = struct{ Type, ChallengeID, Decision string }{string(command.Type), command.ChallengeID, command.Decision}
	case CommandControlTake:
		value = struct {
			Type             string
			ExpectedRevision int
		}{string(command.Type), command.ExpectedRevision}
	case CommandInstructionSend:
		value = struct{ Type, Content string }{string(command.Type), command.Content}
	default:
		return nil, ErrUnsupportedAction
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	keys := map[string]string{
		"Type": "type", "Objective": "objective", "RuntimeID": "runtimeId", "ScopeID": "scopeId",
		"ChallengeID": "challengeId", "Decision": "decision", "ExpectedRevision": "expectedRevision", "Content": "content",
	}
	normalized := make(map[string]json.RawMessage, len(object))
	for key, item := range object {
		normalized[keys[key]] = item
	}
	return json.Marshal(normalized)
}
