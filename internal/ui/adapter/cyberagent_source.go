package adapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cyber-code/internal/cyberagent"
	"cyber-code/internal/permissions"
	"cyber-code/internal/productstate"
	"cyber-code/internal/runtimeapi"
)

type CyberAgentSourceOptions struct {
	StateDir      string
	Workspace     string
	Location      string
	Executable    string
	Endpoint      string
	TokenProvider cyberagent.TokenProvider
	HTTPClient    *http.Client
}

type cyberAgentSource struct {
	mu         sync.Mutex
	ctx        context.Context
	options    CyberAgentSourceOptions
	client     *cyberagent.Client
	supervisor *cyberagent.Supervisor
	store      *runtimeapi.Store
	bindings   *cyberagent.BindingStore
	activePath string
	taskID     string
	sessionID  string
	runtimeID  string
	version    string
	listener   func(json.RawMessage)
	cancelRun  context.CancelFunc
	sequence   int
	closed     bool
}

func NewCyberAgentSource(options CyberAgentSourceOptions) (Source, string, error) {
	if strings.TrimSpace(options.StateDir) == "" || strings.TrimSpace(options.Workspace) == "" {
		return nil, "", fmt.Errorf("cyber-agent state directory and workspace are required")
	}
	location := strings.ToLower(strings.TrimSpace(options.Location))
	if location != "local" && location != "remote" {
		return nil, "", fmt.Errorf("cyber-agent location must be local or remote")
	}
	root := filepath.Join(options.StateDir, "cyber-agent-runtime")
	store, err := runtimeapi.NewStore(filepath.Join(root, "events"))
	if err != nil {
		return nil, "", err
	}
	bindings, err := cyberagent.NewBindingStore(filepath.Join(root, "bindings"))
	if err != nil {
		return nil, "", err
	}
	source := &cyberAgentSource{ctx: context.Background(), options: options, store: store, bindings: bindings, activePath: filepath.Join(root, "active-session.json"), runtimeID: "cyber-agent-" + location}
	if location == "local" {
		discovery, err := cyberagent.Discover(cyberagent.DiscoveryOptions{ExplicitPath: options.Executable})
		if err != nil {
			return nil, "", fmt.Errorf("discover local cyber-agent: %w", err)
		}
		supervisor, err := cyberagent.NewSupervisor(cyberagent.SupervisorOptions{Executable: discovery.Path, HTTPClient: options.HTTPClient, RequiredCapabilities: []string{"session.events.v1"}})
		if err != nil {
			return nil, "", err
		}
		if err := supervisor.Start(context.Background()); err != nil {
			return nil, "", err
		}
		client, err := supervisor.Client()
		if err != nil {
			_ = supervisor.Stop(context.Background())
			return nil, "", err
		}
		source.supervisor, source.client = supervisor, client
	} else {
		client, err := cyberagent.NewClient(cyberagent.ClientOptions{BaseURL: options.Endpoint, HTTPClient: options.HTTPClient, TokenProvider: options.TokenProvider})
		if err != nil {
			return nil, "", err
		}
		source.client = client
	}
	if err := source.loadActiveSession(); err != nil {
		_ = source.Close(context.Background())
		return nil, "", err
	}
	return source, source.runtimeID, nil
}

func (source *cyberAgentSource) Handshake(ctx context.Context, request runtimeapi.HandshakeRequest) (runtimeapi.HandshakeResponse, error) {
	capabilities, err := source.client.Capabilities(ctx)
	if err != nil {
		return runtimeapi.HandshakeResponse{}, err
	}
	if capabilities.ProtocolVersion != runtimeapi.ProtocolVersion {
		return runtimeapi.HandshakeResponse{}, runtimeapi.ErrIncompatible
	}
	source.mu.Lock()
	source.version = capabilities.RuntimeVersion
	source.mu.Unlock()
	productCapabilities := runtimeapi.SelectCapabilities(request.SupportedCapabilities, []string{"events", "snapshot", "commands"})
	return runtimeapi.HandshakeResponse{
		ProtocolVersion: capabilities.ProtocolVersion, RuntimeID: source.runtimeID, Principal: "cyber-agent",
		Role: "owner", Capabilities: productCapabilities,
		Source: runtimeapi.SourceMetadata{Mode: sourceMode(source.options.Location), RuntimeID: source.runtimeID, Principal: "cyber-agent", Capabilities: productCapabilities},
	}, nil
}

func (source *cyberAgentSource) RuntimeIdentity() RuntimeIdentity {
	source.mu.Lock()
	defer source.mu.Unlock()
	return RuntimeIdentity{Name: "cyber-agent", Version: source.version, Location: source.options.Location, SessionID: source.sessionID, Authority: "cyber-agent"}
}

func (source *cyberAgentSource) Subscribe(ctx context.Context, after int, receive func(json.RawMessage)) (func(), error) {
	if receive == nil || after < 0 {
		return nil, fmt.Errorf("invalid cyber-agent subscription")
	}
	source.mu.Lock()
	source.listener = receive
	source.sequence = after
	source.mu.Unlock()
	source.startRun(ctx)
	return func() {
		source.mu.Lock()
		source.listener = nil
		source.mu.Unlock()
	}, nil
}

func (source *cyberAgentSource) Snapshot(ctx context.Context) (Snapshot, error) {
	source.mu.Lock()
	taskID, sessionID := source.taskID, source.sessionID
	source.mu.Unlock()
	if taskID == "" || sessionID == "" {
		return Snapshot{Cursor: 0, State: productstate.Initial()}, nil
	}
	if _, err := source.remote(ctx); err != nil {
		return Snapshot{}, err
	}
	_, state, err := source.store.Load(ctx, taskID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Cursor: state.CommittedCursor, State: state}, nil
}

func (source *cyberAgentSource) Send(ctx context.Context, command Command) error {
	switch command.Type {
	case CommandTaskCreate:
		return source.createSession(ctx, command.Objective, command.InputPaths)
	case CommandScopeConfirm, CommandApprovalRespond:
		return source.respondInteraction(ctx, command)
	case CommandInstructionSend:
		remote, err := source.remote(ctx)
		if err != nil {
			return err
		}
		client, clientErr := remote.Client()
		if clientErr != nil {
			return clientErr
		}
		_, err = client.SubmitTurn(ctx, source.sessionID, command.Content, mutationKey("turn"))
		return err
	case CommandTaskResume:
		remote, err := source.remote(ctx)
		if err != nil {
			return err
		}
		client, clientErr := remote.Client()
		if clientErr != nil {
			return clientErr
		}
		_, err = client.ResumeSession(ctx, source.sessionID, mutationKey("resume"))
		return err
	case CommandTaskCancel:
		remote, err := source.remote(ctx)
		if err != nil {
			return err
		}
		client, clientErr := remote.Client()
		if clientErr != nil {
			return clientErr
		}
		_, err = client.CancelSession(ctx, source.sessionID, mutationKey("cancel"))
		return err
	default:
		return fmt.Errorf("cyber-agent does not support tactical command %s", command.Type)
	}
}

func (source *cyberAgentSource) Close(ctx context.Context) error {
	source.mu.Lock()
	if source.closed {
		source.mu.Unlock()
		return nil
	}
	source.closed = true
	if source.cancelRun != nil {
		source.cancelRun()
	}
	supervisor := source.supervisor
	source.mu.Unlock()
	if supervisor != nil {
		return supervisor.Stop(ctx)
	}
	return nil
}

func (source *cyberAgentSource) createSession(ctx context.Context, objective string, inputPaths []string) error {
	if strings.TrimSpace(objective) == "" {
		return fmt.Errorf("security objective is required")
	}
	taskID := newSecurityTaskID()
	submission := cyberagent.TaskSubmission{Kind: "natural_language", TaskID: taskID, Content: objective}
	if len(inputPaths) > 0 {
		submission.Kind = "input_manifest"
		for index, path := range inputPaths {
			filename, mediaType, content, err := source.loadInput(path)
			if err != nil {
				return fmt.Errorf("load cyber-agent input %d: %w", index+1, err)
			}
			manifest, err := source.client.UploadInput(ctx, filename, mediaType, content, "")
			if err != nil {
				return fmt.Errorf("upload cyber-agent input %d: %w", index+1, err)
			}
			submission.InputIDs = append(submission.InputIDs, manifest.UploadID)
		}
	}
	snapshot, err := source.client.CreateSession(ctx, submission, cyberagent.CreateSessionOptions{}, mutationKey("create"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(snapshot.TaskID) == "" || strings.TrimSpace(snapshot.SessionID) == "" {
		return fmt.Errorf("cyber-agent create session returned invalid identities")
	}
	source.mu.Lock()
	source.taskID, source.sessionID = snapshot.TaskID, snapshot.SessionID
	source.mu.Unlock()
	if err := source.saveActiveSession(ctx); err != nil {
		return err
	}
	remote, err := source.remote(ctx)
	if err != nil {
		return err
	}
	_, err = remote.Attach(ctx)
	if err == nil {
		source.startRun(ctx)
	}
	return err
}

func (source *cyberAgentSource) loadInput(requestedPath string) (string, string, []byte, error) {
	resolved, err := permissions.ResolvePath(source.options.Workspace, requestedPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("input is outside workspace: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", nil, fmt.Errorf("inspect input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("input is not a regular file")
	}
	if info.Size() > 64<<20 {
		return "", "", nil, fmt.Errorf("input exceeds %d bytes", 64<<20)
	}
	file, err := os.Open(filepath.Clean(resolved))
	if err != nil {
		return "", "", nil, fmt.Errorf("open input: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if err != nil {
		return "", "", nil, fmt.Errorf("read input: %w", err)
	}
	if len(content) > 64<<20 {
		return "", "", nil, fmt.Errorf("input exceeds %d bytes", 64<<20)
	}
	mediaType := inputMediaType(filepath.Ext(resolved))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return filepath.Base(resolved), strings.Split(mediaType, ";")[0], content, nil
}

func inputMediaType(extension string) string {
	extension = strings.ToLower(extension)
	switch extension {
	case ".yaml", ".yml":
		return "application/yaml"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt", ".log":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".tgz", ".gz":
		return "application/gzip"
	default:
		return mime.TypeByExtension(extension)
	}
}

type activeSecuritySession struct {
	SchemaVersion int    `json:"schema_version"`
	TaskID        string `json:"task_id"`
	SessionID     string `json:"session_id"`
}

func (source *cyberAgentSource) loadActiveSession() error {
	data, err := os.ReadFile(source.activePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read active cyber-agent session: %w", err)
	}
	var active activeSecuritySession
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&active); err != nil {
		return fmt.Errorf("decode active cyber-agent session: %w", err)
	}
	if active.SchemaVersion != 1 || strings.TrimSpace(active.TaskID) == "" || strings.TrimSpace(active.SessionID) == "" {
		return fmt.Errorf("active cyber-agent session is invalid")
	}
	source.taskID, source.sessionID = active.TaskID, active.SessionID
	return nil
}

func (source *cyberAgentSource) saveActiveSession(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source.mu.Lock()
	active := activeSecuritySession{SchemaVersion: 1, TaskID: source.taskID, SessionID: source.sessionID}
	source.mu.Unlock()
	encoded, err := json.Marshal(active)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(source.activePath), ".active-session-*")
	if err != nil {
		return fmt.Errorf("create active cyber-agent session: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err == nil {
		_, err = file.Write(encoded)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write active cyber-agent session: %w", err)
	}
	if err := os.Rename(temporary, source.activePath); err != nil {
		return fmt.Errorf("replace active cyber-agent session: %w", err)
	}
	return nil
}

func (source *cyberAgentSource) respondInteraction(ctx context.Context, command Command) error {
	remote, err := source.remote(ctx)
	if err != nil {
		return err
	}
	client, clientErr := remote.Client()
	if clientErr != nil {
		return clientErr
	}
	snapshot, err := client.Snapshot(ctx, source.sessionID)
	if err != nil || snapshot.PendingInteraction == nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("cyber-agent has no pending interaction")
	}
	approved := command.Decision == "allow_once" || command.Type == CommandScopeConfirm
	_, err = client.RespondInteraction(ctx, source.sessionID, cyberagent.InteractionResponse{InteractionID: snapshot.PendingInteraction.InteractionID, SessionID: source.sessionID, Approved: &approved}, mutationKey("interaction"))
	return err
}

func (source *cyberAgentSource) remote(ctx context.Context) (*cyberagent.RemoteSource, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.taskID == "" || source.sessionID == "" {
		return nil, fmt.Errorf("cyber-agent security session is not active")
	}
	remote, err := cyberagent.NewRemoteSource(cyberagent.RemoteSourceOptions{Client: source.client, Store: source.store, Bindings: source.bindings, TaskID: source.taskID, SessionID: source.sessionID, RuntimeID: source.runtimeID})
	return remote, err
}

func (source *cyberAgentSource) startRun(ctx context.Context) {
	source.mu.Lock()
	if source.cancelRun != nil || source.sessionID == "" || source.listener == nil {
		source.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	source.cancelRun = cancel
	source.mu.Unlock()
	go func() {
		remote, err := source.remote(context.Background())
		if err == nil {
			err = remote.Run(runCtx, func(state productstate.State) { source.publishState(state) })
		}
		if err != nil && runCtx.Err() == nil {
			source.publishError(err)
		}
		source.mu.Lock()
		source.cancelRun = nil
		source.mu.Unlock()
	}()
}

func (source *cyberAgentSource) publishState(state productstate.State) {
	if state.Task == nil {
		return
	}
	events, _ := source.store.EventsAfter(context.Background(), state.Task.ID, source.sequence)
	source.mu.Lock()
	listener := source.listener
	source.mu.Unlock()
	if listener == nil {
		return
	}
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err == nil {
			listener(raw)
		}
		source.mu.Lock()
		if event.Cursor > source.sequence {
			source.sequence = event.Cursor
		}
		source.mu.Unlock()
	}
}

func (source *cyberAgentSource) publishError(err error) {
	_ = err
}

func sourceMode(location string) runtimeapi.SourceMode {
	if strings.EqualFold(location, "remote") {
		return runtimeapi.SourceModeRemote
	}
	return runtimeapi.SourceModeLocal
}

func mutationKey(prefix string) string { return prefix + "-" + newSecurityTaskID() }

func newSecurityTaskID() string {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "security-task-fallback"
	}
	return "security-task-" + hex.EncodeToString(buffer)
}
