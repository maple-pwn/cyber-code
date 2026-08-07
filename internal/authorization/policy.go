// Package authorization contains server-side organization and membership policy.
package authorization

import (
	"errors"
	"sync"
	"time"
)

type Role string

const (
	RoleOwner    Role = "owner"
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleApprover Role = "approver"
	RoleAuditor  Role = "auditor"
	RoleViewer   Role = "viewer"
)

type Capability string

const (
	CapabilityTaskCreate     Capability = "task.create"
	CapabilityScopeConfirm   Capability = "scope.confirm"
	CapabilityApproval       Capability = "approval.respond"
	CapabilityControlTake    Capability = "control.take"
	CapabilityEvidenceRead   Capability = "evidence.read"
	CapabilityReportExport   Capability = "report.export"
	CapabilityAdministration Capability = "organization.admin"
)

var (
	ErrTenantDenied      = errors.New("tenant_access_denied")
	ErrMembershipRevoked = errors.New("membership_revoked")
	ErrCapabilityDenied  = errors.New("capability_denied")
	ErrInvitationMissing = errors.New("invitation_missing")
	ErrInvitationExpired = errors.New("invitation_expired")
	ErrInvitationUsed    = errors.New("invitation_used")
	ErrSessionMissing    = errors.New("session_missing")
	ErrSessionRevoked    = errors.New("session_revoked")
	ErrSessionExpired    = errors.New("session_expired")
	ErrStepUpRequired    = errors.New("step_up_required")
)

type Member struct {
	TenantID, Principal string
	Role                Role
	Active              bool
}
type Request struct {
	TenantID, Principal string
	SessionID           string
	Capability          Capability
}
type Invitation struct {
	ID, TenantID, Principal string
	Role                    Role
	ExpiresAt               time.Time
	Accepted                bool
}
type Session struct {
	ID, TenantID, Principal string
	ExpiresAt               time.Time
	StepUpUntil             time.Time
	Revoked                 bool
}
type Decision struct {
	At                  time.Time
	TenantID, Principal string
	Capability          Capability
	Allowed             bool
	Reason              string
}

type Policy struct {
	mu          sync.Mutex
	members     map[string]Member
	invitations map[string]Invitation
	sessions    map[string]Session
	decisions   []Decision
	clock       func() time.Time
}

func NewPolicy(members []Member) *Policy {
	index := make(map[string]Member, len(members))
	for _, member := range members {
		index[member.TenantID+"\x00"+member.Principal] = member
	}
	return &Policy{members: index, invitations: make(map[string]Invitation), sessions: make(map[string]Session), clock: time.Now}
}

func (p *Policy) SetClock(clock func() time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if clock == nil {
		clock = time.Now
	}
	p.clock = clock
}

func (p *Policy) Invite(invitation Invitation) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if invitation.ID == "" || invitation.TenantID == "" || invitation.Principal == "" || !invitation.ExpiresAt.After(p.clock().UTC()) {
		return ErrInvitationExpired
	}
	if _, exists := p.invitations[invitation.ID]; exists {
		return ErrInvitationUsed
	}
	p.invitations[invitation.ID] = invitation
	return nil
}

func (p *Policy) AcceptInvitation(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	invitation, ok := p.invitations[id]
	if !ok {
		return ErrInvitationMissing
	}
	if invitation.Accepted {
		return ErrInvitationUsed
	}
	if !invitation.ExpiresAt.After(p.clock().UTC()) {
		return ErrInvitationExpired
	}
	p.members[invitation.TenantID+"\x00"+invitation.Principal] = Member{TenantID: invitation.TenantID, Principal: invitation.Principal, Role: invitation.Role, Active: true}
	invitation.Accepted = true
	p.invitations[id] = invitation
	return nil
}

func (p *Policy) RegisterSession(session Session) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if session.ID == "" || session.TenantID == "" || session.Principal == "" {
		return ErrSessionMissing
	}
	if !session.ExpiresAt.After(p.clock().UTC()) {
		return ErrSessionExpired
	}
	session.Revoked = false
	p.sessions[session.ID] = session
	return nil
}

func (p *Policy) RevokeSession(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[id]
	if !ok {
		return ErrSessionMissing
	}
	session.Revoked = true
	p.sessions[id] = session
	return nil
}

func (p *Policy) ElevateSession(id string, until time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[id]
	if !ok {
		return ErrSessionMissing
	}
	if session.Revoked {
		return ErrSessionRevoked
	}
	if !until.After(p.clock().UTC()) || !session.ExpiresAt.After(until) {
		return ErrSessionExpired
	}
	session.StepUpUntil = until
	p.sessions[id] = session
	return nil
}

func (p *Policy) RevokeMember(tenantID, principal string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := tenantID + "\x00" + principal
	member, ok := p.members[key]
	if !ok {
		return ErrTenantDenied
	}
	member.Active = false
	p.members[key] = member
	return nil
}

func (p *Policy) Authorize(request Request) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	member, ok := p.members[request.TenantID+"\x00"+request.Principal]
	err := error(nil)
	if request.SessionID != "" {
		session, sessionOK := p.sessions[request.SessionID]
		switch {
		case !sessionOK:
			err = ErrSessionMissing
		case session.Revoked:
			err = ErrSessionRevoked
		case !session.ExpiresAt.After(p.clock().UTC()):
			err = ErrSessionExpired
		case session.TenantID != request.TenantID || session.Principal != request.Principal:
			err = ErrTenantDenied
		}
	}
	if err != nil {
		// Session failures are authoritative and must not fall through to stale membership claims.
	} else if !ok {
		err = ErrTenantDenied
	} else if !member.Active {
		err = ErrMembershipRevoked
	} else if !roleAllows(member.Role, request.Capability) {
		err = ErrCapabilityDenied
	} else if isHighRisk(request.Capability) && (request.SessionID == "" || !p.sessions[request.SessionID].StepUpUntil.After(p.clock().UTC())) {
		err = ErrStepUpRequired
	}
	decision := Decision{At: p.clock().UTC(), TenantID: request.TenantID, Principal: request.Principal, Capability: request.Capability, Allowed: err == nil}
	if err != nil {
		decision.Reason = err.Error()
	}
	p.decisions = append(p.decisions, decision)
	return err
}

func isHighRisk(capability Capability) bool {
	return capability == CapabilityApproval || capability == CapabilityReportExport || capability == CapabilityAdministration
}

func (p *Policy) Decisions() []Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Decision(nil), p.decisions...)
}

func roleAllows(role Role, capability Capability) bool {
	switch role {
	case RoleOwner, RoleAdmin:
		return true
	case RoleOperator:
		return capability == CapabilityTaskCreate || capability == CapabilityScopeConfirm || capability == CapabilityEvidenceRead
	case RoleApprover:
		return capability == CapabilityApproval || capability == CapabilityEvidenceRead
	case RoleAuditor:
		return capability == CapabilityEvidenceRead || capability == CapabilityReportExport
	case RoleViewer:
		return capability == CapabilityEvidenceRead
	default:
		return false
	}
}
