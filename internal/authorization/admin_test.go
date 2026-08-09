package authorization

import (
	"context"
	"testing"
	"time"
)

type adminObservation struct {
	requestID string
	operation string
	allowed   bool
	duration  time.Duration
}

type recordingAdminObserver struct{ observations []adminObservation }

func (observer *recordingAdminObserver) ObserveAuthorizationDecision(ctx context.Context, operation string, allowed bool, duration time.Duration) {
	requestID, _ := ctx.Value(adminRequestIDKey{}).(string)
	observer.observations = append(observer.observations, adminObservation{requestID: requestID, operation: operation, allowed: allowed, duration: duration})
}

type adminRequestIDKey struct{}

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
	members, err := admin.Members(Request{TenantID: "tenant-a", Principal: "owner", SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(members); got != 1 {
		t.Fatalf("members = %d", got)
	}
}

func TestObservedAdminServiceRecordsAuthorizationOutcomeWithContext(t *testing.T) {
	clock := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	policy := NewPolicy([]Member{{TenantID: "tenant-a", Principal: "owner", Role: RoleOwner, Active: true}})
	policy.SetClock(func() time.Time { return clock })
	if err := policy.RegisterSession(Session{ID: "session-1", TenantID: "tenant-a", Principal: "owner", ExpiresAt: clock.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := policy.ElevateSession("session-1", clock.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	observer := &recordingAdminObserver{}
	admin := NewObservedAdminService(policy, observer)
	ctx := context.WithValue(context.Background(), adminRequestIDKey{}, "request-admin-1")
	actor := Request{TenantID: "tenant-a", Principal: "owner", SessionID: "session-1"}
	if _, err := admin.MembersContext(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.MembersContext(ctx, Request{TenantID: "tenant-b", Principal: "owner", SessionID: "session-1"}); err != ErrTenantDenied {
		t.Fatalf("denied members = %v", err)
	}
	if len(observer.observations) != 2 {
		t.Fatalf("observations = %#v", observer.observations)
	}
	if got := observer.observations[0]; got.requestID != "request-admin-1" || got.operation != "members" || !got.allowed || got.duration < 0 {
		t.Fatalf("allowed observation = %#v", got)
	}
	if got := observer.observations[1]; got.operation != "members" || got.allowed {
		t.Fatalf("denied observation = %#v", got)
	}
}
