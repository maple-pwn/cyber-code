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
)

type Member struct {
	TenantID, Principal string
	Role                Role
	Active              bool
}
type Request struct {
	TenantID, Principal string
	Capability          Capability
}
type Decision struct {
	At                  time.Time
	TenantID, Principal string
	Capability          Capability
	Allowed             bool
	Reason              string
}

type Policy struct {
	mu        sync.Mutex
	members   map[string]Member
	decisions []Decision
	clock     func() time.Time
}

func NewPolicy(members []Member) *Policy {
	index := make(map[string]Member, len(members))
	for _, member := range members {
		index[member.TenantID+"\x00"+member.Principal] = member
	}
	return &Policy{members: index, clock: time.Now}
}

func (p *Policy) Authorize(request Request) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	member, ok := p.members[request.TenantID+"\x00"+request.Principal]
	err := error(nil)
	if !ok {
		err = ErrTenantDenied
	} else if !member.Active {
		err = ErrMembershipRevoked
	} else if !roleAllows(member.Role, request.Capability) {
		err = ErrCapabilityDenied
	}
	decision := Decision{At: p.clock().UTC(), TenantID: request.TenantID, Principal: request.Principal, Capability: request.Capability, Allowed: err == nil}
	if err != nil {
		decision.Reason = err.Error()
	}
	p.decisions = append(p.decisions, decision)
	return err
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
