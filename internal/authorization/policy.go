// Package authorization contains server-side organization and membership policy.
package authorization

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/security"
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
	CapabilityTaskCreate      Capability = "task.create"
	CapabilityScopeConfirm    Capability = "scope.confirm"
	CapabilityApproval        Capability = "approval.respond"
	CapabilityControlTake     Capability = "control.take"
	CapabilityEvidenceRead    Capability = "evidence.read"
	CapabilityReportExport    Capability = "report.export"
	CapabilityAdministration  Capability = "organization.admin"
	CapabilityEmergencyAccess Capability = "emergency.access.grant"
)

var (
	ErrTenantDenied                = errors.New("tenant_access_denied")
	ErrMembershipRevoked           = errors.New("membership_revoked")
	ErrCapabilityDenied            = errors.New("capability_denied")
	ErrInvitationMissing           = errors.New("invitation_missing")
	ErrInvitationExpired           = errors.New("invitation_expired")
	ErrInvitationUsed              = errors.New("invitation_used")
	ErrInvitationPrincipalMismatch = errors.New("invitation_principal_mismatch")
	ErrSessionMissing              = errors.New("session_missing")
	ErrSessionRevoked              = errors.New("session_revoked")
	ErrSessionExpired              = errors.New("session_expired")
	ErrStepUpRequired              = errors.New("step_up_required")
	ErrInvalidRole                 = errors.New("invalid_role")
	ErrAuditTampered               = errors.New("audit_tampered")
	ErrEmergencyAccessInvalid      = errors.New("emergency_access_invalid")
	ErrRevisionConflict            = errors.New("revision_conflict")
)

type Member struct {
	TenantID, Principal string
	ExternalID          string
	DisplayName         string
	Role                Role
	Active              bool
	ProvisionedAt       time.Time
	UpdatedAt           time.Time
	Revision            uint64
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
	EmergencyUntil          time.Time
	EmergencyReason         string
	Revoked                 bool
}
type Decision struct {
	At                  time.Time
	TenantID, Principal string
	Capability          Capability
	Allowed             bool
	Reason              string
	PreviousHash        string
	Hash                string
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
	if !validRole(invitation.Role) {
		return ErrInvalidRole
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
	return p.acceptInvitationLocked(id, "", "")
}

func (p *Policy) AcceptInvitationFor(actor Request, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if actor.TenantID == "" || actor.Principal == "" {
		return ErrTenantDenied
	}
	return p.acceptInvitationLocked(id, actor.TenantID, actor.Principal)
}

func (p *Policy) acceptInvitationLocked(id, tenantID, principal string) error {
	invitation, ok := p.invitations[id]
	if !ok {
		return ErrInvitationMissing
	}
	if tenantID != "" && invitation.TenantID != tenantID {
		return ErrTenantDenied
	}
	if principal != "" && invitation.Principal != principal {
		return ErrInvitationPrincipalMismatch
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

func (p *Policy) GrantEmergencyAccess(actor Request, sessionID, reason string, until time.Time) error {
	actor.Capability = CapabilityEmergencyAccess
	if err := p.Authorize(actor); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	reason = security.NewRedactor().Text(reason)
	now := p.currentTime()
	if len(reason) < 8 || len(reason) > 256 || strings.ContainsAny(reason, "\x00\r\n") || !until.After(now) || until.After(now.Add(30*time.Minute)) {
		return ErrEmergencyAccessInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[sessionID]
	if !ok {
		return ErrSessionMissing
	}
	if session.TenantID != actor.TenantID {
		return ErrTenantDenied
	}
	if session.Revoked {
		return ErrSessionRevoked
	}
	if !session.ExpiresAt.After(until) {
		return ErrSessionExpired
	}
	session.EmergencyUntil = until.UTC()
	session.EmergencyReason = reason
	p.sessions[sessionID] = session
	p.appendDecisionLocked(Decision{At: now, TenantID: session.TenantID, Principal: session.Principal, Capability: CapabilityEmergencyAccess, Allowed: true, Reason: "emergency access granted: " + reason})
	return nil
}

func (p *Policy) RevokeEmergencyAccess(actor Request, sessionID string) error {
	actor.Capability = CapabilityEmergencyAccess
	if err := p.Authorize(actor); err != nil {
		return err
	}
	now := p.currentTime()
	p.mu.Lock()
	defer p.mu.Unlock()
	session, ok := p.sessions[sessionID]
	if !ok {
		return ErrSessionMissing
	}
	if session.TenantID != actor.TenantID {
		return ErrTenantDenied
	}
	session.EmergencyUntil = time.Time{}
	session.EmergencyReason = ""
	p.sessions[sessionID] = session
	p.appendDecisionLocked(Decision{At: now, TenantID: session.TenantID, Principal: session.Principal, Capability: CapabilityEmergencyAccess, Allowed: false, Reason: "emergency access revoked"})
	return nil
}

func (p *Policy) currentTime() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clock().UTC()
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

// ProvisionMember applies an identity-provider mutation without allowing the
// caller to cross tenant boundaries. Every mutation is appended to the audit
// hash chain through the existing decision log.
func (p *Policy) ProvisionMember(tenantID, actor, principal, externalID, displayName string, role Role, active bool) (Member, error) {
	return p.provisionMember(tenantID, actor, principal, externalID, displayName, role, active, nil)
}

func (p *Policy) ProvisionMemberAtRevision(tenantID, actor, principal, externalID, displayName string, role Role, active bool, expected uint64) (Member, error) {
	return p.provisionMember(tenantID, actor, principal, externalID, displayName, role, active, &expected)
}

func (p *Policy) provisionMember(tenantID, actor, principal, externalID, displayName string, role Role, active bool, expected *uint64) (Member, error) {
	if !validProvisionText(tenantID) || !validProvisionText(actor) || !validProvisionText(principal) || !validRole(role) || strings.ContainsAny(externalID+displayName, "\x00\r\n") || len(externalID) > 256 || len(displayName) > 256 {
		return Member{}, ErrTenantDenied
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := tenantID + "\x00" + principal
	now := p.clock().UTC()
	previous, exists := p.members[key]
	if expected != nil && (!exists || previous.Revision != *expected) {
		return Member{}, ErrRevisionConflict
	}
	created := previous.ProvisionedAt
	if created.IsZero() {
		created = now
	}
	revision := previous.Revision + 1
	member := Member{TenantID: tenantID, Principal: principal, ExternalID: externalID, DisplayName: displayName, Role: role, Active: active, ProvisionedAt: created, UpdatedAt: now, Revision: revision}
	p.members[key] = member
	p.appendDecisionLocked(Decision{At: now, TenantID: tenantID, Principal: actor, Capability: CapabilityAdministration, Allowed: active, Reason: "SCIM membership synchronized"})
	return member, nil
}

func validProvisionText(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n")
}

func (p *Policy) UpdateMemberRole(tenantID, principal string, role Role) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !validRole(role) {
		return ErrInvalidRole
	}
	key := tenantID + "\x00" + principal
	member, ok := p.members[key]
	if !ok {
		return ErrTenantDenied
	}
	member.Role = role
	p.members[key] = member
	return nil
}

func (p *Policy) Members(tenantID string) []Member {
	p.mu.Lock()
	defer p.mu.Unlock()
	members := make([]Member, 0)
	for _, member := range p.members {
		if member.TenantID == tenantID {
			members = append(members, member)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Principal < members[j].Principal })
	return members
}

func (p *Policy) DecisionsForTenant(tenantID string) []Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	decisions := make([]Decision, 0)
	for _, decision := range p.decisions {
		if decision.TenantID == tenantID {
			decisions = append(decisions, decision)
		}
	}
	return decisions
}

func (p *Policy) Invitations(tenantID string) []Invitation {
	p.mu.Lock()
	defer p.mu.Unlock()
	invitations := make([]Invitation, 0)
	for _, invitation := range p.invitations {
		if invitation.TenantID == tenantID {
			invitations = append(invitations, invitation)
		}
	}
	sort.Slice(invitations, func(i, j int) bool { return invitations[i].ID < invitations[j].ID })
	return invitations
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
	} else if isHighRisk(request.Capability) && (request.SessionID == "" ||
		(!p.sessions[request.SessionID].StepUpUntil.After(p.clock().UTC()) && !p.sessions[request.SessionID].EmergencyUntil.After(p.clock().UTC()))) {
		err = ErrStepUpRequired
	}
	decision := Decision{At: p.clock().UTC(), TenantID: request.TenantID, Principal: request.Principal, Capability: request.Capability, Allowed: err == nil}
	if err != nil {
		decision.Reason = err.Error()
	}
	p.appendDecisionLocked(decision)
	return err
}

func (p *Policy) appendDecisionLocked(decision Decision) {
	if len(p.decisions) > 0 {
		decision.PreviousHash = p.decisions[len(p.decisions)-1].Hash
	}
	decision.Hash = decisionHash(decision)
	p.decisions = append(p.decisions, decision)
}

func VerifyDecisions(decisions []Decision) error {
	previous := ""
	for _, decision := range decisions {
		if decision.PreviousHash != previous || decision.Hash != decisionHash(decision) {
			return ErrAuditTampered
		}
		previous = decision.Hash
	}
	return nil
}

func decisionHash(decision Decision) string {
	material := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%t\x00%s\x00%s", decision.At.UTC().Format(time.RFC3339Nano), decision.TenantID, decision.Principal, decision.Capability, decision.Allowed, decision.Reason, decision.PreviousHash)
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:])
}

func isHighRisk(capability Capability) bool {
	return capability == CapabilityApproval || capability == CapabilityReportExport || capability == CapabilityAdministration || capability == CapabilityEmergencyAccess
}

func validRole(role Role) bool {
	switch role {
	case RoleOwner, RoleAdmin, RoleOperator, RoleApprover, RoleAuditor, RoleViewer:
		return true
	default:
		return false
	}
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
