package runtimeapi

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var (
	ErrTerminalCapabilityRequired  = errors.New("terminal_capability_required")
	ErrTerminalProfileNotAllowed   = errors.New("terminal_profile_not_allowed")
	ErrTerminalScopeMismatch       = errors.New("terminal_scope_mismatch")
	ErrTerminalWorkspaceOutOfScope = errors.New("terminal_workspace_out_of_scope")
	ErrTerminalControlRequired     = errors.New("terminal_control_required")
	ErrTerminalUnavailable         = errors.New("terminal_unavailable")
	ErrTerminalApprovalRequired    = errors.New("terminal_approval_required")
)

type terminalOpenCommand struct {
	SessionID             string `json:"sessionId"`
	ProfileID             string `json:"profileId"`
	WorkingDirectory      string `json:"workingDirectory"`
	ScopeID               string `json:"scopeId"`
	Columns               int    `json:"columns"`
	Rows                  int    `json:"rows"`
	OutputLimitBytes      int    `json:"outputLimitBytes"`
	ExpectedLeaseRevision int    `json:"expectedLeaseRevision"`
}

func (s *LocalServer) authorizeTerminalOpen(ctx context.Context, taskID, clientID string, command terminalOpenCommand) error {
	events, state, err := s.service.Store().Load(ctx, taskID)
	if err != nil {
		return err
	}
	if state.Scope == nil || !scopeWasConfirmed(events, state.Scope.ID) {
		return ErrScopeNotConfirmed
	}
	if command.ScopeID != state.Scope.ID {
		return ErrTerminalScopeMismatch
	}
	if !slices.Contains(s.source.Capabilities, "terminal.input") {
		return ErrTerminalCapabilityRequired
	}
	profile, ok := s.terminalProfiles[command.ProfileID]
	if !ok {
		return ErrTerminalProfileNotAllowed
	}
	if !scopeAllows(*state.Scope, profile.RequiredAction, state.Scope.Workspace, profile.Risk) {
		return ErrApprovalOutOfScope
	}
	if !pathWithinWorkspace(state.Scope.Workspace, command.WorkingDirectory) {
		return ErrTerminalWorkspaceOutOfScope
	}
	if state.ControlLease == nil || state.ControlLease.ClientID != clientID {
		return ErrTerminalControlRequired
	}
	if state.ControlLease.Revision != command.ExpectedLeaseRevision {
		return ErrStaleLease
	}
	if profile.ApprovalRequired {
		digest, err := ParameterDigest(profile.RequiredAction, state.Scope.Workspace, terminalApprovalParameters(command))
		if err != nil {
			return err
		}
		approved := false
		for challengeID, approval := range state.Approvals {
			expiresAt, parseErr := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
			if parseErr == nil && !s.service.now().After(expiresAt) && approval.Decision == "allow_once" &&
				approval.Action == profile.RequiredAction && approval.Target == state.Scope.Workspace && approval.Risk == profile.Risk &&
				approval.ParameterDigest == digest && approvalScopeActive(events, challengeID) {
				approved = true
				break
			}
		}
		if !approved {
			return ErrTerminalApprovalRequired
		}
	}
	return nil
}

func terminalApprovalParameters(command terminalOpenCommand) map[string]any {
	return map[string]any{
		"sessionId": command.SessionID, "profileId": command.ProfileID, "workingDirectory": command.WorkingDirectory,
		"scopeId": command.ScopeID, "columns": command.Columns, "rows": command.Rows,
		"outputLimitBytes": command.OutputLimitBytes, "expectedLeaseRevision": command.ExpectedLeaseRevision,
	}
}

func pathWithinWorkspace(workspace, candidate string) bool {
	if !filepath.IsAbs(workspace) || !filepath.IsAbs(candidate) {
		return false
	}
	workspacePath, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return false
	}
	candidatePath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(workspacePath, candidatePath)
	return err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
