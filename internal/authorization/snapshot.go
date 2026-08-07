package authorization

import "sort"

type Snapshot struct {
	Members     []Member     `json:"members"`
	Invitations []Invitation `json:"invitations"`
	Sessions    []Session    `json:"sessions"`
	Decisions   []Decision   `json:"decisions"`
}

func (p *Policy) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot := Snapshot{Members: make([]Member, 0, len(p.members)), Invitations: make([]Invitation, 0, len(p.invitations)), Sessions: make([]Session, 0, len(p.sessions)), Decisions: append([]Decision(nil), p.decisions...)}
	for _, member := range p.members {
		snapshot.Members = append(snapshot.Members, member)
	}
	for _, invitation := range p.invitations {
		snapshot.Invitations = append(snapshot.Invitations, invitation)
	}
	for _, session := range p.sessions {
		snapshot.Sessions = append(snapshot.Sessions, session)
	}
	sort.Slice(snapshot.Members, func(i, j int) bool {
		return snapshot.Members[i].TenantID < snapshot.Members[j].TenantID || snapshot.Members[i].TenantID == snapshot.Members[j].TenantID && snapshot.Members[i].Principal < snapshot.Members[j].Principal
	})
	sort.Slice(snapshot.Invitations, func(i, j int) bool { return snapshot.Invitations[i].ID < snapshot.Invitations[j].ID })
	sort.Slice(snapshot.Sessions, func(i, j int) bool { return snapshot.Sessions[i].ID < snapshot.Sessions[j].ID })
	return snapshot
}

func NewPolicyFromSnapshot(snapshot Snapshot) (*Policy, error) {
	if err := VerifyDecisions(snapshot.Decisions); err != nil {
		return nil, err
	}
	policy := NewPolicy(nil)
	for _, member := range snapshot.Members {
		if member.TenantID == "" || member.Principal == "" || !validRole(member.Role) {
			return nil, ErrTenantDenied
		}
		key := member.TenantID + "\x00" + member.Principal
		if _, exists := policy.members[key]; exists {
			return nil, ErrTenantDenied
		}
		policy.members[key] = member
	}
	for _, invitation := range snapshot.Invitations {
		if invitation.ID == "" || invitation.TenantID == "" || invitation.Principal == "" || !validRole(invitation.Role) {
			return nil, ErrInvitationMissing
		}
		if _, exists := policy.invitations[invitation.ID]; exists {
			return nil, ErrInvitationUsed
		}
		policy.invitations[invitation.ID] = invitation
	}
	for _, session := range snapshot.Sessions {
		if session.ID == "" || session.TenantID == "" || session.Principal == "" {
			return nil, ErrSessionMissing
		}
		if _, exists := policy.sessions[session.ID]; exists {
			return nil, ErrSessionMissing
		}
		policy.sessions[session.ID] = session
	}
	policy.decisions = append([]Decision(nil), snapshot.Decisions...)
	return policy, nil
}
