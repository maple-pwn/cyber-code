package authorization

import (
	"testing"
	"time"
)

func TestAdminServiceRequiresStepUpAndScopesTenant(t *testing.T) {
	clock := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "owner", Role: RoleOwner, Active: true}})
	policy.SetClock(func() time.Time { return clock })
	if err := policy.RegisterSession(Session{ID: "session-1", TenantID: "tenant-a", Principal: "owner", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	admin := NewAdminService(policy)
	invite := Invitation{ID: "invite-1", TenantID: "tenant-a", Principal: "new-user", Role: RoleViewer, ExpiresAt: clock.Add(time.Hour)}
	if err := admin.Invite(Request{TenantID: "tenant-a", Principal: "owner", SessionID: "session-1"}, invite); err != ErrStepUpRequired {
		t.Fatalf("without step-up = %v", err)
	}
	if err := policy.ElevateSession("session-1", clock.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := admin.Invite(Request{TenantID: "tenant-a", Principal: "owner", SessionID: "session-1"}, invite); err != nil {
		t.Fatal(err)
	}
	if err := admin.AcceptInvitation(Request{TenantID: "tenant-a", Principal: "mallory"}, invite.ID); err != ErrInvitationPrincipalMismatch {
		t.Fatalf("wrong invite principal = %v", err)
	}
	if err := admin.UpdateRole(Request{TenantID: "tenant-b", Principal: "owner", SessionID: "session-1"}, "new-user", RoleAdmin); err != ErrTenantDenied {
		t.Fatalf("cross tenant = %v", err)
	}
	if got := len(admin.Members(Request{TenantID: "tenant-a", Principal: "owner", SessionID: "session-1"})); got != 1 {
		t.Fatalf("members = %d", got)
	}
}
