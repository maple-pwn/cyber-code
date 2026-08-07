package authorization

import "testing"

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
