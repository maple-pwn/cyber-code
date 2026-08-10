package runtimeapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyber-code/internal/productprotocol"
)

func TestAuthorizeTerminalOpenBindsScopeLeaseCapabilityAndProfile(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "snapshot", "commands", "terminal.input"})
	workingDirectory := filepath.Join(workspace, "project")
	if err := os.Mkdir(workingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	command := terminalOpenCommand{
		SessionID: "terminal-1", ProfileID: "default-shell", WorkingDirectory: workingDirectory,
		ScopeID: "scope-terminal", Columns: 120, Rows: 40, OutputLimitBytes: 1024, ExpectedLeaseRevision: 1,
	}
	if err := server.authorizeTerminalOpen(context.Background(), taskID, "client-1", command); err != nil {
		t.Fatalf("authorized terminal open = %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*terminalOpenCommand) string
		wantErr error
	}{
		{"wrong owner", func(_ *terminalOpenCommand) string { return "client-2" }, ErrTerminalControlRequired},
		{"stale lease", func(command *terminalOpenCommand) string {
			command.ExpectedLeaseRevision = 2
			return "client-1"
		}, ErrStaleLease},
		{"wrong scope", func(command *terminalOpenCommand) string {
			command.ScopeID = "scope-forged"
			return "client-1"
		}, ErrTerminalScopeMismatch},
		{"workspace escape", func(command *terminalOpenCommand) string {
			command.WorkingDirectory = filepath.Dir(workspace)
			return "client-1"
		}, ErrTerminalWorkspaceOutOfScope},
		{"unknown profile", func(command *terminalOpenCommand) string {
			command.ProfileID = "arbitrary-shell"
			return "client-1"
		}, ErrTerminalProfileNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyCommand := command
			clientID := test.mutate(&copyCommand)
			if err := server.authorizeTerminalOpen(context.Background(), taskID, clientID, copyCommand); !errors.Is(err, test.wantErr) {
				t.Fatalf("authorize error = %v, want %v", err, test.wantErr)
			}
		})
	}
	server.source.Capabilities = []string{"events", "commands"}
	if err := server.authorizeTerminalOpen(context.Background(), taskID, "client-1", command); !errors.Is(err, ErrTerminalCapabilityRequired) {
		t.Fatalf("capability loss error = %v", err)
	}
}

func TestAuthorizeTerminalOpenRequiresConfirmedScope(t *testing.T) {
	t.Parallel()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-1", "operator", time.Now)
	workspace := t.TempDir()
	if _, err = service.CreateTask(context.Background(), "task-unconfirmed", "Terminal task"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ProposeScope(context.Background(), "task-unconfirmed", productprotocol.ScopeSnapshot{
		ID: "scope-terminal", Principal: "operator", Workspace: workspace, Validity: "task",
		Targets: []string{workspace}, AllowedActions: []string{"terminal.open"}, DeniedActions: []string{}, RiskCeiling: "low",
	}); err != nil {
		t.Fatal(err)
	}
	server, err := NewLocalServer(LocalServerOptions{
		Service: service, Bearer: "secret", Role: "owner", Workspace: workspace, ClientID: "client-1",
		Source:           SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-1", Principal: "operator", Capabilities: []string{"terminal.input"}},
		TerminalProfiles: map[string]TerminalProfile{"default-shell": {ID: "default-shell", RequiredAction: "terminal.open", Risk: "low"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	command := terminalOpenCommand{SessionID: "terminal-1", ProfileID: "default-shell", WorkingDirectory: workspace, ScopeID: "scope-terminal", Columns: 80, Rows: 24, OutputLimitBytes: 1024, ExpectedLeaseRevision: 1}
	if err := server.authorizeTerminalOpen(context.Background(), "task-unconfirmed", "client-1", command); !errors.Is(err, ErrScopeNotConfirmed) {
		t.Fatalf("unconfirmed scope error = %v", err)
	}
}

func TestTerminalOpenDispatchEnforcesAuthorityBeforeRuntimeAvailability(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	valid := map[string]any{
		"type": "terminal.open", "sessionId": "terminal-1", "profileId": "default-shell", "workingDirectory": workspace,
		"scopeId": "scope-terminal", "columns": 80, "rows": 24, "outputLimitBytes": 1024, "expectedLeaseRevision": 1,
	}
	for name, test := range map[string]struct {
		command map[string]any
		code    string
	}{
		"authorized but unavailable": {command: valid, code: "terminal_unavailable"},
		"forged scope":               {command: func() map[string]any { copy := cloneCommand(valid); copy["scopeId"] = "scope-forged"; return copy }(), code: "terminal_scope_mismatch"},
		"unknown profile":            {command: func() map[string]any { copy := cloneCommand(valid); copy["profileId"] = "arbitrary-shell"; return copy }(), code: "terminal_profile_not_allowed"},
	} {
		t.Run(name, func(t *testing.T) {
			envelope := mustLocalEnvelope(t, "terminal-"+name, test.command)
			response := server.handleAuthorizedForClient(context.Background(), LocalRequest{ID: name, Type: "command", TaskID: taskID, Command: &envelope}, "client-1")
			if response.Receipt == nil || response.Receipt.Status != "rejected" || response.Receipt.ErrorCode != test.code {
				t.Fatalf("terminal receipt = %+v", response.Receipt)
			}
		})
	}
}

func TestAuthorizeTerminalOpenRequiresRuntimeResolvedApprovalForGatedProfile(t *testing.T) {
	t.Parallel()
	server, taskID, workspace := newTerminalAuthorityServer(t, []string{"events", "commands", "terminal.input"})
	profile := server.terminalProfiles["default-shell"]
	profile.ApprovalRequired = true
	server.terminalProfiles[profile.ID] = profile
	command := terminalOpenCommand{SessionID: "terminal-approved", ProfileID: profile.ID, WorkingDirectory: workspace, ScopeID: "scope-terminal", Columns: 80, Rows: 24, OutputLimitBytes: 1024, ExpectedLeaseRevision: 1}
	if err := server.authorizeTerminalOpen(context.Background(), taskID, "client-1", command); !errors.Is(err, ErrTerminalApprovalRequired) {
		t.Fatalf("terminal without approval error = %v", err)
	}

	expiresAt := server.service.now().Add(time.Minute)
	if _, err := server.service.RequestApproval(context.Background(), ApprovalRequest{
		TaskID: taskID, ChallengeID: "approval-terminal", AgentID: "operator-terminal",
		Action: profile.RequiredAction, Target: workspace, Parameters: terminalApprovalParameters(command), Risk: profile.Risk, ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.service.ResolveApproval(context.Background(), taskID, "approval-terminal", "allow_once"); err != nil {
		t.Fatal(err)
	}
	if err := server.authorizeTerminalOpen(context.Background(), taskID, "client-1", command); err != nil {
		t.Fatalf("terminal with runtime approval = %v", err)
	}

	changed := command
	changed.SessionID = "terminal-forged"
	if err := server.authorizeTerminalOpen(context.Background(), taskID, "client-1", changed); !errors.Is(err, ErrTerminalApprovalRequired) {
		t.Fatalf("approval replay with changed parameters error = %v", err)
	}
}

func cloneCommand(command map[string]any) map[string]any {
	cloned := make(map[string]any, len(command))
	for key, value := range command {
		cloned[key] = value
	}
	return cloned
}

func newTerminalAuthorityServer(t *testing.T, capabilities []string) (*LocalServer, string, string) {
	t.Helper()
	ctx := context.Background()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, "runtime-1", "operator", time.Now)
	workspace := t.TempDir()
	taskID := "task-terminal"
	if _, err = service.CreateTask(ctx, taskID, "Terminal task"); err != nil {
		t.Fatal(err)
	}
	scope := productprotocol.ScopeSnapshot{
		ID: "scope-terminal", Principal: "operator", Workspace: workspace, Validity: "task",
		Targets: []string{workspace}, AllowedActions: []string{"terminal.open"}, DeniedActions: []string{}, RiskCeiling: "low",
	}
	if _, err = service.ProposeScope(ctx, taskID, scope); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ConfirmScope(ctx, taskID, scope.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.TakeControl(ctx, taskID, "client-1", 0); err != nil {
		t.Fatal(err)
	}
	server, err := NewLocalServer(LocalServerOptions{
		Service: service, Bearer: "secret", Role: "owner", Workspace: workspace, ClientID: "client-1",
		Source:           SourceMetadata{Mode: SourceModeLocal, RuntimeID: "runtime-1", Principal: "operator", Capabilities: capabilities},
		TerminalProfiles: map[string]TerminalProfile{"default-shell": {ID: "default-shell", RequiredAction: "terminal.open", Risk: "low"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if server.terminalManager != nil {
			if err := server.terminalManager.Close(context.Background()); err != nil {
				t.Errorf("close terminal manager: %v", err)
			}
		}
	})
	return server, taskID, workspace
}
