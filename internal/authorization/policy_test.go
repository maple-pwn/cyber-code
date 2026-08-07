package authorization

import (
	"testing"
	"time"
)

func TestPolicyEnforcesTenantMembershipAndLeastPrivilege(t *testing.T) {

	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "alice", Role: RoleOperator, Active: true}})
	if err := policy.Authorize(Request{TenantID: "tenant-a", Principal: "alice", Capability: CapabilityTaskCreate}); err != nil {
		t.Fatalf("operator task create: %v", err)
	}
	if err := policy.Authorize(Request{TenantID: "tenant-a", Principal: "alice", Capability: CapabilityAdministration}); err != ErrCapabilityDenied {
		t.Fatalf("admin capability error = %v, want %v", err, ErrCapabilityDenied)
	}
	if err := policy.Authorize(Request{TenantID: "tenant-b", Principal: "alice", Capability: CapabilityTaskCreate}); err != ErrTenantDenied {
		t.Fatalf("cross tenant error = %v, want %v", err, ErrTenantDenied)
	}
}

func TestPolicyRevocationAndAuditAreImmutable(t *testing.T) {
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "auditor", Role: RoleAuditor, Active: false}})
	if err := policy.Authorize(Request{TenantID: "tenant-a", Principal: "auditor", Capability: CapabilityEvidenceRead}); err != ErrMembershipRevoked {
		t.Fatalf("revoked error = %v, want %v", err, ErrMembershipRevoked)
	}
	audit := policy.Decisions()
	if len(audit) != 1 || audit[0].Allowed || audit[0].Reason != ErrMembershipRevoked.Error() {
		t.Fatalf("audit = %+v", audit)
	}
	audit[0].Reason = "tampered"
	if policy.Decisions()[0].Reason == "tampered" {
		t.Fatal("audit decision was mutable")
	}
}

func TestInvitationMembershipAndSessionRevocation(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy(nil)
	policy.SetClock(func() time.Time { return clock })
	if err := policy.Invite(Invitation{ID: "invite-1", TenantID: "tenant-a", Principal: "bob", Role: RoleViewer, ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := policy.AcceptInvitation("invite-1"); err != nil {
		t.Fatal(err)
	}
	if err := policy.RegisterSession(Session{ID: "session-1", TenantID: "tenant-a", Principal: "bob", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	req := Request{TenantID: "tenant-a", Principal: "bob", SessionID: "session-1", Capability: CapabilityEvidenceRead}
	if err := policy.Authorize(req); err != nil {
		t.Fatalf("active session: %v", err)
	}
	if err := policy.RevokeSession("session-1"); err != nil {
		t.Fatal(err)
	}
	if err := policy.Authorize(req); err != ErrSessionRevoked {
		t.Fatalf("revoked session = %v, want %v", err, ErrSessionRevoked)
	}
	if err := policy.RevokeMember("tenant-a", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := policy.Authorize(Request{TenantID: "tenant-a", Principal: "bob", Capability: CapabilityEvidenceRead}); err != ErrMembershipRevoked {
		t.Fatalf("revoked member = %v, want %v", err, ErrMembershipRevoked)
	}
}
