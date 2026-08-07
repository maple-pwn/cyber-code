package authorization

type AdminService struct{ policy *Policy }

func NewAdminService(policy *Policy) *AdminService { return &AdminService{policy: policy} }

func (s *AdminService) authorize(actor Request) error {
	if s == nil || s.policy == nil {
		return ErrTenantDenied
	}
	actor.Capability = CapabilityAdministration
	return s.policy.Authorize(actor)
}

func (s *AdminService) Invite(actor Request, invitation Invitation) error {
	if err := s.authorize(actor); err != nil {
		return err
	}
	if invitation.TenantID != actor.TenantID {
		return ErrTenantDenied
	}
	return s.policy.Invite(invitation)
}

func (s *AdminService) AcceptInvitation(actor Request, invitationID string) error {
	if s == nil || s.policy == nil {
		return ErrTenantDenied
	}
	return s.policy.AcceptInvitationFor(actor, invitationID)
}

func (s *AdminService) UpdateRole(actor Request, principal string, role Role) error {
	if err := s.authorize(actor); err != nil {
		return err
	}
	return s.policy.UpdateMemberRole(actor.TenantID, principal, role)
}

func (s *AdminService) RevokeMember(actor Request, principal string) error {
	if err := s.authorize(actor); err != nil {
		return err
	}
	return s.policy.RevokeMember(actor.TenantID, principal)
}

func (s *AdminService) Members(actor Request) []Member {
	if err := s.authorize(actor); err != nil {
		return nil
	}
	return s.policy.Members(actor.TenantID)
}

func (s *AdminService) Invitations(actor Request) []Invitation {
	if err := s.authorize(actor); err != nil {
		return nil
	}
	return s.policy.Invitations(actor.TenantID)
}

func (s *AdminService) Audit(actor Request) []Decision {
	if err := s.authorize(actor); err != nil {
		return nil
	}
	return s.policy.DecisionsForTenant(actor.TenantID)
}
