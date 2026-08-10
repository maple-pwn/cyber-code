package authorization

import (
	"context"
	"time"
)

type AdminObserver interface {
	ObserveAuthorizationDecision(context.Context, string, bool, time.Duration)
}

type AdminService struct {
	policy   *Policy
	observer AdminObserver
	clock    func() time.Time
}

func NewAdminService(policy *Policy) *AdminService { return NewObservedAdminService(policy, nil) }

func NewObservedAdminService(policy *Policy, observer AdminObserver) *AdminService {
	return &AdminService{policy: policy, observer: observer, clock: time.Now}
}

func (s *AdminService) observe(ctx context.Context, operation string, started time.Time, err error) {
	if s == nil || s.observer == nil {
		return
	}
	s.observer.ObserveAuthorizationDecision(ctx, operation, err == nil, s.clock().Sub(started))
}

func (s *AdminService) start() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func (s *AdminService) authorize(actor Request) error {
	if s == nil || s.policy == nil {
		return ErrTenantDenied
	}
	actor.Capability = CapabilityAdministration
	return s.policy.Authorize(actor)
}

func (s *AdminService) Invite(actor Request, invitation Invitation) error {
	return s.InviteContext(context.Background(), actor, invitation)
}

func (s *AdminService) InviteContext(ctx context.Context, actor Request, invitation Invitation) (err error) {
	started := s.start()
	defer func() { s.observe(ctx, "invite", started, err) }()
	if err := s.authorize(actor); err != nil {
		return err
	}
	if invitation.TenantID != actor.TenantID {
		return ErrTenantDenied
	}
	return s.policy.Invite(invitation)
}

func (s *AdminService) AcceptInvitation(actor Request, invitationID string) error {
	return s.AcceptInvitationContext(context.Background(), actor, invitationID)
}

func (s *AdminService) AcceptInvitationContext(ctx context.Context, actor Request, invitationID string) (err error) {
	started := s.start()
	defer func() { s.observe(ctx, "accept_invitation", started, err) }()
	if s == nil || s.policy == nil {
		return ErrTenantDenied
	}
	return s.policy.AcceptInvitationFor(actor, invitationID)
}

func (s *AdminService) UpdateRole(actor Request, principal string, role Role) error {
	return s.UpdateRoleContext(context.Background(), actor, principal, role)
}

func (s *AdminService) UpdateRoleContext(ctx context.Context, actor Request, principal string, role Role) (err error) {
	started := s.start()
	defer func() { s.observe(ctx, "role", started, err) }()
	if err := s.authorize(actor); err != nil {
		return err
	}
	return s.policy.UpdateMemberRole(actor.TenantID, principal, role)
}

func (s *AdminService) RevokeMember(actor Request, principal string) error {
	return s.RevokeMemberContext(context.Background(), actor, principal)
}

func (s *AdminService) RevokeMemberContext(ctx context.Context, actor Request, principal string) (err error) {
	started := s.start()
	defer func() { s.observe(ctx, "revoke", started, err) }()
	if err := s.authorize(actor); err != nil {
		return err
	}
	return s.policy.RevokeMember(actor.TenantID, principal)
}

func (s *AdminService) Members(actor Request) ([]Member, error) {
	return s.MembersContext(context.Background(), actor)
}

func (s *AdminService) MembersContext(ctx context.Context, actor Request) (members []Member, err error) {
	started := s.start()
	defer func() { s.observe(ctx, "members", started, err) }()
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	return s.policy.Members(actor.TenantID), nil
}

func (s *AdminService) Invitations(actor Request) ([]Invitation, error) {
	return s.InvitationsContext(context.Background(), actor)
}

func (s *AdminService) InvitationsContext(ctx context.Context, actor Request) (invitations []Invitation, err error) {
	started := s.start()
	defer func() { s.observe(ctx, "invitations", started, err) }()
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	return s.policy.Invitations(actor.TenantID), nil
}

func (s *AdminService) Audit(actor Request) ([]Decision, error) {
	return s.AuditContext(context.Background(), actor)
}

func (s *AdminService) AuditContext(ctx context.Context, actor Request) (decisions []Decision, err error) {
	started := s.start()
	defer func() { s.observe(ctx, "audit", started, err) }()
	if err := s.authorize(actor); err != nil {
		return nil, err
	}
	return s.policy.DecisionsForTenant(actor.TenantID), nil
}

func (s *AdminService) GrantEmergencyAccess(actor Request, sessionID, reason string, until time.Time) error {
	return s.GrantEmergencyAccessContext(context.Background(), actor, sessionID, reason, until)
}

func (s *AdminService) GrantEmergencyAccessContext(ctx context.Context, actor Request, sessionID, reason string, until time.Time) (err error) {
	started := s.start()
	defer func() { s.observe(ctx, "emergency_grant", started, err) }()
	if s == nil || s.policy == nil {
		return ErrTenantDenied
	}
	return s.policy.GrantEmergencyAccess(actor, sessionID, reason, until)
}

func (s *AdminService) RevokeEmergencyAccess(actor Request, sessionID string) error {
	return s.RevokeEmergencyAccessContext(context.Background(), actor, sessionID)
}

func (s *AdminService) RevokeEmergencyAccessContext(ctx context.Context, actor Request, sessionID string) (err error) {
	started := s.start()
	defer func() { s.observe(ctx, "emergency_revoke", started, err) }()
	if s == nil || s.policy == nil {
		return ErrTenantDenied
	}
	return s.policy.RevokeEmergencyAccess(actor, sessionID)
}
