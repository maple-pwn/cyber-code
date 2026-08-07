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

func TestInvitationAcceptanceIsBoundToPrincipalAndTenant(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy(nil)
	policy.SetClock(func() time.Time { return clock })
	if err := policy.Invite(Invitation{ID: "invite-1", TenantID: "tenant-a", Principal: "bob", Role: RoleViewer, ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := policy.AcceptInvitationFor(Request{TenantID: "tenant-a", Principal: "mallory"}, "invite-1"); err != ErrInvitationPrincipalMismatch {
		t.Fatalf("wrong principal = %v", err)
	}
	if err := policy.AcceptInvitationFor(Request{TenantID: "tenant-b", Principal: "bob"}, "invite-1"); err != ErrTenantDenied {
		t.Fatalf("wrong tenant = %v", err)
	}
	if err := policy.AcceptInvitationFor(Request{TenantID: "tenant-a", Principal: "bob"}, "invite-1"); err != nil {
		t.Fatal(err)
	}
}

func TestHighRiskCapabilityRequiresStepUpSession(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "alice", Role: RoleApprover, Active: true}})
	policy.SetClock(func() time.Time { return clock })
	if err := policy.RegisterSession(Session{ID: "session-1", TenantID: "tenant-a", Principal: "alice", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	req := Request{TenantID: "tenant-a", Principal: "alice", SessionID: "session-1", Capability: CapabilityApproval}
	if err := policy.Authorize(req); err != ErrStepUpRequired {
		t.Fatalf("without step-up = %v, want %v", err, ErrStepUpRequired)
	}
	if err := policy.ElevateSession("session-1", clock.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := policy.Authorize(req); err != nil {
		t.Fatalf("with step-up: %v", err)
	}
	clock = clock.Add(11 * time.Minute)
	if err := policy.Authorize(req); err != ErrStepUpRequired {
		t.Fatalf("expired step-up = %v, want %v", err, ErrStepUpRequired)
	}
}

func TestMemberAdministrationAndTenantScopedAudit(t *testing.T) {
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "alice", Role: RoleOperator, Active: true}, {TenantID: "tenant-b", Principal: "bob", Role: RoleViewer, Active: true}})
	if err := policy.UpdateMemberRole("tenant-a", "alice", RoleAuditor); err != nil {
		t.Fatal(err)
	}
	members := policy.Members("tenant-a")
	if len(members) != 1 || members[0].Role != RoleAuditor {
		t.Fatalf("members = %+v", members)
	}
	members[0].Role = RoleOwner
	if policy.Members("tenant-a")[0].Role != RoleAuditor {
		t.Fatal("member result was mutable")
	}
	if err := policy.Authorize(Request{TenantID: "tenant-a", Principal: "alice", Capability: CapabilityEvidenceRead}); err != nil {
		t.Fatal(err)
	}
	if len(policy.DecisionsForTenant("tenant-b")) != 0 {
		t.Fatal("cross-tenant audit leakage")
	}
	if err := policy.UpdateMemberRole("tenant-a", "missing", RoleViewer); err != ErrTenantDenied {
		t.Fatalf("missing member = %v", err)
	}
}
